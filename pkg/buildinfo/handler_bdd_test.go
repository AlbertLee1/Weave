package buildinfo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// TestBDD_BuildInfoHandler covers round 123 — server build metadata
// surfaced via GET /api/v2/build-info.
//
// Contract:
//   - 200 OK with JSON body
//   - {version, commit, goVersion, buildTime} fields always present
//   - goVersion is runtime.Version() (not the package var — captured
//     at request time so a swap of Go toolchain shows up immediately
//     without rebuild)
//   - version / commit / buildTime are package vars (-ldflags
//     overridable); default "unknown" when not set
//   - Public — handler does not check authentication, the on-call
//     frequently needs build info without a token
func TestBDD_BuildInfoHandler(t *testing.T) {
	t.Run("Returns 200 with JSON body including all 4 fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info", nil)
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type=%q, want application/json", got)
		}

		var body Response
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
		}
		// All 4 fields populated — defaults for package vars cover
		// the local-dev case.
		if body.Version == "" {
			t.Errorf("Version empty; want non-empty (default or ldflags)")
		}
		if body.Commit == "" {
			t.Errorf("Commit empty; want non-empty")
		}
		if body.BuildTime == "" {
			t.Errorf("BuildTime empty; want non-empty")
		}
		if body.GoVersion == "" {
			t.Errorf("GoVersion empty; want runtime.Version()")
		}
		// goVersion captured live, NOT from package var.
		if body.GoVersion != runtime.Version() {
			t.Errorf("GoVersion=%q, want runtime.Version()=%q",
				body.GoVersion, runtime.Version())
		}
	})

	t.Run("Package var defaults survive when ldflags not applied", func(t *testing.T) {
		// Defaults ensure the response is well-formed during local
		// `go run ./cmd/server` without ldflags. This subtest pins
		// the default to "unknown" so a future PR doesn't silently
		// flip it to empty string (which would break wire-shape
		// guarantees and confuse on-call).
		if Version != "unknown" {
			t.Skipf("Version is %q — ldflags applied, skipping default check", Version)
		}
		if Commit != "unknown" || BuildTime != "unknown" {
			t.Errorf("default Commit/BuildTime drifted; got %q/%q",
				Commit, BuildTime)
		}
	})

	t.Run("ldflags override surfaces in response", func(t *testing.T) {
		// Save + override package vars to prove the handler reads
		// THEM not some constant. Restore on test teardown so other
		// subtests aren't polluted.
		origVersion, origCommit, origBuild := Version, Commit, BuildTime
		t.Cleanup(func() {
			Version, Commit, BuildTime = origVersion, origCommit, origBuild
		})
		Version = "9.9.9-test"
		Commit = "deadbeef"
		BuildTime = "2026-05-25T12:34:56Z"

		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info", nil)
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)

		var body Response
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Version != "9.9.9-test" {
			t.Errorf("Version=%q, want 9.9.9-test", body.Version)
		}
		if body.Commit != "deadbeef" {
			t.Errorf("Commit=%q, want deadbeef", body.Commit)
		}
		if body.BuildTime != "2026-05-25T12:34:56Z" {
			t.Errorf("BuildTime=%q, want override value", body.BuildTime)
		}
	})

	t.Run("Endpoint is unauthenticated (no Authorization header check)", func(t *testing.T) {
		// Handler must not inspect Authorization — build metadata
		// is public to the on-call regardless of token presence.
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info", nil)
		// Deliberately no Authorization header.
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 — endpoint should be public",
				rec.Code)
		}
	})

	t.Run("Other HTTP methods are not the handler's concern", func(t *testing.T) {
		// The handler accepts any method (it's just an http.HandlerFunc).
		// Method-based routing is the caller's concern — the chi router
		// in cmd/server/main.go mounts only GET. This subtest documents
		// the boundary so a future PR doesn't assume the handler does
		// method validation itself.
		req := httptest.NewRequest(http.MethodPost, "/api/v2/build-info", nil)
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 — method validation lives at the router", rec.Code)
		}
	})
}
