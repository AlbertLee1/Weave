package serverinfo

import (
	"encoding/json"
	"net/http"
	"sync"
)

// PostgresStats is a thin wire-format projection of pgxpool.Stat(). All
// fields are int32-shaped on the wire (JSON numbers). On-call hits
// this to answer "is the DB pool healthy?" — high acquired vs max,
// or many max-lifetime-destroyed in a short window, usually means
// the app is leaking or PG is rejecting connections.
type PostgresStats struct {
	AcquiredConns              int32 `json:"acquiredConns"`
	IdleConns                  int32 `json:"idleConns"`
	TotalConns                 int32 `json:"totalConns"`
	MaxConns                   int32 `json:"maxConns"`
	NewConnsCount              int64 `json:"newConnsCount"`
	MaxLifetimeDestroyCount    int64 `json:"maxLifetimeDestroyCount"`
	MaxIdleDestroyCount        int64 `json:"maxIdleDestroyCount"`
}

// NATSStats wraps nats.Conn.Status + a couple of useful counters
// (in/out msgs). Connected/Reconnecting/Closed/etc. all stringify
// to one of nats.Status.String() values; we pass it through as
// uppercase so JS callers can `if (stats.status === 'CONNECTED')`.
type NATSStats struct {
	Status     string `json:"status"`
	ServerURL  string `json:"serverUrl,omitempty"`
	InMsgs     uint64 `json:"inMsgs"`
	OutMsgs    uint64 `json:"outMsgs"`
	Reconnects uint64 `json:"reconnects"`
}

// ConnectionStats bundles all per-service snapshots. Each pointer
// is nullable: a degraded-boot server (no PG, no NATS) emits
// {"postgres": null, "nats": null} so the SPA can render "PG not
// configured" without crashing.
type ConnectionStats struct {
	Postgres *PostgresStats `json:"postgres"`
	NATS     *NATSStats     `json:"nats"`
}

// StatsProvider is called by the handler at request time to read
// live stats. Pattern: handler stays free of pgx/nats imports;
// cmd/server registers a closure that captures the live pgxpool.Pool
// + nats.Conn and reads from them. Per-request execution means
// successive calls show the latest counter values.
type StatsProvider func() ConnectionStats

var statsState = struct {
	mu       sync.RWMutex
	provider StatsProvider
}{}

// SetStatsProvider registers the per-request stats producer. Pass
// nil to clear (handler will emit {postgres: null, nats: null}).
// Safe to call multiple times — latest wins so tests can swap per
// case.
func SetStatsProvider(p StatsProvider) {
	statsState.mu.Lock()
	statsState.provider = p
	statsState.mu.Unlock()
}

func currentProvider() StatsProvider {
	statsState.mu.RLock()
	defer statsState.mu.RUnlock()
	return statsState.provider
}

// ConnectionsHandler returns an http.Handler for
// GET /api/v2/server-info/connections — round-131 connection-pool
// health snapshot. Sibling of round-129 server-info; same public
// unauthenticated convention.
//
// Degraded boot (no provider registered) returns
// {"postgres": null, "nats": null} so the wire response stays
// well-formed; SPA renders "service not configured" rather than
// crashing on missing fields. Per-service nullability lets a
// partial boot (PG up, NATS down) surface accurately.
func ConnectionsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var stats ConnectionStats
		if p := currentProvider(); p != nil {
			stats = p()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(stats)
	})
}
