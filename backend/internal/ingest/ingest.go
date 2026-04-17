package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/storage"
)

type Importer struct {
	db *sqlx.DB
}

type Stats struct {
	RowsRead      int `json:"rowsRead"`
	RowsInserted  int `json:"rowsInserted"`
	OrdersTouched int `json:"ordersTouched"`
}

type ValidationStats struct {
	RowsRead      int  `json:"rowsRead"`
	ValidRows     int  `json:"validRows"`
	InvalidRows   int  `json:"invalidRows"`
	ReportWritten bool `json:"reportWritten"`
}

func New(db *sqlx.DB) *Importer {
	return &Importer{db: db}
}

func (i *Importer) NeedsImport(ctx context.Context) (bool, error) {
	var count int
	if err := i.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM order_lines`); err != nil {
		return false, err
	}
	return count == 0, nil
}

func (i *Importer) ValidateCSV(ctx context.Context, csvPath, reportPath string) (ValidationStats, error) {
	file, err := os.Open(csvPath)
	if err != nil {
		return ValidationStats{}, fmt.Errorf("open csv: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	if _, err := reader.Read(); err != nil {
		return ValidationStats{}, fmt.Errorf("read csv header: %w", err)
	}

	var reportFile *os.File
	if strings.TrimSpace(reportPath) != "" {
		reportFile, err = os.Create(reportPath)
		if err != nil {
			return ValidationStats{}, fmt.Errorf("create error report: %w", err)
		}
		defer reportFile.Close()
		if _, err := reportFile.WriteString("line,error,raw\n"); err != nil {
			return ValidationStats{}, fmt.Errorf("write error report header: %w", err)
		}
	}

	stats := ValidationStats{}
	lineNo := 1
	for {
		lineNo++
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			stats.InvalidRows++
			if reportFile != nil {
				_, _ = fmt.Fprintf(reportFile, "%d,%q,%q\n", lineNo, err.Error(), strings.Join(record, "|"))
			}
			continue
		}
		if len(record) == 0 {
			continue
		}
		stats.RowsRead++
		if _, err := normalizeRecord(record); err != nil {
			stats.InvalidRows++
			if reportFile != nil {
				_, _ = fmt.Fprintf(reportFile, "%d,%q,%q\n", lineNo, err.Error(), strings.Join(record, "|"))
			}
			continue
		}
		stats.ValidRows++
		if stats.RowsRead%1000 == 0 {
			slog.Info("csv dry-run progress", "rows_read", stats.RowsRead, "valid_rows", stats.ValidRows, "invalid_rows", stats.InvalidRows)
		}
	}
	stats.ReportWritten = reportFile != nil
	return stats, nil
}

func (i *Importer) ImportCSV(ctx context.Context, csvPath string) (Stats, error) {
	file, err := os.Open(csvPath)
	if err != nil {
		return Stats{}, fmt.Errorf("open csv: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return Stats{}, fmt.Errorf("read csv header: %w", err)
	}
	if len(header) < 10 {
		return Stats{}, fmt.Errorf("unexpected csv header width: %d", len(header))
	}

	conn, err := i.db.Connx(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("open import conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return Stats{}, fmt.Errorf("begin immediate import tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	if _, err := conn.ExecContext(ctx, `DELETE FROM order_lines`); err != nil {
		return Stats{}, fmt.Errorf("clear order lines: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE orders SET csv_present = 0`); err != nil {
		return Stats{}, fmt.Errorf("mark orders absent: %w", err)
	}

	stats := Stats{}
	seenOrders := map[string]struct{}{}
	lineNo := 1
	for {
		lineNo++
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return Stats{}, fmt.Errorf("read csv row %d: %w", lineNo, err)
		}
		if len(record) == 0 {
			continue
		}

		row, err := normalizeRecord(record)
		if err != nil {
			return Stats{}, fmt.Errorf("normalize csv row %d: %w", lineNo, err)
		}
		stats.RowsRead++
		if stats.RowsRead%1000 == 0 {
			slog.Info("csv import progress", "rows_read", stats.RowsRead, "rows_inserted", stats.RowsInserted)
		}

		result, err := conn.ExecContext(ctx, `
INSERT OR IGNORE INTO order_lines (
	year, order_no, order_date, order_date_sort, customer, product, quantity, amount,
	total_with_tax, tax_rate, invoice_no, source_hash, source_line
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.Year, row.OrderNo, row.OrderDate, row.OrderDateSort, row.Customer, row.Product, row.Quantity,
			row.Amount, row.TotalWithTax, row.TaxRate, row.InvoiceNo, row.SourceHash, lineNo,
		)
		if err != nil {
			return Stats{}, fmt.Errorf("insert order line %d: %w", lineNo, err)
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			stats.RowsInserted++
		}

		if _, err := conn.ExecContext(ctx, `
INSERT INTO orders (year, order_no, customer, customer_clean, csv_present)
VALUES (?, ?, ?, ?, 1)
ON CONFLICT (year, order_no) DO UPDATE SET
	customer = excluded.customer,
	customer_clean = excluded.customer_clean,
	csv_present = 1`,
			row.Year, row.OrderNo, row.Customer, row.CustomerClean,
		); err != nil {
			return Stats{}, fmt.Errorf("upsert order %d: %w", lineNo, err)
		}
		seenOrders[fmt.Sprintf("%d:%s", row.Year, row.OrderNo)] = struct{}{}
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return Stats{}, fmt.Errorf("commit import: %w", err)
	}
	committed = true

	stats.OrdersTouched = len(seenOrders)
	return stats, nil
}

type normalizedRow struct {
	Year          int
	OrderDate     string
	OrderDateSort string
	OrderNo       string
	Customer      string
	CustomerClean string
	Product       string
	Quantity      float64
	Amount        float64
	TotalWithTax  float64
	TaxRate       float64
	InvoiceNo     string
	SourceHash    string
}

func normalizeRecord(record []string) (normalizedRow, error) {
	if len(record) < 10 {
		return normalizedRow{}, fmt.Errorf("record has %d fields", len(record))
	}

	yearRaw := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(record[0], "\ufeff"), "Y"))
	year, err := strconv.Atoi(yearRaw)
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid year %q", record[0])
	}
	orderDate := strings.TrimSpace(record[1])
	orderDateSort := orderDate
	if parsed, err := time.Parse("2006/1/2", orderDate); err == nil {
		orderDateSort = parsed.Format("2006-01-02")
	}

	orderNo := storage.SanitizeOrderNo(record[2])
	if orderNo == "" {
		return normalizedRow{}, fmt.Errorf("invalid order_no %q", record[2])
	}
	if err := storage.ValidatePathSegment(orderNo); err != nil {
		return normalizedRow{}, fmt.Errorf("invalid order_no %q", record[2])
	}
	customer := strings.TrimSpace(record[3])
	customerClean := storage.SanitizeCustomerName(customer)
	product := strings.TrimSpace(record[4])

	quantity, err := parseFloat(record[5])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid quantity: %w", err)
	}
	amount, err := parseFloat(record[6])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid amount: %w", err)
	}
	totalWithTax, err := parseFloat(record[7])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid total_with_tax: %w", err)
	}
	taxRate, err := parseFloat(record[8])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid tax rate: %w", err)
	}
	invoiceNo := strings.TrimSpace(record[9])

	hashInput := strings.Join([]string{
		strconv.Itoa(year),
		orderDate,
		orderNo,
		customer,
		product,
		strconv.FormatFloat(quantity, 'f', -1, 64),
		strconv.FormatFloat(amount, 'f', -1, 64),
		strconv.FormatFloat(totalWithTax, 'f', -1, 64),
		strconv.FormatFloat(taxRate, 'f', -1, 64),
		invoiceNo,
	}, "\t")
	sum := sha256.Sum256([]byte(hashInput))

	return normalizedRow{
		Year:          year,
		OrderDate:     orderDate,
		OrderDateSort: orderDateSort,
		OrderNo:       orderNo,
		Customer:      firstNonEmpty(customer, "未知客户"),
		CustomerClean: customerClean,
		Product:       product,
		Quantity:      quantity,
		Amount:        amount,
		TotalWithTax:  totalWithTax,
		TaxRate:       taxRate,
		InvoiceNo:     invoiceNo,
		SourceHash:    hex.EncodeToString(sum[:]),
	}, nil
}

func parseFloat(value string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(value), 64)
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
