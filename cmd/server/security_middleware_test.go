package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddleware_PreflightReturnsCorrectHeaders(t *testing.T) {
	origins := []string{"https://app.example.com", "https://admin.example.com"}
	mw := CORSMiddleware(origins)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v2/ontologies", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("preflight: expected 204, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Allow-Origin: got %q, want %q", got, "https://app.example.com")
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Allow-Methods: expected non-empty")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Allow-Headers: expected non-empty")
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got == "" {
		t.Error("Max-Age: expected non-empty")
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials: got %q, want %q", got, "true")
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary: got %q, want %q", got, "Origin")
	}
}

func TestCORSMiddleware_RejectsDisallowedOrigin(t *testing.T) {
	origins := []string{"https://app.example.com"}
	mw := CORSMiddleware(origins)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v2/ontologies", nil)
	req.Header.Set("Origin", "https://evil.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin should not get Allow-Origin, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("disallowed origin should not get Allow-Credentials, got %q", got)
	}
}

func TestCORSMiddleware_SimpleRequestSetsOrigin(t *testing.T) {
	origins := []string{"https://app.example.com"}
	mw := CORSMiddleware(origins)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	req.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("simple request: expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Allow-Origin: got %q, want %q", got, "https://app.example.com")
	}
}

func TestCORSMiddleware_WildcardAllowsAll(t *testing.T) {
	origins := []string{"*"}
	mw := CORSMiddleware(origins)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("wildcard: Allow-Origin: got %q, want %q", got, "*")
	}
}

func TestCORSMiddleware_WildcardDoesNotAllowCredentials(t *testing.T) {
	mw := CORSMiddleware([]string{"*"})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tt := range []struct {
		name   string
		method string
	}{
		{name: "simple", method: http.MethodGet},
		{name: "preflight", method: http.MethodOptions},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v2/ontologies", nil)
			req.Header.Set("Origin", "https://preview.example.com")
			if tt.method == http.MethodOptions {
				req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("Allow-Origin: got %q, want %q", got, "*")
			}
			if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
				t.Fatalf("wildcard CORS must not allow credentials, got %q", got)
			}
		})
	}
}

func TestCORSMiddleware_NoOriginHeader(t *testing.T) {
	origins := []string{"https://app.example.com"}
	mw := CORSMiddleware(origins)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	// No Origin header
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("no origin: expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("no origin: expected no Allow-Origin header, got %q", got)
	}
}

func TestSecurityHeaders_SetsAllHeaders(t *testing.T) {
	mw := SecurityHeadersMiddleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	tests := []struct {
		header string
		want   string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Strict-Transport-Security", "max-age=63072000; includeSubDomains"},
		{"Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"},
	}

	for _, tt := range tests {
		if got := w.Header().Get(tt.header); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.header, got, tt.want)
		}
	}
}
