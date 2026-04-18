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
	if cfg.Port != 9117 {
		t.Errorf("expected port 9117, got %d", cfg.Port)
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

// --- Tier 2.6 metrics + tracing config tests ---

func TestLoadConfig_MetricsDefaults(t *testing.T) {
	os.Unsetenv("WEAVE_METRICS_ENABLED")
	os.Unsetenv("WEAVE_METRICS_PATH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Metrics.Enabled {
		t.Errorf("Metrics.Enabled default: got false, want true")
	}
	if cfg.Metrics.Path != "/metrics" {
		t.Errorf("Metrics.Path default: got %q, want /metrics", cfg.Metrics.Path)
	}
}

func TestLoadConfig_MetricsOverrides(t *testing.T) {
	t.Setenv("WEAVE_METRICS_ENABLED", "false")
	t.Setenv("WEAVE_METRICS_PATH", "/internal/metrics")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Metrics.Enabled {
		t.Errorf("Metrics.Enabled: got true, want false")
	}
	if cfg.Metrics.Path != "/internal/metrics" {
		t.Errorf("Metrics.Path: got %q", cfg.Metrics.Path)
	}
}

func TestLoadConfig_TracingDefaults(t *testing.T) {
	os.Unsetenv("WEAVE_TRACING_ENABLED")
	os.Unsetenv("WEAVE_TRACING_SERVICE_NAME")
	os.Unsetenv("WEAVE_OTLP_ENDPOINT")
	os.Unsetenv("WEAVE_TRACING_EXPORTER")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tracing.Enabled {
		t.Errorf("Tracing.Enabled default: got true, want false")
	}
	if cfg.Tracing.ServiceName != "weave" {
		t.Errorf("Tracing.ServiceName default: got %q, want weave", cfg.Tracing.ServiceName)
	}
	if cfg.Tracing.OTLPEndpoint != "" {
		t.Errorf("Tracing.OTLPEndpoint default: got %q, want empty", cfg.Tracing.OTLPEndpoint)
	}
}

func TestLoadConfig_TracingOverrides(t *testing.T) {
	t.Setenv("WEAVE_TRACING_ENABLED", "true")
	t.Setenv("WEAVE_TRACING_SERVICE_NAME", "weave-prod")
	t.Setenv("WEAVE_OTLP_ENDPOINT", "otel-collector:4318")
	t.Setenv("WEAVE_TRACING_EXPORTER", "otlp")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Tracing.Enabled {
		t.Errorf("Tracing.Enabled: got false, want true")
	}
	if cfg.Tracing.ServiceName != "weave-prod" {
		t.Errorf("Tracing.ServiceName: got %q", cfg.Tracing.ServiceName)
	}
	if cfg.Tracing.OTLPEndpoint != "otel-collector:4318" {
		t.Errorf("Tracing.OTLPEndpoint: got %q", cfg.Tracing.OTLPEndpoint)
	}
	if cfg.Tracing.Exporter != "otlp" {
		t.Errorf("Tracing.Exporter: got %q", cfg.Tracing.Exporter)
	}
}

// --- Tier 3.2 functions config tests ---

func TestLoadConfig_FunctionsDefaults(t *testing.T) {
	os.Unsetenv("WEAVE_FUNCTIONS_ENABLED")
	os.Unsetenv("WEAVE_FUNCTIONS_BASE_URL")
	os.Unsetenv("WEAVE_FUNCTIONS_TIMEOUT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Functions.Enabled {
		t.Errorf("Functions.Enabled default: got true, want false")
	}
	if cfg.Functions.BaseURL != "" {
		t.Errorf("Functions.BaseURL default: got %q, want empty", cfg.Functions.BaseURL)
	}
	if cfg.Functions.Timeout != 30*time.Second {
		t.Errorf("Functions.Timeout default: got %v, want 30s", cfg.Functions.Timeout)
	}
}

func TestLoadConfig_FunctionsOverrides(t *testing.T) {
	t.Setenv("WEAVE_FUNCTIONS_ENABLED", "true")
	t.Setenv("WEAVE_FUNCTIONS_BASE_URL", "http://functions.local:9000/functions")
	t.Setenv("WEAVE_FUNCTIONS_TIMEOUT", "5s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Functions.Enabled {
		t.Errorf("Functions.Enabled: got false, want true")
	}
	if cfg.Functions.BaseURL != "http://functions.local:9000/functions" {
		t.Errorf("Functions.BaseURL: got %q", cfg.Functions.BaseURL)
	}
	if cfg.Functions.Timeout != 5*time.Second {
		t.Errorf("Functions.Timeout: got %v, want 5s", cfg.Functions.Timeout)
	}
}

func TestLoadConfig_InvalidFunctionsTimeout(t *testing.T) {
	t.Setenv("WEAVE_FUNCTIONS_TIMEOUT", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid functions timeout")
	}
}

func TestLoadConfig_InvalidFunctionsEnabled(t *testing.T) {
	t.Setenv("WEAVE_FUNCTIONS_ENABLED", "not-a-bool")
	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid functions enabled flag")
	}
}

func TestConfig_Validate_FunctionsEnabledRequiresBaseURL(t *testing.T) {
	cfg := validDevConfig()
	cfg.Functions.Enabled = true
	cfg.Functions.BaseURL = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error when functions enabled without base URL")
	}
	if !strings.Contains(err.Error(), "Functions") && !strings.Contains(err.Error(), "function") {
		t.Errorf("expected error to mention functions, got %v", err)
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

// US-246: OIDC config loading + validation.

func TestLoadConfig_OIDC_AuthModeShortcut(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("WEAVE_OIDC_ISSUER", "https://keycloak.example.com/realms/weave")
	t.Setenv("WEAVE_OIDC_CLIENT_ID", "weave-client")
	t.Setenv("WEAVE_OIDC_CLIENT_SECRET", "shh")
	t.Setenv("WEAVE_OIDC_REDIRECT_URL", "https://weave.example.com/api/auth/oidc/callback")
	t.Setenv("WEAVE_OIDC_SCOPES", "openid,email,profile,groups")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.OIDC.Enabled {
		t.Error("AUTH_MODE=oidc should set OIDC.Enabled=true")
	}
	if cfg.OIDC.IssuerURL != "https://keycloak.example.com/realms/weave" {
		t.Errorf("IssuerURL=%q", cfg.OIDC.IssuerURL)
	}
	if cfg.OIDC.ClientID != "weave-client" {
		t.Errorf("ClientID=%q", cfg.OIDC.ClientID)
	}
	if cfg.OIDC.ClientSecret != "shh" {
		t.Errorf("ClientSecret=%q", cfg.OIDC.ClientSecret)
	}
	want := []string{"openid", "email", "profile", "groups"}
	if len(cfg.OIDC.Scopes) != len(want) {
		t.Fatalf("Scopes=%v, want %v", cfg.OIDC.Scopes, want)
	}
	for i, s := range want {
		if cfg.OIDC.Scopes[i] != s {
			t.Errorf("Scopes[%d]=%q, want %q", i, cfg.OIDC.Scopes[i], s)
		}
	}
}

func TestLoadConfig_OIDC_DisabledByDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.Enabled {
		t.Error("OIDC should default to Enabled=false")
	}
}

func TestConfig_Validate_OIDC_MissingFields(t *testing.T) {
	cfg := validDevConfig()
	cfg.OIDC.Enabled = true
	// Intentionally leave IssuerURL / ClientID / ClientSecret / RedirectURL unset.

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error when OIDC.Enabled=true but no creds set")
	}
	msg := err.Error()
	for _, need := range []string{
		"WEAVE_OIDC_ISSUER",
		"WEAVE_OIDC_CLIENT_ID",
		"WEAVE_OIDC_CLIENT_SECRET",
		"WEAVE_OIDC_REDIRECT_URL",
	} {
		if !strings.Contains(msg, need) {
			t.Errorf("expected error to mention %s, got: %v", need, err)
		}
	}
}

func TestConfig_Validate_OIDC_FullyPopulatedPasses(t *testing.T) {
	cfg := validDevConfig()
	cfg.OIDC = OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://idp.example.com",
		ClientID:     "weave",
		ClientSecret: "shh",
		RedirectURL:  "https://weave.example.com/api/auth/oidc/callback",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected OIDC-full config to validate, got: %v", err)
	}
}

// US-248: SAML config loading + validation.

func TestLoadConfig_SAML_AuthModeShortcut(t *testing.T) {
	t.Setenv("AUTH_MODE", "saml")
	t.Setenv("WEAVE_SAML_IDP_SSO_URL", "https://idp.example.com/sso")
	t.Setenv("WEAVE_SAML_IDP_ISSUER", "https://idp.example.com")
	t.Setenv("WEAVE_SAML_IDP_CERT_PEM", "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----")
	t.Setenv("WEAVE_SAML_SP_ENTITY_ID", "https://weave.example.com")
	t.Setenv("WEAVE_SAML_SP_ACS_URL", "https://weave.example.com/api/auth/saml/acs")
	t.Setenv("WEAVE_SAML_ATTRIBUTE_EMAIL", "mail")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SAML.Enabled {
		t.Error("AUTH_MODE=saml should set SAML.Enabled=true")
	}
	if cfg.SAML.IdPSSOURL != "https://idp.example.com/sso" {
		t.Errorf("IdPSSOURL=%q", cfg.SAML.IdPSSOURL)
	}
	if cfg.SAML.SPACSURL != "https://weave.example.com/api/auth/saml/acs" {
		t.Errorf("SPACSURL=%q", cfg.SAML.SPACSURL)
	}
	if cfg.SAML.AttributeEmail != "mail" {
		t.Errorf("AttributeEmail=%q", cfg.SAML.AttributeEmail)
	}
}

func TestLoadConfig_SAML_DisabledByDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SAML.Enabled {
		t.Error("SAML should default to Enabled=false")
	}
}

func TestConfig_Validate_SAML_MissingFields(t *testing.T) {
	cfg := validDevConfig()
	cfg.SAML.Enabled = true

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error when SAML.Enabled=true but config is empty")
	}
	msg := err.Error()
	for _, need := range []string{
		"WEAVE_SAML_IDP_SSO_URL",
		"WEAVE_SAML_IDP_ISSUER",
		"WEAVE_SAML_IDP_CERT_PEM",
		"WEAVE_SAML_SP_ENTITY_ID",
		"WEAVE_SAML_SP_ACS_URL",
	} {
		if !strings.Contains(msg, need) {
			t.Errorf("expected error to mention %s, got: %v", need, err)
		}
	}
}

func TestConfig_Validate_SAML_FullyPopulatedPasses(t *testing.T) {
	cfg := validDevConfig()
	cfg.SAML = SAMLConfig{
		Enabled:           true,
		IdPSSOURL:         "https://idp.example.com/sso",
		IdPIssuer:         "https://idp.example.com",
		IdPCertificatePEM: "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		SPEntityID:        "https://weave.example.com",
		SPACSURL:          "https://weave.example.com/api/auth/saml/acs",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected SAML-full config to validate, got: %v", err)
	}
}

// US-252: LDAP config loading + validation.

func TestLoadConfig_LDAP_AuthModeShortcut(t *testing.T) {
	t.Setenv("AUTH_MODE", "ldap")
	t.Setenv("WEAVE_LDAP_URL", "ldap://dc.example.com:389")
	t.Setenv("WEAVE_LDAP_USER_BASE_DN", "ou=users,dc=example,dc=com")
	t.Setenv("WEAVE_LDAP_GROUP_BASE_DN", "ou=groups,dc=example,dc=com")
	t.Setenv("WEAVE_LDAP_BIND_DN", "cn=svc,dc=example,dc=com")
	t.Setenv("WEAVE_LDAP_BIND_PASSWORD", "shh")
	t.Setenv("WEAVE_LDAP_INTERVAL", "30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.LDAP.Enabled {
		t.Error("AUTH_MODE=ldap should set LDAP.Enabled=true")
	}
	if cfg.LDAP.URL != "ldap://dc.example.com:389" {
		t.Errorf("URL=%q", cfg.LDAP.URL)
	}
	if cfg.LDAP.UserBaseDN != "ou=users,dc=example,dc=com" {
		t.Errorf("UserBaseDN=%q", cfg.LDAP.UserBaseDN)
	}
	if cfg.LDAP.BindDN != "cn=svc,dc=example,dc=com" {
		t.Errorf("BindDN=%q", cfg.LDAP.BindDN)
	}
	if cfg.LDAP.Interval != 30*time.Minute {
		t.Errorf("Interval=%s, want 30m", cfg.LDAP.Interval)
	}
}

func TestLoadConfig_LDAP_DisabledByDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LDAP.Enabled {
		t.Error("LDAP should default to Enabled=false")
	}
}

func TestConfig_Validate_LDAP_MissingFields(t *testing.T) {
	cfg := validDevConfig()
	cfg.LDAP.Enabled = true

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error when LDAP.Enabled=true but no URL/BaseDN set")
	}
	msg := err.Error()
	for _, need := range []string{
		"WEAVE_LDAP_URL",
		"WEAVE_LDAP_USER_BASE_DN",
	} {
		if !strings.Contains(msg, need) {
			t.Errorf("expected error to mention %s, got: %v", need, err)
		}
	}
}

func TestConfig_Validate_LDAP_FullyPopulatedPasses(t *testing.T) {
	cfg := validDevConfig()
	cfg.LDAP = LDAPConfig{
		Enabled:    true,
		URL:        "ldaps://dc.example.com:636",
		UserBaseDN: "ou=users,dc=example,dc=com",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected LDAP-full config to validate, got: %v", err)
	}
}
