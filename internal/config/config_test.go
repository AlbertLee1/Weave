package config

import (
	"os"
	"strings"
	"testing"
	"time"
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

// --- Validate() tests (Tier 1.3) ---

// validDevConfig returns a Config that satisfies Validate() in dev mode.
// Tests build on top of this and mutate the field they want to break.
func validDevConfig() *Config {
	return &Config{
		Port:     8080,
		LogLevel: "info",
		DataDir:  "./data",
		AuthMode: "dev",
		JWT: JWTConfig{
			Issuer:          "weave",
			Audience:        "weave-api",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 168 * time.Hour,
			BcryptCost:      12,
		},
	}
}

func TestConfig_Validate_ValidDevMode(t *testing.T) {
	cfg := validDevConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected dev mode minimal config to validate, got: %v", err)
	}
}

func TestConfig_Validate_JWTModeWithoutKey(t *testing.T) {
	cfg := validDevConfig()
	cfg.AuthMode = "jwt"
	// No PrivateKeyPath / PrivateKeyPEM provided.
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error when AUTH_MODE=jwt but no key material set")
	}
	msg := err.Error()
	if !strings.Contains(msg, "jwt") || !strings.Contains(msg, "key") {
		t.Errorf("expected error to mention jwt and key material, got: %v", err)
	}
}

func TestConfig_Validate_JWTModeWithKeyPath(t *testing.T) {
	cfg := validDevConfig()
	cfg.AuthMode = "jwt"
	cfg.JWT.PrivateKeyPath = "/etc/weave/priv.pem"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected jwt mode with PrivateKeyPath to validate, got: %v", err)
	}
}

func TestConfig_Validate_JWTModeWithKeyPEM(t *testing.T) {
	cfg := validDevConfig()
	cfg.AuthMode = "jwt"
	cfg.JWT.PrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\n..."
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected jwt mode with PrivateKeyPEM to validate, got: %v", err)
	}
}

func TestConfig_Validate_InvalidPort(t *testing.T) {
	tests := []int{-1, 0, 65536, 100000}
	for _, p := range tests {
		cfg := validDevConfig()
		cfg.Port = p
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected validation error for port %d", p)
		}
	}
}

func TestConfig_Validate_EmptyDataDir(t *testing.T) {
	cfg := validDevConfig()
	cfg.DataDir = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty DataDir")
	}
	if !strings.Contains(err.Error(), "DataDir") && !strings.Contains(err.Error(), "data") {
		t.Errorf("expected error to mention DataDir, got: %v", err)
	}
}

func TestConfig_Validate_EmptyPGDSN_OK(t *testing.T) {
	// PG is optional (degraded mode).
	cfg := validDevConfig()
	cfg.PGDSN = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected empty PGDSN to be allowed (degraded mode), got: %v", err)
	}
}

func TestConfig_Validate_EmptyNATSURL_OK(t *testing.T) {
	// NATS is optional (degraded mode).
	cfg := validDevConfig()
	cfg.NATSURL = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected empty NATSURL to be allowed (degraded mode), got: %v", err)
	}
}

func TestConfig_Validate_MultipleErrors(t *testing.T) {
	cfg := validDevConfig()
	cfg.Port = -5
	cfg.DataDir = ""
	cfg.AuthMode = "jwt"
	// No JWT keys → error 3.

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation errors")
	}
	msg := err.Error()

	// Verify all three problems surface, not just the first.
	if !strings.Contains(msg, "port") && !strings.Contains(msg, "Port") {
		t.Errorf("expected error to mention port, got: %v", err)
	}
	if !strings.Contains(msg, "DataDir") && !strings.Contains(msg, "data") {
		t.Errorf("expected error to mention DataDir, got: %v", err)
	}
	if !strings.Contains(msg, "jwt") {
		t.Errorf("expected error to mention jwt, got: %v", err)
	}
}
