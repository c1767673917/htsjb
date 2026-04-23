package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/db"
	"product-collection-form/backend/internal/httpapi/limits"
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/storage"
	"product-collection-form/backend/internal/uploads"
)

type adminPDFBuilder struct{}

func (adminPDFBuilder) Build(_ context.Context, _ []string, w io.Writer) (int, error) {
	_, err := w.Write([]byte("%PDF-1.4\nadmin"))
	return 2, err
}

type failingAdminPDFBuilder struct{}

func (failingAdminPDFBuilder) Build(_ context.Context, _ []string, _ io.Writer) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestAdminExportsRebuildAndCSRF(t *testing.T) {
	t.Parallel()

	env := newAdminTestEnv(t)
	csrfToken, cookie := adminLogin(t, env.router)

	t.Run("csrf required for mutation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/2021/orders/RX2101-22926/rebuild-pdf", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("rebuild pdf idempotent", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/2021/orders/RX2101-22926/rebuild-pdf", nil)
			req.AddCookie(cookie)
			req.Header.Set("X-Admin-Csrf", csrfToken)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected rebuild 200, got %d", rec.Code)
			}
		}
	})

	t.Run("year export only contains merged pdf and delivery", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/2021/export.zip", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected export 200, got %d", rec.Code)
		}
		names := zipNames(t, rec.Body.Bytes())
		sort.Strings(names)
		want := []string{
			"RX2101-22926/RX2101-22926-哈尔滨金诺食品有限公司-发货单-01.jpg",
			"RX2101-22926/RX2101-22926-哈尔滨金诺食品有限公司-合同与发票.pdf",
		}
		if diff := compareNames(names, want); diff != "" {
			t.Fatalf("unexpected year export contents: %s", diff)
		}
	})

	t.Run("bundle contains originals and merged pdf", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/2021/orders/RX2101-22926/bundle.zip", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected bundle 200, got %d", rec.Code)
		}
		names := zipNames(t, rec.Body.Bytes())
		sort.Strings(names)
		want := []string{
			"RX2101-22926/RX2101-22926-哈尔滨金诺食品有限公司-发货单-01.jpg",
			"RX2101-22926/RX2101-22926-哈尔滨金诺食品有限公司-发票-01.jpg",
			"RX2101-22926/RX2101-22926-哈尔滨金诺食品有限公司-合同-01.jpg",
			"RX2101-22926/RX2101-22926-哈尔滨金诺食品有限公司-合同与发票.pdf",
		}
		sort.Strings(want)
		if diff := compareNames(names, want); diff != "" {
			t.Fatalf("unexpected bundle contents: %s", diff)
		}
	})

	t.Run("delete upload returns mergedPdfStale after commit rebuild failure", func(t *testing.T) {
		failEnv := newAdminTestEnvWithBuilder(t, failingAdminPDFBuilder{})
		failCSRF, failCookie := adminLogin(t, failEnv.router)

		req := httptest.NewRequest(http.MethodDelete, "/api/admin/2021/orders/RX2101-22926/uploads/1", nil)
		req.AddCookie(failCookie)
		req.Header.Set("X-Admin-Csrf", failCSRF)
		rec := httptest.NewRecorder()
		failEnv.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected delete 200, got %d", rec.Code)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"mergedPdfStale":true`)) {
			t.Fatalf("expected mergedPdfStale response, got %s", rec.Body.String())
		}
	})

	t.Run("session cookie survives service restart", func(t *testing.T) {
		restarted := rebuildAdminRouter(t, env)

		req := httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		restarted.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected restarted service to accept cookie, got %d", rec.Code)
		}
	})
}

type adminTestEnv struct {
	router  *gin.Engine
	db      *sqlx.DB
	cfg     config.Config
	storage *storage.Service
}

func newAdminTestEnv(t *testing.T) adminTestEnv {
	return newAdminTestEnvWithBuilder(t, adminPDFBuilder{})
}

func newAdminTestEnvWithBuilder(t *testing.T, builder uploads.PDFBuilder) adminTestEnv {
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

	seedAdminOrder(t, conn)
	orderSvc := orders.NewService(conn, storageSvc)
	limiter := limits.New(cfg.Concurrency)
	uploadSvc := uploads.NewService(conn, cfg, orderSvc, storageSvc, builder, limiter)
	adminSvc, err := NewService(conn, cfg, orderSvc, storageSvc, uploadSvc, nil, nil, limiter)
	if err != nil {
		t.Fatalf("create admin service: %v", err)
	}
	seedAdminFiles(t, storageSvc)

	router := gin.New()
	group := router.Group("/api/admin")
	adminSvc.RegisterRoutes(group)
	return adminTestEnv{router: router, db: conn, cfg: cfg, storage: storageSvc}
}

func rebuildAdminRouter(t *testing.T, env adminTestEnv) *gin.Engine {
	t.Helper()

	orderSvc := orders.NewService(env.db, env.storage)
	limiter := limits.New(env.cfg.Concurrency)
	uploadSvc := uploads.NewService(env.db, env.cfg, orderSvc, env.storage, adminPDFBuilder{}, limiter)
	adminSvc, err := NewService(env.db, env.cfg, orderSvc, env.storage, uploadSvc, nil, nil, limiter)
	if err != nil {
		t.Fatalf("recreate admin service: %v", err)
	}
	router := gin.New()
	group := router.Group("/api/admin")
	adminSvc.RegisterRoutes(group)
	return router
}

func seedAdminOrder(t *testing.T, conn *sqlx.DB) {
	t.Helper()
	_, err := conn.Exec(`
INSERT INTO orders (year, order_no, customer, customer_clean, csv_present)
VALUES (2021, 'RX2101-22926', '哈尔滨金诺食品有限公司', '哈尔滨金诺食品有限公司', 1);
INSERT INTO order_lines (
	year, order_no, order_date, order_date_sort, customer, product, quantity, amount,
	total_with_tax, tax_rate, invoice_no, source_hash, source_line
) VALUES (
	2021, 'RX2101-22926', '2021/1/4', '2021-01-04', '哈尔滨金诺食品有限公司', '满特起酥油（FM）', 1000, 114545.45,
	126000, 10, '2021/50122444', 'admin-seed-hash', 1
);
INSERT INTO uploads (year, order_no, kind, seq, filename, byte_size, sha256) VALUES
	(2021, 'RX2101-22926', '合同', 1, 'RX2101-22926-哈尔滨金诺食品有限公司-合同-01.jpg', 1, 'a'),
	(2021, 'RX2101-22926', '发票', 1, 'RX2101-22926-哈尔滨金诺食品有限公司-发票-01.jpg', 1, 'b'),
	(2021, 'RX2101-22926', '发货单', 1, 'RX2101-22926-哈尔滨金诺食品有限公司-发货单-01.jpg', 1, 'c');`)
	if err != nil {
		t.Fatalf("seed admin order: %v", err)
	}
}

func seedAdminFiles(t *testing.T, storageSvc *storage.Service) {
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
		t.Fatalf("write merged pdf: %v", err)
	}
}

func adminLogin(t *testing.T, router *gin.Engine) (string, *http.Cookie) {
	t.Helper()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginRec.Code)
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie")
	}

	pingReq := httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil)
	pingReq.AddCookie(cookies[0])
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
	return payload.CSRFToken, cookies[0]
}

func zipNames(t *testing.T, body []byte) []string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}

func compareNames(got, want []string) string {
	if len(got) != len(want) {
		return "zip entry count mismatch"
	}
	for i := range got {
		if got[i] != want[i] {
			return got[i] + " != " + want[i]
		}
	}
	return ""
}

func writeJPEG(t *testing.T, path string, fill color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg: %v", err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
}
