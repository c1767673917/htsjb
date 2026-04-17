package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	moderncsqlite "modernc.org/sqlite"

	"product-collection-form/backend/internal/config"
)

const currentUserVersion = 3

const driverName = "sqlite-app"

var registerDriverOnce sync.Once

type Options struct {
	Path string
	Pool config.DBConfig
}

func Open(ctx context.Context, opts Options) (*sqlx.DB, error) {
	dbPath := opts.Path
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}

	registerDriverOnce.Do(func() {
		driver := &moderncsqlite.Driver{}
		driver.RegisterConnectionHook(func(conn moderncsqlite.ExecQuerierContext, _ string) error {
			return applyConnPragmas(context.Background(), conn)
		})
		sql.Register(driverName, driver)
	})

	conn, err := sqlx.Open(driverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(opts.Pool.MaxOpenConns)
	conn.SetMaxIdleConns(opts.Pool.MaxIdleConns)
	conn.SetConnMaxLifetime(time.Duration(opts.Pool.ConnMaxLifetimeMinutes) * time.Minute)

	if err := Migrate(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func applyConnPragmas(ctx context.Context, conn moderncsqlite.ExecQuerierContext) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA temp_store=MEMORY",
	}
	for _, query := range pragmas {
		if _, err := conn.ExecContext(ctx, query, nil); err != nil {
			return fmt.Errorf("apply pragma %q: %w", query, err)
		}
	}
	return nil
}

func Migrate(ctx context.Context, conn *sqlx.DB) error {
	var version int
	if err := conn.GetContext(ctx, &version, "PRAGMA user_version"); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if version >= currentUserVersion {
		return nil
	}

	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()

	if version < 1 {
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS order_lines (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				year INTEGER NOT NULL,
				order_no TEXT NOT NULL,
				order_date TEXT NOT NULL,
				order_date_sort TEXT NOT NULL,
				customer TEXT NOT NULL,
				product TEXT NOT NULL,
				quantity REAL NOT NULL,
				amount REAL NOT NULL,
				total_with_tax REAL NOT NULL,
				tax_rate REAL NOT NULL,
				invoice_no TEXT NOT NULL,
				source_hash TEXT NOT NULL UNIQUE,
				source_line INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_order_lines_year_order ON order_lines(year, order_no)`,
			`CREATE INDEX IF NOT EXISTS idx_order_lines_order_like ON order_lines(order_no)`,
			`CREATE TABLE IF NOT EXISTS orders (
				year INTEGER NOT NULL,
				order_no TEXT NOT NULL,
				customer TEXT NOT NULL,
				customer_clean TEXT NOT NULL,
				csv_present INTEGER NOT NULL DEFAULT 1,
				first_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (year, order_no)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_orders_csv_present ON orders(csv_present)`,
			`CREATE TABLE IF NOT EXISTS uploads (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				year INTEGER NOT NULL,
				order_no TEXT NOT NULL,
				kind TEXT NOT NULL CHECK (kind IN ('合同','发票','发货单')),
				seq INTEGER NOT NULL,
				filename TEXT NOT NULL,
				byte_size INTEGER NOT NULL,
				sha256 TEXT NOT NULL,
				uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				UNIQUE (year, order_no, kind, seq),
				FOREIGN KEY (year, order_no) REFERENCES orders(year, order_no)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_uploads_year_order ON uploads(year, order_no)`,
			`CREATE INDEX IF NOT EXISTS idx_uploads_year_kind ON uploads(year, kind)`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migrate schema: %w", err)
			}
		}
	}

	if version < 2 {
		stmts := []string{
			`ALTER TABLE uploads ADD COLUMN operator TEXT NOT NULL DEFAULT ''`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migrate schema v2: %w", err)
			}
		}
	}

	if version < 3 {
		stmts := []string{
			`ALTER TABLE orders ADD COLUMN check_status TEXT NOT NULL DEFAULT '未检查' CHECK (check_status IN ('未检查','已检查','错误'))`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migrate schema v3: %w", err)
			}
		}
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", currentUserVersion)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
