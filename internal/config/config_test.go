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

func TestLoadConfig_JWTDefaults(t *testing.T) {
	os.Unsetenv("WEAVE_JWT_PRIVATE_KEY_PATH")
	os.Unsetenv("WEAVE_JWT_PUBLIC_KEY_PATH")
	os.Unsetenv("WEAVE_JWT_ACCESS_TTL")
	os.Unsetenv("WEAVE_JWT_REFRESH_TTL")
	os.Unsetenv("WEAVE_JWT_ISSUER")
	os.Unsetenv("WEAVE_JWT_AUDIENCE")
	os.Unsetenv("BCRYPT_COST")
	os.Unsetenv("AUTH_MODE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JWT.AccessTokenTTL.Minutes() != 15 {
		t.Errorf("expected default access TTL 15m, got %v", cfg.JWT.AccessTokenTTL)
	}
	if cfg.JWT.RefreshTokenTTL.Hours() != 168 {
		t.Errorf("expected default refresh TTL 168h, got %v", cfg.JWT.RefreshTokenTTL)
	}
	if cfg.JWT.Issuer != "weave" {
		t.Errorf("expected default issuer weave, got %q", cfg.JWT.Issuer)
	}
	if cfg.JWT.Audience != "weave-api" {
		t.Errorf("expected default audience weave-api, got %q", cfg.JWT.Audience)
	}
	if cfg.JWT.BcryptCost != 12 {
		t.Errorf("expected default bcrypt cost 12, got %d", cfg.JWT.BcryptCost)
	}
}

func TestLoadConfig_JWTOverrides(t *testing.T) {
	t.Setenv("WEAVE_JWT_ISSUER", "myissuer")
	t.Setenv("WEAVE_JWT_AUDIENCE", "myaudience")
	t.Setenv("WEAVE_JWT_ACCESS_TTL", "5m")
	t.Setenv("WEAVE_JWT_REFRESH_TTL", "24h")
	t.Setenv("WEAVE_JWT_PRIVATE_KEY_PATH", "/tmp/priv.pem")
	t.Setenv("WEAVE_JWT_PUBLIC_KEY_PATH", "/tmp/pub.pem")
	t.Setenv("BCRYPT_COST", "10")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.Issuer != "myissuer" {
		t.Errorf("issuer: got %q", cfg.JWT.Issuer)
	}
	if cfg.JWT.Audience != "myaudience" {
		t.Errorf("audience: got %q", cfg.JWT.Audience)
	}
	if cfg.JWT.AccessTokenTTL.Minutes() != 5 {
		t.Errorf("access TTL: got %v", cfg.JWT.AccessTokenTTL)
	}
	if cfg.JWT.RefreshTokenTTL.Hours() != 24 {
		t.Errorf("refresh TTL: got %v", cfg.JWT.RefreshTokenTTL)
	}
	if cfg.JWT.PrivateKeyPath != "/tmp/priv.pem" {
		t.Errorf("priv path: got %q", cfg.JWT.PrivateKeyPath)
	}
	if cfg.JWT.PublicKeyPath != "/tmp/pub.pem" {
		t.Errorf("pub path: got %q", cfg.JWT.PublicKeyPath)
	}
	if cfg.JWT.BcryptCost != 10 {
		t.Errorf("bcrypt cost: got %d", cfg.JWT.BcryptCost)
	}
}

func TestLoadConfig_AuthMode(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMode != "jwt" {
		t.Errorf("AuthMode: got %q", cfg.AuthMode)
	}
}

func TestLoadConfig_InvalidJWTAccessTTL(t *testing.T) {
	t.Setenv("WEAVE_JWT_ACCESS_TTL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}
