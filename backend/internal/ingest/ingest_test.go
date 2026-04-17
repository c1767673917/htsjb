package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/db"
)

func TestImportCSVIdempotentAndCSVPresentFlip(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	conn, err := db.Open(context.Background(), db.Options{
		Path: filepath.Join(tempDir, "app.db"),
		Pool: config.Default().DB,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	importer := New(conn)
	firstCSV := filepath.Join(tempDir, "first.csv")
	writeCSV(t, firstCSV, `年,日期,单据编号,购货单位,产品名称,数量,金额,价税合计,税率(%),发票号
Y2021,2021/1/4,RX2101-22926.,哈尔滨金诺食品有限公司,满特起酥油（FM）,1000,114545.45,126000,10,2021/50122444
Y2021,2021/1/4,RX2101-22927.,海通（郫县）,百圣深层煎炸起酥油(PDFF),200,22545.45,24800,10,2021/21529873
`)

	if _, err := importer.ImportCSV(context.Background(), firstCSV); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := importer.ImportCSV(context.Background(), firstCSV); err != nil {
		t.Fatalf("second import: %v", err)
	}

	var lineCount int
	if err := conn.GetContext(context.Background(), &lineCount, `SELECT COUNT(*) FROM order_lines`); err != nil {
		t.Fatalf("count lines: %v", err)
	}
	if lineCount != 2 {
		t.Fatalf("expected 2 order lines after idempotent import, got %d", lineCount)
	}

	secondCSV := filepath.Join(tempDir, "second.csv")
	writeCSV(t, secondCSV, `年,日期,单据编号,购货单位,产品名称,数量,金额,价税合计,税率(%),发票号
Y2021,2021/1/4,RX2101-22926.,哈尔滨金诺食品有限公司,满特起酥油（FM）,1000,114545.45,126000,10,2021/50122444
`)
	if _, err := importer.ImportCSV(context.Background(), secondCSV); err != nil {
		t.Fatalf("third import: %v", err)
	}

	var csvPresent int
	if err := conn.GetContext(context.Background(), &csvPresent, `SELECT csv_present FROM orders WHERE year = 2021 AND order_no = 'RX2101-22927'`); err != nil {
		t.Fatalf("load csv_present: %v", err)
	}
	if csvPresent != 0 {
		t.Fatalf("expected removed order csv_present=0, got %d", csvPresent)
	}
}

func TestImportCSVRejectsUnsafeOrderNo(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	conn, err := db.Open(context.Background(), db.Options{
		Path: filepath.Join(tempDir, "app.db"),
		Pool: config.Default().DB,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	importer := New(conn)
	csvPath := filepath.Join(tempDir, "bad.csv")
	writeCSV(t, csvPath, `年,日期,单据编号,购货单位,产品名称,数量,金额,价税合计,税率(%),发票号
Y2021,2021/1/4,%2e%2e,哈尔滨金诺食品有限公司,满特起酥油（FM）,1000,114545.45,126000,10,2021/50122444
`)

	if _, err := importer.ImportCSV(context.Background(), csvPath); err == nil {
		t.Fatalf("expected unsafe order_no to fail import")
	}
}

func writeCSV(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write csv %s: %v", path, err)
	}
}
