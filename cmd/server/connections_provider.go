package main

import (
	"github.com/liyang/weave/pkg/serverinfo"
)

// connectionsProvider returns a StatsProvider closure that reads
// live pgxpool + nats.Conn state at request time. Each per-service
// pointer is nil when the underlying dep is nil (degraded boot),
// matching round-131's per-service-nullability wire contract.
//
// Provider lives in cmd/server so pkg/serverinfo stays free of
// pgx/nats imports — same single-resolver pattern as the round-95
// auth ↔ oms bridge (cmd/server/ontology_me_resolver.go).
func connectionsProvider(deps *ServerDeps) serverinfo.StatsProvider {
	return func() serverinfo.ConnectionStats {
		var stats serverinfo.ConnectionStats
		if deps != nil && deps.PGPool != nil {
			s := deps.PGPool.Stat()
			stats.Postgres = &serverinfo.PostgresStats{
				AcquiredConns:           s.AcquiredConns(),
				IdleConns:               s.IdleConns(),
				TotalConns:              s.TotalConns(),
				MaxConns:                s.MaxConns(),
				NewConnsCount:           s.NewConnsCount(),
				MaxLifetimeDestroyCount: s.MaxLifetimeDestroyCount(),
				MaxIdleDestroyCount:     s.MaxIdleDestroyCount(),
			}
		}
		if deps != nil && deps.NATSConn != nil {
			nc := deps.NATSConn
			stats.NATS = &serverinfo.NATSStats{
				Status:     nc.Status().String(),
				ServerURL:  nc.ConnectedUrl(),
				InMsgs:     nc.Stats().InMsgs,
				OutMsgs:    nc.Stats().OutMsgs,
				Reconnects: nc.Stats().Reconnects,
			}
		}
		return stats
	}
}
