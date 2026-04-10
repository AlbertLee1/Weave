package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/internal/config"
)

func TestConfig_Validate_TokenModeRejected(t *testing.T) {
	cfg := &config.Config{
		Port:     9117,
		LogLevel: "info",
		DataDir:  "./data",
		AuthMode: "token",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for AUTH_MODE=token (deprecated)")
	}
}

func TestCORSConfig_LoadedFromEnv(t *testing.T) {
	t.Setenv("WEAVE_CORS_ORIGINS", "https://app.example.com,https://admin.example.com")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Fatalf("CORSOrigins: got %d entries, want 2", len(cfg.CORSOrigins))
	}
	if cfg.CORSOrigins[0] != "https://app.example.com" {
		t.Errorf("CORSOrigins[0]: got %q", cfg.CORSOrigins[0])
	}
	if cfg.CORSOrigins[1] != "https://admin.example.com" {
		t.Errorf("CORSOrigins[1]: got %q", cfg.CORSOrigins[1])
	}
}

func TestCORSConfig_DefaultEmpty(t *testing.T) {
	t.Setenv("WEAVE_CORS_ORIGINS", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.CORSOrigins) != 0 {
		t.Errorf("CORSOrigins default: got %d entries, want 0", len(cfg.CORSOrigins))
	}
}

func TestFullRouter_SecurityHeaders(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options: got %q, want nosniff", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options: got %q, want DENY", got)
	}
}
