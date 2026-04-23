package integration

import (
	"archive/zip"
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
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/admin"
	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/db"
	"product-collection-form/backend/internal/httpapi"
	"product-collection-form/backend/internal/httpapi/limits"
	"product-collection-form/backend/internal/invoices"
	"product-collection-form/backend/internal/invoiceuploads"
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/storage"
	"product-collection-form/backend/internal/uploads"
)

type integrationPDFBuilder struct {
	fail bool
}

func (b integrationPDFBuilder) Build(_ context.Context, _ []string, w io.Writer) (int, error) {
	if b.fail {
		return 0, fmt.Errorf("forced pdf failure")
	}
	_, err := w.Write([]byte("%PDF-1.4\nintegration"))
	return 2, err
}

func TestUploadRoundTrip(t *testing.T) {
	t.Parallel()

	app := newIntegrationApp(t, integrationPDFBuilder{})

	req := multipartRequest(t, http.MethodPost, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
		"contract[]": {jpegBytes(t, color.RGBA{R: 255, A: 255})},
		"invoice[]":  {jpegBytes(t, color.RGBA{G: 255, A: 255})},
	})
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected upload 200, got %d", rec.Code)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/y/2021/orders/RX2101-22926", nil)
	detailRec := httptest.NewRecorder()
	app.router.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("expected detail 200, got %d", detailRec.Code)
	}
	if !bytes.Contains(detailRec.Body.Bytes(), []byte(`"合同"`)) || !bytes.Contains(detailRec.Body.Bytes(), []byte(`"发票"`)) {
		t.Fatalf("expected detail to include uploaded kinds: %s", detailRec.Body.String())
	}

	progressReq := httptest.NewRequest(http.MethodGet, "/api/y/2021/progress", nil)
	progressRec := httptest.NewRecorder()
	app.router.ServeHTTP(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected progress 200, got %d", progressRec.Code)
	}
	if !bytes.Contains(progressRec.Body.Bytes(), []byte(`"uploaded":1`)) {
		t.Fatalf("expected uploaded count in progress, got %s", progressRec.Body.String())
	}
}

func TestAdminFlowAndExports(t *testing.T) {
	t.Parallel()

	app := newIntegrationApp(t, integrationPDFBuilder{})
	seedOriginalFiles(t, app.storage)
	csrfToken, cookie := loginAndPing(t, app.router)

	exportReq := httptest.NewRequest(http.MethodGet, "/api/admin/2021/export.zip", nil)
	exportReq.AddCookie(cookie)
	exportRec := httptest.NewRecorder()
	app.router.ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("expected export 200, got %d", exportRec.Code)
	}

	names := zipEntryNames(t, exportRec.Body.Bytes())
	if len(names) != 2 {
		t.Fatalf("expected 2 year-export entries, got %d: %v", len(names), names)
	}

	bundleReq := httptest.NewRequest(http.MethodGet, "/api/admin/2021/orders/RX2101-22926/bundle.zip", nil)
	bundleReq.AddCookie(cookie)
	bundleRec := httptest.NewRecorder()
	app.router.ServeHTTP(bundleRec, bundleReq)
	if bundleRec.Code != http.StatusOK {
		t.Fatalf("expected bundle 200, got %d", bundleRec.Code)
	}
	if len(zipEntryNames(t, bundleRec.Body.Bytes())) != 4 {
		t.Fatalf("expected full bundle with 4 entries")
	}

	csrfReq := httptest.NewRequest(http.MethodPost, "/api/admin/2021/orders/RX2101-22926/rebuild-pdf", nil)
	csrfReq.AddCookie(cookie)
	csrfReq.Header.Set("X-Admin-Csrf", csrfToken)
	csrfRec := httptest.NewRecorder()
	app.router.ServeHTTP(csrfRec, csrfReq)
	if csrfRec.Code != http.StatusOK {
		t.Fatalf("expected rebuild 200, got %d", csrfRec.Code)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	app.router.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected healthz 200, got %d", healthRec.Code)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	app.router.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected readyz 200, got %d", readyRec.Code)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	app.router.ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("expected metrics 200, got %d", metricsRec.Code)
	}
	if !bytes.Contains(metricsRec.Body.Bytes(), []byte("app_http_requests_total")) {
		t.Fatalf("expected prometheus metrics body, got %s", metricsRec.Body.String())
	}
}

func TestPostCommitPDFFailureAndRebuildRecovery(t *testing.T) {
	t.Parallel()

	app := newIntegrationApp(t, integrationPDFBuilder{})
	seedOriginalFiles(t, app.storage)
	customerClean, err := app.orders.CustomerClean(context.Background(), 2021, "RX2101-22926")
	if err != nil {
		t.Fatalf("customer clean: %v", err)
	}
	orderDir, err := app.storage.OrderDir(2021, "RX2101-22926")
	if err != nil {
		t.Fatalf("order dir: %v", err)
	}
	pdfPath := filepath.Join(orderDir, storage.MergedPDFName("RX2101-22926", customerClean))
	if err := os.WriteFile(pdfPath, []byte("old-pdf"), 0o644); err != nil {
		t.Fatalf("seed old pdf: %v", err)
	}

	app.uploads.SetHooks(uploads.Hooks{
		AfterCommit: func() error { return fmt.Errorf("post-commit fail") },
	})
	defer app.uploads.SetHooks(uploads.Hooks{})

	req := multipartRequest(t, http.MethodPost, "/api/y/2021/orders/RX2101-22926/uploads", map[string][][]byte{
		"contract[]": {jpegBytes(t, color.RGBA{R: 1, A: 255})},
	})
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected upload 200, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"mergedPdfStale":true`)) {
		t.Fatalf("expected mergedPdfStale=true, got %s", rec.Body.String())
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	if string(data) != "old-pdf" {
		t.Fatalf("expected old pdf preserved, got %q", string(data))
	}

	csrfToken, cookie := loginAndPing(t, app.router)
	rebuildReq := httptest.NewRequest(http.MethodPost, "/api/admin/2021/orders/RX2101-22926/rebuild-pdf", nil)
	rebuildReq.AddCookie(cookie)
	rebuildReq.Header.Set("X-Admin-Csrf", csrfToken)
	rebuildRec := httptest.NewRecorder()
	app.router.ServeHTTP(rebuildRec, rebuildReq)
	if rebuildRec.Code != http.StatusOK {
		t.Fatalf("expected rebuild 200, got %d", rebuildRec.Code)
	}
}

func TestInvoiceProgressInvalidatesAfterUploadAndDelete(t *testing.T) {
	t.Parallel()

	app := newIntegrationApp(t, integrationPDFBuilder{})

	progressReq := httptest.NewRequest(http.MethodGet, "/api/invoices/progress", nil)
	progressRec := httptest.NewRecorder()
	app.router.ServeHTTP(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected initial invoice progress 200, got %d", progressRec.Code)
	}
	if !bytes.Contains(progressRec.Body.Bytes(), []byte(`"uploaded":0`)) {
		t.Fatalf("expected initial uploaded count 0, got %s", progressRec.Body.String())
	}

	uploadReq := multipartRequest(t, http.MethodPost, "/api/invoices/INV-001/uploads", map[string][][]byte{
		"invoice_photo[]": {jpegBytes(t, color.RGBA{R: 9, A: 255})},
	})
	uploadRec := httptest.NewRecorder()
	app.router.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("expected invoice upload 200, got %d", uploadRec.Code)
	}

	progressRec = httptest.NewRecorder()
	app.router.ServeHTTP(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected post-upload invoice progress 200, got %d", progressRec.Code)
	}
	if !bytes.Contains(progressRec.Body.Bytes(), []byte(`"uploaded":1`)) {
		t.Fatalf("expected uploaded count 1 after upload, got %s", progressRec.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/invoices/INV-001", nil)
	detailRec := httptest.NewRecorder()
	app.router.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("expected invoice detail 200, got %d", detailRec.Code)
	}
	var detail struct {
		Uploads []struct {
			ID int64 `json:"id"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode invoice detail: %v", err)
	}
	if len(detail.Uploads) != 1 {
		t.Fatalf("expected 1 invoice upload, got %d", len(detail.Uploads))
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/invoices/INV-001/uploads/%d", detail.Uploads[0].ID), nil)
	deleteRec := httptest.NewRecorder()
	app.router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected invoice delete 200, got %d", deleteRec.Code)
	}

	progressRec = httptest.NewRecorder()
	app.router.ServeHTTP(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected post-delete invoice progress 200, got %d", progressRec.Code)
	}
	if !bytes.Contains(progressRec.Body.Bytes(), []byte(`"uploaded":0`)) {
		t.Fatalf("expected uploaded count 0 after delete, got %s", progressRec.Body.String())
	}
}

func TestPublicInvoiceEndpointsIgnoreInactiveInvoices(t *testing.T) {
	t.Parallel()

	app := newIntegrationApp(t, integrationPDFBuilder{})

	searchReq := httptest.NewRequest(http.MethodGet, "/api/invoices/search?q=INV&limit=20", nil)
	searchRec := httptest.NewRecorder()
	app.router.ServeHTTP(searchRec, searchReq)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("expected invoice search 200, got %d", searchRec.Code)
	}
	if bytes.Contains(searchRec.Body.Bytes(), []byte(`INV-OLD`)) {
		t.Fatalf("expected inactive invoice hidden from public search, got %s", searchRec.Body.String())
	}
	if !bytes.Contains(searchRec.Body.Bytes(), []byte(`INV-001`)) {
		t.Fatalf("expected active invoice in public search, got %s", searchRec.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/invoices/INV-OLD", nil)
	detailRec := httptest.NewRecorder()
	app.router.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusNotFound {
		t.Fatalf("expected inactive invoice detail 404, got %d", detailRec.Code)
	}

	uploadReq := multipartRequest(t, http.MethodPost, "/api/invoices/INV-OLD/uploads", map[string][][]byte{
		"invoice_photo[]": {jpegBytes(t, color.RGBA{B: 9, A: 255})},
	})
	uploadRec := httptest.NewRecorder()
	app.router.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusNotFound {
		t.Fatalf("expected inactive invoice upload 404, got %d", uploadRec.Code)
	}

	progressReq := httptest.NewRequest(http.MethodGet, "/api/invoices/progress", nil)
	progressRec := httptest.NewRecorder()
	app.router.ServeHTTP(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected invoice progress 200, got %d", progressRec.Code)
	}
	if !bytes.Contains(progressRec.Body.Bytes(), []byte(`"total":1`)) {
		t.Fatalf("expected invoice progress total 1, got %s", progressRec.Body.String())
	}
}

type integrationApp struct {
	router         *gin.Engine
	db             *sqlx.DB
	storage        *storage.Service
	orders         *orders.Service
	uploads        *uploads.Service
	invoices       *invoices.Service
	invoiceUploads *invoiceuploads.Service
}

func newIntegrationApp(t *testing.T, pdfBuilder integrationPDFBuilder) integrationApp {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(tempDir, "data")
	cfg.AdminPassword = "secret"

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
	seedIntegrationOrder(t, conn)
	seedIntegrationInvoices(t, conn)

	orderSvc := orders.NewService(conn, storageSvc)
	invoiceSvc := invoices.NewService(conn, storageSvc)
	limiter := limits.New(cfg.Concurrency)
	uploadSvc := uploads.NewService(conn, cfg, orderSvc, storageSvc, pdfBuilder, limiter)
	invoiceUploadSvc := invoiceuploads.NewService(conn, cfg, storageSvc, limiter, invoiceSvc)
	adminSvc, err := admin.NewService(conn, cfg, orderSvc, storageSvc, uploadSvc, invoiceSvc, invoiceUploadSvc, limiter)
	if err != nil {
		t.Fatalf("create admin service: %v", err)
	}
	distFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<p>placeholder</p>")}}
	router := httpapi.New(conn, orderSvc, storageSvc, uploadSvc, adminSvc, invoiceSvc, invoiceUploadSvc, distFS).Engine()
	return integrationApp{
		router:         router,
		db:             conn,
		storage:        storageSvc,
		orders:         orderSvc,
		uploads:        uploadSvc,
		invoices:       invoiceSvc,
		invoiceUploads: invoiceUploadSvc,
	}
}

func seedIntegrationOrder(t *testing.T, conn *sqlx.DB) {
	t.Helper()
	_, err := conn.Exec(`
INSERT INTO orders (year, order_no, customer, customer_clean, csv_present)
VALUES (2021, 'RX2101-22926', '哈尔滨金诺食品有限公司', '哈尔滨金诺食品有限公司', 1);
INSERT INTO order_lines (
	year, order_no, order_date, order_date_sort, customer, product, quantity, amount,
	total_with_tax, tax_rate, invoice_no, source_hash, source_line
) VALUES (
	2021, 'RX2101-22926', '2021/1/4', '2021-01-04', '哈尔滨金诺食品有限公司', '满特起酥油（FM）', 1000, 114545.45,
	126000, 10, '2021/50122444', 'integration-seed-hash', 1
);
INSERT INTO uploads (year, order_no, kind, seq, filename, byte_size, sha256) VALUES
	(2021, 'RX2101-22926', '合同', 1, 'RX2101-22926-哈尔滨金诺食品有限公司-合同-01.jpg', 1, 'a'),
	(2021, 'RX2101-22926', '发票', 1, 'RX2101-22926-哈尔滨金诺食品有限公司-发票-01.jpg', 1, 'b'),
	(2021, 'RX2101-22926', '发货单', 1, 'RX2101-22926-哈尔滨金诺食品有限公司-发货单-01.jpg', 1, 'c');`)
	if err != nil {
		t.Fatalf("seed integration order: %v", err)
	}
}

func seedIntegrationInvoices(t *testing.T, conn *sqlx.DB) {
	t.Helper()
	_, err := conn.Exec(`
INSERT INTO invoices (invoice_no, customer, customer_clean, seller, invoice_date, csv_present) VALUES
	('INV-001', '发票客户', '发票客户', '销方 A', '2026-01-05', 1),
	('INV-OLD', '旧发票客户', '旧发票客户', '销方 B', '2026-01-06', 0);
INSERT INTO invoice_lines (
	invoice_no, year, invoice_date, seller, customer, product, quantity, amount,
	tax_amount, total_with_tax, tax_rate, source_hash, source_line
) VALUES
	('INV-001', 2026, '2026-01-05', '销方 A', '发票客户', '产品一', 1, 100, 13, 113, '13%', 'invoice-seed-1', 1),
	('INV-OLD', 2026, '2026-01-06', '销方 B', '旧发票客户', '产品二', 2, 200, 26, 226, '13%', 'invoice-seed-2', 2);`)
	if err != nil {
		t.Fatalf("seed integration invoices: %v", err)
	}
}

func seedOriginalFiles(t *testing.T, storageSvc *storage.Service) {
	t.Helper()
	orderDir, err := storageSvc.OrderDir(2021, "RX2101-22926")
	if err != nil {
		t.Fatalf("order dir: %v", err)
	}
	if err := os.MkdirAll(orderDir, 0o755); err != nil {
		t.Fatalf("mkdir order dir: %v", err)
	}
	writeJPEG(t, filepath.Join(orderDir, "RX2101-22926-哈尔滨金诺食品有限公司-合同-01.jpg"), color.RGBA{R: 255, A: 255})
	writeJPEG(t, filepath.Join(orderDir, "RX2101-22926-哈尔滨金诺食品有限公司-发票-01.jpg"), color.RGBA{G: 255, A: 255})
	writeJPEG(t, filepath.Join(orderDir, "RX2101-22926-哈尔滨金诺食品有限公司-发货单-01.jpg"), color.RGBA{B: 255, A: 255})
	if err := os.WriteFile(filepath.Join(orderDir, "RX2101-22926-哈尔滨金诺食品有限公司-合同与发票.pdf"), []byte("%PDF-1.4\nseed"), 0o644); err != nil {
		t.Fatalf("seed merged pdf: %v", err)
	}
}

func loginAndPing(t *testing.T, router *gin.Engine) (string, *http.Cookie) {
	t.Helper()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginRec.Code)
	}
	cookie := loginRec.Result().Cookies()[0]

	pingReq := httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil)
	pingReq.AddCookie(cookie)
	pingRec := httptest.NewRecorder()
	router.ServeHTTP(pingRec, pingReq)
	if pingRec.Code != http.StatusOK {
		t.Fatalf("expected ping 200, got %d", pingRec.Code)
	}
	var payload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(pingRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode ping payload: %v", err)
	}
	return payload.CSRFToken, cookie
}

func multipartRequest(t *testing.T, method, target string, files map[string][][]byte) *http.Request {
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
				t.Fatalf("write form file: %v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(method, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func zipEntryNames(t *testing.T, payload []byte) []string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
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

func writeJPEG(t *testing.T, path string, fill color.RGBA) {
	t.Helper()
	if err := os.WriteFile(path, jpegBytes(t, fill), 0o644); err != nil {
		t.Fatalf("write jpeg: %v", err)
	}
}
