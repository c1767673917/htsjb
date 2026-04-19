package uploads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"product-collection-form/backend/internal/apierror"
	"product-collection-form/backend/internal/metrics"
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/storage"
)

// DeleteResult mirrors the admin contract: the caller needs to know whether
// the merged PDF could not be rebuilt after the delete so the UI can warn the
// operator that an admin may need to regenerate it.
type DeleteResult struct {
	MergedPDFStale bool `json:"mergedPdfStale"`
}

type renamePlan struct {
	ID       int64
	Original string
	Temp     string
	Final    string
	Seq      int
	Filename string
}

// DeleteUpload removes a single upload and compacts the remaining seq numbers
// for the same kind before rebuilding the merged PDF. The heavy lifting lives
// here (rather than in the admin package) so both admin and the end-user flow
// can share the exact same transactional semantics.
func (s *Service) DeleteUpload(ctx context.Context, year int, orderNo string, uploadID int64) (DeleteResult, error) {
	release, err := s.storage.Acquire(ctx, year, orderNo)
	if err != nil {
		return DeleteResult{}, err
	}
	defer release()

	type uploadRecord struct {
		ID       int64  `db:"id"`
		Kind     string `db:"kind"`
		Seq      int    `db:"seq"`
		Filename string `db:"filename"`
	}
	var target uploadRecord
	if err := s.db.GetContext(ctx, &target, `SELECT id, kind, seq, filename FROM uploads WHERE id = ? AND year = ? AND order_no = ?`, uploadID, year, orderNo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeleteResult{}, apierror.ErrFileNotFound
		}
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "读取上传记录失败")
	}

	customerClean, err := s.orders.CustomerClean(ctx, year, orderNo)
	if err != nil {
		return DeleteResult{}, err
	}
	orderDir, err := s.storage.OrderDir(year, orderNo)
	if err != nil {
		return DeleteResult{}, err
	}
	txID, err := randomHex(8)
	if err != nil {
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建事务标识失败")
	}

	pdfPath := filepath.Join(orderDir, storage.MergedPDFName(orderNo, customerClean))
	bakPath := pdfPath + ".bak-" + txID
	backupTaken := false
	if _, err := os.Stat(pdfPath); err == nil {
		if err := renameAndSync(pdfPath, bakPath); err != nil {
			return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "备份 PDF 失败")
		}
		backupTaken = true
	}

	trashPath := filepath.Join(s.storage.TrashRoot(), fmt.Sprintf("%d-%s-%d-%s.jpg", year, orderNo, uploadID, txID))
	originalPath, err := s.storage.ValidateOrderFilePath(year, orderNo, target.Filename)
	if err != nil {
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return DeleteResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(trashPath), 0o700); err != nil {
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建回收目录失败")
	}
	if err := renameAndSync(originalPath, trashPath); err != nil {
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "移动待删除文件失败")
	}

	var remaining []struct {
		ID       int64  `db:"id"`
		Seq      int    `db:"seq"`
		Filename string `db:"filename"`
	}
	if err := s.db.SelectContext(ctx, &remaining, `
SELECT id, seq, filename FROM uploads
WHERE year = ? AND order_no = ? AND kind = ? AND id <> ?
ORDER BY seq ASC, id ASC`, year, orderNo, target.Kind, uploadID); err != nil {
		s.restoreMovedFile(trashPath, originalPath)
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "查询重排计划失败")
	}

	plans := make([]renamePlan, 0, len(remaining))
	for idx, row := range remaining {
		desiredSeq := idx + 1
		desiredName := fmt.Sprintf("%s-%s-%s-%02d.jpg", orderNo, customerClean, target.Kind, desiredSeq)
		if row.Seq == desiredSeq && row.Filename == desiredName {
			continue
		}
		origPath, err := s.storage.ValidateOrderFilePath(year, orderNo, row.Filename)
		if err != nil {
			s.restoreMovedFile(trashPath, originalPath)
			s.restorePDF(pdfPath, bakPath, backupTaken)
			return DeleteResult{}, err
		}
		plans = append(plans, renamePlan{
			ID:       row.ID,
			Original: origPath,
			Temp:     filepath.Join(orderDir, "."+filepath.Base(origPath)+".rename-"+txID),
			Final:    filepath.Join(orderDir, desiredName),
			Seq:      desiredSeq,
			Filename: desiredName,
		})
	}

	for _, plan := range plans {
		if err := renameAndSync(plan.Original, plan.Temp); err != nil {
			s.rollbackRenamePlans(plans)
			s.restoreMovedFile(trashPath, originalPath)
			s.restorePDF(pdfPath, bakPath, backupTaken)
			return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "第一阶段重命名失败")
		}
	}
	for _, plan := range plans {
		if err := renameAndSync(plan.Temp, plan.Final); err != nil {
			s.rollbackRenamePlans(plans)
			s.restoreMovedFile(trashPath, originalPath)
			s.restorePDF(pdfPath, bakPath, backupTaken)
			return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "第二阶段重命名失败")
		}
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		s.rollbackRenamePlans(plans)
		s.restoreMovedFile(trashPath, originalPath)
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "创建删除事务失败")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM uploads WHERE id = ?`, uploadID); err != nil {
		s.rollbackRenamePlans(plans)
		s.restoreMovedFile(trashPath, originalPath)
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "删除上传记录失败")
	}
	for _, plan := range plans {
		if _, err := tx.ExecContext(ctx, `UPDATE uploads SET seq = ?, filename = ? WHERE id = ?`, plan.Seq, filepath.Base(plan.Final), plan.ID); err != nil {
			s.rollbackRenamePlans(plans)
			s.restoreMovedFile(trashPath, originalPath)
			s.restorePDF(pdfPath, bakPath, backupTaken)
			return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "更新重排结果失败")
		}
	}
	if err := tx.Commit(); err != nil {
		s.rollbackRenamePlans(plans)
		s.restoreMovedFile(trashPath, originalPath)
		s.restorePDF(pdfPath, bakPath, backupTaken)
		return DeleteResult{}, apierror.Wrap(err, http.StatusInternalServerError, "INTERNAL", "提交删除事务失败")
	}

	if _, err := s.RebuildMergedPDF(ctx, year, orderNo); err != nil {
		s.restorePDF(pdfPath, bakPath, backupTaken)
		slog.WarnContext(ctx, "delete upload left merged pdf stale", "year", year, "order_no", orderNo, "upload_id", uploadID, "error", err)
		return DeleteResult{MergedPDFStale: true}, nil
	}
	if backupTaken {
		_ = os.Remove(bakPath)
	}
	go os.Remove(trashPath)
	return DeleteResult{}, nil
}

// HandleUserDelete exposes a user-facing delete endpoint.
func (s *Service) HandleUserDelete(c *gin.Context) {
	year, err := orders.ParseAndValidateYear(c.Param("year"))
	if err != nil {
		writeError(c, err)
		return
	}
	orderNo := strings.TrimSpace(c.Param("orderNo"))
	if orderNo == "" || storage.ValidatePathSegment(orderNo) != nil {
		writeError(c, apierror.ErrOrderNotFound)
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

	result, err := s.DeleteUpload(c.Request.Context(), year, orderNo, uploadID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "mergedPdfStale": result.MergedPDFStale})
}

func (s *Service) restoreMovedFile(from, to string) {
	if _, err := os.Stat(from); err == nil {
		_ = renameAndSync(from, to)
	}
}

func (s *Service) rollbackRenamePlans(plans []renamePlan) {
	for i := len(plans) - 1; i >= 0; i-- {
		plan := plans[i]
		if _, err := os.Stat(plan.Final); err == nil {
			_ = renameAndSync(plan.Final, plan.Original)
			continue
		}
		if _, err := os.Stat(plan.Temp); err == nil {
			_ = renameAndSync(plan.Temp, plan.Original)
		}
	}
}
