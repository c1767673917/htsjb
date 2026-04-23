package invoiceuploads

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	_ "golang.org/x/image/webp"
	"golang.org/x/time/rate"

	"product-collection-form/backend/internal/apierror"
	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/httpapi/limits"
	"product-collection-form/backend/internal/metrics"
	"product-collection-form/backend/internal/storage"
)

type Service struct {
	db      *sqlx.DB
	cfg     config.Config
	storage *storage.Service
	limits  *limits.Manager
	cache   invoiceCacheInvalidator

	uploadRateLimits sync.Map
	rateLimitMu      sync.Mutex
	lastRateSweep    time.Time
}

type invoiceCacheInvalidator interface {
	InvalidateCache()
}

type SubmitResponse struct {
	UploadCount int `json:"uploadCount"`
}

type stagedFile struct {
	Path         string
	MIME         string
	Size         int64
	OriginalName string
}

type uploadPlan struct {
	Seq       int
	Filename  string
	Source    stagedFile
	FinalPath string
	IsPDF     bool
}

type materializedUpload struct {
	Seq          int
	Filename     string
	ByteSize     int64
	SHA256       string
	ContentType  string
	OriginalName string
}

type uploadRateBucket struct {
	mu       sync.Mutex
	limiter  *rate.Limiter
	lastSeen time.Time
}

const perInvoiceUploadCap = 1

var errInvoiceUploadCapExceeded = apierror.New(http.StatusConflict, "INVOICE_UPLOAD_CAP_EXCEEDED", "发票录入专区最多上传 1 个文件")

func NewService(db *sqlx.DB, cfg config.Config, storageSvc *storage.Service, limiter *limits.Manager, cache invoiceCacheInvalidator) *Service {
	return &Service{
		db:      db,
		cfg:     cfg,
		storage: storageSvc,
		limits:  limiter,
		cache:   cache,
	}
}

func (s *Service) HandleSubmit(c *gin.Context) {
	invoiceNo := strings.TrimSpace(c.Param("invoiceNo"))
	if invoiceNo == "" || storage.ValidatePathSegment(invoiceNo) != nil {
		writeError(c, apierror.New(404, "INVOICE_NOT_FOUND", "发票不存在"))
		return
	}

	if !s.allowUploadAttempt(c.ClientIP()) {
		metrics.Default.IncRateLimited()
		writeError(c, apierror.ErrRateLimited)
		return
	}

	response, err := s.submit(c, invoiceNo)
	if err != nil {
		writeError(c, err)
		return
	}
	metrics.Default.IncUploads()
	c.JSON(http.StatusOK, response)
}

func (s *Service) submit(c *gin.Context, invoiceNo string) (SubmitResponse, error) {
	ctx := c.Request.Context()

	// Verify invoice exists
	var exists int
	if err := s.db.GetContext(ctx, &exists, `SELECT COUNT(*) FROM invoices WHERE invoice_no = ? AND csv_present = 1`, invoiceNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "查询发票失败")
	}
	if exists == 0 {
		return SubmitResponse{}, apierror.New(404, "INVOICE_NOT_FOUND", "发票不存在")
	}

	releaseUpload, err := s.acquireUploadGate(ctx)
	if err != nil {
		return SubmitResponse{}, err
	}
	defer releaseUpload()

	release, err := s.storage.AcquireInvoice(ctx, invoiceNo)
	if err != nil {
		return SubmitResponse{}, err
	}
	defer release()

	txID, err := randomHex(8)
	if err != nil {
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "生成事务标识失败")
	}

	incomingDir, err := s.storage.InvoiceIncomingDir(txID)
	if err != nil {
		return SubmitResponse{}, err
	}
	if err := os.MkdirAll(incomingDir, 0o700); err != nil {
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建临时目录失败")
	}
	defer os.RemoveAll(incomingDir)
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = os.RemoveAll(incomingDir)
			slog.ErrorContext(ctx, "invoice upload submit panicked", "invoice_no", invoiceNo, "txid", txID, "panic", recovered)
			panic(recovered)
		}
	}()

	limitedBody := http.MaxBytesReader(c.Writer, c.Request.Body, int64(s.cfg.Limits.SubmitBodyMaxMB)*1024*1024)
	c.Request.Body = limitedBody

	reader, err := c.Request.MultipartReader()
	if err != nil {
		if isMaxBytesError(err) {
			return SubmitResponse{}, apierror.ErrRequestTooLarge
		}
		return SubmitResponse{}, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "无法解析上传请求")
	}

	var stagedImages []stagedFile
	var stagedPDFs []stagedFile
	operator := ""

	acceptedImage := s.cfg.Image.AcceptedMIME
	maxFileBytes := int64(s.cfg.Limits.SingleFileMaxMB) * 1024 * 1024

	for {
		part, err := reader.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if isMaxBytesError(err) {
				return SubmitResponse{}, apierror.ErrRequestTooLarge
			}
			return SubmitResponse{}, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "读取上传文件失败")
		}
		if part.FormName() == "operator" && part.FileName() == "" {
			buf, err := io.ReadAll(io.LimitReader(part, 128))
			_ = part.Close()
			if err != nil {
				if isMaxBytesError(err) {
					return SubmitResponse{}, apierror.ErrRequestTooLarge
				}
				return SubmitResponse{}, apierror.Wrap(err, http.StatusBadRequest, "BAD_REQUEST", "读取录入人字段失败")
			}
			operator = sanitizeOperator(string(buf))
			continue
		}

		fieldName := part.FormName()
		isPDFField := fieldName == "invoice_pdf" || fieldName == "invoice_pdf[]"
		isPhotoField := fieldName == "invoice_photo" || fieldName == "invoice_photo[]"

		if !isPDFField && !isPhotoField {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}
		if part.FileName() == "" {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}

		originalName := part.FileName()

		if isPDFField {
			stagePath := filepath.Join(incomingDir, fmt.Sprintf("pdf-%02d.bin", len(stagedPDFs)+1))
			mimeType, size, err := streamPartToFile(ctx, part, stagePath, []string{"application/pdf"}, maxFileBytes)
			_ = part.Close()
			if err != nil {
				return SubmitResponse{}, err
			}
			stagedPDFs = append(stagedPDFs, stagedFile{Path: stagePath, MIME: mimeType, Size: size, OriginalName: originalName})
		} else {
			stagePath := filepath.Join(incomingDir, fmt.Sprintf("img-%02d.bin", len(stagedImages)+1))
			mimeType, size, err := streamPartToFile(ctx, part, stagePath, acceptedImage, maxFileBytes)
			_ = part.Close()
			if err != nil {
				return SubmitResponse{}, err
			}
			stagedImages = append(stagedImages, stagedFile{Path: stagePath, MIME: mimeType, Size: size, OriginalName: originalName})
		}
	}

	totalFiles := len(stagedImages) + len(stagedPDFs)
	if totalFiles == 0 {
		return SubmitResponse{}, apierror.ErrNoStagedFiles
	}
	if totalFiles > perInvoiceUploadCap {
		return SubmitResponse{}, errInvoiceUploadCapExceeded
	}

	invoiceDir, err := s.storage.InvoiceDir(invoiceNo)
	if err != nil {
		return SubmitResponse{}, err
	}
	if err := os.MkdirAll(invoiceDir, 0o700); err != nil {
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建发票目录失败")
	}

	var existingCount int
	if err := s.db.GetContext(ctx, &existingCount, `SELECT COUNT(*) FROM invoice_uploads WHERE invoice_no = ?`, invoiceNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "查询上传数量失败")
	}
	if existingCount+totalFiles > perInvoiceUploadCap {
		return SubmitResponse{}, errInvoiceUploadCapExceeded
	}

	// Get max seq
	var maxSeq int
	if err := s.db.GetContext(ctx, &maxSeq, `SELECT COALESCE(MAX(seq), 0) FROM invoice_uploads WHERE invoice_no = ?`, invoiceNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "查询上传序号失败")
	}

	plans := make([]uploadPlan, 0, totalFiles)
	seq := maxSeq

	// Plan images first
	for _, file := range stagedImages {
		seq++
		filename := fmt.Sprintf("%s-%02d.jpg", invoiceNo, seq)
		plans = append(plans, uploadPlan{
			Seq:       seq,
			Filename:  filename,
			Source:    file,
			FinalPath: filepath.Join(invoiceDir, filename),
			IsPDF:     false,
		})
	}
	// Plan PDFs
	for _, file := range stagedPDFs {
		seq++
		filename := fmt.Sprintf("%s-%02d.pdf", invoiceNo, seq)
		plans = append(plans, uploadPlan{
			Seq:       seq,
			Filename:  filename,
			Source:    file,
			FinalPath: filepath.Join(invoiceDir, filename),
			IsPDF:     true,
		})
	}

	createdPaths := make([]string, 0, len(plans))
	materialized := make([]materializedUpload, 0, len(plans))
	for _, plan := range plans {
		if plan.IsPDF {
			finalPath, size, sha, err := s.materializePDF(plan.Source, plan.FinalPath)
			if err != nil {
				s.rollbackCreatedFiles(createdPaths)
				return SubmitResponse{}, wrapStorageError(err, "保存PDF失败")
			}
			createdPaths = append(createdPaths, finalPath)
			materialized = append(materialized, materializedUpload{
				Seq:          plan.Seq,
				Filename:     plan.Filename,
				ByteSize:     size,
				SHA256:       sha,
				ContentType:  "application/pdf",
				OriginalName: plan.Source.OriginalName,
			})
		} else {
			finalPath, size, sha, err := s.materializeJPEG(ctx, plan.Source, plan.FinalPath)
			if err != nil {
				s.rollbackCreatedFiles(createdPaths)
				return SubmitResponse{}, wrapStorageError(err, "保存图片失败")
			}
			createdPaths = append(createdPaths, finalPath)
			materialized = append(materialized, materializedUpload{
				Seq:          plan.Seq,
				Filename:     plan.Filename,
				ByteSize:     size,
				SHA256:       sha,
				ContentType:  "image/jpeg",
				OriginalName: plan.Source.OriginalName,
			})
		}
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		s.rollbackCreatedFiles(createdPaths)
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建事务失败")
	}
	defer tx.Rollback()

	for _, upload := range materialized {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO invoice_uploads (invoice_no, seq, filename, original_name, content_type, byte_size, sha256, operator)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			invoiceNo, upload.Seq, upload.Filename, upload.OriginalName, upload.ContentType, upload.ByteSize, upload.SHA256, operator,
		); err != nil {
			s.rollbackCreatedFiles(createdPaths)
			return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "写入上传记录失败")
		}
	}

	if err := tx.Commit(); err != nil {
		s.rollbackCreatedFiles(createdPaths)
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "提交上传事务失败")
	}
	s.invalidateCache()

	var uploadCount int
	if err := s.db.GetContext(ctx, &uploadCount, `SELECT COUNT(*) FROM invoice_uploads WHERE invoice_no = ?`, invoiceNo); err != nil {
		uploadCount = totalFiles // fallback
	}

	return SubmitResponse{UploadCount: uploadCount}, nil
}

func (s *Service) HandleDelete(c *gin.Context) {
	invoiceNo := strings.TrimSpace(c.Param("invoiceNo"))
	if invoiceNo == "" || storage.ValidatePathSegment(invoiceNo) != nil {
		writeError(c, apierror.New(404, "INVOICE_NOT_FOUND", "发票不存在"))
		return
	}
	uploadID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, apierror.ErrBadRequest)
		return
	}

	if !s.allowUploadAttempt(c.ClientIP()) {
		metrics.Default.IncRateLimited()
		writeError(c, apierror.ErrRateLimited)
		return
	}

	if err := s.deleteUpload(c.Request.Context(), invoiceNo, uploadID); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) DeleteUpload(ctx context.Context, invoiceNo string, uploadID int64) error {
	return s.deleteUpload(ctx, invoiceNo, uploadID)
}

func (s *Service) deleteUpload(ctx context.Context, invoiceNo string, uploadID int64) error {
	release, err := s.storage.AcquireInvoice(ctx, invoiceNo)
	if err != nil {
		return err
	}
	defer release()

	var target struct {
		ID       int64  `db:"id"`
		Seq      int    `db:"seq"`
		Filename string `db:"filename"`
	}
	if err := s.db.GetContext(ctx, &target, `SELECT id, seq, filename FROM invoice_uploads WHERE id = ? AND invoice_no = ?`, uploadID, invoiceNo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apierror.ErrFileNotFound
		}
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取上传记录失败")
	}

	fullPath, err := s.storage.ValidateInvoiceFilePath(invoiceNo, target.Filename)
	if err != nil {
		return err
	}

	trashDir := s.storage.TrashRoot()
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建回收目录失败")
	}
	txID, err := randomHex(8)
	if err != nil {
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建事务标识失败")
	}
	trashPath := filepath.Join(trashDir, fmt.Sprintf("inv-%s-%d-%s", invoiceNo, uploadID, txID))
	if err := renameAndSync(fullPath, trashPath); err != nil {
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "移动待删除文件失败")
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		_ = renameAndSync(trashPath, fullPath)
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建删除事务失败")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM invoice_uploads WHERE id = ?`, uploadID); err != nil {
		_ = renameAndSync(trashPath, fullPath)
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "删除上传记录失败")
	}
	if err := tx.Commit(); err != nil {
		_ = renameAndSync(trashPath, fullPath)
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "提交删除事务失败")
	}
	s.invalidateCache()

	go os.Remove(trashPath)
	return nil
}

func (s *Service) ResetInvoice(ctx context.Context, invoiceNo string) error {
	release, err := s.storage.AcquireInvoice(ctx, invoiceNo)
	if err != nil {
		return err
	}
	defer release()

	invoiceDir, err := s.storage.InvoiceDir(invoiceNo)
	if err != nil {
		return err
	}
	txID, err := randomHex(8)
	if err != nil {
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建事务标识失败")
	}
	trashDir := filepath.Join(s.storage.TrashRoot(), fmt.Sprintf("inv-%s-%s", invoiceNo, txID))
	dirMoved := false
	if _, err := os.Stat(invoiceDir); err == nil {
		if err := renameAndSync(invoiceDir, trashDir); err != nil {
			return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "移动发票目录失败")
		}
		dirMoved = true
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		if dirMoved {
			_ = renameAndSync(trashDir, invoiceDir)
		}
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建重置事务失败")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM invoice_uploads WHERE invoice_no = ?`, invoiceNo); err != nil {
		if dirMoved {
			_ = renameAndSync(trashDir, invoiceDir)
		}
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "删除上传记录失败")
	}
	if err := tx.Commit(); err != nil {
		if dirMoved {
			_ = renameAndSync(trashDir, invoiceDir)
		}
		return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "提交重置事务失败")
	}
	s.invalidateCache()
	if dirMoved {
		go os.RemoveAll(trashDir)
	}
	return nil
}

func (s *Service) invalidateCache() {
	if s.cache != nil {
		s.cache.InvalidateCache()
	}
}

func (s *Service) materializeJPEG(ctx context.Context, file stagedFile, dstPath string) (string, int64, string, error) {
	tmpPath := dstPath + ".tmp"
	src, err := os.Open(file.Path)
	if err != nil {
		return "", 0, "", err
	}
	defer src.Close()

	if file.Size > int64(s.cfg.Limits.SingleFileDecodeCapMB)*1024*1024 {
		return "", 0, "", apierror.Wrap(nil, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "图片超过服务端解码上限")
	}

	if s.limits != nil {
		release, err := s.limits.ImageDecode.Acquire(ctx)
		if err != nil {
			return "", 0, "", err
		}
		defer release()
	}

	cfg, _, err := image.DecodeConfig(src)
	if err != nil {
		return "", 0, "", apierror.ErrUnsupportedMediaType
	}
	if int64(cfg.Width)*int64(cfg.Height) > int64(s.cfg.Limits.MaxPixels) {
		return "", 0, "", apierror.Wrap(nil, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "图片像素超出服务端限制")
	}
	if _, err := src.Seek(0, 0); err != nil {
		return "", 0, "", err
	}

	img, _, err := image.Decode(src)
	if err != nil {
		return "", 0, "", apierror.ErrUnsupportedMediaType
	}

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, "", err
	}
	defer func() {
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	counter := &countingWriter{}
	writer := io.MultiWriter(out, hash, counter)
	if err := jpeg.Encode(writer, img, &jpeg.Options{Quality: 85}); err != nil {
		return "", 0, "", err
	}
	if err := out.Sync(); err != nil {
		return "", 0, "", err
	}
	if err := out.Close(); err != nil {
		return "", 0, "", err
	}

	if err := renameAndSync(tmpPath, dstPath); err != nil {
		return "", 0, "", err
	}
	return dstPath, counter.n, hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Service) materializePDF(file stagedFile, dstPath string) (string, int64, string, error) {
	tmpPath := dstPath + ".tmp"
	src, err := os.Open(file.Path)
	if err != nil {
		return "", 0, "", err
	}
	defer src.Close()

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, "", err
	}
	defer func() {
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	counter := &countingWriter{}
	writer := io.MultiWriter(out, hash, counter)
	if _, err := io.Copy(writer, src); err != nil {
		return "", 0, "", err
	}
	if err := out.Sync(); err != nil {
		return "", 0, "", err
	}
	if err := out.Close(); err != nil {
		return "", 0, "", err
	}

	if err := renameAndSync(tmpPath, dstPath); err != nil {
		return "", 0, "", err
	}
	return dstPath, counter.n, hex.EncodeToString(hash.Sum(nil)), nil
}

func streamPartToFile(ctx context.Context, part *multipart.Part, dstPath string, accepted []string, maxBytes int64) (mimeType string, size int64, err error) {
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, err
	}
	defer func() {
		_ = dst.Close()
		if err != nil {
			_ = os.Remove(dstPath)
		}
	}()

	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: part}, N: maxBytes + 1}

	sniff := make([]byte, 512)
	n, err := io.ReadFull(limited, sniff)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		if isMaxBytesError(err) {
			return "", 0, apierror.ErrRequestTooLarge
		}
		return "", 0, err
	}
	mimeType = http.DetectContentType(sniff[:n])
	if !contains(accepted, mimeType) {
		return "", 0, apierror.ErrUnsupportedMediaType
	}

	if _, err := dst.Write(sniff[:n]); err != nil {
		return "", 0, err
	}
	copied, err := io.Copy(dst, limited)
	if err != nil {
		if isMaxBytesError(err) {
			return "", 0, apierror.ErrRequestTooLarge
		}
		return "", 0, err
	}
	size = int64(n) + copied
	if size > maxBytes {
		return "", 0, apierror.ErrRequestTooLarge
	}
	return mimeType, size, nil
}

func sanitizeOperator(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0 || r == '\n' || r == '\r' || r == '\t' || r == 0x1f {
			continue
		}
		b.WriteRune(r)
	}
	result := b.String()
	if len([]rune(result)) > 32 {
		runes := []rune(result)
		result = string(runes[:32])
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isMaxBytesError(err error) bool {
	var target *http.MaxBytesError
	return errors.As(err, &target)
}

func (s *Service) rollbackCreatedFiles(paths []string) {
	for i := len(paths) - 1; i >= 0; i-- {
		_ = os.Remove(paths[i])
	}
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

type countingWriter struct {
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
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

func wrapStorageError(err error, message string) error {
	if err == nil {
		return nil
	}
	if apiErr, ok := apierror.As(err); ok {
		return apiErr
	}
	return apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", message)
}

func (s *Service) acquireUploadGate(ctx context.Context) (func(), error) {
	if s.limits == nil {
		return func() {}, nil
	}
	return s.limits.Upload.Acquire(ctx)
}

func (s *Service) allowUploadAttempt(ip string) bool {
	now := time.Now()
	s.sweepUploadRateLimits(now)

	value, _ := s.uploadRateLimits.LoadOrStore(ip, &uploadRateBucket{
		limiter:  rate.NewLimiter(rate.Every(time.Minute/600), 600),
		lastSeen: now,
	})
	bucket := value.(*uploadRateBucket)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	bucket.lastSeen = now
	return bucket.limiter.Allow()
}

func (s *Service) sweepUploadRateLimits(now time.Time) {
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()
	if !s.lastRateSweep.IsZero() && now.Sub(s.lastRateSweep) < time.Minute {
		return
	}
	s.lastRateSweep = now
	s.uploadRateLimits.Range(func(key, value any) bool {
		bucket := value.(*uploadRateBucket)
		bucket.mu.Lock()
		lastSeen := bucket.lastSeen
		bucket.mu.Unlock()
		if now.Sub(lastSeen) > 10*time.Minute {
			s.uploadRateLimits.Delete(key)
		}
		return true
	})
}
