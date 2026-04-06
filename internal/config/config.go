package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// JWTConfig holds all JWT/auth-related configuration loaded from env.
// See .omc/scientist/reports/20260406_jwt_auth_design.md "New Config Variables".
type JWTConfig struct {
	// Issuer / Audience values that are baked into every issued token and
	// required on every verified token.
	Issuer   string
	Audience string

	// Token lifetimes.
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// Key material — file paths take precedence over inline PEM strings.
	PrivateKeyPath string
	PublicKeyPath  string
	PrivateKeyPEM  string
	PublicKeyPEM   string

	// Bcrypt cost factor (passwords). 12 is the OWASP-recommended default.
	BcryptCost int
}

// Config holds all process-wide settings loaded from env.
type Config struct {
	Port     int
	LogLevel string
	DataDir  string
	PGDSN    string
	NATSURL  string

	AuthMode string
	JWT      JWTConfig
}

func Load() (*Config, error) {
	cfg := &Config{
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

	if v := os.Getenv("WEAVE_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_PORT %q: %w", v, err)
		}
		cfg.Port = p
	}

	if v := os.Getenv("WEAVE_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv("WEAVE_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}

	if v := os.Getenv("PG_DSN"); v != "" {
		cfg.PGDSN = v
	}

	if v := os.Getenv("NATS_URL"); v != "" {
		cfg.NATSURL = v
	}

	if v := os.Getenv("AUTH_MODE"); v != "" {
		cfg.AuthMode = v
	}

	if v := os.Getenv("WEAVE_JWT_ISSUER"); v != "" {
		cfg.JWT.Issuer = v
	}
	if v := os.Getenv("WEAVE_JWT_AUDIENCE"); v != "" {
		cfg.JWT.Audience = v
	}
	if v := os.Getenv("WEAVE_JWT_PRIVATE_KEY_PATH"); v != "" {
		cfg.JWT.PrivateKeyPath = v
	}
	if v := os.Getenv("WEAVE_JWT_PUBLIC_KEY_PATH"); v != "" {
		cfg.JWT.PublicKeyPath = v
	}
	if v := os.Getenv("WEAVE_JWT_PRIVATE_KEY_PEM"); v != "" {
		cfg.JWT.PrivateKeyPEM = v
	}
	if v := os.Getenv("WEAVE_JWT_PUBLIC_KEY_PEM"); v != "" {
		cfg.JWT.PublicKeyPEM = v
	}
	if v := os.Getenv("WEAVE_JWT_ACCESS_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_JWT_ACCESS_TTL %q: %w", v, err)
		}
		cfg.JWT.AccessTokenTTL = d
	}
	if v := os.Getenv("WEAVE_JWT_REFRESH_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_JWT_REFRESH_TTL %q: %w", v, err)
		}
		cfg.JWT.RefreshTokenTTL = d
	}
	if v := os.Getenv("BCRYPT_COST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid BCRYPT_COST %q: %w", v, err)
		}
		cfg.JWT.BcryptCost = n
	}

	return cfg, nil
}
