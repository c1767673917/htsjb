package invoiceuploads

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/db"
	"product-collection-form/backend/internal/httpapi/limits"
	"product-collection-form/backend/internal/storage"
)

func TestInvoiceSubmitCap(t *testing.T) {
	t.Parallel()

	env := newInvoiceUploadTestEnv(t)

	t.Run("allows first upload", func(t *testing.T) {
		req := newInvoiceMultipartRequest(t, "/api/invoices/INV-001/uploads", map[string][][]byte{
			"invoice_photo[]": {invoiceJPEGBytes(t, color.RGBA{R: 1, A: 255})},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertInvoiceStatusCode(t, rec.Code, http.StatusOK)
	})

	t.Run("rejects second upload after one already exists", func(t *testing.T) {
		req := newInvoiceMultipartRequest(t, "/api/invoices/INV-001/uploads", map[string][][]byte{
			"invoice_photo[]": {invoiceJPEGBytes(t, color.RGBA{G: 1, A: 255})},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertInvoiceStatusCode(t, rec.Code, http.StatusConflict)
		assertInvoiceErrorCode(t, rec.Body.Bytes(), "INVOICE_UPLOAD_CAP_EXCEEDED")
	})

	t.Run("rejects multi-file request", func(t *testing.T) {
		otherEnv := newInvoiceUploadTestEnv(t)
		req := newInvoiceMultipartRequest(t, "/api/invoices/INV-001/uploads", map[string][][]byte{
			"invoice_photo[]": {
				invoiceJPEGBytes(t, color.RGBA{B: 1, A: 255}),
				invoiceJPEGBytes(t, color.RGBA{R: 2, A: 255}),
			},
		})
		rec := httptest.NewRecorder()
		otherEnv.router.ServeHTTP(rec, req)
		assertInvoiceStatusCode(t, rec.Code, http.StatusConflict)
		assertInvoiceErrorCode(t, rec.Body.Bytes(), "INVOICE_UPLOAD_CAP_EXCEEDED")
	})
}

func TestInvoiceSubmitRejectsInactiveInvoice(t *testing.T) {
	t.Parallel()

	env := newInvoiceUploadTestEnv(t)
	_, err := env.db.Exec(`
INSERT INTO invoices (invoice_no, customer, customer_clean, seller, invoice_date, csv_present)
VALUES ('INV-OLD', '测试客户', '测试客户', '测试销方', '2026-04-23', 0)`)
	if err != nil {
		t.Fatalf("seed inactive invoice: %v", err)
	}

	req := newInvoiceMultipartRequest(t, "/api/invoices/INV-OLD/uploads", map[string][][]byte{
		"invoice_photo[]": {invoiceJPEGBytes(t, color.RGBA{R: 3, A: 255})},
	})
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	assertInvoiceStatusCode(t, rec.Code, http.StatusNotFound)
	assertInvoiceErrorCode(t, rec.Body.Bytes(), "INVOICE_NOT_FOUND")
}

type invoiceUploadTestEnv struct {
	router  *gin.Engine
	db      *sqlx.DB
	storage *storage.Service
	uploads *Service
}

func newInvoiceUploadTestEnv(t *testing.T) invoiceUploadTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = tempDir

	storageSvc := storage.New(cfg.DataDir)
	if err := storageSvc.EnsureLayout(); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	conn, err := db.Open(context.Background(), db.Options{
		Path: tempDir + "/app.db",
		Pool: cfg.DB,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	seedInvoice(t, conn, "INV-001")

	uploadSvc := NewService(conn, cfg, storageSvc, limits.New(cfg.Concurrency), nil)
	router := gin.New()
	router.POST("/api/invoices/:invoiceNo/uploads", uploadSvc.HandleSubmit)
	return invoiceUploadTestEnv{router: router, db: conn, storage: storageSvc, uploads: uploadSvc}
}

func seedInvoice(t *testing.T, conn *sqlx.DB, invoiceNo string) {
	t.Helper()
	_, err := conn.Exec(`
INSERT INTO invoices (invoice_no, customer, customer_clean, seller, invoice_date, csv_present)
VALUES (?, '测试客户', '测试客户', '测试销方', '2026-04-23', 1)`,
		invoiceNo,
	)
	if err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
}

func newInvoiceMultipartRequest(t *testing.T, target string, files map[string][][]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for field, blobs := range files {
		for i, blob := range blobs {
			part, err := writer.CreateFormFile(field, fmt.Sprintf("%s-%d.jpg", field, i))
			if err != nil {
				t.Fatalf("create form file: %v", err)
			}
			if _, err := part.Write(blob); err != nil {
				t.Fatalf("write part: %v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func invoiceJPEGBytes(t *testing.T, fill color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func assertInvoiceStatusCode(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("expected status %d, got %d", want, got)
	}
}

func assertInvoiceErrorCode(t *testing.T, body []byte, code string) {
	t.Helper()
	if !strings.Contains(string(body), code) {
		t.Fatalf("expected error code %q in body %s", code, string(body))
	}
}
