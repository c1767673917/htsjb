package storage

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"product-collection-form/backend/internal/apierror"
)

var illegalFilenameChars = regexp.MustCompile(`[\\/:*?"<>|\s]+`)
var illegalPathSegmentChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

type Service struct {
	dataDir     string
	locks       sync.Map
	lockTimeout time.Duration
}

func New(dataDir string) *Service {
	return &Service{
		dataDir:     filepath.Clean(dataDir),
		lockTimeout: 30 * time.Second,
	}
}

func (s *Service) SetLockTimeout(timeout time.Duration) {
	if timeout > 0 {
		s.lockTimeout = timeout
	}
}

func (s *Service) DataDir() string {
	return s.dataDir
}

func (s *Service) UploadsRoot() string {
	return filepath.Join(s.dataDir, "uploads")
}

func (s *Service) IncomingRoot() string {
	return filepath.Join(s.UploadsRoot(), ".incoming")
}

func (s *Service) InvoiceUploadsRoot() string {
	return filepath.Join(s.UploadsRoot(), "invoices")
}

func (s *Service) InvoiceIncomingRoot() string {
	return filepath.Join(s.InvoiceUploadsRoot(), ".incoming")
}

func (s *Service) TrashRoot() string {
	return filepath.Join(s.UploadsRoot(), ".trash")
}

func (s *Service) EnsureLayout() error {
	for _, dir := range []string{s.dataDir, s.UploadsRoot(), s.IncomingRoot(), s.TrashRoot(), s.InvoiceUploadsRoot(), s.InvoiceIncomingRoot(), filepath.Join(s.dataDir, "exports")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Acquire(ctx context.Context, year int, orderNo string) (func(), error) {
	key := lockKey(year, orderNo)
	actual, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	mu := actual.(*sync.Mutex)

	deadline := time.Now().Add(s.lockTimeout)
	for {
		if mu.TryLock() {
			return mu.Unlock, nil
		}
		if time.Now().After(deadline) {
			return nil, apierror.ErrOrderLocked
		}
		select {
		case <-ctx.Done():
			return nil, apierror.ErrOrderLocked
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *Service) OrderDir(year int, orderNo string) (string, error) {
	if err := ValidatePathSegment(orderNo); err != nil {
		return "", apierror.ErrFileNotFound
	}
	return s.ensureWithinUploads(filepath.Join(s.UploadsRoot(), YearDir(year), orderNo))
}

func (s *Service) ValidateOrderFilePath(year int, orderNo, filename string) (string, error) {
	if err := ValidatePathSegment(orderNo); err != nil {
		return "", apierror.ErrFileNotFound
	}
	if err := ValidatePathSegment(filename); err != nil {
		return "", apierror.ErrFileNotFound
	}
	fullPath, err := s.ensureWithinUploads(filepath.Join(s.UploadsRoot(), YearDir(year), orderNo, filename))
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", apierror.ErrFileNotFound
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", apierror.Wrap(err, 500, "INTERNAL", "校验文件路径失败")
	}
	return fullPath, nil
}

func (s *Service) InvoiceDir(invoiceNo string) (string, error) {
	if err := ValidatePathSegment(invoiceNo); err != nil {
		return "", apierror.ErrFileNotFound
	}
	return s.ensureWithinUploads(filepath.Join(s.InvoiceUploadsRoot(), invoiceNo))
}

func (s *Service) ValidateInvoiceFilePath(invoiceNo, filename string) (string, error) {
	if err := ValidatePathSegment(invoiceNo); err != nil {
		return "", apierror.ErrFileNotFound
	}
	if err := ValidatePathSegment(filename); err != nil {
		return "", apierror.ErrFileNotFound
	}
	fullPath, err := s.ensureWithinUploads(filepath.Join(s.InvoiceUploadsRoot(), invoiceNo, filename))
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", apierror.ErrFileNotFound
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", apierror.Wrap(err, 500, "INTERNAL", "校验文件路径失败")
	}
	return fullPath, nil
}

func (s *Service) AcquireInvoice(ctx context.Context, invoiceNo string) (func(), error) {
	key := "inv:" + invoiceNo
	actual, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	mu := actual.(*sync.Mutex)

	deadline := time.Now().Add(s.lockTimeout)
	for {
		if mu.TryLock() {
			return mu.Unlock, nil
		}
		if time.Now().After(deadline) {
			return nil, apierror.ErrOrderLocked
		}
		select {
		case <-ctx.Done():
			return nil, apierror.ErrOrderLocked
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *Service) InvoiceIncomingDir(txID string) (string, error) {
	return s.ensureWithinUploads(filepath.Join(s.InvoiceIncomingRoot(), txID))
}

func (s *Service) IncomingDir(txID string) (string, error) {
	return s.ensureWithinUploads(filepath.Join(s.IncomingRoot(), txID))
}

func (s *Service) TrashDir(txID string) (string, error) {
	return s.ensureWithinUploads(filepath.Join(s.TrashRoot(), txID))
}

func (s *Service) RunJanitor(now time.Time) error {
	if err := s.removeOldEntries(s.IncomingRoot(), now, time.Hour); err != nil {
		return err
	}
	if err := s.removeOldEntries(s.TrashRoot(), now, time.Hour); err != nil {
		return err
	}
	return filepath.WalkDir(s.UploadsRoot(), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.Contains(name, ".bak-") || strings.Contains(name, ".new-") || strings.Contains(name, ".rename-") {
			return os.Remove(path)
		}
		return nil
	})
}

func (s *Service) removeOldEntries(root string, now time.Time, maxAge time.Duration) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) < maxAge {
			continue
		}
		target := filepath.Join(root, entry.Name())
		if entry.IsDir() {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(target); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureWithinUploads(path string) (string, error) {
	root := filepath.Clean(s.UploadsRoot())
	clean := filepath.Clean(path)
	prefix := root + string(os.PathSeparator)
	if clean != root && !strings.HasPrefix(clean, prefix) {
		return "", apierror.ErrFileNotFound
	}
	return clean, nil
}

func SanitizeCustomerName(name string) string {
	clean := strings.Trim(illegalFilenameChars.ReplaceAllString(strings.TrimSpace(name), "_"), "_")
	if clean == "" {
		return "未知客户"
	}
	return clean
}

// SanitizeOrderNo replaces filesystem-unsafe characters in an 单据编号 with
// "_" so the normalised form is safe to use as a directory segment. Returns
// empty string if the result has no printable characters left.
func SanitizeOrderNo(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ".")
	trimmed = strings.TrimLeft(trimmed, ".")
	return strings.Trim(illegalFilenameChars.ReplaceAllString(trimmed, "_"), "_")
}

func YearDir(year int) string {
	return strconv.Itoa(year)
}

func MergedPDFName(orderNo, customerClean string) string {
	return orderNo + "-" + customerClean + "-合同与发票.pdf"
}

func ValidatePathSegment(segment string) error {
	if segment == "" {
		return apierror.ErrFileNotFound
	}
	candidate := segment
	if decoded, err := url.PathUnescape(segment); err == nil {
		candidate = decoded
	}
	if hasIllegalPathSegment(segment) || hasIllegalPathSegment(candidate) {
		return apierror.ErrFileNotFound
	}
	return nil
}

func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func lockKey(year int, orderNo string) string {
	return YearDir(year) + ":" + orderNo
}

func hasIllegalPathSegment(value string) bool {
	if value == "" ||
		strings.HasPrefix(value, ".") ||
		strings.Contains(value, "/") ||
		strings.Contains(value, "\\") ||
		filepath.IsAbs(value) ||
		filepath.VolumeName(value) != "" {
		return true
	}
	return illegalPathSegmentChars.MatchString(value)
}
