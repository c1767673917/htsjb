package uploads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/db"
	"product-collection-form/backend/internal/httpapi/limits"
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/storage"
)

type stubPDFBuilder struct {
	err error
}

func (s stubPDFBuilder) Build(_ context.Context, _ []string, w io.Writer) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if _, err := w.Write([]byte("%PDF-1.4\n")); err != nil {
		return 0, err
	}
	return 1, nil
}

func TestSubmitErrorCodesAndAtomicPaths(t *testing.T) {
	t.Parallel()

	env := newUploadTestEnv(t, stubPDFBuilder{})

	t.Run("400 when no staged files", func(t *testing.T) {
		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", nil)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusBadRequest)
		assertErrorCode(t, rec.Body.Bytes(), "ORDER_HAS_NO_STAGED_FILES")
	})

	t.Run("409 when cap exceeded", func(t *testing.T) {
		files := map[string][][]byte{"contract[]": {}}
		for i := 0; i < 21; i++ {
			files["contract[]"] = append(files["contract[]"], jpegBytes(t, color.RGBA{R: uint8(i + 1), A: 255}))
		}
		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", files)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusConflict)
		assertErrorCode(t, rec.Body.Bytes(), "UPLOAD_CAP_EXCEEDED")
	})

	t.Run("415 when mime unsupported", func(t *testing.T) {
		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
			"contract[]": {[]byte("not-an-image")},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusUnsupportedMediaType)
		assertErrorCode(t, rec.Body.Bytes(), "UNSUPPORTED_MEDIA_TYPE")
	})

	t.Run("413 when request too large", func(t *testing.T) {
		body, contentType := streamingMultipartRequest(t, map[string]io.Reader{
			"contract[]": io.MultiReader(bytes.NewReader(jpegBytes(t, color.RGBA{R: 1, A: 255})), io.LimitReader(zeroReader{}, 61*1024*1024)),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/y/2021/orders/RX2101-22926/uploads", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusRequestEntityTooLarge)
		assertErrorCode(t, rec.Body.Bytes(), "REQUEST_TOO_LARGE")
	})

	t.Run("413 when single file exceeds cap", func(t *testing.T) {
		env.uploads.cfg.Limits.SingleFileMaxMB = 1
		defer func() { env.uploads.cfg.Limits.SingleFileMaxMB = 10 }()

		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
			"contract[]": {append(jpegBytes(t, color.RGBA{R: 9, A: 255}), bytes.Repeat([]byte{0}, 2*1024*1024)...)},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusRequestEntityTooLarge)
		assertErrorCode(t, rec.Body.Bytes(), "REQUEST_TOO_LARGE")
	})

	t.Run("415 when decode cap exceeded", func(t *testing.T) {
		env.uploads.cfg.Limits.SingleFileDecodeCapMB = 1
		defer func() { env.uploads.cfg.Limits.SingleFileDecodeCapMB = 20 }()

		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
			"invoice[]": {append(jpegBytes(t, color.RGBA{G: 9, A: 255}), bytes.Repeat([]byte{1}, 2*1024*1024)...)},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusUnsupportedMediaType)
		assertErrorCode(t, rec.Body.Bytes(), "UNSUPPORTED_MEDIA_TYPE")
	})

	t.Run("415 when pixel cap exceeded", func(t *testing.T) {
		env.uploads.cfg.Limits.MaxPixels = 1000
		defer func() { env.uploads.cfg.Limits.MaxPixels = 50_000_000 }()

		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
			"delivery[]": {jpegBytes(t, color.RGBA{B: 9, A: 255})},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusUnsupportedMediaType)
		assertErrorCode(t, rec.Body.Bytes(), "UNSUPPORTED_MEDIA_TYPE")
	})

	t.Run("423 when order locked", func(t *testing.T) {
		env.storage.SetLockTimeout(20 * time.Millisecond)
		release, err := env.storage.Acquire(context.Background(), 2021, "RX2101-22926")
		if err != nil {
			t.Fatalf("acquire lock: %v", err)
		}
		defer release()

		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
			"contract[]": {jpegBytes(t, color.RGBA{R: 3, A: 255})},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusLocked)
		assertErrorCode(t, rec.Body.Bytes(), "ORDER_LOCKED")
		env.storage.SetLockTimeout(30 * time.Second)
	})

	t.Run("pre-commit failure rolls back db and files", func(t *testing.T) {
		env.uploads.SetHooks(Hooks{
			BeforeCommit: func() error { return io.ErrUnexpectedEOF },
		})
		defer env.uploads.SetHooks(Hooks{})

		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
			"contract[]": {jpegBytes(t, color.RGBA{R: 4, A: 255})},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusInternalServerError)

		var count int
		if err := env.db.GetContext(context.Background(), &count, `SELECT COUNT(*) FROM uploads WHERE year = 2021 AND order_no = 'RX2101-22926'`); err != nil {
			t.Fatalf("count uploads: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected rollback to remove upload rows, got %d", count)
		}
	})

	t.Run("post-commit failure returns 200 stale and preserves previous pdf", func(t *testing.T) {
		customerClean, err := env.orders.CustomerClean(context.Background(), 2021, "RX2101-22926")
		if err != nil {
			t.Fatalf("customer clean: %v", err)
		}
		orderDir, err := env.storage.OrderDir(2021, "RX2101-22926")
		if err != nil {
			t.Fatalf("order dir: %v", err)
		}
		pdfPath := filepath.Join(orderDir, storage.MergedPDFName("RX2101-22926", customerClean))
		if err := os.MkdirAll(orderDir, 0o755); err != nil {
			t.Fatalf("mkdir order dir: %v", err)
		}
		if err := os.WriteFile(pdfPath, []byte("old-pdf"), 0o644); err != nil {
			t.Fatalf("write old pdf: %v", err)
		}

		env.uploads.SetHooks(Hooks{
			AfterCommit: func() error { return io.ErrUnexpectedEOF },
		})
		defer env.uploads.SetHooks(Hooks{})

		req := newMultipartRequest(t, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
			"contract[]": {jpegBytes(t, color.RGBA{R: 5, A: 255})},
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusOK)

		var payload struct {
			MergedPDFStale bool `json:"mergedPdfStale"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !payload.MergedPDFStale {
			t.Fatalf("expected mergedPdfStale=true")
		}
		data, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatalf("read preserved pdf: %v", err)
		}
		if string(data) != "old-pdf" {
			t.Fatalf("expected previous pdf preserved, got %q", string(data))
		}
	})

	t.Run("streaming request via pipe succeeds", func(t *testing.T) {
		body, contentType := streamingMultipartRequest(t, map[string]io.Reader{
			"invoice[]": bytes.NewReader(jpegBytes(t, color.RGBA{G: 255, A: 255})),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/y/2021/orders/RX2101-22926/uploads", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		assertStatusCode(t, rec.Code, http.StatusOK)
	})

	t.Run("429 when ip upload rate limit exceeded", func(t *testing.T) {
		ip := "192.168.1.55"
		for i := 0; i < 20; i++ {
			if !env.uploads.allowUploadAttempt(ip) {
				t.Fatalf("attempt %d should be allowed", i+1)
			}
		}
		if env.uploads.allowUploadAttempt(ip) {
			t.Fatalf("expected 21st attempt to be rate limited")
		}
	})
}

type uploadTestEnv struct {
	router  *gin.Engine
	db      *sqlx.DB
	storage *storage.Service
	orders  *orders.Service
	uploads *Service
}

func newUploadTestEnv(t *testing.T, builder PDFBuilder) uploadTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(tempDir, "data")
	cfg.Limits.SubmitBodyMaxMB = 60

	storageSvc := storage.New(cfg.DataDir)
	if err := storageSvc.EnsureLayout(); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	conn, err := db.Open(context.Background(), db.Options{
		Path: filepath.Join(cfg.DataDir, "app.db"),
		Pool: cfg.DB,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	seedOrder(t, conn)

	orderSvc := orders.NewService(conn, storageSvc)
	uploadSvc := NewService(conn, cfg, orderSvc, storageSvc, builder, limits.New(cfg.Concurrency))

	router := gin.New()
	router.POST("/api/y/:year/orders/:orderNo/uploads", uploadSvc.HandleSubmit)
	return uploadTestEnv{router: router, db: conn, storage: storageSvc, orders: orderSvc, uploads: uploadSvc}
}

func seedOrder(t *testing.T, conn *sqlx.DB) {
	t.Helper()
	_, err := conn.Exec(`
INSERT INTO orders (year, order_no, customer, customer_clean, csv_present)
VALUES (2021, 'RX2101-22926', '哈尔滨金诺食品有限公司', '哈尔滨金诺食品有限公司', 1);
INSERT INTO order_lines (
	year, order_no, order_date, order_date_sort, customer, product, quantity, amount,
	total_with_tax, tax_rate, invoice_no, source_hash, source_line
) VALUES (
	2021, 'RX2101-22926', '2021/1/4', '2021-01-04', '哈尔滨金诺食品有限公司', '满特起酥油（FM）', 1000, 114545.45,
	126000, 10, '2021/50122444', 'seed-hash', 1
);`)
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

func newMultipartRequest(t *testing.T, target string, files map[string][][]byte) *http.Request {
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

func streamingMultipartRequest(t *testing.T, files map[string]io.Reader) (io.Reader, string) {
	t.Helper()
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	go func() {
		defer pipeWriter.Close()
		defer writer.Close()
		for field, reader := range files {
			part, err := writer.CreateFormFile(field, field+".jpg")
			if err != nil {
				_ = pipeWriter.CloseWithError(err)
				return
			}
			if _, err := io.Copy(part, reader); err != nil {
				_ = pipeWriter.CloseWithError(err)
				return
			}
		}
	}()
	return pipeReader, writer.FormDataContentType()
}

func jpegBytes(t *testing.T, fill color.RGBA) []byte {
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

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func assertStatusCode(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("expected status %d, got %d", want, got)
	}
}

func assertErrorCode(t *testing.T, body []byte, code string) {
	t.Helper()
	if !strings.Contains(string(body), code) {
		t.Fatalf("expected error code %q in body %s", code, string(body))
	}
}
