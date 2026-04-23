package invoiceingest

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

	"github.com/jmoiron/sqlx"

	"product-collection-form/backend/internal/storage"
)

type Importer struct {
	db *sqlx.DB
}

type Stats struct {
	RowsRead        int `json:"rowsRead"`
	RowsInserted    int `json:"rowsInserted"`
	InvoicesTouched int `json:"invoicesTouched"`
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
	if err := i.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM invoice_lines`); err != nil {
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
		if _, err := normalizeRecord(record, lineNo); err != nil {
			stats.InvalidRows++
			if reportFile != nil {
				_, _ = fmt.Fprintf(reportFile, "%d,%q,%q\n", lineNo, err.Error(), strings.Join(record, "|"))
			}
			continue
		}
		stats.ValidRows++
		if stats.RowsRead%1000 == 0 {
			slog.Info("invoice csv dry-run progress", "rows_read", stats.RowsRead, "valid_rows", stats.ValidRows, "invalid_rows", stats.InvalidRows)
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
	if len(header) < 29 {
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

	if _, err := conn.ExecContext(ctx, `DELETE FROM invoice_lines`); err != nil {
		return Stats{}, fmt.Errorf("clear invoice lines: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE invoices SET csv_present = 0`); err != nil {
		return Stats{}, fmt.Errorf("mark invoices absent: %w", err)
	}

	stats := Stats{}
	seenInvoices := map[string]struct{}{}
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

		row, err := normalizeRecord(record, lineNo)
		if err != nil {
			return Stats{}, fmt.Errorf("normalize csv row %d: %w", lineNo, err)
		}
		stats.RowsRead++
		if stats.RowsRead%1000 == 0 {
			slog.Info("invoice csv import progress", "rows_read", stats.RowsRead, "rows_inserted", stats.RowsInserted)
		}

		if _, err := conn.ExecContext(ctx, `
INSERT INTO invoices (invoice_no, customer, customer_clean, seller, invoice_date, csv_present)
VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT (invoice_no) DO UPDATE SET
	customer = excluded.customer,
	customer_clean = excluded.customer_clean,
	seller = excluded.seller,
	invoice_date = excluded.invoice_date,
	csv_present = 1`,
			row.InvoiceNo, row.Customer, row.CustomerClean, row.Seller, row.InvoiceDate,
		); err != nil {
			return Stats{}, fmt.Errorf("upsert invoice %d: %w", lineNo, err)
		}

		result, err := conn.ExecContext(ctx, `
INSERT OR IGNORE INTO invoice_lines (
	invoice_no, year, invoice_date, seller, customer, product, quantity, amount,
	tax_amount, total_with_tax, tax_rate, source_hash, source_line
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.InvoiceNo, row.Year, row.InvoiceDate, row.Seller, row.Customer, row.Product, row.Quantity,
			row.Amount, row.TaxAmount, row.TotalWithTax, row.TaxRate, row.SourceHash, lineNo,
		)
		if err != nil {
			return Stats{}, fmt.Errorf("insert invoice line %d: %w", lineNo, err)
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			stats.RowsInserted++
		}
		seenInvoices[row.InvoiceNo] = struct{}{}
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return Stats{}, fmt.Errorf("commit import: %w", err)
	}
	committed = true

	stats.InvoicesTouched = len(seenInvoices)
	return stats, nil
}

type normalizedRow struct {
	InvoiceNo    string
	Year         int
	InvoiceDate  string
	Seller       string
	Customer     string
	CustomerClean string
	Product      string
	Quantity     float64
	Amount       float64
	TaxAmount    float64
	TotalWithTax float64
	TaxRate      string
	SourceHash   string
}

func normalizeRecord(record []string, lineNo int) (normalizedRow, error) {
	if len(record) < 29 {
		return normalizedRow{}, fmt.Errorf("record has %d fields, need at least 29", len(record))
	}

	// Strip BOM from first field if present
	invoiceNo := strings.TrimSpace(strings.TrimPrefix(record[4], "\ufeff"))
	if invoiceNo == "" {
		return normalizedRow{}, fmt.Errorf("empty invoice_no at line %d", lineNo)
	}

	seller := strings.TrimSpace(record[6])
	customer := strings.TrimSpace(record[8])
	invoiceDate := strings.TrimSpace(record[9])
	product := strings.TrimSpace(record[12])

	quantity, err := parseFloat(record[15])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid quantity: %w", err)
	}
	amount, err := parseFloat(record[17])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid amount: %w", err)
	}
	taxRate := strings.TrimSpace(record[18])
	taxAmount, err := parseFloat(record[19])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid tax_amount: %w", err)
	}
	totalWithTax, err := parseFloat(record[20])
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid total_with_tax: %w", err)
	}

	yearRaw := strings.TrimSpace(record[28])
	yearRaw = strings.TrimPrefix(yearRaw, "Y")
	yearRaw = strings.TrimPrefix(yearRaw, "y")
	year, err := strconv.Atoi(yearRaw)
	if err != nil {
		return normalizedRow{}, fmt.Errorf("invalid year %q", record[28])
	}

	customerClean := storage.SanitizeCustomerName(customer)

	hashInput := strings.Join([]string{
		invoiceNo,
		strconv.Itoa(year),
		invoiceDate,
		seller,
		customer,
		product,
		strconv.FormatFloat(quantity, 'f', -1, 64),
		strconv.FormatFloat(amount, 'f', -1, 64),
		strconv.FormatFloat(taxAmount, 'f', -1, 64),
		strconv.FormatFloat(totalWithTax, 'f', -1, 64),
		taxRate,
	}, "\t")
	sum := sha256.Sum256([]byte(hashInput))

	return normalizedRow{
		InvoiceNo:    invoiceNo,
		Year:         year,
		InvoiceDate:  invoiceDate,
		Seller:       firstNonEmpty(seller, "未知销方"),
		Customer:     firstNonEmpty(customer, "未知客户"),
		CustomerClean: customerClean,
		Product:      product,
		Quantity:     quantity,
		Amount:       amount,
		TaxAmount:    taxAmount,
		TotalWithTax: totalWithTax,
		TaxRate:      taxRate,
		SourceHash:   hex.EncodeToString(sum[:]),
	}, nil
}

func parseFloat(value string) (float64, error) {
	s := strings.TrimSpace(value)
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
