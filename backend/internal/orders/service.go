package orders

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/apierror"
	"product-collection-form/backend/internal/metrics"
	"product-collection-form/backend/internal/storage"
)

var validYears = map[int]struct{}{
	2021: {},
	2022: {},
	2023: {},
	2024: {},
	2025: {},
}

var kinds = []string{"合同", "发票", "发货单"}

type Service struct {
	db      *sqlx.DB
	storage *storage.Service
	cacheMu sync.Mutex
	cache   serviceCache
}

type serviceCache struct {
	progress   map[int]cachedProgress
	search     map[string]cachedSearch
	adminYears cachedAdminYears
}

type cachedProgress struct {
	value     Progress
	expiresAt time.Time
}

type cachedSearch struct {
	value     []SearchItem
	expiresAt time.Time
}

type cachedAdminYears struct {
	value     []AdminYear
	expiresAt time.Time
}

type Counts map[string]int

type Progress struct {
	Total    int     `db:"total" json:"total"`
	Uploaded int     `db:"uploaded" json:"uploaded"`
	Percent  float64 `json:"percent"`
}

type SearchItem struct {
	OrderNo    string `db:"order_no" json:"orderNo"`
	Customer   string `db:"customer" json:"customer"`
	CSVPresent bool   `db:"csv_present" json:"csvPresent"`
	Uploaded   bool   `json:"uploaded"`
	Counts     Counts `json:"counts"`
}

type Line struct {
	OrderNo      string  `db:"order_no" json:"orderNo"`
	Customer     string  `db:"customer" json:"customer"`
	Date         string  `db:"order_date" json:"date"`
	Product      string  `db:"product" json:"product"`
	Quantity     float64 `db:"quantity" json:"quantity"`
	Amount       float64 `db:"amount" json:"amount"`
	TotalWithTax float64 `db:"total_with_tax" json:"totalWithTax"`
	TaxRate      float64 `db:"tax_rate" json:"taxRate"`
	InvoiceNo    string  `db:"invoice_no" json:"invoiceNo"`
}

type UploadFile struct {
	ID         int64     `db:"id" json:"id"`
	Seq        int       `db:"seq" json:"seq"`
	Filename   string    `db:"filename" json:"filename"`
	Size       int64     `db:"byte_size" json:"size"`
	URL        string    `json:"url"`
	Operator   string    `db:"operator" json:"operator"`
	Kind       string    `db:"kind" json:"-"`
	UploadedAt time.Time `db:"uploaded_at" json:"-"`
}

type Detail struct {
	OrderNo     string                  `json:"orderNo"`
	Year        int                     `json:"year"`
	Customer    string                  `json:"customer"`
	CSVPresent  bool                    `json:"csvPresent"`
	CheckStatus string                  `json:"checkStatus"`
	Lines       []Line                  `json:"lines"`
	Uploads     map[string][]UploadFile `json:"uploads"`
}

var validCheckStatuses = map[string]struct{}{
	"未检查": {},
	"已检查": {},
	"错误":  {},
}

func ValidCheckStatus(status string) bool {
	_, ok := validCheckStatuses[status]
	return ok
}

type AdminYear struct {
	Year       int `db:"year" json:"year"`
	Total      int `db:"total" json:"total"`
	Uploaded   int `db:"uploaded" json:"uploaded"`
	CSVRemoved int `db:"csv_removed" json:"csvRemoved"`
}

type AdminListItem struct {
	OrderNo      string     `db:"order_no" json:"orderNo"`
	Customer     string     `db:"customer" json:"customer"`
	CSVRemoved   bool       `db:"csv_removed" json:"csvRemoved"`
	CheckStatus  string     `db:"check_status" json:"checkStatus"`
	LastUploadAt *time.Time `db:"last_upload_at" json:"lastUploadAt"`
	Uploaded     bool       `json:"uploaded"`
	Counts       Counts     `json:"counts"`
	Operators    []string   `json:"operators"`
}

type AdminList struct {
	Page  int             `json:"page"`
	Size  int             `json:"size"`
	Total int             `json:"total"`
	Items []AdminListItem `json:"items"`
}

type UploadRow struct {
	ID       int64  `db:"id"`
	Year     int    `db:"year"`
	OrderNo  string `db:"order_no"`
	Kind     string `db:"kind"`
	Seq      int    `db:"seq"`
	Filename string `db:"filename"`
}

type adminListRow struct {
	OrderNo         string         `db:"order_no"`
	Customer        string         `db:"customer"`
	CSVRemoved      bool           `db:"csv_removed"`
	CheckStatus     string         `db:"check_status"`
	LastUploadAtRaw sql.NullString `db:"last_upload_at"`
	ContractCnt     int            `db:"contract_cnt"`
	InvoiceCnt      int            `db:"invoice_cnt"`
	DeliveryCnt     int            `db:"delivery_cnt"`
	OperatorsRaw    sql.NullString `db:"operators"`
}

type searchRow struct {
	OrderNo    string `db:"order_no"`
	Customer   string `db:"customer"`
	CSVPresent bool   `db:"csv_present"`
}

func NewService(db *sqlx.DB, storageSvc *storage.Service) *Service {
	return &Service{
		db:      db,
		storage: storageSvc,
		cache: serviceCache{
			progress: make(map[int]cachedProgress),
			search:   make(map[string]cachedSearch),
		},
	}
}

func ValidYear(year int) bool {
	_, ok := validYears[year]
	return ok
}

func ParseAndValidateYear(value string) (int, error) {
	year, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || !ValidYear(year) {
		return 0, apierror.ErrYearNotFound
	}
	return year, nil
}

func EmptyCounts() Counts {
	return Counts{"合同": 0, "发票": 0, "发货单": 0}
}

func (s *Service) Progress(ctx context.Context, year int) (Progress, error) {
	if !ValidYear(year) {
		return Progress{}, apierror.ErrYearNotFound
	}
	if cached, ok := s.cachedProgress(year); ok {
		return cached, nil
	}

	var result struct {
		Total    int `db:"total"`
		Uploaded int `db:"uploaded"`
	}
	query := `
SELECT
  COUNT(*) AS total,
  SUM(
    CASE WHEN EXISTS (
      SELECT 1 FROM uploads u WHERE u.year = o.year AND u.order_no = o.order_no
    ) THEN 1 ELSE 0 END
  ) AS uploaded
FROM orders o
WHERE o.year = ? AND o.csv_present = 1`
	if err := s.db.GetContext(ctx, &result, query, year); err != nil {
		metrics.Default.IncSQLiteErrors()
		return Progress{}, fmt.Errorf("query progress: %w", err)
	}

	progress := Progress{Total: result.Total, Uploaded: result.Uploaded}
	if progress.Total > 0 {
		progress.Percent = float64(progress.Uploaded) / float64(progress.Total)
	}
	s.storeProgress(year, progress)
	return progress, nil
}

func (s *Service) Search(ctx context.Context, year int, q string, limit int) ([]SearchItem, error) {
	if !ValidYear(year) {
		return nil, apierror.ErrYearNotFound
	}
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
	cacheKey := fmt.Sprintf("%d:%s:%d", year, strings.ToUpper(q), limit)
	if cached, ok := s.cachedSearch(cacheKey); ok {
		return cached, nil
	}

	var rows []searchRow
	query := `
SELECT
  o.order_no,
  o.customer,
  o.csv_present
FROM orders o
WHERE o.year = ? AND o.order_no LIKE '%' || ? || '%' COLLATE NOCASE
ORDER BY o.order_no ASC
LIMIT ?`
	if err := s.db.SelectContext(ctx, &rows, query, year, q, limit); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, fmt.Errorf("search orders: %w", err)
	}

	countsByOrder, err := s.countsForOrders(ctx, year, searchOrderNos(rows))
	if err != nil {
		return nil, err
	}
	items := make([]SearchItem, 0, len(rows))
	for _, row := range rows {
		counts := countsByOrder[row.OrderNo]
		if counts == nil {
			counts = EmptyCounts()
		}
		items = append(items, SearchItem{
			OrderNo:    row.OrderNo,
			Customer:   row.Customer,
			CSVPresent: row.CSVPresent,
			Uploaded:   counts["合同"]+counts["发票"]+counts["发货单"] > 0,
			Counts:     counts,
		})
	}
	s.storeSearch(cacheKey, items)
	return items, nil
}

func (s *Service) Detail(ctx context.Context, year int, orderNo string) (Detail, error) {
	if !ValidYear(year) {
		return Detail{}, apierror.ErrYearNotFound
	}
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" || storage.ValidatePathSegment(orderNo) != nil {
		return Detail{}, apierror.ErrOrderNotFound
	}

	var order struct {
		OrderNo       string `db:"order_no"`
		Customer      string `db:"customer"`
		CustomerClean string `db:"customer_clean"`
		CSVPresent    bool   `db:"csv_present"`
		CheckStatus   string `db:"check_status"`
	}
	if err := s.db.GetContext(ctx, &order, `SELECT order_no, customer, customer_clean, csv_present, check_status FROM orders WHERE year = ? AND order_no = ?`, year, orderNo); err != nil {
		if err == sql.ErrNoRows {
			return Detail{}, apierror.ErrOrderNotFound
		}
		metrics.Default.IncSQLiteErrors()
		return Detail{}, fmt.Errorf("load order: %w", err)
	}

	var lines []Line
	if err := s.db.SelectContext(ctx, &lines, `
SELECT order_no, customer, order_date, product, quantity, amount, total_with_tax, tax_rate, invoice_no
FROM order_lines
WHERE year = ? AND order_no = ?
	ORDER BY order_date_sort ASC, id ASC`, year, orderNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return Detail{}, fmt.Errorf("load order lines: %w", err)
	}

	var uploadRows []UploadFile
	if err := s.db.SelectContext(ctx, &uploadRows, `
SELECT id, seq, filename, byte_size, kind, uploaded_at, operator
FROM uploads
WHERE year = ? AND order_no = ?
	ORDER BY CASE kind WHEN '合同' THEN 1 WHEN '发票' THEN 2 ELSE 3 END, seq ASC, id ASC`, year, orderNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return Detail{}, fmt.Errorf("load uploads: %w", err)
	}

	uploads := map[string][]UploadFile{
		"合同":  {},
		"发票":  {},
		"发货单": {},
	}
	for _, file := range uploadRows {
		file.URL = fmt.Sprintf("/files/y/%d/%s/%s", year, orderNo, file.Filename)
		uploads[file.Kind] = append(uploads[file.Kind], file)
	}

	return Detail{
		OrderNo:     order.OrderNo,
		Year:        year,
		Customer:    order.Customer,
		CSVPresent:  order.CSVPresent,
		CheckStatus: order.CheckStatus,
		Lines:       lines,
		Uploads:     uploads,
	}, nil
}

func (s *Service) SetCheckStatus(ctx context.Context, year int, orderNo, status string) error {
	if !ValidYear(year) {
		return apierror.ErrYearNotFound
	}
	if storage.ValidatePathSegment(orderNo) != nil {
		return apierror.ErrOrderNotFound
	}
	if !ValidCheckStatus(status) {
		return apierror.ErrBadRequest
	}
	res, err := s.db.ExecContext(ctx, `UPDATE orders SET check_status = ? WHERE year = ? AND order_no = ?`, status, year, orderNo)
	if err != nil {
		metrics.Default.IncSQLiteErrors()
		return fmt.Errorf("update check_status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return apierror.ErrOrderNotFound
	}
	return nil
}

func (s *Service) CustomerClean(ctx context.Context, year int, orderNo string) (string, error) {
	if storage.ValidatePathSegment(orderNo) != nil {
		return "", apierror.ErrOrderNotFound
	}
	var customerClean string
	if err := s.db.GetContext(ctx, &customerClean, `SELECT customer_clean FROM orders WHERE year = ? AND order_no = ?`, year, orderNo); err != nil {
		if err == sql.ErrNoRows {
			return "", apierror.ErrOrderNotFound
		}
		metrics.Default.IncSQLiteErrors()
		return "", err
	}
	return customerClean, nil
}

func (s *Service) CountsForOrder(ctx context.Context, year int, orderNo string) (Counts, error) {
	if storage.ValidatePathSegment(orderNo) != nil {
		return nil, apierror.ErrOrderNotFound
	}
	counts := EmptyCounts()
	type row struct {
		Kind  string `db:"kind"`
		Count int    `db:"count"`
	}
	var rows []row
	if err := s.db.SelectContext(ctx, &rows, `SELECT kind, COUNT(*) AS count FROM uploads WHERE year = ? AND order_no = ? GROUP BY kind`, year, orderNo); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, err
	}
	for _, row := range rows {
		counts[row.Kind] = row.Count
	}
	return counts, nil
}

func (s *Service) UploadRowsByKinds(ctx context.Context, year int, orderNo string, kinds []string) ([]UploadRow, error) {
	if storage.ValidatePathSegment(orderNo) != nil {
		return nil, apierror.ErrOrderNotFound
	}
	query, args, err := sqlx.In(`SELECT id, year, order_no, kind, seq, filename FROM uploads WHERE year = ? AND order_no = ? AND kind IN (?) ORDER BY CASE kind WHEN '合同' THEN 1 WHEN '发票' THEN 2 ELSE 3 END, seq ASC, id ASC`, year, orderNo, kinds)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	var rows []UploadRow
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, err
	}
	return rows, nil
}

func (s *Service) AdminYears(ctx context.Context) ([]AdminYear, error) {
	if cached, ok := s.cachedAdminYears(); ok {
		return cached, nil
	}
	var items []AdminYear
	query := `
SELECT
  o.year,
  SUM(CASE WHEN o.csv_present = 1 THEN 1 ELSE 0 END) AS total,
  SUM(CASE WHEN o.csv_present = 1 AND EXISTS (
      SELECT 1 FROM uploads u WHERE u.year = o.year AND u.order_no = o.order_no
    ) THEN 1 ELSE 0 END) AS uploaded,
  SUM(CASE WHEN o.csv_present = 0 AND EXISTS (
      SELECT 1 FROM uploads u WHERE u.year = o.year AND u.order_no = o.order_no
    ) THEN 1 ELSE 0 END) AS csv_removed
FROM orders o
GROUP BY o.year
ORDER BY o.year ASC`
	if err := s.db.SelectContext(ctx, &items, query); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, err
	}
	s.storeAdminYears(items)
	return items, nil
}

func (s *Service) AdminList(ctx context.Context, year, page, size int, onlyUploaded, onlyCSVRemoved bool, query, checkStatus string) (AdminList, error) {
	if !ValidYear(year) {
		return AdminList{}, apierror.ErrYearNotFound
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}

	where := []string{"o.year = ?"}
	args := []any{year}
	if onlyUploaded {
		where = append(where, `EXISTS (SELECT 1 FROM uploads u WHERE u.year = o.year AND u.order_no = o.order_no)`)
	}
	if onlyCSVRemoved {
		where = append(where, `o.csv_present = 0 AND EXISTS (SELECT 1 FROM uploads u WHERE u.year = o.year AND u.order_no = o.order_no)`)
	}
	switch checkStatus {
	case "未检查", "已检查", "错误":
		where = append(where, `COALESCE(o.check_status, '未检查') = ?`)
		args = append(args, checkStatus)
	}
	query = strings.TrimSpace(query)
	if r := []rune(query); len(r) > 64 {
		query = string(r[:64])
	}
	if query != "" {
		where = append(where, `(
  o.order_no LIKE '%' || ? || '%' COLLATE NOCASE
  OR o.customer LIKE '%' || ? || '%' COLLATE NOCASE
  OR EXISTS (
    SELECT 1 FROM uploads uq
    WHERE uq.year = o.year AND uq.order_no = o.order_no
      AND uq.operator LIKE '%' || ? || '%' COLLATE NOCASE
  )
)`)
		args = append(args, query, query, query)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	countQuery := `SELECT COUNT(*) FROM orders o WHERE ` + whereSQL
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		metrics.Default.IncSQLiteErrors()
		return AdminList{}, err
	}

	cursor := ""
	for i := 1; i < page; i++ {
		batch, nextCursor, err := s.adminListBatch(ctx, whereSQL, args, size, cursor)
		if err != nil {
			return AdminList{}, err
		}
		if len(batch) == 0 {
			return AdminList{Page: page, Size: size, Total: total, Items: []AdminListItem{}}, nil
		}
		cursor = nextCursor
	}

	rows, _, err := s.adminListBatch(ctx, whereSQL, args, size, cursor)
	if err != nil {
		return AdminList{}, err
	}
	items := make([]AdminListItem, 0, len(rows))
	for _, row := range rows {
		counts := Counts{"合同": row.ContractCnt, "发票": row.InvoiceCnt, "发货单": row.DeliveryCnt}
		status := row.CheckStatus
		if status == "" {
			status = "未检查"
		}
		items = append(items, AdminListItem{
			OrderNo:      row.OrderNo,
			Customer:     row.Customer,
			CSVRemoved:   row.CSVRemoved,
			CheckStatus:  status,
			LastUploadAt: parseSQLiteTime(row.LastUploadAtRaw),
			Uploaded:     row.ContractCnt+row.InvoiceCnt+row.DeliveryCnt > 0,
			Counts:       counts,
			Operators:    splitOperators(row.OperatorsRaw),
		})
	}
	return AdminList{Page: page, Size: size, Total: total, Items: items}, nil
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

func (s *Service) adminListBatch(ctx context.Context, whereSQL string, args []any, size int, cursor string) ([]adminListRow, string, error) {
	query := `
SELECT
  o.order_no,
  o.customer,
  CASE WHEN o.csv_present = 0 AND EXISTS (
    SELECT 1 FROM uploads u WHERE u.year = o.year AND u.order_no = o.order_no
  ) THEN 1 ELSE 0 END AS csv_removed,
  COALESCE(o.check_status, '未检查') AS check_status,
  MAX(u.uploaded_at) AS last_upload_at,
  COALESCE(SUM(CASE WHEN u.kind = '合同' THEN 1 ELSE 0 END), 0) AS contract_cnt,
  COALESCE(SUM(CASE WHEN u.kind = '发票' THEN 1 ELSE 0 END), 0) AS invoice_cnt,
  COALESCE(SUM(CASE WHEN u.kind = '发货单' THEN 1 ELSE 0 END), 0) AS delivery_cnt,
  COALESCE(GROUP_CONCAT(NULLIF(u.operator, ''), char(31)), '') AS operators
FROM orders o
LEFT JOIN uploads u ON u.year = o.year AND u.order_no = o.order_no
WHERE ` + whereSQL + `
AND (? = '' OR o.order_no > ?)
GROUP BY o.order_no, o.customer, o.csv_present, o.check_status
ORDER BY o.order_no ASC
LIMIT ?`

	queryArgs := append(append([]any{}, args...), cursor, cursor, size)
	var rows []adminListRow
	if err := s.db.SelectContext(ctx, &rows, query, queryArgs...); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, "", err
	}
	nextCursor := cursor
	if len(rows) > 0 {
		nextCursor = rows[len(rows)-1].OrderNo
	}
	return rows, nextCursor, nil
}

func AllKinds() []string {
	out := make([]string, len(kinds))
	copy(out, kinds)
	return out
}

// parseSQLiteTime turns a SQLite TEXT datetime (stored as
// "2006-01-02 15:04:05" by CURRENT_TIMESTAMP) into *time.Time in UTC.
// Returns nil when the column is NULL or cannot be parsed.
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

func (s *Service) countsForOrders(ctx context.Context, year int, orderNos []string) (map[string]Counts, error) {
	result := make(map[string]Counts, len(orderNos))
	for _, orderNo := range orderNos {
		result[orderNo] = EmptyCounts()
	}
	if len(orderNos) == 0 {
		return result, nil
	}

	query, args, err := sqlx.In(`SELECT order_no, kind, COUNT(*) AS count FROM uploads WHERE year = ? AND order_no IN (?) GROUP BY order_no, kind`, year, orderNos)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	type row struct {
		OrderNo string `db:"order_no"`
		Kind    string `db:"kind"`
		Count   int    `db:"count"`
	}
	var rows []row
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		metrics.Default.IncSQLiteErrors()
		return nil, err
	}
	for _, row := range rows {
		result[row.OrderNo][row.Kind] = row.Count
	}
	return result, nil
}

func searchOrderNos(rows []searchRow) []string {
	orderNos := make([]string, 0, len(rows))
	for _, row := range rows {
		orderNos = append(orderNos, row.OrderNo)
	}
	return orderNos
}

func (s *Service) cachedProgress(year int) (Progress, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache.progress[year]
	if !ok || time.Now().After(entry.expiresAt) {
		return Progress{}, false
	}
	return entry.value, true
}

func (s *Service) storeProgress(year int, value Progress) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache.progress[year] = cachedProgress{value: value, expiresAt: time.Now().Add(5 * time.Second)}
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

func (s *Service) cachedAdminYears() ([]AdminYear, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if time.Now().After(s.cache.adminYears.expiresAt) {
		return nil, false
	}
	out := make([]AdminYear, len(s.cache.adminYears.value))
	copy(out, s.cache.adminYears.value)
	return out, true
}

func (s *Service) storeAdminYears(value []AdminYear) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	out := make([]AdminYear, len(value))
	copy(out, value)
	s.cache.adminYears = cachedAdminYears{value: out, expiresAt: time.Now().Add(5 * time.Second)}
}
