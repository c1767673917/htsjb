package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen          string      `yaml:"listen"`
	CSVPath         string      `yaml:"csv_path"`
	InvoiceCSVPath  string      `yaml:"invoice_csv_path"`
	DataDir         string      `yaml:"data_dir"`
	DBPath          string      `yaml:"db_path"`
	AdminPassword   string      `yaml:"admin_password"`
	SessionTTLHours int         `yaml:"session_ttl_hours"`
	Limits          Limits      `yaml:"limits"`
	Concurrency     Concurrency `yaml:"concurrency"`
	DB              DBConfig    `yaml:"db"`
	Image           ImageConfig `yaml:"image"`
}

type Limits struct {
	PerKindMax            int `yaml:"per_kind_max"`
	SubmitBodyMaxMB       int `yaml:"submit_body_max_mb"`
	SingleFileMaxMB       int `yaml:"single_file_max_mb"`
	SingleFileDecodeCapMB int `yaml:"single_file_decode_cap_mb"`
	MaxPixels             int `yaml:"max_pixels"`
}

type Concurrency struct {
	MaxUploads            int `yaml:"max_uploads"`
	MaxPDFRebuilds        int `yaml:"max_pdf_rebuilds"`
	MaxYearExports        int `yaml:"max_year_exports"`
	MaxBundleExports      int `yaml:"max_bundle_exports"`
	MaxImageDecodes       int `yaml:"max_image_decodes"`
	AcquireTimeoutSeconds int `yaml:"acquire_timeout_seconds"`
}

type DBConfig struct {
	MaxOpenConns           int `yaml:"max_open_conns"`
	MaxIdleConns           int `yaml:"max_idle_conns"`
	ConnMaxLifetimeMinutes int `yaml:"conn_max_lifetime_minutes"`
}

type ImageConfig struct {
	PDFOrder     []string `yaml:"pdf_order"`
	AcceptedMIME []string `yaml:"accepted_mime"`
}

type LoadOptions struct {
	AllowUnsafeAdminPassword bool
}

func Default() Config {
	return Config{
		Listen:          "0.0.0.0:8080",
		CSVPath:         "./21-25订单.csv",
		InvoiceCSVPath:  "./油脂发票.csv",
		DataDir:         "./data",
		AdminPassword:   "CHANGE-ME",
		SessionTTLHours: 12,
		Limits: Limits{
			PerKindMax:            50,
			SubmitBodyMaxMB:       60,
			SingleFileMaxMB:       10,
			SingleFileDecodeCapMB: 20,
			MaxPixels:             50_000_000,
		},
		Concurrency: Concurrency{
			MaxUploads:            4,
			MaxPDFRebuilds:        4,
			MaxYearExports:        1,
			MaxBundleExports:      4,
			MaxImageDecodes:       4,
			AcquireTimeoutSeconds: 5,
		},
		DB: DBConfig{
			MaxOpenConns:           8,
			MaxIdleConns:           4,
			ConnMaxLifetimeMinutes: 30,
		},
		Image: ImageConfig{
			PDFOrder:     []string{"合同", "发票"},
			AcceptedMIME: []string{"image/jpeg", "image/png", "image/webp"},
		},
	}
}

func Load(path string, options ...LoadOptions) (Config, error) {
	cfg := Default()
	if path == "" {
		path = "config.yaml"
	}
	loadOptions := LoadOptions{}
	if len(options) > 0 {
		loadOptions = options[0]
	}

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	applyEnv(&cfg)
	cfg.CSVPath = filepath.Clean(cfg.CSVPath)
	cfg.InvoiceCSVPath = filepath.Clean(cfg.InvoiceCSVPath)
	cfg.DataDir = filepath.Clean(cfg.DataDir)
	if strings.TrimSpace(cfg.DBPath) == "" {
		cfg.DBPath = filepath.Join(cfg.DataDir, "app.db")
	}
	cfg.DBPath = filepath.Clean(cfg.DBPath)

	if err := cfg.Validate(loadOptions); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	setString(&cfg.Listen, "APP_LISTEN")
	setString(&cfg.CSVPath, "APP_CSV_PATH")
	setString(&cfg.InvoiceCSVPath, "APP_INVOICE_CSV_PATH")
	setString(&cfg.DataDir, "APP_DATA_DIR")
	setString(&cfg.DBPath, "APP_DB_PATH")
	setString(&cfg.AdminPassword, "APP_ADMIN_PASSWORD")
	setInt(&cfg.SessionTTLHours, "APP_SESSION_TTL_HOURS")
	setInt(&cfg.Limits.PerKindMax, "APP_LIMITS_PER_KIND_MAX")
	setInt(&cfg.Limits.SubmitBodyMaxMB, "APP_LIMITS_SUBMIT_BODY_MAX_MB")
	setInt(&cfg.Limits.SingleFileMaxMB, "APP_LIMITS_SINGLE_FILE_MAX_MB")
	setInt(&cfg.Limits.SingleFileDecodeCapMB, "APP_LIMITS_SINGLE_FILE_DECODE_CAP_MB")
	setInt(&cfg.Limits.MaxPixels, "APP_LIMITS_MAX_PIXELS")
	setInt(&cfg.Concurrency.MaxUploads, "APP_CONCURRENCY_MAX_UPLOADS")
	setInt(&cfg.Concurrency.MaxPDFRebuilds, "APP_CONCURRENCY_MAX_PDF_REBUILDS")
	setInt(&cfg.Concurrency.MaxYearExports, "APP_CONCURRENCY_MAX_YEAR_EXPORTS")
	setInt(&cfg.Concurrency.MaxBundleExports, "APP_CONCURRENCY_MAX_BUNDLE_EXPORTS")
	setInt(&cfg.Concurrency.MaxImageDecodes, "APP_CONCURRENCY_MAX_IMAGE_DECODES")
	setInt(&cfg.Concurrency.AcquireTimeoutSeconds, "APP_CONCURRENCY_ACQUIRE_TIMEOUT_SECONDS")
	setInt(&cfg.DB.MaxOpenConns, "APP_DB_MAX_OPEN_CONNS")
	setInt(&cfg.DB.MaxIdleConns, "APP_DB_MAX_IDLE_CONNS")
	setInt(&cfg.DB.ConnMaxLifetimeMinutes, "APP_DB_CONN_MAX_LIFETIME_MINUTES")
	setSlice(&cfg.Image.PDFOrder, "APP_IMAGE_PDF_ORDER")
	setSlice(&cfg.Image.AcceptedMIME, "APP_IMAGE_ACCEPTED_MIME")
}

func setString(dst *string, key string) {
	if value, ok := os.LookupEnv(key); ok {
		*dst = value
	}
}

func setInt(dst *int, key string) {
	if value, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			*dst = n
		}
	}
}

func setSlice(dst *[]string, key string) {
	if value, ok := os.LookupEnv(key); ok {
		parts := strings.Split(value, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		if len(out) > 0 {
			*dst = out
		}
	}
}

func (c Config) Validate(opts LoadOptions) error {
	if c.Listen == "" {
		return errors.New("listen must not be empty")
	}
	if c.CSVPath == "" {
		return errors.New("csv_path must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("data_dir must not be empty")
	}
	if c.DBPath == "" {
		return errors.New("db_path must not be empty")
	}
	if !opts.AllowUnsafeAdminPassword {
		if strings.TrimSpace(c.AdminPassword) == "" || c.AdminPassword == "CHANGE-ME" {
			return errors.New("admin_password 不能是空值或默认值 CHANGE-ME，请先编辑 config.yaml 或设置 APP_ADMIN_PASSWORD")
		}
	}
	if c.SessionTTLHours <= 0 {
		return errors.New("session_ttl_hours must be positive")
	}
	if c.Limits.PerKindMax <= 0 {
		return errors.New("limits.per_kind_max must be positive")
	}
	if c.Limits.SubmitBodyMaxMB <= 0 {
		return errors.New("limits.submit_body_max_mb must be positive")
	}
	if c.Limits.SingleFileMaxMB <= 0 {
		return errors.New("limits.single_file_max_mb must be positive")
	}
	if c.Limits.SingleFileDecodeCapMB <= 0 {
		return errors.New("limits.single_file_decode_cap_mb must be positive")
	}
	if c.Limits.MaxPixels <= 0 {
		return errors.New("limits.max_pixels must be positive")
	}
	if c.Concurrency.MaxUploads <= 0 {
		return errors.New("concurrency.max_uploads must be positive")
	}
	if c.Concurrency.MaxPDFRebuilds <= 0 {
		return errors.New("concurrency.max_pdf_rebuilds must be positive")
	}
	if c.Concurrency.MaxYearExports <= 0 {
		return errors.New("concurrency.max_year_exports must be positive")
	}
	if c.Concurrency.MaxBundleExports <= 0 {
		return errors.New("concurrency.max_bundle_exports must be positive")
	}
	if c.Concurrency.MaxImageDecodes <= 0 {
		return errors.New("concurrency.max_image_decodes must be positive")
	}
	if c.Concurrency.AcquireTimeoutSeconds <= 0 {
		return errors.New("concurrency.acquire_timeout_seconds must be positive")
	}
	if c.DB.MaxOpenConns <= 0 {
		return errors.New("db.max_open_conns must be positive")
	}
	if c.DB.MaxIdleConns < 0 {
		return errors.New("db.max_idle_conns must be non-negative")
	}
	if c.DB.MaxIdleConns > c.DB.MaxOpenConns {
		return errors.New("db.max_idle_conns must not exceed db.max_open_conns")
	}
	if c.DB.ConnMaxLifetimeMinutes <= 0 {
		return errors.New("db.conn_max_lifetime_minutes must be positive")
	}
	if len(c.Image.PDFOrder) == 0 {
		return errors.New("image.pdf_order must not be empty")
	}
	if len(c.Image.AcceptedMIME) == 0 {
		return errors.New("image.accepted_mime must not be empty")
	}
	return nil
}
