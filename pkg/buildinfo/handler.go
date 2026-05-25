// Package buildinfo exposes server build metadata via a small HTTP
// handler. Round 123 pivot — public unauthenticated endpoint useful
// for debugging "which version is the production server running"
// without grepping logs. Foundry-parity sibling of /healthz.
package buildinfo

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// Package-level vars are populated at build time via -ldflags:
//
//	go build -ldflags="-X github.com/liyang/weave/pkg/buildinfo.Version=1.2.3 \
//	                   -X github.com/liyang/weave/pkg/buildinfo.Commit=abc123 \
//	                   -X github.com/liyang/weave/pkg/buildinfo.BuildTime=2026-05-25T10:00:00Z"
//
// Defaults are "unknown" so callers see something sensible during
// local development without ldflags wiring.
var (
	Version   = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Response is the JSON shape returned by GET /api/v2/build-info.
// goVersion is always runtime.Version() — captures the Go toolchain
// the binary was built with, which matters for incident triage when
// runtime-specific bugs surface (e.g. a 1.22.x vs 1.23.x regression).
type Response struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	BuildTime string `json:"buildTime"`
}

// Handler returns an http.Handler for GET /api/v2/build-info. Unlike
// most /api/v2 routes this is intentionally unauthenticated — build
// metadata leaks no secrets and the on-call frequently needs to
// answer "which commit is this?" without an access token. Foundry's
// equivalent endpoint follows the same convention.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			Version:   Version,
			Commit:    Commit,
			GoVersion: runtime.Version(),
			BuildTime: BuildTime,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
