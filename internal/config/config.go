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
//
// US-271: OTLPProtocol selects HTTP (default) or gRPC transport when
// Exporter is the generic "otlp" alias. Operators can also pin the
// transport explicitly via Exporter="otlphttp" / "otlpgrpc". OTLPInsecure
// defaults to true so a sidecar collector at otel-collector:4317 / 4318
// works out of the box without TLS plumbing.
type TracingConfig struct {
	Enabled      bool
	Exporter     string // "stdout" | "otlp" | "otlphttp" | "otlpgrpc" | "none"
	OTLPEndpoint string
	OTLPProtocol string // "http" | "grpc"
	OTLPInsecure bool
	ServiceName  string

	// SampleRate (US-439) is the head-based sampling probability for
	// non-error / non-slow spans, in [0, 1]. Defaults to 1.0 in dev /
	// test setups via the bootstrapped config; production deployments
	// should set WEAVE_TRACE_SAMPLE_RATE=0.01 (1%) so error / slow
	// spans dominate the trace stream.
	SampleRate float64

	// SlowSpanThreshold (US-439) is the duration above which a span is
	// force-sampled regardless of SampleRate. Defaults to 1s; matches
	// PRD US-439 spec.
	SlowSpanThreshold time.Duration
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

// FunctionRuntimeConfig points the Vertex pkg/vertex/funcruntime client at
// the Python sandbox runtime (VTX-049). The runtime lives in
// runtime/python/ — a FastAPI + pydantic + sklearn process that owns
// function execution and the sandbox boundary. Separate from FunctionsConfig
// because the wire contract is different (sandboxed Vertex functions vs.
// the older Tier-3.2 function-backed action dispatcher) and we want
// operators to be able to point them at different processes.
//
// When URL is empty the client is not constructed and Vertex scenario runs
// fall back to in-process execution paths (or fail loudly when the caller
// requires Python). Timeout defaults to 30s; matches funcruntime.DefaultTimeout
// so config-loaded clients behave identically to those constructed with nil.
type FunctionRuntimeConfig struct {
	URL     string
	Timeout time.Duration
}

// IngestRateLimitConfig controls the per-ontology token-bucket rate limiter
// on the stream ingest endpoint (US-063). Defaults: 1000 rps, burst 1000.
type IngestRateLimitConfig struct {
	RatePerSec float64
	Burst      int
}

// OIDCConfig controls the OIDC Authorization Code flow front-door (US-246).
// When Enabled is true AND IssuerURL / ClientID / ClientSecret / RedirectURL
// are all populated, the server mounts /api/auth/oidc/{login,callback} and
// uses coreos/go-oidc v3 to verify IdP-issued id_tokens. The caller's
// identity is mapped to a Weave UserRecord by email, and a standard
// LoginResponse (access + refresh JWTs) is returned — so downstream API
// calls keep going through the existing JWT middleware unchanged.
type OIDCConfig struct {
	Enabled            bool
	IssuerURL          string
	ClientID           string
	ClientSecret       string
	RedirectURL        string
	Scopes             []string
	SuccessRedirectURL string
	// StateSecret is the HMAC secret (US-492) the OIDC handler uses to sign
	// and verify the `state` parameter that round-trips through the IdP.
	// Loaded from WEAVE_OIDC_STATE_SECRET (raw string, must be ≥16 bytes).
	// When empty, cmd/server generates an ephemeral secret at boot and logs
	// a loud warning — restarts will invalidate any in-flight authorize
	// redirects, so production deployments MUST set this.
	StateSecret string
}

// SAMLConfig controls the SAML 2.0 SSO front-door (US-248). When Enabled is
// true AND IdPSSOURL / IdPIssuer / IdPCertificatePEM / SPEntityID / SPACSURL
// are all populated, the server mounts /api/auth/saml/{metadata,login,acs}
// and uses russellhaering/gosaml2 to verify IdP-issued assertions. The
// caller's identity is mapped to a Weave UserRecord by email and a standard
// LoginResponse is returned — same shape as OIDC + password login so
// downstream API calls keep going through the existing JWT middleware
// unchanged.
type SAMLConfig struct {
	Enabled            bool
	IdPSSOURL          string
	IdPIssuer          string
	IdPCertificatePEM  string
	SPEntityID         string
	SPACSURL           string
	SuccessRedirectURL string
	AttributeEmail     string
	AttributeName      string
}

// LDAPConfig controls the periodic LDAP/AD directory sync (US-252). When
// Enabled is true AND URL + UserBaseDN are populated, the server starts a
// background scheduler that pulls users + groups from the directory at
// the configured Interval (default 1h). Users that vanish from the
// directory are soft-marked disabled; users that reappear are re-enabled.
// Errors degrade loudly — a misconfigured directory leaves the rest of
// the server running, so password / OIDC / SAML logins keep working.
type LDAPConfig struct {
	Enabled  bool
	Interval time.Duration

	URL          string // ldap[s]://host:port
	BindDN       string
	BindPassword string
	StartTLS     bool
	InsecureSkip bool // TEST ONLY — disables cert verification

	UserBaseDN         string
	UserFilter         string
	UserEmailAttribute string
	UserNameAttribute  string
	UserLoginAttribute string

	GroupBaseDN          string
	GroupFilter          string
	GroupNameAttribute   string
	GroupMemberAttribute string
	GroupDescriptionAttr string
}

// AuditExportConfig controls the SIEM-facing audit log exporter (US-265).
// Kind selects the transport — "disabled" (default), "stdout", "syslog",
// or "s3". BatchSize and Retry tune the BatchedExporter wrapper. Syslog /
// S3 options are only consulted when the corresponding Kind is selected.
type AuditExportConfig struct {
	Kind      string // "disabled" | "stdout" | "syslog" | "s3"
	BatchSize int

	RetryMaxAttempts    int
	RetryInitialBackoff time.Duration
	RetryMaxBackoff     time.Duration
	RetryMultiplier     float64

	// Syslog transport. Network is "udp" or "tcp"; Address is host:port.
	SyslogNetwork  string
	SyslogAddress  string
	SyslogFacility int
	SyslogSeverity int
	SyslogHostname string
	SyslogAppName  string

	// S3 destination. Bucket is required when Kind="s3". Prefix is an
	// optional key prefix.
	S3Bucket string
	S3Prefix string

	// RootHashFile enables the US-266 daily root-hash publisher when
	// non-empty. Every RootHashInterval the server appends a
	// `YYYY-MM-DD\t<hex>\n` line anchoring the previous UTC day's chain
	// root to this file. Operator must place the file on append-only
	// storage (or back it with an immutable FS snapshot) so the anchors
	// survive a later attacker.
	RootHashFile     string
	RootHashInterval time.Duration

	// RetentionDays caps how long audit rows live in Postgres (US-269).
	// A nightly scheduler archives rows older than the cutoff to the
	// optional archive sink (reuses the S3 transport when configured)
	// then deletes them. 0 disables retention entirely — the default so
	// fresh deployments retain forever until an operator opts in.
	RetentionDays      int
	RetentionInterval  time.Duration
	RetentionBatchSize int

	// RetentionArchive selects the destination where expired rows are
	// shipped before DB deletion. "none" (default) skips the archive
	// step and simply deletes; "s3" reuses the S3 transport configured
	// by S3Bucket + optional S3Prefix but under the RetentionS3Prefix
	// key namespace so archive objects can be life-cycled separately
	// from live SIEM export objects.
	RetentionArchive  string // "none" | "s3"
	RetentionS3Prefix string
}

// Config holds all process-wide settings loaded from env.
type Config struct {
	Port     int
	LogLevel string
	DataDir  string
	PGDSN    string
	NATSURL  string

	AuthMode    string
	CORSOrigins []string // Parsed from WEAVE_CORS_ORIGINS (comma-separated)
	JWT         JWTConfig

	Metrics          MetricsConfig
	Tracing          TracingConfig
	Functions        FunctionsConfig
	FunctionRuntime  FunctionRuntimeConfig
	IngestRateLimit  IngestRateLimitConfig
	OIDC             OIDCConfig
	SAML             SAMLConfig
	LDAP             LDAPConfig
	AuditExport      AuditExportConfig
	TimeSeries       TimeSeriesConfig
	ColdTier         ColdTierConfig
	ParquetRetention ParquetRetentionConfig
}

// ParquetRetentionConfig drives the materialize.Retainer (US-410). The
// only operator-facing knob is RetentionDays (WEAVE_PARQUET_RETENTION_DAYS,
// default 30) — files past that age are hard-deleted from disk. The 24h
// compaction cadence and 7d archive threshold are PRD-fixed; future
// stories can promote them to env knobs without breaking compatibility.
type ParquetRetentionConfig struct {
	CompactInterval time.Duration
	ArchiveAfter    time.Duration
	RetentionDays   int
}

// ColdTierConfig drives the OSS executor's hot/cold tier router (US-407).
// HotWindow is the rolling window the hot tier (Bleve) is authoritative
// for; rows older than `now - HotWindow` are read from the Parquet cold
// tier and merged into the hot result. Zero / negative values disable
// cold-tier reads entirely so degraded-mode boots with no Materializer
// stay clean.
type ColdTierConfig struct {
	// HotWindow defaults to 24h via WEAVE_HOT_WINDOW_HOURS=24 (PRD).
	HotWindow time.Duration
}

// TimeSeriesConfig selects the backend for pkg/timeseries (US-400).
//
// Backend = "memory" (default in degraded mode), "postgres" (the previous
// default whenever PG was wired), or "victoriametrics" (write-through to
// a VictoriaMetrics single-node deployment via /api/v1/import +
// /api/v1/export). When Backend="victoriametrics", URL must point at the
// VM HTTP listener (e.g. http://victoriametrics:8428) — empty URL fails
// validation up front so a misconfigured deploy can't silently lose
// points.
type TimeSeriesConfig struct {
	Backend string
	URL     string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:     9117,
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
			Enabled:           false,
			ServiceName:       "weave",
			Exporter:          "stdout",
			OTLPProtocol:      "http",
			OTLPInsecure:      true,
			SampleRate:        1.0,
			SlowSpanThreshold: 1 * time.Second,
		},
		Functions: FunctionsConfig{
			Enabled: false,
			BaseURL: "",
			Timeout: 30 * time.Second,
		},
		FunctionRuntime: FunctionRuntimeConfig{
			URL:     "",
			Timeout: 30 * time.Second,
		},
		IngestRateLimit: IngestRateLimitConfig{
			RatePerSec: 1000,
			Burst:      1000,
		},
		AuditExport: AuditExportConfig{
			Kind:                "disabled",
			BatchSize:           100,
			RetryMaxAttempts:    3,
			RetryInitialBackoff: 500 * time.Millisecond,
			RetryMaxBackoff:     10 * time.Second,
			RetryMultiplier:     2,
			SyslogNetwork:       "udp",
			SyslogFacility:      1, // user
			SyslogSeverity:      6, // info
			RootHashInterval:    24 * time.Hour,
			RetentionInterval:   24 * time.Hour,
			RetentionBatchSize:  1000,
			RetentionArchive:    "none",
		},
		TimeSeries: TimeSeriesConfig{
			Backend: "auto",
		},
		ColdTier: ColdTierConfig{
			HotWindow: 24 * time.Hour,
		},
		ParquetRetention: ParquetRetentionConfig{
			CompactInterval: 24 * time.Hour,
			ArchiveAfter:    7 * 24 * time.Hour,
			RetentionDays:   30,
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

	if v := os.Getenv("WEAVE_CORS_ORIGINS"); v != "" {
		for _, origin := range strings.Split(v, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				cfg.CORSOrigins = append(cfg.CORSOrigins, origin)
			}
		}
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
	if v := os.Getenv("WEAVE_OTLP_PROTOCOL"); v != "" {
		cfg.Tracing.OTLPProtocol = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("WEAVE_OTLP_INSECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_OTLP_INSECURE %q: %w", v, err)
		}
		cfg.Tracing.OTLPInsecure = b
	}
	// US-439: head-based sampling probability + slow-span carve-out.
	// SampleRate values outside [0,1] reach pkg/tracing as-is and are
	// clamped there; the config parser only enforces the float syntax.
	if v := os.Getenv("WEAVE_TRACE_SAMPLE_RATE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_TRACE_SAMPLE_RATE %q: %w", v, err)
		}
		cfg.Tracing.SampleRate = f
	}
	if v := os.Getenv("WEAVE_TRACE_SLOW_THRESHOLD"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_TRACE_SLOW_THRESHOLD %q: %w", v, err)
		}
		cfg.Tracing.SlowSpanThreshold = d
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

	// Vertex Python function runtime (VTX-049). PRD spells the URL
	// knob as FUNCTION_RUNTIME_URL (no WEAVE_ prefix) so it matches
	// the BDD spec verbatim; the timeout knob picks up the WEAVE_
	// prefix because it's a per-deploy ergonomic concern rather than
	// part of the public wire contract.
	if v := os.Getenv("FUNCTION_RUNTIME_URL"); v != "" {
		cfg.FunctionRuntime.URL = strings.TrimSpace(v)
	}
	if v := os.Getenv("WEAVE_FUNCTION_RUNTIME_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_FUNCTION_RUNTIME_TIMEOUT %q: %w", v, err)
		}
		cfg.FunctionRuntime.Timeout = d
	}

	if v := os.Getenv("WEAVE_INGEST_RATE_PER_SEC"); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_INGEST_RATE_PER_SEC %q: must be a positive number", v)
		}
		cfg.IngestRateLimit.RatePerSec = parsed
	}
	if v := os.Getenv("WEAVE_INGEST_RATE_BURST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_INGEST_RATE_BURST %q: must be a positive integer", v)
		}
		cfg.IngestRateLimit.Burst = n
	}

	// OIDC (US-246). AUTH_MODE=oidc is shorthand for "enable OIDC front-door
	// on top of jwt-mode middleware"; operators can also leave AUTH_MODE=jwt
	// and just set WEAVE_OIDC_ENABLED=true if they want to keep
	// password-login alongside SSO.
	if cfg.AuthMode == "oidc" {
		cfg.OIDC.Enabled = true
	}
	if v := os.Getenv("WEAVE_OIDC_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_OIDC_ENABLED %q: %w", v, err)
		}
		cfg.OIDC.Enabled = b
	}
	if v := os.Getenv("WEAVE_OIDC_ISSUER"); v != "" {
		cfg.OIDC.IssuerURL = v
	}
	if v := os.Getenv("WEAVE_OIDC_CLIENT_ID"); v != "" {
		cfg.OIDC.ClientID = v
	}
	if v := os.Getenv("WEAVE_OIDC_CLIENT_SECRET"); v != "" {
		cfg.OIDC.ClientSecret = v
	}
	if v := os.Getenv("WEAVE_OIDC_REDIRECT_URL"); v != "" {
		cfg.OIDC.RedirectURL = v
	}
	if v := os.Getenv("WEAVE_OIDC_SCOPES"); v != "" {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.OIDC.Scopes = append(cfg.OIDC.Scopes, s)
			}
		}
	}
	if v := os.Getenv("WEAVE_OIDC_SUCCESS_REDIRECT_URL"); v != "" {
		cfg.OIDC.SuccessRedirectURL = v
	}
	if v := os.Getenv("WEAVE_OIDC_STATE_SECRET"); v != "" {
		cfg.OIDC.StateSecret = v
	}

	// SAML 2.0 SSO front-door (US-248). AUTH_MODE=saml is shorthand for
	// "enable SAML on top of jwt-mode middleware"; operators can also leave
	// AUTH_MODE=jwt and just set WEAVE_SAML_ENABLED=true to keep
	// password/OIDC alongside SAML.
	if cfg.AuthMode == "saml" {
		cfg.SAML.Enabled = true
	}
	if v := os.Getenv("WEAVE_SAML_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_SAML_ENABLED %q: %w", v, err)
		}
		cfg.SAML.Enabled = b
	}
	if v := os.Getenv("WEAVE_SAML_IDP_SSO_URL"); v != "" {
		cfg.SAML.IdPSSOURL = v
	}
	if v := os.Getenv("WEAVE_SAML_IDP_ISSUER"); v != "" {
		cfg.SAML.IdPIssuer = v
	}
	if v := os.Getenv("WEAVE_SAML_IDP_CERT_PEM"); v != "" {
		cfg.SAML.IdPCertificatePEM = v
	}
	// Operators can either inline the cert (WEAVE_SAML_IDP_CERT_PEM) or
	// point at a file on disk (WEAVE_SAML_IDP_CERT_PATH). The path-based
	// form is friendlier for K8s secret mounts and Docker volumes.
	if v := os.Getenv("WEAVE_SAML_IDP_CERT_PATH"); v != "" {
		body, err := os.ReadFile(v)
		if err != nil {
			return nil, fmt.Errorf("read WEAVE_SAML_IDP_CERT_PATH %q: %w", v, err)
		}
		cfg.SAML.IdPCertificatePEM = string(body)
	}
	if v := os.Getenv("WEAVE_SAML_SP_ENTITY_ID"); v != "" {
		cfg.SAML.SPEntityID = v
	}
	if v := os.Getenv("WEAVE_SAML_SP_ACS_URL"); v != "" {
		cfg.SAML.SPACSURL = v
	}
	if v := os.Getenv("WEAVE_SAML_SUCCESS_REDIRECT_URL"); v != "" {
		cfg.SAML.SuccessRedirectURL = v
	}
	if v := os.Getenv("WEAVE_SAML_ATTRIBUTE_EMAIL"); v != "" {
		cfg.SAML.AttributeEmail = v
	}
	if v := os.Getenv("WEAVE_SAML_ATTRIBUTE_NAME"); v != "" {
		cfg.SAML.AttributeName = v
	}

	// LDAP/AD directory sync (US-252). AUTH_MODE=ldap is shorthand for
	// "enable LDAP sync alongside the regular auth pipeline"; operators
	// can also leave AUTH_MODE=jwt and just set WEAVE_LDAP_ENABLED=true
	// to keep password/OIDC/SAML running alongside directory sync.
	if cfg.AuthMode == "ldap" {
		cfg.LDAP.Enabled = true
	}
	if v := os.Getenv("WEAVE_LDAP_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_LDAP_ENABLED %q: %w", v, err)
		}
		cfg.LDAP.Enabled = b
	}
	if v := os.Getenv("WEAVE_LDAP_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_LDAP_INTERVAL %q: %w", v, err)
		}
		cfg.LDAP.Interval = d
	}
	if v := os.Getenv("WEAVE_LDAP_URL"); v != "" {
		cfg.LDAP.URL = v
	}
	if v := os.Getenv("WEAVE_LDAP_BIND_DN"); v != "" {
		cfg.LDAP.BindDN = v
	}
	if v := os.Getenv("WEAVE_LDAP_BIND_PASSWORD"); v != "" {
		cfg.LDAP.BindPassword = v
	}
	if v := os.Getenv("WEAVE_LDAP_STARTTLS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_LDAP_STARTTLS %q: %w", v, err)
		}
		cfg.LDAP.StartTLS = b
	}
	if v := os.Getenv("WEAVE_LDAP_INSECURE_SKIP_VERIFY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_LDAP_INSECURE_SKIP_VERIFY %q: %w", v, err)
		}
		cfg.LDAP.InsecureSkip = b
	}
	if v := os.Getenv("WEAVE_LDAP_USER_BASE_DN"); v != "" {
		cfg.LDAP.UserBaseDN = v
	}
	if v := os.Getenv("WEAVE_LDAP_USER_FILTER"); v != "" {
		cfg.LDAP.UserFilter = v
	}
	if v := os.Getenv("WEAVE_LDAP_USER_EMAIL_ATTR"); v != "" {
		cfg.LDAP.UserEmailAttribute = v
	}
	if v := os.Getenv("WEAVE_LDAP_USER_NAME_ATTR"); v != "" {
		cfg.LDAP.UserNameAttribute = v
	}
	if v := os.Getenv("WEAVE_LDAP_USER_LOGIN_ATTR"); v != "" {
		cfg.LDAP.UserLoginAttribute = v
	}
	if v := os.Getenv("WEAVE_LDAP_GROUP_BASE_DN"); v != "" {
		cfg.LDAP.GroupBaseDN = v
	}
	if v := os.Getenv("WEAVE_LDAP_GROUP_FILTER"); v != "" {
		cfg.LDAP.GroupFilter = v
	}
	if v := os.Getenv("WEAVE_LDAP_GROUP_NAME_ATTR"); v != "" {
		cfg.LDAP.GroupNameAttribute = v
	}
	if v := os.Getenv("WEAVE_LDAP_GROUP_MEMBER_ATTR"); v != "" {
		cfg.LDAP.GroupMemberAttribute = v
	}
	if v := os.Getenv("WEAVE_LDAP_GROUP_DESCRIPTION_ATTR"); v != "" {
		cfg.LDAP.GroupDescriptionAttr = v
	}

	// Audit log export (US-265). WEAVE_AUDIT_EXPORT_KIND selects the
	// transport; "disabled" (default) leaves the pipeline off.
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_KIND"); v != "" {
		cfg.AuditExport.Kind = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_BATCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_EXPORT_BATCH_SIZE %q: must be a positive integer", v)
		}
		cfg.AuditExport.BatchSize = n
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_RETRY_MAX_ATTEMPTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_EXPORT_RETRY_MAX_ATTEMPTS %q: must be a positive integer", v)
		}
		cfg.AuditExport.RetryMaxAttempts = n
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_RETRY_INITIAL_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_EXPORT_RETRY_INITIAL_BACKOFF %q: %w", v, err)
		}
		cfg.AuditExport.RetryInitialBackoff = d
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_RETRY_MAX_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_EXPORT_RETRY_MAX_BACKOFF %q: %w", v, err)
		}
		cfg.AuditExport.RetryMaxBackoff = d
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_RETRY_MULTIPLIER"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_EXPORT_RETRY_MULTIPLIER %q: must be a positive number", v)
		}
		cfg.AuditExport.RetryMultiplier = f
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_SYSLOG_NETWORK"); v != "" {
		cfg.AuditExport.SyslogNetwork = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_SYSLOG_ADDRESS"); v != "" {
		cfg.AuditExport.SyslogAddress = v
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_SYSLOG_FACILITY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_EXPORT_SYSLOG_FACILITY %q: %w", v, err)
		}
		cfg.AuditExport.SyslogFacility = n
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_SYSLOG_SEVERITY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_EXPORT_SYSLOG_SEVERITY %q: %w", v, err)
		}
		cfg.AuditExport.SyslogSeverity = n
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_SYSLOG_HOSTNAME"); v != "" {
		cfg.AuditExport.SyslogHostname = v
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_SYSLOG_APP_NAME"); v != "" {
		cfg.AuditExport.SyslogAppName = v
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_S3_BUCKET"); v != "" {
		cfg.AuditExport.S3Bucket = v
	}
	if v := os.Getenv("WEAVE_AUDIT_EXPORT_S3_PREFIX"); v != "" {
		cfg.AuditExport.S3Prefix = v
	}

	// Audit log tamper-proof chain root-hash publisher (US-266). Setting
	// WEAVE_AUDIT_ROOTHASH_FILE to a path enables the daily publisher.
	if v := os.Getenv("WEAVE_AUDIT_ROOTHASH_FILE"); v != "" {
		cfg.AuditExport.RootHashFile = v
	}
	if v := os.Getenv("WEAVE_AUDIT_ROOTHASH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_ROOTHASH_INTERVAL %q: must be a positive duration", v)
		}
		cfg.AuditExport.RootHashInterval = d
	}

	// Audit log retention (US-269). AUDIT_RETENTION_DAYS > 0 enables the
	// nightly archive-and-delete scheduler; 0 (default) retains forever.
	if v := os.Getenv("AUDIT_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid AUDIT_RETENTION_DAYS %q: must be a non-negative integer", v)
		}
		cfg.AuditExport.RetentionDays = n
	}
	if v := os.Getenv("WEAVE_AUDIT_RETENTION_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_RETENTION_INTERVAL %q: must be a positive duration", v)
		}
		cfg.AuditExport.RetentionInterval = d
	}
	if v := os.Getenv("WEAVE_AUDIT_RETENTION_BATCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid WEAVE_AUDIT_RETENTION_BATCH_SIZE %q: must be a positive integer", v)
		}
		cfg.AuditExport.RetentionBatchSize = n
	}
	if v := os.Getenv("WEAVE_AUDIT_RETENTION_ARCHIVE"); v != "" {
		cfg.AuditExport.RetentionArchive = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("WEAVE_AUDIT_RETENTION_S3_PREFIX"); v != "" {
		cfg.AuditExport.RetentionS3Prefix = v
	}

	// TimeSeries backend selection (US-400). Default "auto" preserves the
	// historical behaviour: PG when wired, otherwise in-memory. Operators
	// opt into VictoriaMetrics by setting WEAVE_TS_BACKEND=victoriametrics
	// + WEAVE_TS_URL.
	if v := os.Getenv("WEAVE_TS_BACKEND"); v != "" {
		cfg.TimeSeries.Backend = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("WEAVE_TS_URL"); v != "" {
		cfg.TimeSeries.URL = strings.TrimSpace(v)
	}

	// Cold-tier router (US-407). The PRD spells the knob as
	// WEAVE_HOT_WINDOW_HOURS, an integer count of hours; non-numeric or
	// negative values fail loudly so a typo doesn't silently disable the
	// cold tier on a deploy that depends on it.
	if v := os.Getenv("WEAVE_HOT_WINDOW_HOURS"); v != "" {
		hours, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_HOT_WINDOW_HOURS %q: %w", v, err)
		}
		if hours < 0 {
			return nil, fmt.Errorf("invalid WEAVE_HOT_WINDOW_HOURS %q: must be >= 0", v)
		}
		cfg.ColdTier.HotWindow = time.Duration(hours) * time.Hour
	}

	// Parquet retention (US-410). WEAVE_PARQUET_RETENTION_DAYS is an
	// integer count of days; a value of 0 disables hard deletion so
	// archived files accumulate forever. Negative or non-numeric values
	// fail loudly so a typo can't accidentally drop the floor.
	if v := os.Getenv("WEAVE_PARQUET_RETENTION_DAYS"); v != "" {
		days, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_PARQUET_RETENTION_DAYS %q: %w", v, err)
		}
		if days < 0 {
			return nil, fmt.Errorf("invalid WEAVE_PARQUET_RETENTION_DAYS %q: must be >= 0", v)
		}
		cfg.ParquetRetention.RetentionDays = days
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

	if c.AuthMode == "token" {
		problems = append(problems,
			"AUTH_MODE=token is removed: use AUTH_MODE=jwt with proper key material for production")
	}

	if c.AuthMode == "jwt" && c.JWT.PrivateKeyPath == "" && c.JWT.PrivateKeyPEM == "" {
		problems = append(problems,
			"AUTH_MODE=jwt requires JWT key material: set WEAVE_JWT_PRIVATE_KEY_PATH or WEAVE_JWT_PRIVATE_KEY_PEM")
	}

	if c.OIDC.Enabled {
		var missing []string
		if strings.TrimSpace(c.OIDC.IssuerURL) == "" {
			missing = append(missing, "WEAVE_OIDC_ISSUER")
		}
		if strings.TrimSpace(c.OIDC.ClientID) == "" {
			missing = append(missing, "WEAVE_OIDC_CLIENT_ID")
		}
		if strings.TrimSpace(c.OIDC.ClientSecret) == "" {
			missing = append(missing, "WEAVE_OIDC_CLIENT_SECRET")
		}
		if strings.TrimSpace(c.OIDC.RedirectURL) == "" {
			missing = append(missing, "WEAVE_OIDC_REDIRECT_URL")
		}
		if len(missing) > 0 {
			problems = append(problems,
				"OIDC.Enabled=true but missing: "+strings.Join(missing, ", "))
		}
	}

	if c.SAML.Enabled {
		var missing []string
		if strings.TrimSpace(c.SAML.IdPSSOURL) == "" {
			missing = append(missing, "WEAVE_SAML_IDP_SSO_URL")
		}
		if strings.TrimSpace(c.SAML.IdPIssuer) == "" {
			missing = append(missing, "WEAVE_SAML_IDP_ISSUER")
		}
		if strings.TrimSpace(c.SAML.IdPCertificatePEM) == "" {
			missing = append(missing, "WEAVE_SAML_IDP_CERT_PEM (or WEAVE_SAML_IDP_CERT_PATH)")
		}
		if strings.TrimSpace(c.SAML.SPEntityID) == "" {
			missing = append(missing, "WEAVE_SAML_SP_ENTITY_ID")
		}
		if strings.TrimSpace(c.SAML.SPACSURL) == "" {
			missing = append(missing, "WEAVE_SAML_SP_ACS_URL")
		}
		if len(missing) > 0 {
			problems = append(problems,
				"SAML.Enabled=true but missing: "+strings.Join(missing, ", "))
		}
	}

	if c.Functions.Enabled && strings.TrimSpace(c.Functions.BaseURL) == "" {
		problems = append(problems,
			"Functions.Enabled=true requires WEAVE_FUNCTIONS_BASE_URL to be set")
	}

	if c.LDAP.Enabled {
		var missing []string
		if strings.TrimSpace(c.LDAP.URL) == "" {
			missing = append(missing, "WEAVE_LDAP_URL")
		}
		if strings.TrimSpace(c.LDAP.UserBaseDN) == "" {
			missing = append(missing, "WEAVE_LDAP_USER_BASE_DN")
		}
		if len(missing) > 0 {
			problems = append(problems,
				"LDAP.Enabled=true but missing: "+strings.Join(missing, ", "))
		}
	}

	switch strings.ToLower(c.AuditExport.Kind) {
	case "", "disabled", "stdout":
		// no transport-specific requirements
	case "syslog":
		if strings.TrimSpace(c.AuditExport.SyslogAddress) == "" {
			problems = append(problems,
				"AuditExport.Kind=syslog requires WEAVE_AUDIT_EXPORT_SYSLOG_ADDRESS (host:port)")
		}
		net := strings.ToLower(c.AuditExport.SyslogNetwork)
		if net != "" && net != "udp" && net != "tcp" {
			problems = append(problems,
				fmt.Sprintf("AuditExport.SyslogNetwork %q: must be udp or tcp", c.AuditExport.SyslogNetwork))
		}
	case "s3":
		if strings.TrimSpace(c.AuditExport.S3Bucket) == "" {
			problems = append(problems,
				"AuditExport.Kind=s3 requires WEAVE_AUDIT_EXPORT_S3_BUCKET")
		}
	default:
		problems = append(problems,
			fmt.Sprintf("AuditExport.Kind %q: must be one of disabled, stdout, syslog, s3", c.AuditExport.Kind))
	}

	// Retention (US-269). RetentionDays<=0 disables retention entirely;
	// only validate dependent fields when enabled.
	if c.AuditExport.RetentionDays > 0 {
		archive := strings.ToLower(strings.TrimSpace(c.AuditExport.RetentionArchive))
		switch archive {
		case "", "none":
			// delete-only — no archive destination required
		case "s3":
			if strings.TrimSpace(c.AuditExport.S3Bucket) == "" {
				problems = append(problems,
					"AuditExport.RetentionArchive=s3 requires WEAVE_AUDIT_EXPORT_S3_BUCKET (reused for archive uploads)")
			}
		default:
			problems = append(problems,
				fmt.Sprintf("AuditExport.RetentionArchive %q: must be one of none, s3", c.AuditExport.RetentionArchive))
		}
	}

	// TimeSeries backend (US-400).
	switch strings.ToLower(strings.TrimSpace(c.TimeSeries.Backend)) {
	case "", "auto", "memory", "postgres":
		// no-op
	case "victoriametrics":
		if strings.TrimSpace(c.TimeSeries.URL) == "" {
			problems = append(problems,
				"TimeSeries.Backend=victoriametrics requires WEAVE_TS_URL (e.g. http://victoriametrics:8428)")
		}
	default:
		problems = append(problems,
			fmt.Sprintf("TimeSeries.Backend %q: must be one of auto, memory, postgres, victoriametrics", c.TimeSeries.Backend))
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New("config validation failed: " + strings.Join(problems, "; "))
}
