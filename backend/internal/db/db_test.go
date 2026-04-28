package db

import (
	"context"
	"testing"
	"time"

	"product-collection-form/backend/internal/config"
)

func TestOpenAppliesConfiguredSQLiteConnectionLimit(t *testing.T) {
	conn, err := Open(context.Background(), Options{
		Path: t.TempDir() + "/app.db",
		Pool: config.DBConfig{
			MaxOpenConns:           1,
			MaxIdleConns:           1,
			ConnMaxLifetimeMinutes: 30,
		},
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	held, err := conn.Connx(ctx)
	if err != nil {
		t.Fatalf("hold first conn: %v", err)
	}
	defer held.Close()

	_, err = conn.Connx(ctx)
	if err == nil {
		t.Fatal("expected second sqlite connection to wait until context timeout")
	}
}
