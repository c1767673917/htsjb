package invoices

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/apierror"
	"product-collection-form/backend/internal/metrics"
	"product-collection-form/backend/internal/storage"
)

var ErrInvoiceNotFound = apierror.New(404, "INVOICE_NOT_FOUND", "发票不存在")

type Service struct {
	db      *sqlx.DB
	storage *storage.Service
	cacheMu sync.Mutex
	cache   serviceCache
}

type serviceCache struct {
	search map[string]cachedSearch
}

type cachedSearch struct {
	value     []SearchItem
	expiresAt time.Time
}

type SearchItem struct {
	InvoiceNo   string `db:"invoice_no" json:"invoiceNo"`
	Customer    string `db:"customer" json:"customer"`
	InvoiceDate string `db:"invoice_date" json:"invoiceDate"`
	Uploaded    bool   `json:"uploaded"`
	UploadCount int    `json:"uploadCount"`
}

type Line struct {
	Product      string  `db:"product" json:"product"`
	Quantity     float64 `db:"quantity" json:"quantity"`
	Amount       float64 `db:"amount" json:"amount"`
	TaxAmount    float64 `db:"tax_amount" json:"taxAmount"`
	TotalWithTax float64 `db:"total_with_tax" json:"totalWithTax"`
	TaxRate      string  `db:"tax_rate" json:"taxRate"`
}

type UploadFile struct {
	ID          int64     `db:"id" json:"id"`
	Seq         int       `db:"seq" json:"seq"`
	Filename    string    `db:"filename" json:"filename"`
	Size        int64     `db:"byte_size" json:"size"`
	ContentType string    `db:"content_type" json:"contentType"`
	URL         string    `json:"url"`
	Operator    string    `db:"operator" json:"operator"`
	UploadedAt  time.Time `db:"uploaded_at" json:"-"`
}

type Detail struct {
	InvoiceNo   string       `json:"invoiceNo"`
	Customer    string       `json:"customer"`
	Seller      string       `json:"seller"`
	InvoiceDate string       `json:"invoiceDate"`
	Lines       []Line       `json:"lines"`
	Uploads     []UploadFile `json:"uploads"`
}

type AdminListItem struct {
	InvoiceNo    string     `db:"invoice_no" json:"invoiceNo"`
	Customer     string     `db:"customer" json:"customer"`
	Seller       string     `db:"seller" json:"seller"`
	InvoiceDate  string     `db:"invoice_date" json:"invoiceDate"`
	Uploaded     bool       `json:"uploaded"`
	UploadCount  int        `json:"uploadCount"`
	Operators    []string   `json:"operators"`
	LastUploadAt *time.Time `json:"lastUploadAt"`
}

type AdminList struct {
	Page  int             `json:"page"`
	Size  int             `json:"size"`
	Total int             `json:"total"`
	Items []AdminListItem `json:"items"`
}

type searchRow struct {
	InvoiceNo   string `db:"invoice_no"`
	Customer    string `db:"customer"`
	InvoiceDate string `db:"invoice_date"`
	UploadCount int    `db:"upload_count"`
}

type adminListRow struct {
	InvoiceNo      string         `db:"invoice_no"`
	Customer       string         `db:"customer"`
	Seller         string         `db:"seller"`
	InvoiceDate    string         `db:"invoice_date"`
	UploadCount    int            `db:"upload_count"`
	OperatorsRaw   sql.NullString `db:"operators"`
	LastUploadAtRaw sql.NullString `db:"last_upload_at"`
}

func NewService(db *sqlx.DB, storageSvc *storage.Service) *Service {
	return &Service{
		db:      db,
		storage: storageSvc,
		cache: serviceCache{
			search: make(map[string]cachedSearch),
		},
	}
}

func (s *Service) Search(ctx context.Context, q string, limit int) ([]SearchItem, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return nil, apierror.ErrBadRequest
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	cacheKey := fmt.Sprintf("inv:%s:%d", strings.ToUpper(q), limit)
	if cached, ok := s.cachedSearch(cacheKey); ok {
		return cached, nil
	}

	var rows []searchRow
	query := `
SELECT
  i.invoice_no,
  i.customer,
  i.invoice_date,
  COALESCE((SELECT COUNT(*) FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no), 0) AS upload_count
FROM invoices i
WHERE i.invoice_no LIKE '%' || ? || '%' COLLATE NOCASE
ORDER BY i.invoice_no ASC
LIMIT ?`
	if err := s.db.SelectContext(ctx, &rows, query, q, limit); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, fmt.Errorf("search invoices: %w", err)
	}

	items := make([]SearchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, SearchItem{
			InvoiceNo:   row.InvoiceNo,
			Customer:    row.Customer,
			InvoiceDate: row.InvoiceDate,
			Uploaded:    row.UploadCount > 0,
			UploadCount: row.UploadCount,
		})
	}
	s.storeSearch(cacheKey, items)
	return items, nil
}

func (s *Service) Detail(ctx context.Context, invoiceNo string) (Detail, error) {
	invoiceNo = strings.TrimSpace(invoiceNo)
	if invoiceNo == "" || storage.ValidatePathSegment(invoiceNo) != nil {
		return Detail{}, ErrInvoiceNotFound
	}

	var invoice struct {
		InvoiceNo   string `db:"invoice_no"`
		Customer    string `db:"customer"`
		Seller      string `db:"seller"`
		InvoiceDate string `db:"invoice_date"`
	}
	if err := s.db.GetContext(ctx, &invoice, `SELECT invoice_no, customer, seller, invoice_date FROM invoices WHERE invoice_no = ?`, invoiceNo); err != nil {
		if err == sql.ErrNoRows {
			return Detail{}, ErrInvoiceNotFound
		}
		metrics.Default.IncSQLiteErrors()
		return Detail{}, fmt.Errorf("load invoice: %w", err)
	}

	var lines []Line
	if err := s.db.SelectContext(ctx, &lines, `
SELECT product, quantity, amount, tax_amount, total_with_tax, tax_rate
FROM invoice_lines
WHERE invoice_no = ?
ORDER BY id ASC`, invoiceNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return Detail{}, fmt.Errorf("load invoice lines: %w", err)
	}

	var uploadRows []UploadFile
	if err := s.db.SelectContext(ctx, &uploadRows, `
SELECT id, seq, filename, byte_size, content_type, uploaded_at, operator
FROM invoice_uploads
WHERE invoice_no = ?
ORDER BY seq ASC, id ASC`, invoiceNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return Detail{}, fmt.Errorf("load invoice uploads: %w", err)
	}

	uploads := make([]UploadFile, 0, len(uploadRows))
	for _, file := range uploadRows {
		file.URL = fmt.Sprintf("/files/invoices/%s/%s", invoiceNo, file.Filename)
		uploads = append(uploads, file)
	}

	return Detail{
		InvoiceNo:   invoice.InvoiceNo,
		Customer:    invoice.Customer,
		Seller:      invoice.Seller,
		InvoiceDate: invoice.InvoiceDate,
		Lines:       lines,
		Uploads:     uploads,
	}, nil
}

func (s *Service) AdminList(ctx context.Context, page, size int, query string, onlyUploaded bool) (AdminList, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}

	whereSQL, args := buildAdminWhere(query, onlyUploaded)

	var total int
	countQuery := `SELECT COUNT(*) FROM invoices i WHERE ` + whereSQL
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		metrics.Default.IncSQLiteErrors()
		return AdminList{}, err
	}

	offset := (page - 1) * size
	listQuery := `
SELECT
  i.invoice_no,
  i.customer,
  i.seller,
  i.invoice_date,
  COALESCE((SELECT COUNT(*) FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no), 0) AS upload_count,
  COALESCE((SELECT GROUP_CONCAT(NULLIF(u.operator, ''), char(31)) FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no), '') AS operators,
  (SELECT MAX(u.uploaded_at) FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no) AS last_upload_at
FROM invoices i
WHERE ` + whereSQL + `
ORDER BY i.invoice_no ASC
LIMIT ? OFFSET ?`
	listArgs := append(append([]any{}, args...), size, offset)

	var rows []adminListRow
	if err := s.db.SelectContext(ctx, &rows, listQuery, listArgs...); err != nil {
		metrics.Default.IncSQLiteErrors()
		return AdminList{}, err
	}

	items := make([]AdminListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, AdminListItem{
			InvoiceNo:    row.InvoiceNo,
			Customer:     row.Customer,
			Seller:       row.Seller,
			InvoiceDate:  row.InvoiceDate,
			Uploaded:     row.UploadCount > 0,
			UploadCount:  row.UploadCount,
			Operators:    splitOperators(row.OperatorsRaw),
			LastUploadAt: parseSQLiteTime(row.LastUploadAtRaw),
		})
	}
	return AdminList{Page: page, Size: size, Total: total, Items: items}, nil
}

func (s *Service) AdminExportAll(ctx context.Context, query string, onlyUploaded bool) ([]AdminListItem, error) {
	whereSQL, args := buildAdminWhere(query, onlyUploaded)

	sqlStr := `
SELECT
  i.invoice_no,
  i.customer,
  i.seller,
  i.invoice_date,
  COALESCE((SELECT COUNT(*) FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no), 0) AS upload_count,
  COALESCE((SELECT GROUP_CONCAT(NULLIF(u.operator, ''), char(31)) FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no), '') AS operators,
  (SELECT MAX(u.uploaded_at) FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no) AS last_upload_at
FROM invoices i
WHERE ` + whereSQL + `
ORDER BY i.invoice_no ASC`

	var rows []adminListRow
	if err := s.db.SelectContext(ctx, &rows, sqlStr, args...); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, err
	}
	items := make([]AdminListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, AdminListItem{
			InvoiceNo:    row.InvoiceNo,
			Customer:     row.Customer,
			Seller:       row.Seller,
			InvoiceDate:  row.InvoiceDate,
			Uploaded:     row.UploadCount > 0,
			UploadCount:  row.UploadCount,
			Operators:    splitOperators(row.OperatorsRaw),
			LastUploadAt: parseSQLiteTime(row.LastUploadAtRaw),
		})
	}
	return items, nil
}

func buildAdminWhere(query string, onlyUploaded bool) (string, []any) {
	where := []string{"1=1"}
	var args []any
	if onlyUploaded {
		where = append(where, `EXISTS (SELECT 1 FROM invoice_uploads u WHERE u.invoice_no = i.invoice_no)`)
	}
	query = strings.TrimSpace(query)
	if r := []rune(query); len(r) > 64 {
		query = string(r[:64])
	}
	if query != "" {
		where = append(where, `(
  i.invoice_no LIKE '%' || ? || '%' COLLATE NOCASE
  OR i.customer LIKE '%' || ? || '%' COLLATE NOCASE
  OR i.seller LIKE '%' || ? || '%' COLLATE NOCASE
)`)
		args = append(args, query, query, query)
	}
	return strings.Join(where, " AND "), args
}

func splitOperators(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return []string{}
	}
	parts := strings.Split(raw.String, "\x1f")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func parseSQLiteTime(raw sql.NullString) *time.Time {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, raw.String); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

func (s *Service) cachedSearch(key string) ([]SearchItem, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache.search[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	out := make([]SearchItem, len(entry.value))
	copy(out, entry.value)
	return out, true
}

func (s *Service) storeSearch(key string, value []SearchItem) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	out := make([]SearchItem, len(value))
	copy(out, value)
	s.cache.search[key] = cachedSearch{value: out, expiresAt: time.Now().Add(5 * time.Second)}
}
