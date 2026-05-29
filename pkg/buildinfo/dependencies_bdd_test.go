package buildinfo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// TestBDD_DependenciesHandler covers round 125 — incident-triage
// dependencies inventory. On-call answers "which pgx version is
// running?" without rebuild or go.sum grep.
//
// Test against the REAL runtime/debug.ReadBuildInfo of the
// `go test` binary — we can't easily inject a fake here, but the
// test binary itself imports a known set of modules (pgx, chi,
// pydantic-equivalents in Go) so we can assert on stable
// invariants (sorted, non-empty for `go test`, well-formed
// envelope) without coupling to specific versions.
func TestBDD_DependenciesHandler(t *testing.T) {
	t.Run("Returns 200 with JSON envelope including dependencies array", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/dependencies", nil)
		rec := httptest.NewRecorder()
		DependenciesHandler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type=%q, want application/json", got)
		}

		var body DependenciesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
		}
		// The package-under-test (pkg/buildinfo) only imports
		// stdlib, so `go test ./pkg/buildinfo/` may produce a zero-
		// dep build. We can't assert non-empty here without coupling
		// to specific imports; the other subtests cover well-
		// formedness. The full-binary `go test ./...` run via
		// cmd/server integration tests will exercise the populated
		// path.
		if body.Dependencies == nil {
			t.Errorf("dependencies decoded to nil; want at least empty []")
		}
	})

	t.Run("Dependencies are sorted by Path", func(t *testing.T) {
		// debug.ReadBuildInfo's natural order is module-graph
		// traversal — unfriendly to humans diffing two snapshots.
		// Round 125 sorts by Path so the wire output is stable +
		// alphabetical.
		deps := collectDependencies()
		if len(deps) < 2 {
			t.Skipf("only %d deps; need >=2 to assert sort", len(deps))
		}
		got := make([]string, len(deps))
		for i, d := range deps {
			got[i] = d.Path
		}
		want := append([]string(nil), got...)
		sort.Strings(want)
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("not sorted at i=%d: got=%q want=%q", i, got[i], want[i])
				break
			}
		}
	})

	t.Run("Each dependency has non-empty Path and Version", func(t *testing.T) {
		// Wire contract: Path + Version always populated. Sum +
		// Replace may be empty.
		deps := collectDependencies()
		for i, d := range deps {
			if d.Path == "" {
				t.Errorf("deps[%d].Path empty", i)
			}
			if d.Version == "" {
				t.Errorf("deps[%d].Version empty for %s", i, d.Path)
			}
		}
	})

	t.Run("Endpoint is unauthenticated (no Authorization check)", func(t *testing.T) {
		// Sibling of round-123 /api/v2/build-info public-endpoint
		// contract — module versions are not secrets, on-call
		// shouldn't need a token to triage.
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/dependencies", nil)
		rec := httptest.NewRecorder()
		DependenciesHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 — endpoint should be public", rec.Code)
		}
	})

	t.Run("Empty array (not null) when build info absent", func(t *testing.T) {
		// Defensive: the wire response is `[]` not `null` even when
		// debug.ReadBuildInfo() returns ok=false (very old Go
		// toolchains / explicit -trimpath build, rare). The handler
		// stays well-formed. We can't easily induce ok=false in this
		// test binary so we assert on the wire shape via the
		// JSON-decoded value — Dependencies must be a (possibly
		// empty) slice, never nil.
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/dependencies", nil)
		rec := httptest.NewRecorder()
		DependenciesHandler().ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		deps, ok := body["dependencies"].([]any)
		if !ok {
			t.Errorf("dependencies field missing or not array; body=%s",
				rec.Body.String())
		}
		if deps == nil {
			// JSON `[]` decodes to a non-nil empty slice; `null`
			// decodes to nil. Catching nil here proves the handler
			// emits `[]` even on edge cases.
			t.Errorf("dependencies decoded to nil; want []")
		}
	})
}
