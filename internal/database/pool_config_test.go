package database

import (
	"testing"
	"time"
)

func TestPoolConfig_DefaultValues(t *testing.T) {
	cfg := DefaultPoolConfig()

	if cfg.MaxConns != 25 {
		t.Errorf("MaxConns: got %d, want 25", cfg.MaxConns)
	}
	if cfg.MaxConnLifetime != 1*time.Hour {
		t.Errorf("MaxConnLifetime: got %v, want 1h", cfg.MaxConnLifetime)
	}
	if cfg.HealthCheckPeriod != 30*time.Second {
		t.Errorf("HealthCheckPeriod: got %v, want 30s", cfg.HealthCheckPeriod)
	}
}

func TestConnectWithConfig_AppliesPoolSettings(t *testing.T) {
	// This test only verifies the config is parsed — it does not connect.
	// A real connection would require a running PostgreSQL.
	cfg := DefaultPoolConfig()
	poolCfg, err := ParsePoolConfig("postgres://weave:weave@localhost:5432/weave?sslmode=disable", cfg)
	if err != nil {
		t.Fatalf("ParsePoolConfig: %v", err)
	}
	if poolCfg.MaxConns != 25 {
		t.Errorf("MaxConns: got %d, want 25", poolCfg.MaxConns)
	}
	if poolCfg.MaxConnLifetime != 1*time.Hour {
		t.Errorf("MaxConnLifetime: got %v, want 1h", poolCfg.MaxConnLifetime)
	}
	if poolCfg.HealthCheckPeriod != 30*time.Second {
		t.Errorf("HealthCheckPeriod: got %v, want 30s", poolCfg.HealthCheckPeriod)
	}
}
