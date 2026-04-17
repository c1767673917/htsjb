package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsDefaultAdminPassword(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.DBPath = filepath.Join(cfg.DataDir, "app.db")
	if err := cfg.Validate(LoadOptions{}); err == nil {
		t.Fatalf("expected default admin password to be rejected")
	}
}

func TestValidateAllowsDefaultPasswordForImportTools(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.DBPath = filepath.Join(cfg.DataDir, "app.db")
	if err := cfg.Validate(LoadOptions{AllowUnsafeAdminPassword: true}); err != nil {
		t.Fatalf("expected import mode validation to pass: %v", err)
	}
}

func TestLoadDefaultsDBPathFromDataDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("data_dir: /tmp/example\nadmin_password: secret\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DBPath != filepath.Clean("/tmp/example/app.db") {
		t.Fatalf("expected db_path derived from data_dir, got %q", cfg.DBPath)
	}
}
