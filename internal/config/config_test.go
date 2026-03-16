package config

import (
	"os"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear any env vars that might interfere
	os.Unsetenv("WEAVE_PORT")
	os.Unsetenv("WEAVE_LOG_LEVEL")
	os.Unsetenv("WEAVE_DATA_DIR")
	os.Unsetenv("PG_DSN")
	os.Unsetenv("NATS_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log level 'info', got %q", cfg.LogLevel)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("expected data dir './data', got %q", cfg.DataDir)
	}
}

func TestLoadConfig_FromEnv(t *testing.T) {
	t.Setenv("WEAVE_PORT", "9090")
	t.Setenv("WEAVE_LOG_LEVEL", "debug")
	t.Setenv("WEAVE_DATA_DIR", "/tmp/weave")
	t.Setenv("PG_DSN", "postgres://user:pass@localhost:5432/weave")
	t.Setenv("NATS_URL", "nats://localhost:4222")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected log level 'debug', got %q", cfg.LogLevel)
	}
	if cfg.DataDir != "/tmp/weave" {
		t.Errorf("expected data dir '/tmp/weave', got %q", cfg.DataDir)
	}
	if cfg.PGDSN != "postgres://user:pass@localhost:5432/weave" {
		t.Errorf("expected PG DSN, got %q", cfg.PGDSN)
	}
	if cfg.NATSURL != "nats://localhost:4222" {
		t.Errorf("expected NATS URL, got %q", cfg.NATSURL)
	}
}

func TestLoadConfig_InvalidPort(t *testing.T) {
	t.Setenv("WEAVE_PORT", "abc")

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for invalid port")
	}
}
