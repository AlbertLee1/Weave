package buildinfo

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
	"sort"
)

// Dependency is one row in the round-125 dependencies response.
// Mirrors the runtime/debug.Module shape but flattens Replace into
// a sibling field so wire consumers don't have to walk a recursive
// linked list — the only piece on-call ever cares about is "what
// is the EFFECTIVE version" (post-replace) so we surface that
// directly while preserving the original path for traceability.
type Dependency struct {
	Path string `json:"path"`
	// Version is the effective version: if the original module had
	// a `replace` directive in go.mod, this carries the replacement
	// version (or "(devel)" for a local-path replace). Otherwise
	// it's the upstream version. Always populated.
	Version string `json:"version"`
	// Sum is the H1 module checksum when available. Empty for
	// modules without a go.sum entry (typically the main module).
	Sum string `json:"sum,omitempty"`
	// Replace, when non-empty, carries the path of the replacement
	// module — surfaces "we pinned chi from upstream to our fork"
	// without forcing callers to diff Version strings against
	// known-upstream values.
	Replace string `json:"replace,omitempty"`
}

// DependenciesResponse is the JSON body of
// GET /api/v2/build-info/dependencies. Sorted by Path so the wire
// output is stable across calls (debug.ReadBuildInfo's natural
// order matches build-time module-graph traversal, which is
// deterministic but unfriendly to humans diffing two snapshots).
type DependenciesResponse struct {
	Dependencies []Dependency `json:"dependencies"`
}

// DependenciesHandler returns an http.Handler for
// GET /api/v2/build-info/dependencies — round-125 incident-triage
// surface. The on-call frequently needs "which version of pgx do we
// have running?" without rebuilding or grepping go.sum; this
// endpoint reads from the binary's own embedded build info
// (runtime/debug.ReadBuildInfo), so the answer is whatever was
// baked in at compile time even if go.sum has since drifted.
//
// Same security posture as the sibling /api/v2/build-info (round
// 123): public unauthenticated, no Authorization inspection.
// Module names + versions are not secrets and the on-call shouldn't
// need a token to triage.
//
// When runtime/debug.ReadBuildInfo() returns ok=false (the binary
// was built without module info — rare; only happens on very old
// Go toolchains or when explicitly stripped), the handler returns
// an empty array rather than 500: the response stays well-formed.
func DependenciesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(DependenciesResponse{
			Dependencies: collectDependencies(),
		})
	})
}

// collectDependencies walks runtime/debug.ReadBuildInfo and returns
// a stable-sorted Dependency slice. Extracted so tests can pin the
// exact shape (sort order, replace-resolution) without bringing up
// a full HTTP handler.
func collectDependencies() []Dependency {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return []Dependency{}
	}
	out := make([]Dependency, 0, len(info.Deps))
	for _, mod := range info.Deps {
		if mod == nil {
			continue
		}
		d := Dependency{
			Path:    mod.Path,
			Version: mod.Version,
			Sum:     mod.Sum,
		}
		if mod.Replace != nil {
			// A replace directive: the EFFECTIVE version is the
			// replacement's. Surface both so callers can tell at a
			// glance "this isn't upstream pgx, it's our fork".
			d.Version = mod.Replace.Version
			d.Sum = mod.Replace.Sum
			d.Replace = mod.Replace.Path
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}
