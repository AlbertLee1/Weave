package buildinfo

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
)

// Feature describes one optional server capability the SPA/SDK can
// detect at runtime. Round 127 — caller-side gating like "does this
// server have vertex wired?" or "is rid-versioning supported?" — runs
// against this manifest instead of poking endpoints for 404s.
//
// Description is human-readable (used by the SPA settings page);
// Reason is optional and explains WHY a feature is disabled (e.g.
// "PG pool not configured") — surfaces the actionable next step
// without forcing the operator to read server logs.
type Feature struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// FeaturesResponse is the JSON body of GET /api/v2/build-info/features.
// Always an array, never null — same defensive contract as round-125
// dependencies endpoint.
type FeaturesResponse struct {
	Features []Feature `json:"features"`
}

// featuresState is the package-level registry the handler reads from.
// Populated once at boot via SetFeatures; reads use RLock so the
// handler stays lock-free in the common case. Empty by default — the
// FeaturesHandler returns `[]` not null when no features have been
// registered (e.g. in tests or degraded mode).
var featuresState = struct {
	mu   sync.RWMutex
	list []Feature
}{}

// SetFeatures replaces the registered feature list. Intended to be
// called ONCE at server boot from cmd/server/main.go after the dep
// wiring decisions are final. Safe to call multiple times (latest
// wins) so tests can swap the list per-case.
//
// The input slice is copied so subsequent caller mutations don't
// race the handler's reads.
func SetFeatures(features []Feature) {
	cp := make([]Feature, len(features))
	copy(cp, features)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	featuresState.mu.Lock()
	featuresState.list = cp
	featuresState.mu.Unlock()
}

// currentFeatures returns a snapshot of the registered features.
// Exported as unexported package-private so tests in the same
// package can inspect state without going through HTTP.
func currentFeatures() []Feature {
	featuresState.mu.RLock()
	defer featuresState.mu.RUnlock()
	// Copy so callers can mutate without racing future SetFeatures.
	out := make([]Feature, len(featuresState.list))
	copy(out, featuresState.list)
	return out
}

// FeaturesHandler returns an http.Handler for
// GET /api/v2/build-info/features — round-127 capability discovery
// surface. Sibling of /api/v2/build-info (r123) and
// /api/v2/build-info/dependencies (r125).
//
// Public unauthenticated — capability flags are not secrets and the
// SPA needs them at page-load to decide which routes to render.
// Foundry-parity convention: feature flags surface via a public
// /capabilities endpoint, not gated by auth.
//
// Features are sorted by name in SetFeatures so the wire output is
// stable across calls — SPA / CI diffs read consistently regardless
// of which cmd/server boot path constructed the list.
func FeaturesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := currentFeatures()
		if out == nil {
			out = []Feature{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(FeaturesResponse{Features: out})
	})
}
