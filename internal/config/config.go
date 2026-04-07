package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// MetricsConfig controls the Prometheus /metrics endpoint and the
// metrics middleware. Defaults: enabled=true, path=/metrics. The
// endpoint is exposed unauthenticated so a sidecar / scraper can hit it.
type MetricsConfig struct {
	Enabled bool
	Path    string
}

// TracingConfig controls the OpenTelemetry tracer provider built in
// pkg/tracing. Defaults: disabled, ServiceName=weave, no exporter.
// Set Exporter to "stdout" for local debugging or "otlp" with an
// OTLPEndpoint to ship spans to a real collector.
type TracingConfig struct {
	Enabled      bool
	Exporter     string // "stdout" | "otlp" | "none"
	OTLPEndpoint string
	ServiceName  string
}

// FunctionsConfig controls the Tier 3.2 function-backed action runtime.
// When Enabled, the action executor will dispatch IsFunctionBacked action
// types to BaseURL/{functionRid} via HTTP. When Enabled is false the
// dispatcher is not constructed and function-backed action types fall back
// to the local rules path so dev environments still work without a
// function service running.
type FunctionsConfig struct {
	Enabled bool
	BaseURL string
	Timeout time.Duration
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

	Metrics   MetricsConfig
	Tracing   TracingConfig
	Functions FunctionsConfig
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
		Metrics: MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
		Tracing: TracingConfig{
			Enabled:     false,
			ServiceName: "weave",
			Exporter:    "stdout",
		},
		Functions: FunctionsConfig{
			Enabled: false,
			BaseURL: "",
			Timeout: 30 * time.Second,
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

	if v := os.Getenv("WEAVE_METRICS_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_METRICS_ENABLED %q: %w", v, err)
		}
		cfg.Metrics.Enabled = b
	}
	if v := os.Getenv("WEAVE_METRICS_PATH"); v != "" {
		cfg.Metrics.Path = v
	}

	if v := os.Getenv("WEAVE_TRACING_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_TRACING_ENABLED %q: %w", v, err)
		}
		cfg.Tracing.Enabled = b
	}
	if v := os.Getenv("WEAVE_TRACING_SERVICE_NAME"); v != "" {
		cfg.Tracing.ServiceName = v
	}
	if v := os.Getenv("WEAVE_TRACING_EXPORTER"); v != "" {
		cfg.Tracing.Exporter = v
	}
	if v := os.Getenv("WEAVE_OTLP_ENDPOINT"); v != "" {
		cfg.Tracing.OTLPEndpoint = v
	}

	if v := os.Getenv("WEAVE_FUNCTIONS_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_FUNCTIONS_ENABLED %q: %w", v, err)
		}
		cfg.Functions.Enabled = b
	}
	if v := os.Getenv("WEAVE_FUNCTIONS_BASE_URL"); v != "" {
		cfg.Functions.BaseURL = v
	}
	if v := os.Getenv("WEAVE_FUNCTIONS_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_FUNCTIONS_TIMEOUT %q: %w", v, err)
		}
		cfg.Functions.Timeout = d
	}

	return cfg, nil
}

// Validate checks the loaded Config for required fields and consistency
// across modes. It collects ALL problems before returning so operators see
// every misconfiguration in one boot attempt rather than fixing them one
// at a time. Returns nil when the config is acceptable. PGDSN and NATSURL
// are deliberately allowed to be empty so the server can boot in degraded
// mode (in-memory OMS, no funnel) for local dev and disaster recovery.
func (c *Config) Validate() error {
	var problems []string

	if c.Port <= 0 || c.Port >= 65536 {
		problems = append(problems, fmt.Sprintf("invalid Port %d: must be in range 1..65535", c.Port))
	}

	if strings.TrimSpace(c.DataDir) == "" {
		problems = append(problems, "DataDir must be non-empty (set WEAVE_DATA_DIR)")
	}

	if c.AuthMode == "jwt" && c.JWT.PrivateKeyPath == "" && c.JWT.PrivateKeyPEM == "" {
		problems = append(problems,
			"AUTH_MODE=jwt requires JWT key material: set WEAVE_JWT_PRIVATE_KEY_PATH or WEAVE_JWT_PRIVATE_KEY_PEM")
	}

	if c.Functions.Enabled && strings.TrimSpace(c.Functions.BaseURL) == "" {
		problems = append(problems,
			"Functions.Enabled=true requires WEAVE_FUNCTIONS_BASE_URL to be set")
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New("config validation failed: " + strings.Join(problems, "; "))
}
