package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/apierror"
	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/httpapi/limits"
	"product-collection-form/backend/internal/invoices"
	"product-collection-form/backend/internal/invoiceuploads"
	"product-collection-form/backend/internal/metrics"
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/storage"
	"product-collection-form/backend/internal/uploads"
)

const sessionCookieName = "admin_session"

type Service struct {
	db             *sqlx.DB
	cfg            config.Config
	orders         *orders.Service
	storage        *storage.Service
	uploads        *uploads.Service
	invoices       *invoices.Service
	invoiceUploads *invoiceuploads.Service
	limits         *limits.Manager
	sessionKey     []byte
	csrfSecret     []byte
	limitMu        sync.Mutex
	loginLimits    map[string]*loginBucket
}

type loginBucket struct {
	Attempts []time.Time
}

type yearExportOrder struct {
	OrderNo       string
	CustomerClean string
	DeliveryFiles []string
}

func NewService(db *sqlx.DB, cfg config.Config, orderSvc *orders.Service, storageSvc *storage.Service, uploadSvc *uploads.Service, invoiceSvc *invoices.Service, invoiceUploadSvc *invoiceuploads.Service, limiter *limits.Manager) (*Service, error) {
	sessionKey := deriveStableKey(cfg, "admin-session")
	csrfSecret := deriveStableKey(cfg, "admin-csrf")
	return &Service{
		db:             db,
		cfg:            cfg,
		orders:         orderSvc,
		storage:        storageSvc,
		uploads:        uploadSvc,
		invoices:       invoiceSvc,
		invoiceUploads: invoiceUploadSvc,
		limits:         limiter,
		sessionKey:     sessionKey,
		csrfSecret:     csrfSecret,
		loginLimits:    map[string]*loginBucket{},
	}, nil
}

func (s *Service) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/login", s.handleLogin)

	authed := group.Group("")
	authed.Use(s.requireSession)
	authed.GET("/ping", s.handlePing)
	authed.GET("/years", s.handleYears)
	authed.GET("/:year/orders", s.handleListOrders)
	authed.GET("/:year/orders/:orderNo", s.handleOrderDetail)
	authed.GET("/:year/orders/:orderNo/merged.pdf", s.handleMergedPDF)
	authed.GET("/:year/orders/:orderNo/bundle.zip", s.handleOrderBundle)
	authed.GET("/:year/export.zip", s.handleYearExport)
	authed.GET("/:year/export.csv", s.handleCSVExport)

	authed.GET("/invoices", s.handleInvoiceList)
	authed.GET("/invoices/export.csv", s.handleInvoiceCSVExport)
	authed.GET("/invoices/:invoiceNo", s.handleInvoiceDetail)

	mutating := authed.Group("")
	mutating.Use(s.requireCSRF)
	mutating.POST("/logout", s.handleLogout)
	mutating.POST("/:year/orders/:orderNo/rebuild-pdf", s.handleRebuildPDF)
	mutating.POST("/:year/orders/:orderNo/check", s.handleSetCheckStatus)
	mutating.DELETE("/:year/orders/:orderNo/uploads/:id", s.handleDeleteUpload)
	mutating.DELETE("/:year/orders/:orderNo", s.handleResetOrder)
	mutating.DELETE("/invoices/:invoiceNo/uploads/:id", s.handleDeleteInvoiceUpload)
	mutating.DELETE("/invoices/:invoiceNo", s.handleResetInvoice)
}

func (s *Service) requireSession(c *gin.Context) {
	token, err := c.Cookie(sessionCookieName)
	if err != nil {
		writeError(c, apierror.ErrUnauthenticated)
		c.Abort()
		return
	}
	if _, err := s.parseSessionToken(token); err != nil {
		writeError(c, apierror.ErrUnauthenticated)
		c.Abort()
		return
	}
	c.Set("adminSessionToken", token)
	c.Next()
}

func (s *Service) requireCSRF(c *gin.Context) {
	token := c.GetString("adminSessionToken")
	if token == "" {
		writeError(c, apierror.ErrUnauthenticated)
		c.Abort()
		return
	}
	expected := s.csrfToken(token)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(c.GetHeader("X-Admin-Csrf"))) != 1 {
		writeError(c, apierror.Wrap(nil, http.StatusBadRequest, "BAD_REQUEST", "CSRF token 无效"))
		c.Abort()
		return
	}
	c.Next()
}

func (s *Service) handleLogin(c *gin.Context) {
	if !s.allowLoginAttempt(c.ClientIP()) {
		metrics.Default.IncRateLimited()
		writeError(c, apierror.ErrRateLimited)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := decodeStrictJSON(c.Request.Body, &req); err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "登录请求格式错误"))
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		writeError(c, apierror.Wrap(nil, http.StatusBadRequest, "BAD_REQUEST", "password 字段不能为空"))
		return
	}

	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.cfg.AdminPassword)) != 1 || len(req.Password) != len(s.cfg.AdminPassword) {
		writeError(c, apierror.ErrUnauthenticated)
		return
	}

	token, err := randomHex(32)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建会话失败"))
		return
	}
	expiry := time.Now().Add(time.Duration(s.cfg.SessionTTLHours) * time.Hour)
	signedToken, err := s.issueSessionToken(token, expiry)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建会话失败"))
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, signedToken, int(time.Until(expiry).Seconds()), "/", "", c.Request.TLS != nil, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) handleLogout(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", c.Request.TLS != nil, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) handlePing(c *gin.Context) {
	token := c.GetString("adminSessionToken")
	c.JSON(http.StatusOK, gin.H{"ok": true, "csrfToken": s.csrfToken(token)})
}

func (s *Service) handleYears(c *gin.Context) {
	items, err := s.orders.AdminYears(c.Request.Context())
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取年份统计失败"))
		return
	}
	c.JSON(http.StatusOK, items)
}

func (s *Service) handleListOrders(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "page 参数无效"))
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("size", "50"))
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "size 参数无效"))
		return
	}
	onlyUploaded := c.Query("onlyUploaded") == "true"
	onlyCSVRemoved := c.Query("onlyCsvRemoved") == "true"
	searchQuery := strings.TrimSpace(c.Query("q"))
	checkStatus := strings.TrimSpace(c.Query("checkStatus"))

	result, err := s.orders.AdminList(c.Request.Context(), year, page, size, onlyUploaded, onlyCSVRemoved, searchQuery, checkStatus)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取订单列表失败"))
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Service) handleOrderDetail(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	detail, err := s.orders.Detail(c.Request.Context(), year, c.Param("orderNo"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (s *Service) handleMergedPDF(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	orderNo := c.Param("orderNo")
	customerClean, err := s.orders.CustomerClean(c.Request.Context(), year, orderNo)
	if err != nil {
		writeError(c, err)
		return
	}
	filename := storage.MergedPDFName(orderNo, customerClean)
	path, err := s.storage.ValidateOrderFilePath(year, orderNo, filename)
	if err != nil {
		writeError(c, apierror.ErrFileNotFound)
		return
	}
	if _, err := os.Stat(path); err != nil {
		writeError(c, apierror.ErrFileNotFound)
		return
	}
	c.FileAttachment(path, filename)
}

func (s *Service) handleOrderBundle(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	orderNo := c.Param("orderNo")
	releaseBundle, err := s.acquireBundleGate(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	defer releaseBundle()

	release, err := s.storage.Acquire(c.Request.Context(), year, orderNo)
	if err != nil {
		writeError(c, err)
		return
	}
	defer release()

	files, err := s.bundleFiles(c.Request.Context(), year, orderNo)
	if err != nil {
		writeError(c, err)
		return
	}

	var payload bytes.Buffer
	zw := zip.NewWriter(&payload)
	for _, file := range files {
		if err := writeZipEntry(c.Request.Context(), zw, filepath.Join(orderNo, filepath.Base(file)), file); err != nil {
			_ = zw.Close()
			writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "构建资料包失败"))
			return
		}
	}
	if err := zw.Close(); err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "关闭资料包失败"))
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-bundle.zip"`, orderNo))
	metrics.Default.IncZIPExports()
	c.Data(http.StatusOK, "application/zip", payload.Bytes())
}

func (s *Service) handleDeleteUpload(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	orderNo := c.Param("orderNo")
	uploadID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, apierror.ErrBadRequest)
		return
	}

	result, err := s.uploads.DeleteUpload(c.Request.Context(), year, orderNo, uploadID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "mergedPdfStale": result.MergedPDFStale})
}

func (s *Service) handleSetCheckStatus(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeStrictJSON(c.Request.Body, &req); err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误"))
		return
	}
	status := strings.TrimSpace(req.Status)
	if !orders.ValidCheckStatus(status) {
		writeError(c, apierror.Wrap(nil, http.StatusBadRequest, "BAD_REQUEST", "status 必须为 未检查/已检查/错误"))
		return
	}
	if err := s.orders.SetCheckStatus(c.Request.Context(), year, c.Param("orderNo"), status); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "checkStatus": status})
}

func (s *Service) handleResetOrder(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	if err := s.resetOrder(c.Request.Context(), year, c.Param("orderNo")); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) handleRebuildPDF(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	orderNo := c.Param("orderNo")
	var count int
	if err := s.db.GetContext(c.Request.Context(), &count, `SELECT COUNT(*) FROM uploads WHERE year = ? AND order_no = ?`, year, orderNo); err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "检查上传记录失败"))
		return
	}
	if count == 0 {
		writeError(c, apierror.ErrOrderNotFound)
		return
	}

	release, err := s.storage.Acquire(c.Request.Context(), year, orderNo)
	if err != nil {
		writeError(c, err)
		return
	}
	defer release()

	pages, err := s.uploads.RebuildMergedPDF(c.Request.Context(), year, orderNo)
	if err != nil {
		if apiErr, ok := apierror.As(err); ok {
			writeError(c, apiErr)
			return
		}
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "重建 PDF 失败"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "pages": pages})
}

func (s *Service) handleYearExport(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	releaseExport, err := s.acquireYearExportGate(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	defer releaseExport()

	operator := strings.TrimSpace(c.Query("operator"))
	uploadFrom := strings.TrimSpace(c.Query("uploadFrom"))
	uploadTo := strings.TrimSpace(c.Query("uploadTo"))

	exportOrders, err := s.yearExportOrders(c.Request.Context(), year, operator, uploadFrom, uploadTo)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取导出订单失败"))
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%d-完整资料.zip"`, year))
	c.Header("X-Content-Type-Options", "nosniff")
	zw := zip.NewWriter(c.Writer)
	var exportErrors []string

	for _, order := range exportOrders {
		release, err := s.storage.Acquire(c.Request.Context(), year, order.OrderNo)
		if err != nil {
			exportErrors = append(exportErrors, fmt.Sprintf("%s: 获取订单锁失败: %v", order.OrderNo, err))
			continue
		}

		pdfName := storage.MergedPDFName(order.OrderNo, order.CustomerClean)
		pdfPath, err := s.storage.ValidateOrderFilePath(year, order.OrderNo, pdfName)
		if err == nil {
			if _, statErr := os.Stat(pdfPath); statErr == nil {
				if err := writeZipEntry(c.Request.Context(), zw, filepath.Join(order.OrderNo, pdfName), pdfPath); err != nil {
					if c.Request.Context().Err() != nil {
						release()
						slog.WarnContext(c.Request.Context(), "write year export pdf cancelled", "year", year, "order_no", order.OrderNo, "error", err)
						_ = zw.Close()
						return
					}
					exportErrors = append(exportErrors, fmt.Sprintf("%s: 写入合并PDF失败: %v", order.OrderNo, err))
					slog.WarnContext(c.Request.Context(), "write year export pdf failed", "year", year, "order_no", order.OrderNo, "error", err)
				}
			}
		}

		for _, filename := range order.DeliveryFiles {
			fullPath, err := s.storage.ValidateOrderFilePath(year, order.OrderNo, filename)
			if err != nil {
				exportErrors = append(exportErrors, fmt.Sprintf("%s: 发货单路径非法: %v", order.OrderNo, err))
				continue
			}
			if err := writeZipEntry(c.Request.Context(), zw, filepath.Join(order.OrderNo, filename), fullPath); err != nil {
				if c.Request.Context().Err() != nil {
					release()
					slog.WarnContext(c.Request.Context(), "write year export delivery cancelled", "year", year, "order_no", order.OrderNo, "filename", filename, "error", err)
					_ = zw.Close()
					return
				}
				exportErrors = append(exportErrors, fmt.Sprintf("%s: 写入发货单失败 %s: %v", order.OrderNo, filename, err))
				slog.WarnContext(c.Request.Context(), "write year export delivery failed", "year", year, "order_no", order.OrderNo, "filename", filename, "error", err)
			}
		}
		release()
	}

	if len(exportErrors) > 0 {
		if err := writeErrorsEntry(zw, exportErrors); err != nil {
			slog.WarnContext(c.Request.Context(), "write year export errors.txt failed", "year", year, "error", err)
		}
	}
	if err := zw.Close(); err != nil {
		slog.WarnContext(c.Request.Context(), "close year export zip failed", "year", year, "error", err)
	}
	metrics.Default.IncZIPExports()
}

func (s *Service) handleCSVExport(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	onlyUploaded := c.Query("onlyUploaded") == "true"
	onlyCSVRemoved := c.Query("onlyCsvRemoved") == "true"
	searchQuery := strings.TrimSpace(c.Query("q"))
	checkStatus := strings.TrimSpace(c.Query("checkStatus"))

	items, err := s.orders.AdminExportAll(c.Request.Context(), year, onlyUploaded, onlyCSVRemoved, searchQuery, checkStatus)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取导出数据失败"))
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%d-订单列表.csv"`, year))
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM for Excel
	w := csv.NewWriter(c.Writer)
	w.Write([]string{"单据编号", "客户", "已上传", "检查状态", "合同数量", "发票数量", "发货单数量", "录入人", "最后上传时间"})
	for _, item := range items {
		lastUpload := ""
		if item.LastUploadAt != nil {
			lastUpload = item.LastUploadAt.Format("2006-01-02 15:04:05")
		}
		uploaded := "否"
		if item.Uploaded {
			uploaded = "是"
		}
		w.Write([]string{
			item.OrderNo,
			item.Customer,
			uploaded,
			item.CheckStatus,
			strconv.Itoa(item.Counts["合同"]),
			strconv.Itoa(item.Counts["发票"]),
			strconv.Itoa(item.Counts["发货单"]),
			strings.Join(item.Operators, "、"),
			lastUpload,
		})
	}
	w.Flush()
}

func (s *Service) handleInvoiceList(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "page 参数无效"))
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("size", "50"))
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "size 参数无效"))
		return
	}
	onlyUploaded := c.Query("onlyUploaded") == "true"
	searchQuery := strings.TrimSpace(c.Query("q"))

	result, err := s.invoices.AdminList(c.Request.Context(), page, size, searchQuery, onlyUploaded)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取发票列表失败"))
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Service) handleInvoiceDetail(c *gin.Context) {
	detail, err := s.invoices.Detail(c.Request.Context(), c.Param("invoiceNo"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (s *Service) handleDeleteInvoiceUpload(c *gin.Context) {
	invoiceNo := c.Param("invoiceNo")
	uploadID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, apierror.ErrBadRequest)
		return
	}
	if err := s.invoiceUploads.DeleteUpload(c.Request.Context(), invoiceNo, uploadID); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) handleResetInvoice(c *gin.Context) {
	invoiceNo := c.Param("invoiceNo")
	if err := s.invoiceUploads.ResetInvoice(c.Request.Context(), invoiceNo); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) handleInvoiceCSVExport(c *gin.Context) {
	onlyUploaded := c.Query("onlyUploaded") == "true"
	searchQuery := strings.TrimSpace(c.Query("q"))

	items, err := s.invoices.AdminExportAll(c.Request.Context(), searchQuery, onlyUploaded)
	if err != nil {
		writeError(c, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取导出数据失败"))
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="发票列表.csv"`)
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM for Excel
	w := csv.NewWriter(c.Writer)
	w.Write([]string{"发票号码", "客户", "销方", "开票日期", "已上传", "上传数量", "录入人", "最后上传时间"})
	for _, item := range items {
		lastUpload := ""
		if item.LastUploadAt != nil {
			lastUpload = item.LastUploadAt.Format("2006-01-02 15:04:05")
		}
		uploaded := "否"
		if item.Uploaded {
			uploaded = "是"
		}
		w.Write([]string{
			item.InvoiceNo,
			item.Customer,
			item.Seller,
			item.InvoiceDate,
			uploaded,
			strconv.Itoa(item.UploadCount),
			strings.Join(item.Operators, "、"),
			lastUpload,
		})
	}
	w.Flush()
}

func (s *Service) resetOrder(ctx context.Context, year int, orderNo string) error {
	release, err := s.storage.Acquire(ctx, year, orderNo)
	if err != nil {
		return err
	}
	defer release()

	orderDir, err := s.storage.OrderDir(year, orderNo)
	if err != nil {
		return err
	}
	txID, err := randomHex(8)
	if err != nil {
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建事务标识失败")
	}
	trashDir := filepath.Join(s.storage.TrashRoot(), fmt.Sprintf("%d-%s-%s", year, orderNo, txID))
	dirMoved := false
	if _, err := os.Stat(orderDir); err == nil {
		if err := renameAndSync(orderDir, trashDir); err != nil {
			return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "移动订单目录失败")
		}
		dirMoved = true
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		if dirMoved {
			_ = renameAndSync(trashDir, orderDir)
		}
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建重置事务失败")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM uploads WHERE year = ? AND order_no = ?`, year, orderNo); err != nil {
		if dirMoved {
			_ = renameAndSync(trashDir, orderDir)
		}
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "删除上传记录失败")
	}
	if err := tx.Commit(); err != nil {
		if dirMoved {
			_ = renameAndSync(trashDir, orderDir)
		}
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "提交重置事务失败")
	}
	if dirMoved {
		go os.RemoveAll(trashDir)
	}
	return nil
}

func (s *Service) yearExportOrders(ctx context.Context, year int, operator, uploadFrom, uploadTo string) ([]yearExportOrder, error) {
	type row struct {
		OrderNo       string  `db:"order_no"`
		CustomerClean string  `db:"customer_clean"`
		Filename      *string `db:"filename"`
	}

	existsConds := []string{"ux.year = o.year", "ux.order_no = o.order_no"}
	args := []any{year}
	if operator != "" {
		existsConds = append(existsConds, "ux.operator LIKE '%' || ? || '%' COLLATE NOCASE")
		args = append(args, operator)
	}
	if uploadFrom != "" {
		existsConds = append(existsConds, "ux.uploaded_at >= datetime(?, 'utc')")
		args = append(args, uploadFrom)
	}
	if uploadTo != "" {
		existsConds = append(existsConds, "ux.uploaded_at <= datetime(?, 'utc')")
		args = append(args, uploadTo+" 23:59:59")
	}

	query := `
SELECT
	o.order_no,
	o.customer_clean,
	u.filename
FROM orders o
LEFT JOIN uploads u
	ON u.year = o.year
	AND u.order_no = o.order_no
	AND u.kind = '发货单'
WHERE o.year = ? AND EXISTS (
	SELECT 1 FROM uploads ux WHERE ` + strings.Join(existsConds, " AND ") + `
)
ORDER BY o.order_no ASC, u.seq ASC, u.id ASC`

	var rows []row
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	ordersByNo := make([]yearExportOrder, 0)
	index := make(map[string]int)
	for _, row := range rows {
		pos, ok := index[row.OrderNo]
		if !ok {
			pos = len(ordersByNo)
			index[row.OrderNo] = pos
			ordersByNo = append(ordersByNo, yearExportOrder{
				OrderNo:       row.OrderNo,
				CustomerClean: row.CustomerClean,
			})
		}
		if row.Filename != nil && *row.Filename != "" {
			ordersByNo[pos].DeliveryFiles = append(ordersByNo[pos].DeliveryFiles, *row.Filename)
		}
	}
	return ordersByNo, nil
}

func (s *Service) bundleFiles(ctx context.Context, year int, orderNo string) ([]string, error) {
	rows, err := s.orders.UploadRowsByKinds(ctx, year, orderNo, orders.AllKinds())
	if err != nil {
		return nil, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取订单文件失败")
	}
	if len(rows) == 0 {
		return nil, apierror.ErrOrderNotFound
	}

	files := make([]string, 0, len(rows)+1)
	for _, row := range rows {
		fullPath, err := s.storage.ValidateOrderFilePath(year, orderNo, row.Filename)
		if err != nil {
			return nil, err
		}
		files = append(files, fullPath)
	}

	customerClean, err := s.orders.CustomerClean(ctx, year, orderNo)
	if err != nil {
		return nil, err
	}
	pdfPath, err := s.storage.ValidateOrderFilePath(year, orderNo, storage.MergedPDFName(orderNo, customerClean))
	if err == nil {
		if _, statErr := os.Stat(pdfPath); statErr == nil {
			files = append(files, pdfPath)
		}
	}
	sort.Strings(files)
	return files, nil
}

func (s *Service) csrfToken(sessionToken string) string {
	mac := hmac.New(sha256.New, s.csrfSecret)
	mac.Write([]byte(sessionToken))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) allowLoginAttempt(ip string) bool {
	s.limitMu.Lock()
	defer s.limitMu.Unlock()

	now := time.Now()
	for key, bucket := range s.loginLimits {
		bucket.Attempts = pruneAttempts(bucket.Attempts, now)
		if len(bucket.Attempts) == 0 {
			delete(s.loginLimits, key)
		}
	}

	bucket, ok := s.loginLimits[ip]
	if !ok {
		bucket = &loginBucket{}
		s.loginLimits[ip] = bucket
	}
	bucket.Attempts = pruneAttempts(bucket.Attempts, now)
	if len(bucket.Attempts) >= 5 {
		return false
	}
	bucket.Attempts = append(bucket.Attempts, now)
	return true
}

func writeZipEntry(ctx context.Context, zw *zip.Writer, name, source string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(name)
	header.Method = zip.Store
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, &contextReader{ctx: ctx, reader: file})
	return err
}

func writeErrorsEntry(zw *zip.Writer, lines []string) error {
	writer, err := zw.Create("ERRORS.txt")
	if err != nil {
		return err
	}
	_, err = writer.Write([]byte(strings.Join(lines, "\n") + "\n"))
	return err
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeError(c *gin.Context, err error) {
	if apiErr, ok := apierror.As(err); ok {
		c.Set("error_code", apiErr.Code)
		c.JSON(apiErr.Status, gin.H{"error": gin.H{"code": apiErr.Code, "message": apiErr.Message}})
		return
	}
	c.Set("error_code", apierror.ErrInternal.Code)
	c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL", "message": "服务器内部错误"}})
}

func decodeStrictJSON(body io.ReadCloser, dst any) error {
	defer body.Close()

	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing json tokens")
		}
		return err
	}
	return nil
}

func pruneAttempts(attempts []time.Time, now time.Time) []time.Time {
	out := attempts[:0]
	for _, attempt := range attempts {
		if now.Sub(attempt) <= 5*time.Minute {
			out = append(out, attempt)
		}
	}
	return out
}

func deriveStableKey(cfg config.Config, purpose string) []byte {
	mac := hmac.New(sha256.New, []byte("product-collection-form/"+purpose))
	io.WriteString(mac, cfg.AdminPassword)
	io.WriteString(mac, "\n")
	io.WriteString(mac, cfg.DataDir)
	io.WriteString(mac, "\n")
	io.WriteString(mac, cfg.DBPath)
	io.WriteString(mac, "\n")
	io.WriteString(mac, cfg.Listen)
	return mac.Sum(nil)
}

func (s *Service) issueSessionToken(nonce string, expiry time.Time) (string, error) {
	payload := fmt.Sprintf("%d:%s", expiry.Unix(), nonce)
	sig := s.signSessionPayload(payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *Service) parseSessionToken(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return time.Time{}, errors.New("invalid session token")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}
	expected := s.signSessionPayload(string(payloadBytes))
	if subtle.ConstantTimeCompare(expected, signature) != 1 {
		return time.Time{}, errors.New("invalid session signature")
	}
	payloadParts := strings.SplitN(string(payloadBytes), ":", 2)
	if len(payloadParts) != 2 {
		return time.Time{}, errors.New("invalid session payload")
	}
	expiresAtUnix, err := strconv.ParseInt(payloadParts[0], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	expiresAt := time.Unix(expiresAtUnix, 0)
	if time.Now().After(expiresAt) {
		return time.Time{}, errors.New("session expired")
	}
	return expiresAt, nil
}

func (s *Service) signSessionPayload(payload string) []byte {
	mac := hmac.New(sha256.New, s.sessionKey)
	io.WriteString(mac, payload)
	return mac.Sum(nil)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func renameAndSync(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	return storage.SyncDir(filepath.Dir(newPath))
}

func (s *Service) acquireBundleGate(ctx context.Context) (func(), error) {
	if s.limits == nil {
		return func() {}, nil
	}
	return s.limits.Bundle.Acquire(ctx)
}

func (s *Service) acquireYearExportGate(ctx context.Context) (func(), error) {
	if s.limits == nil {
		return func() {}, nil
	}
	return s.limits.YearExport.Acquire(ctx)
}
