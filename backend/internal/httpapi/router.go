package httpapi

import (
	"context"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/admin"
	"product-collection-form/backend/internal/apierror"
	"product-collection-form/backend/internal/invoices"
	"product-collection-form/backend/internal/invoiceuploads"
	"product-collection-form/backend/internal/metrics"
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/storage"
	"product-collection-form/backend/internal/uploads"
)

type Router struct {
	db             *sqlx.DB
	orders         *orders.Service
	storage        *storage.Service
	uploads        *uploads.Service
	admin          *admin.Service
	invoices       *invoices.Service
	invoiceUploads *invoiceuploads.Service
	distFS         fs.FS
}

func New(db *sqlx.DB, orderSvc *orders.Service, storageSvc *storage.Service, uploadSvc *uploads.Service, adminSvc *admin.Service, invoiceSvc *invoices.Service, invoiceUploadSvc *invoiceuploads.Service, distFS fs.FS) *Router {
	return &Router{
		db:             db,
		orders:         orderSvc,
		storage:        storageSvc,
		uploads:        uploadSvc,
		admin:          adminSvc,
		invoices:       invoiceSvc,
		invoiceUploads: invoiceUploadSvc,
		distFS:         distFS,
	}
}

func (r *Router) Engine() *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery(), securityHeaders(), requestLogger())
	if err := engine.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	api := engine.Group("/api")
	yearGroup := api.Group("/y/:year")
	yearGroup.GET("/progress", r.handleProgress)
	yearGroup.GET("/search", r.handleSearch)
	yearGroup.GET("/orders/:orderNo", r.handleDetail)
	yearGroup.POST("/orders/:orderNo/uploads", r.uploads.HandleSubmit)
	yearGroup.DELETE("/orders/:orderNo/uploads/:id", r.uploads.HandleUserDelete)

	invGroup := api.Group("/invoices")
	invGroup.GET("/search", r.handleInvoiceSearch)
	invGroup.GET("/:invoiceNo", r.handleInvoiceDetail)
	invGroup.POST("/:invoiceNo/uploads", r.invoiceUploads.HandleSubmit)
	invGroup.DELETE("/:invoiceNo/uploads/:id", r.invoiceUploads.HandleDelete)

	adminGroup := api.Group("/admin")
	r.admin.RegisterRoutes(adminGroup)

	engine.GET("/healthz", r.handleHealth)
	engine.GET("/readyz", r.handleReady)
	engine.GET("/metrics", gin.WrapH(metrics.Default.Handler()))
	engine.GET("/files/y/:year/:orderNo/:filename", r.handleFile)
	engine.GET("/files/invoices/:invoiceNo/:filename", r.handleInvoiceFile)
	engine.NoRoute(r.handleSPA)
	return engine
}

func (r *Router) Handler() http.Handler {
	return timeoutDispatcher(r.Engine())
}

func (r *Router) handleProgress(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	progress, err := r.orders.Progress(c.Request.Context(), year)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取进度失败"))
		return
	}
	c.JSON(http.StatusOK, progress)
}

func (r *Router) handleSearch(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "limit 参数无效"))
		return
	}
	items, err := r.orders.Search(c.Request.Context(), year, c.Query("q"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (r *Router) handleDetail(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	detail, err := r.orders.Detail(c.Request.Context(), year, c.Param("orderNo"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (r *Router) handleFile(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	orderNo := c.Param("orderNo")
	filename := c.Param("filename")

	customerClean, err := r.orders.CustomerClean(c.Request.Context(), year, orderNo)
	if err != nil {
		writeError(c, err)
		return
	}

	allowed := false
	if filename == storage.MergedPDFName(orderNo, customerClean) {
		allowed = true
	} else {
		var count int
		if err := r.db.GetContext(c.Request.Context(), &count, `SELECT COUNT(*) FROM uploads WHERE year = ? AND order_no = ? AND filename = ?`, year, orderNo, filename); err != nil {
			writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取文件索引失败"))
			return
		}
		allowed = count > 0
	}
	if !allowed {
		writeError(c, apierror.ErrFileNotFound)
		return
	}

	fullPath, err := r.storage.ValidateOrderFilePath(year, orderNo, filename)
	if err != nil {
		writeError(c, apierror.ErrFileNotFound)
		return
	}
	if _, err := os.Stat(fullPath); err != nil {
		writeError(c, apierror.ErrFileNotFound)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.File(fullPath)
}

func (r *Router) handleInvoiceSearch(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "limit 参数无效"))
		return
	}
	items, err := r.invoices.Search(c.Request.Context(), c.Query("q"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (r *Router) handleInvoiceDetail(c *gin.Context) {
	detail, err := r.invoices.Detail(c.Request.Context(), c.Param("invoiceNo"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (r *Router) handleInvoiceFile(c *gin.Context) {
	invoiceNo := c.Param("invoiceNo")
	filename := c.Param("filename")

	var count int
	if err := r.db.GetContext(c.Request.Context(), &count, `SELECT COUNT(*) FROM invoice_uploads WHERE invoice_no = ? AND filename = ?`, invoiceNo, filename); err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取文件索引失败"))
		return
	}
	if count == 0 {
		writeError(c, apierror.ErrFileNotFound)
		return
	}

	fullPath, err := r.storage.ValidateInvoiceFilePath(invoiceNo, filename)
	if err != nil {
		writeError(c, apierror.ErrFileNotFound)
		return
	}
	if _, err := os.Stat(fullPath); err != nil {
		writeError(c, apierror.ErrFileNotFound)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.File(fullPath)
}

func (r *Router) handleSPA(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/files/") {
		writeError(c, apierror.ErrFileNotFound)
		return
	}

	requestPath := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
	if requestPath == "." || requestPath == "" {
		requestPath = "index.html"
	}
	if data, err := fs.ReadFile(r.distFS, requestPath); err == nil {
		contentType := mime.TypeByExtension(path.Ext(requestPath))
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		c.Data(http.StatusOK, contentType, data)
		return
	}

	index, err := fs.ReadFile(r.distFS, "index.html")
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取前端资源失败"))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", index)
}

func (r *Router) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (r *Router) handleReady(c *gin.Context) {
	if err := r.db.PingContext(c.Request.Context()); err != nil {
		writeError(c, apierror.Wrap(err, http.StatusServiceUnavailable, "SERVER_BUSY", "数据库未就绪"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func writeError(c *gin.Context, err error) {
	if apiErr, ok := apierror.As(err); ok {
		markErrorCode(c, apiErr.Code)
		c.JSON(apiErr.Status, gin.H{"error": gin.H{"code": apiErr.Code, "message": apiErr.Message}})
		return
	}
	markErrorCode(c, apierror.ErrInternal.Code)
	c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL", "message": "服务器内部错误"}})
}

func timeoutDispatcher(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline := routeTimeout(r.Method, r.URL.Path)
		if deadline <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), deadline)
		defer cancel()

		if isStreamingRoute(r.Method, r.URL.Path) {
			rc := http.NewResponseController(w)
			if err := rc.SetWriteDeadline(time.Now().Add(deadline)); err == nil {
				defer func() { _ = rc.SetWriteDeadline(time.Time{}) }()
			}
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		handler := http.TimeoutHandler(http.HandlerFunc(func(inner http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(inner, req.WithContext(ctx))
		}), deadline, timeoutResponseJSON())
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func routeTimeout(method, requestPath string) time.Duration {
	switch {
	case method == http.MethodGet && strings.HasPrefix(requestPath, "/api/admin/") && strings.HasSuffix(requestPath, "/export.csv"):
		return 60 * time.Second
	case method == http.MethodGet && strings.HasPrefix(requestPath, "/api/admin/") && strings.HasSuffix(requestPath, "/export.zip"):
		return 15 * time.Minute
	case method == http.MethodGet && strings.HasPrefix(requestPath, "/api/admin/") && strings.HasSuffix(requestPath, "/bundle.zip"):
		return 120 * time.Second
	case method == http.MethodGet && strings.HasPrefix(requestPath, "/api/admin/") && strings.HasSuffix(requestPath, "/merged.pdf"):
		return 120 * time.Second
	case method == http.MethodPost && strings.HasPrefix(requestPath, "/api/admin/") && strings.HasSuffix(requestPath, "/rebuild-pdf"):
		return 60 * time.Second
	case method == http.MethodPost && strings.HasPrefix(requestPath, "/api/y/") && strings.HasSuffix(requestPath, "/uploads"):
		return 120 * time.Second
	case method == http.MethodPost && strings.HasPrefix(requestPath, "/api/invoices/") && strings.HasSuffix(requestPath, "/uploads"):
		return 120 * time.Second
	case method == http.MethodGet && strings.HasPrefix(requestPath, "/files/"):
		return 120 * time.Second
	case strings.HasPrefix(requestPath, "/api/"):
		return 10 * time.Second
	default:
		return 0
	}
}

func isStreamingRoute(method, requestPath string) bool {
	if method != http.MethodGet {
		return false
	}
	switch {
	case strings.HasPrefix(requestPath, "/api/admin/") && strings.HasSuffix(requestPath, "/export.zip"):
		return true
	case strings.HasPrefix(requestPath, "/api/admin/") && strings.HasSuffix(requestPath, "/merged.pdf"):
		return true
	case strings.HasPrefix(requestPath, "/files/"):
		return true
	}
	return false
}
