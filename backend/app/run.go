package app

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	frontendembed "product-collection-form"
	"product-collection-form/backend/internal/admin"
	"product-collection-form/backend/internal/config"
	"product-collection-form/backend/internal/db"
	"product-collection-form/backend/internal/httpapi"
	"product-collection-form/backend/internal/httpapi/limits"
	"product-collection-form/backend/internal/ingest"
	"product-collection-form/backend/internal/orders"
	"product-collection-form/backend/internal/pdfmerge"
	"product-collection-form/backend/internal/storage"
	"product-collection-form/backend/internal/uploads"
)

func Run(args []string) int {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cmd := "serve"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "serve":
		if err := serve(args); err != nil {
			slog.Error("serve failed", "error", err)
			return 1
		}
		return 0
	case "import-csv":
		if err := importCSV(args); err != nil {
			slog.Error("import-csv failed", "error", err)
			return 1
		}
		return 0
	default:
		slog.Error("unknown subcommand", "command", cmd)
		return 2
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := flags.String("config", "config.yaml", "config file path")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, conn, storageSvc, importer, limiter, err := bootstrap(*configPath, config.LoadOptions{})
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := storageSvc.RunJanitor(time.Now()); err != nil {
		return fmt.Errorf("run janitor: %w", err)
	}
	needsImport, err := importer.NeedsImport(context.Background())
	if err != nil {
		return fmt.Errorf("check import state: %w", err)
	}
	if needsImport {
		if _, err := importer.ImportCSV(context.Background(), cfg.CSVPath); err != nil {
			return fmt.Errorf("initial csv import: %w", err)
		}
	}

	distFS, err := fs.Sub(frontendembed.DistFS, "frontend/dist")
	if err != nil {
		return fmt.Errorf("resolve embedded dist: %w", err)
	}

	orderSvc := orders.NewService(conn, storageSvc)
	pdfSvc := pdfmerge.New(limiter)
	uploadSvc := uploads.NewService(conn, cfg, orderSvc, storageSvc, pdfSvc, limiter)
	adminSvc, err := admin.NewService(conn, cfg, orderSvc, storageSvc, uploadSvc, limiter)
	if err != nil {
		return err
	}
	router := httpapi.New(conn, orderSvc, storageSvc, uploadSvc, adminSvc, distFS)

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	slog.Info("listening", "addr", cfg.Listen)

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrCh:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-shutdownCtx.Done():
		stop()
		drainCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(drainCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		if err := checkpointWAL(drainCtx, conn); err != nil {
			slog.Warn("wal checkpoint failed", "error", err)
		}
		if err := <-serverErrCh; err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

func importCSV(args []string) error {
	flags := flag.NewFlagSet("import-csv", flag.ContinueOnError)
	configPath := flags.String("config", "config.yaml", "config file path")
	reimport := flags.Bool("reimport", false, "force a full CSV re-import")
	dryRun := flags.Bool("dry-run", false, "validate CSV without writing to the database")
	errorReport := flags.String("error-report", "", "write CSV validation errors to this report file")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, conn, _, importer, _, err := bootstrap(*configPath, config.LoadOptions{AllowUnsafeAdminPassword: true})
	if err != nil {
		return err
	}
	defer conn.Close()

	if *dryRun {
		stats, err := importer.ValidateCSV(context.Background(), cfg.CSVPath, *errorReport)
		if err != nil {
			return err
		}
		slog.Info("csv dry-run finished", "rows_read", stats.RowsRead, "valid_rows", stats.ValidRows, "invalid_rows", stats.InvalidRows, "report_written", stats.ReportWritten)
		return nil
	}

	if !*reimport {
		needsImport, err := importer.NeedsImport(context.Background())
		if err != nil {
			return fmt.Errorf("check import state: %w", err)
		}
		if !needsImport {
			slog.Info("csv import skipped", "reason", "already imported", "hint", "use --reimport to force full refresh")
			return nil
		}
	}
	stats, err := importer.ImportCSV(context.Background(), cfg.CSVPath)
	if err != nil {
		return err
	}
	slog.Info("csv imported", "rows_read", stats.RowsRead, "rows_inserted", stats.RowsInserted, "orders_touched", stats.OrdersTouched)
	return nil
}

func bootstrap(configPath string, loadOptions config.LoadOptions) (config.Config, *sqlx.DB, *storage.Service, *ingest.Importer, *limits.Manager, error) {
	cfg, err := config.Load(configPath, loadOptions)
	if err != nil {
		return config.Config{}, nil, nil, nil, nil, err
	}
	storageSvc := storage.New(cfg.DataDir)
	if err := storageSvc.EnsureLayout(); err != nil {
		return config.Config{}, nil, nil, nil, nil, fmt.Errorf("prepare storage: %w", err)
	}
	conn, err := db.Open(context.Background(), db.Options{
		Path: cfg.DBPath,
		Pool: cfg.DB,
	})
	if err != nil {
		return config.Config{}, nil, nil, nil, nil, err
	}
	return cfg, conn, storageSvc, ingest.New(conn), limits.New(cfg.Concurrency), nil
}

func checkpointWAL(ctx context.Context, conn *sqlx.DB) error {
	_, err := conn.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}
