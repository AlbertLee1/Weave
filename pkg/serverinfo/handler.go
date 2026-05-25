// Package serverinfo exposes runtime statistics via a small HTTP
// handler. Round 129 sibling of pkg/buildinfo: where build-info is
// about the binary's compile-time identity (immutable), server-info
// is about the LIVE runtime state (mutates on every call). On-call
// hits this to answer "how many goroutines? how much heap?" without
// a Prometheus scrape or restart, paired with build-info's "which
// commit" for full debug context.
package serverinfo

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// startedState carries the boot-time timestamp the Uptime field
// is measured against. SetStartedAt is intended to be called once
// at server boot from cmd/server/main.go; tests can override
// per-case. Default is the package's import time so the handler
// always has a sensible value even if main.go forgets to wire it.
var startedState = struct {
	mu sync.RWMutex
	t  time.Time
}{t: time.Now()}

// SetStartedAt replaces the boot timestamp the Uptime field is
// measured against. Idempotent — callable from tests too via
// t.Cleanup restore.
func SetStartedAt(t time.Time) {
	startedState.mu.Lock()
	startedState.t = t
	startedState.mu.Unlock()
}

// startedAt returns the current boot timestamp. Package-private
// helper exposed for tests in the same package.
func startedAt() time.Time {
	startedState.mu.RLock()
	defer startedState.mu.RUnlock()
	return startedState.t
}

// Response is the JSON body of GET /api/v2/server-info. All
// numeric fields are unsigned-int-shaped on the wire (JSON
// numbers); the SPA / on-call tools parse them as int64. uptime
// is seconds-since-startedAt (integer for stable diff vs RFC3339
// strings); started_at is RFC3339 with timezone for human
// readability.
type Response struct {
	StartedAt        string `json:"startedAt"`
	UptimeSeconds    int64  `json:"uptimeSeconds"`
	GoroutineCount   int    `json:"goroutineCount"`
	MemoryAllocBytes uint64 `json:"memoryAllocBytes"`
	MemorySysBytes   uint64 `json:"memorySysBytes"`
	GCCycles         uint32 `json:"gcCycles"`
}

// Handler returns an http.Handler for GET /api/v2/server-info.
// Public unauthenticated — runtime stats are not secrets and the
// on-call frequently needs them without a token, paired with
// /api/v2/build-info for full debug context. Foundry-parity
// convention: server-state endpoints surface unauthenticated.
//
// uptime is measured against startedAt at request time so
// successive calls show steady increase. MemStats is read via
// runtime.ReadMemStats — note this stops-the-world for a brief
// moment, which is fine for an infrequently-hit debug endpoint.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := startedAt()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		resp := Response{
			StartedAt:        started.UTC().Format(time.RFC3339),
			UptimeSeconds:    int64(time.Since(started).Seconds()),
			GoroutineCount:   runtime.NumGoroutine(),
			MemoryAllocBytes: m.Alloc,
			MemorySysBytes:   m.Sys,
			GCCycles:         m.NumGC,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
