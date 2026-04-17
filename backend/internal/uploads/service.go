package uploads

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/pdfmerge"
	"product-collection-form/backend/internal/storage"
)

type PDFBuilder interface {
	Build(ctx context.Context, imagePaths []string, w io.Writer) (int, error)
}

type Hooks struct {
	BeforeCommit func() error
	AfterCommit  func() error
}

type Service struct {
	db      *sqlx.DB
	cfg     config.Config
	orders  *orders.Service
	storage *storage.Service
	pdf     PDFBuilder
	limits  *limits.Manager
	hooks   Hooks

	uploadRateLimits sync.Map
	rateLimitMu      sync.Mutex
	lastRateSweep    time.Time
}

type SubmitResponse struct {
	Counts         orders.Counts   `json:"counts"`
	Progress       orders.Progress `json:"progress"`
	MergedPDFStale bool            `json:"mergedPdfStale"`
}

type stagedFile struct {
	Kind string
	Path string
	MIME string
	Size int64
}

type insertRecord struct {
	ID       int64
	Kind     string
	Seq      int
	Filename string
}

type uploadRateBucket struct {
	mu       sync.Mutex
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewService(db *sqlx.DB, cfg config.Config, orderSvc *orders.Service, storageSvc *storage.Service, pdfSvc PDFBuilder, limiter *limits.Manager) *Service {
	if pdfSvc == nil {
		pdfSvc = pdfmerge.New(limiter)
	}
	return &Service{
		db:      db,
		cfg:     cfg,
		orders:  orderSvc,
		storage: storageSvc,
		pdf:     pdfSvc,
		limits:  limiter,
	}
}

func (s *Service) SetHooks(h Hooks) {
	s.hooks = h
}

func (s *Service) HandleSubmit(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	orderNo := strings.TrimSpace(c.Param("orderNo"))
	if !s.allowUploadAttempt(c.ClientIP()) {
		metrics.Default.IncRateLimited()
		writeError(c, apierror.ErrRateLimited)
		return
	}

	response, err := s.Submit(c, year, orderNo)
	if err != nil {
		writeError(c, err)
		return
	}
	metrics.Default.IncUploads()
	c.JSON(http.StatusOK, response)
}

func (s *Service) Submit(c *gin.Context, year int, orderNo string) (SubmitResponse, error) {
	ctx := c.Request.Context()

	customerClean, err := s.orders.CustomerClean(ctx, year, orderNo)
	if err != nil {
		return SubmitResponse{}, err
	}

	releaseUpload, err := s.acquireUploadGate(ctx)
	if err != nil {
		return SubmitResponse{}, err
	}
	defer releaseUpload()

	releasePDF, err := s.acquirePDFGate(ctx)
	if err != nil {
		return SubmitResponse{}, err
	}
	defer releasePDF()

	release, err := s.storage.Acquire(ctx, year, orderNo)
	if err != nil {
		return SubmitResponse{}, err
	}
	defer release()

	txID, err := randomHex(8)
	if err != nil {
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "生成事务标识失败")
	}

	incomingDir, err := s.storage.IncomingDir(txID)
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
			slog.ErrorContext(ctx, "upload submit panicked", "year", year, "order_no", orderNo, "txid", txID, "panic", recovered)
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

	staged := map[string][]stagedFile{
		"合同":  {},
		"发票":  {},
		"发货单": {},
	}
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
		kind, ok := normalizeField(part.FormName())
		if !ok {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}
		if part.FileName() == "" {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}

		stagePath := filepath.Join(incomingDir, fmt.Sprintf("%s-%02d.bin", kind, len(staged[kind])+1))
		mimeType, size, err := streamPartToFile(ctx, part, stagePath, s.cfg.Image.AcceptedMIME, int64(s.cfg.Limits.SingleFileMaxMB)*1024*1024)
		_ = part.Close()
		if err != nil {
			return SubmitResponse{}, err
		}
		staged[kind] = append(staged[kind], stagedFile{Kind: kind, Path: stagePath, MIME: mimeType, Size: size})
	}

	totalFiles := 0
	for kind, files := range staged {
		totalFiles += len(files)
		if len(files) > s.cfg.Limits.PerKindMax {
			return SubmitResponse{}, apierror.ErrUploadCapExceeded
		}
		_ = kind
	}
	if totalFiles == 0 {
		return SubmitResponse{}, apierror.ErrNoStagedFiles
	}

	orderDir, err := s.storage.OrderDir(year, orderNo)
	if err != nil {
		return SubmitResponse{}, err
	}
	if err := os.MkdirAll(orderDir, 0o700); err != nil {
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建订单目录失败")
	}

	pdfName := storage.MergedPDFName(orderNo, customerClean)
	pdfPath := filepath.Join(orderDir, pdfName)
	bakPath := pdfPath + ".bak-" + txID
	backupTaken := false
	if _, err := os.Stat(pdfPath); err == nil {
		if err := renameAndSync(pdfPath, bakPath); err != nil {
			return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "备份旧 PDF 失败")
		}
		backupTaken = true
	}

	createdPaths := make([]string, 0, totalFiles)
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建事务失败")
	}
	defer tx.Rollback()

	inserted := make([]insertRecord, 0, totalFiles)
	for _, kind := range orders.AllKinds() {
		files := staged[kind]
		if len(files) == 0 {
			continue
		}
		var maxSeq int
		if err := tx.GetContext(ctx, &maxSeq, `SELECT COALESCE(MAX(seq), 0) FROM uploads WHERE year = ? AND order_no = ? AND kind = ?`, year, orderNo, kind); err != nil {
			s.rollbackCreatedFiles(createdPaths)
			s.restorePDF(pdfPath, bakPath, backupTaken)
			return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "查询上传序号失败")
		}

		for idx, file := range files {
			seq := maxSeq + idx + 1
			filename := fmt.Sprintf("%s-%s-%s-%02d.jpg", orderNo, customerClean, kind, seq)
			dstPath := filepath.Join(orderDir, filename)
			finalPath, size, sha, err := s.materializeJPEG(ctx, file, dstPath)
			if err != nil {
				s.rollbackCreatedFiles(createdPaths)
				s.restorePDF(pdfPath, bakPath, backupTaken)
				return SubmitResponse{}, wrapStorageError(err, "保存图片失败")
			}
			createdPaths = append(createdPaths, finalPath)

			result, err := tx.ExecContext(ctx, `
INSERT INTO uploads (year, order_no, kind, seq, filename, byte_size, sha256)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
				year, orderNo, kind, seq, filename, size, sha,
			)
			if err != nil {
				s.rollbackCreatedFiles(createdPaths)
				s.restorePDF(pdfPath, bakPath, backupTaken)
				return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "写入上传记录失败")
			}
			id, _ := result.LastInsertId()
			inserted = append(inserted, insertRecord{ID: id, Kind: kind, Seq: seq, Filename: filename})
		}
	}

	if s.hooks.BeforeCommit != nil {
		if err := s.hooks.BeforeCommit(); err != nil {
			s.rollbackCreatedFiles(createdPaths)
			s.restorePDF(pdfPath, bakPath, backupTaken)
			return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "提交前故障")
		}
	}
	if err := tx.Commit(); err != nil {
		s.rollbackCreatedFiles(createdPaths)
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "提交上传事务失败")
	}

	mergedPDFStale := false
	if s.hooks.AfterCommit != nil {
		if err := s.hooks.AfterCommit(); err != nil {
			mergedPDFStale = true
			s.restorePDF(pdfPath, bakPath, backupTaken)
		}
	}
	if !mergedPDFStale {
		if _, err := s.rebuildMergedPDF(ctx, year, orderNo, true); err != nil {
			mergedPDFStale = true
			s.restorePDF(pdfPath, bakPath, backupTaken)
		} else if backupTaken {
			_ = os.Remove(bakPath)
		}
	}

	counts, err := s.orders.CountsForOrder(ctx, year, orderNo)
	if err != nil {
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取上传统计失败")
	}
	progress, err := s.orders.Progress(ctx, year)
	if err != nil {
		return SubmitResponse{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取年度进度失败")
	}

	_ = inserted
	return SubmitResponse{Counts: counts, Progress: progress, MergedPDFStale: mergedPDFStale}, nil
}

func (s *Service) RebuildMergedPDF(ctx context.Context, year int, orderNo string) (int, error) {
	return s.rebuildMergedPDF(ctx, year, orderNo, false)
}

func (s *Service) rebuildMergedPDF(ctx context.Context, year int, orderNo string, gateHeld bool) (int, error) {
	if !gateHeld {
		release, err := s.acquirePDFGate(ctx)
		if err != nil {
			return 0, err
		}
		defer release()
	}

	customerClean, err := s.orders.CustomerClean(ctx, year, orderNo)
	if err != nil {
		return 0, err
	}
	rows, err := s.orders.UploadRowsByKinds(ctx, year, orderNo, s.cfg.Image.PDFOrder)
	if err != nil {
		return 0, err
	}

	orderDir, err := s.storage.OrderDir(year, orderNo)
	if err != nil {
		return 0, err
	}
	pdfPath := filepath.Join(orderDir, storage.MergedPDFName(orderNo, customerClean))
	if len(rows) == 0 {
		if removeErr := os.Remove(pdfPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return 0, removeErr
		}
		if err := storage.SyncDir(filepath.Dir(pdfPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		return 0, nil
	}

	imagePaths := make([]string, 0, len(rows))
	for _, row := range rows {
		fullPath, err := s.storage.ValidateOrderFilePath(year, orderNo, row.Filename)
		if err != nil {
			return 0, err
		}
		imagePaths = append(imagePaths, fullPath)
	}

	newPath := pdfPath + ".new-" + mustRandomSuffix()
	file, err := os.OpenFile(newPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(newPath)
	}()

	pages, err := s.pdf.Build(ctx, imagePaths, file)
	if err != nil {
		return 0, err
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := renameAndSync(newPath, pdfPath); err != nil {
		return 0, err
	}
	metrics.Default.IncPDFRebuilds()
	return pages, nil
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

func normalizeField(name string) (string, bool) {
	switch name {
	case "contract", "contract[]":
		return "合同", true
	case "invoice", "invoice[]":
		return "发票", true
	case "delivery", "delivery[]":
		return "发货单", true
	default:
		return "", false
	}
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

func mustRandomSuffix() string {
	value, err := randomHex(4)
	if err != nil {
		return "fallback"
	}
	return value
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

func (s *Service) restorePDF(finalPath, bakPath string, backupTaken bool) {
	if !backupTaken {
		return
	}
	if _, err := os.Stat(bakPath); err == nil {
		_ = os.Remove(finalPath)
		_ = renameAndSync(bakPath, finalPath)
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

func (s *Service) acquirePDFGate(ctx context.Context) (func(), error) {
	if s.limits == nil {
		return func() {}, nil
	}
	return s.limits.PDFRebuild.Acquire(ctx)
}

func (s *Service) allowUploadAttempt(ip string) bool {
	now := time.Now()
	s.sweepUploadRateLimits(now)

	value, _ := s.uploadRateLimits.LoadOrStore(ip, &uploadRateBucket{
		limiter:  rate.NewLimiter(rate.Every(time.Minute/20), 20),
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
