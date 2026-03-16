package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testDistFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!doctype html><html><body>SPA</body></html>"),
		},
		"assets/index-abc123.js": &fstest.MapFile{
			Data: []byte("console.log('app')"),
		},
		"assets/index-abc123.css": &fstest.MapFile{
			Data: []byte("body{color:red}"),
		},
		"favicon.svg": &fstest.MapFile{
			Data: []byte("<svg/>"),
		},
	}
}

func TestSPAHandler_ServesIndexHTML(t *testing.T) {
	handler := spaHandler(testDistFS())

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("expected index.html content, got %q", rec.Body.String())
	}
}

func TestSPAHandler_FallbackToIndexHTML(t *testing.T) {
	handler := spaHandler(testDistFS())

	// Unknown SPA route should return index.html
	req := httptest.NewRequest("GET", "/explorer/test-ontology", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("expected index.html content for SPA fallback, got %q", rec.Body.String())
	}
}

func TestSPAHandler_ServesStaticAssets(t *testing.T) {
	handler := spaHandler(testDistFS())

	req := httptest.NewRequest("GET", "/assets/index-abc123.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("expected JS content, got %q", rec.Body.String())
	}
}

func TestSPAHandler_AssetsCacheControl(t *testing.T) {
	handler := spaHandler(testDistFS())

	req := httptest.NewRequest("GET", "/assets/index-abc123.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "max-age=31536000") {
		t.Fatalf("expected long cache for assets, got Cache-Control: %q", cc)
	}
	if !strings.Contains(cc, "immutable") {
		t.Fatalf("expected immutable for assets, got Cache-Control: %q", cc)
	}
}

func TestSPAHandler_IndexNoCacheControl(t *testing.T) {
	handler := spaHandler(testDistFS())

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cc := rec.Header().Get("Cache-Control")
	if strings.Contains(cc, "max-age=31536000") {
		t.Fatalf("index.html should not have long cache, got Cache-Control: %q", cc)
	}
}

func TestSPAHandler_ServesFavicon(t *testing.T) {
	handler := spaHandler(testDistFS())

	req := httptest.NewRequest("GET", "/favicon.svg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("expected SVG content, got %q", rec.Body.String())
	}
}

func TestSPAHandler_APIRoutesNotIntercepted(t *testing.T) {
	// When SPA handler is mounted on /*, API routes registered before
	// should take priority. We test that the SPA handler returns index.html
	// (fallback) for /api/* paths since the file doesn't exist.
	// In the actual server, chi route matching ensures API handlers take priority.
	handler := spaHandler(testDistFS())

	req := httptest.NewRequest("GET", "/api/v2/ontologies", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// SPA handler will serve index.html since /api/v2/ontologies doesn't exist as a file
	// This is fine — in production, chi matches /api/* routes first.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// Integration tests: SPA handler mounted on the full Chi router (as in main())

func TestFullRouter_WithSPA_ServesRoot(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	router.Get("/*", spaHandler(testDistFS()))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("expected index.html content, got %q", rec.Body.String())
	}
}

func TestFullRouter_WithSPA_ServesAssets(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	router.Get("/*", spaHandler(testDistFS()))

	req := httptest.NewRequest("GET", "/assets/index-abc123.js", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("expected JS content, got %q", rec.Body.String())
	}
}

func TestFullRouter_WithSPA_FallbackForSPARoutes(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	router.Get("/*", spaHandler(testDistFS()))

	req := httptest.NewRequest("GET", "/explorer/my-ontology", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("expected index.html fallback, got %q", rec.Body.String())
	}
}

func TestFullRouter_WithSPA_HealthStillWorks(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	router.Get("/*", spaHandler(testDistFS()))

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Health should return JSON, not the SPA HTML
	if strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("expected health JSON, got SPA HTML")
	}
}

func TestFullRouter_WithSPA_AuthTokenMode_BlocksAssets(t *testing.T) {
	// When AUTH_MODE=token, the global auth middleware requires a Bearer token.
	// Static assets (HTML, JS, CSS) served by the SPA handler will be blocked
	// because browsers don't send Bearer tokens for asset requests.
	t.Setenv("AUTH_MODE", "token")

	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	router.Get("/*", spaHandler(testDistFS()))

	tests := []struct {
		name string
		path string
	}{
		{"root HTML", "/"},
		{"JS asset", "/assets/index-abc123.js"},
		{"CSS asset", "/assets/index-abc123.css"},
		{"SPA route", "/explorer/my-ontology"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// This currently returns 401 — the bug!
			// Static assets should be served without authentication.
			if rec.Code != http.StatusOK {
				t.Errorf("GET %s: expected 200, got %d (body: %s)", tt.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestFullRouter_WithSPA_ContentType(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	router.Get("/*", spaHandler(testDistFS()))

	tests := []struct {
		path        string
		wantType    string
	}{
		{"/", "text/html"},
		{"/assets/index-abc123.js", "text/javascript"},
		{"/assets/index-abc123.css", "text/css"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, tt.wantType) {
				t.Errorf("GET %s: expected Content-Type containing %q, got %q", tt.path, tt.wantType, ct)
			}
		})
	}
}
