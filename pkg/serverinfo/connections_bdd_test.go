package serverinfo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBDD_ConnectionsHandler covers round 131 — PG pool + NATS
// connection state snapshot. On-call answers "is the DB pool
// healthy? is NATS still connected?" without a Prometheus scrape.

func TestBDD_ConnectionsHandler(t *testing.T) {
	// Restore provider after each test so subtests don't leak.
	t.Cleanup(func() { SetStatsProvider(nil) })

	t.Run("Returns 200 with both services populated", func(t *testing.T) {
		SetStatsProvider(func() ConnectionStats {
			return ConnectionStats{
				Postgres: &PostgresStats{
					AcquiredConns: 5,
					IdleConns:     3,
					TotalConns:    8,
					MaxConns:      20,
					NewConnsCount: 100,
				},
				NATS: &NATSStats{
					Status:    "CONNECTED",
					ServerURL: "nats://localhost:4222",
					InMsgs:    1000,
					OutMsgs:   500,
				},
			}
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info/connections", nil)
		rec := httptest.NewRecorder()
		ConnectionsHandler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var body ConnectionStats
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Postgres == nil {
			t.Fatalf("postgres nil; want populated")
		}
		if body.Postgres.AcquiredConns != 5 {
			t.Errorf("AcquiredConns=%d, want 5", body.Postgres.AcquiredConns)
		}
		if body.Postgres.MaxConns != 20 {
			t.Errorf("MaxConns=%d, want 20", body.Postgres.MaxConns)
		}
		if body.NATS == nil {
			t.Fatalf("nats nil; want populated")
		}
		if body.NATS.Status != "CONNECTED" {
			t.Errorf("Status=%q, want CONNECTED", body.NATS.Status)
		}
		if body.NATS.InMsgs != 1000 {
			t.Errorf("InMsgs=%d, want 1000", body.NATS.InMsgs)
		}
	})

	t.Run("Degraded boot (no provider) returns nulls", func(t *testing.T) {
		// Critical contract — degraded server (no PG, no NATS) emits
		// {"postgres": null, "nats": null} so the SPA renders
		// "service not configured" rather than crashing.
		SetStatsProvider(nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info/connections", nil)
		rec := httptest.NewRecorder()
		ConnectionsHandler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200 (degraded boot is not an error)",
				rec.Code)
		}
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		if raw["postgres"] != nil {
			t.Errorf("postgres=%v, want null", raw["postgres"])
		}
		if raw["nats"] != nil {
			t.Errorf("nats=%v, want null", raw["nats"])
		}
	})

	t.Run("Per-service nullability — PG up, NATS down", func(t *testing.T) {
		// Partial boot: PG configured but NATS not. The split lets
		// on-call see exactly which service is degraded.
		SetStatsProvider(func() ConnectionStats {
			return ConnectionStats{
				Postgres: &PostgresStats{AcquiredConns: 1, MaxConns: 10},
				NATS:     nil,
			}
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info/connections", nil)
		rec := httptest.NewRecorder()
		ConnectionsHandler().ServeHTTP(rec, req)
		var body ConnectionStats
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Postgres == nil {
			t.Errorf("postgres should be populated")
		}
		if body.NATS != nil {
			t.Errorf("nats should be null in this scenario; got %+v", body.NATS)
		}
	})

	t.Run("Provider invoked per request (not cached)", func(t *testing.T) {
		// Round-131 contract: each request re-reads live counters.
		// Confirm by mutating a captured value between calls.
		var counter int32
		SetStatsProvider(func() ConnectionStats {
			counter++
			return ConnectionStats{
				Postgres: &PostgresStats{AcquiredConns: counter},
			}
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info/connections", nil)
		rec1 := httptest.NewRecorder()
		ConnectionsHandler().ServeHTTP(rec1, req)
		var b1 ConnectionStats
		_ = json.Unmarshal(rec1.Body.Bytes(), &b1)

		rec2 := httptest.NewRecorder()
		ConnectionsHandler().ServeHTTP(rec2, req)
		var b2 ConnectionStats
		_ = json.Unmarshal(rec2.Body.Bytes(), &b2)

		if b1.Postgres.AcquiredConns == b2.Postgres.AcquiredConns {
			t.Errorf("provider not invoked per request; got same value %d twice",
				b1.Postgres.AcquiredConns)
		}
		if b2.Postgres.AcquiredConns <= b1.Postgres.AcquiredConns {
			t.Errorf("counter went backwards: %d -> %d",
				b1.Postgres.AcquiredConns, b2.Postgres.AcquiredConns)
		}
	})

	t.Run("Endpoint is unauthenticated", func(t *testing.T) {
		SetStatsProvider(func() ConnectionStats {
			return ConnectionStats{}
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info/connections", nil)
		rec := httptest.NewRecorder()
		ConnectionsHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 — endpoint should be public", rec.Code)
		}
	})

	t.Run("SetStatsProvider(nil) clears prior registration", func(t *testing.T) {
		// Idempotency contract — setter is callable multiple times.
		SetStatsProvider(func() ConnectionStats {
			return ConnectionStats{
				Postgres: &PostgresStats{AcquiredConns: 99},
			}
		})
		SetStatsProvider(nil) // clear

		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info/connections", nil)
		rec := httptest.NewRecorder()
		ConnectionsHandler().ServeHTTP(rec, req)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		if raw["postgres"] != nil {
			t.Errorf("postgres should be null after nil-clear; got %v",
				raw["postgres"])
		}
	})
}
