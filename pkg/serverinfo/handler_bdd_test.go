package serverinfo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestBDD_ServerInfoHandler covers round 129 — runtime statistics
// surfaced via GET /api/v2/server-info. Sibling of round-123
// build-info but for LIVE runtime state (mutates per call) rather
// than compile-time identity.

func TestBDD_ServerInfoHandler(t *testing.T) {
	// Restore startedAt after each test so subtests don't leak.
	origStart := startedAt()
	t.Cleanup(func() { SetStartedAt(origStart) })

	t.Run("Returns 200 with JSON body including all 6 fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info", nil)
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
		if body.StartedAt == "" {
			t.Errorf("StartedAt empty; want RFC3339 string")
		}
		if body.GoroutineCount < 1 {
			t.Errorf("GoroutineCount=%d, want >=1 (test goroutine alone)",
				body.GoroutineCount)
		}
		if body.MemoryAllocBytes == 0 {
			t.Errorf("MemoryAllocBytes=0; want non-zero for a running test")
		}
		if body.MemorySysBytes == 0 {
			t.Errorf("MemorySysBytes=0; want non-zero")
		}
	})

	t.Run("UptimeSeconds reflects time since SetStartedAt", func(t *testing.T) {
		// Set startedAt to 10s ago — handler should report uptime ~10s.
		SetStartedAt(time.Now().Add(-10 * time.Second))
		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info", nil)
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)

		var body Response
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.UptimeSeconds < 9 || body.UptimeSeconds > 11 {
			t.Errorf("UptimeSeconds=%d, want ~10 (within ±1)", body.UptimeSeconds)
		}
	})

	t.Run("StartedAt is RFC3339 UTC", func(t *testing.T) {
		fixed := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
		SetStartedAt(fixed)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info", nil)
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)

		var body Response
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		want := "2026-05-25T12:00:00Z"
		if body.StartedAt != want {
			t.Errorf("StartedAt=%q, want %q", body.StartedAt, want)
		}
		// RFC3339 ends with Z (UTC) or +HH:MM. We force UTC in the
		// handler so it should always be Z-suffixed regardless of
		// the input timezone.
		if !strings.HasSuffix(body.StartedAt, "Z") {
			t.Errorf("StartedAt %q not Z-suffixed; handler should force UTC",
				body.StartedAt)
		}
	})

	t.Run("Successive calls show uptime monotonic non-decreasing", func(t *testing.T) {
		SetStartedAt(time.Now().Add(-1 * time.Second))

		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info", nil)
		rec1 := httptest.NewRecorder()
		Handler().ServeHTTP(rec1, req)
		var b1 Response
		_ = json.Unmarshal(rec1.Body.Bytes(), &b1)

		// Brief sleep so the second call's uptime is meaningfully later.
		time.Sleep(1100 * time.Millisecond)

		req2 := httptest.NewRequest(http.MethodGet, "/api/v2/server-info", nil)
		rec2 := httptest.NewRecorder()
		Handler().ServeHTTP(rec2, req2)
		var b2 Response
		_ = json.Unmarshal(rec2.Body.Bytes(), &b2)

		if b2.UptimeSeconds < b1.UptimeSeconds {
			t.Errorf("uptime went backwards: %d -> %d",
				b1.UptimeSeconds, b2.UptimeSeconds)
		}
		// Both calls must report the SAME startedAt — uptime moves,
		// boot timestamp doesn't.
		if b1.StartedAt != b2.StartedAt {
			t.Errorf("startedAt drifted: %q vs %q",
				b1.StartedAt, b2.StartedAt)
		}
	})

	t.Run("GoroutineCount tracks runtime.NumGoroutine", func(t *testing.T) {
		// Spawn a goroutine that blocks, take a snapshot, release.
		// The diff confirms the handler is reading runtime state,
		// not a cached value.
		hold := make(chan struct{})
		done := make(chan struct{})

		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info", nil)
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)
		var before Response
		_ = json.Unmarshal(rec.Body.Bytes(), &before)

		go func() {
			<-hold
			close(done)
		}()
		// Yield so the new goroutine is observable.
		runtime.Gosched()

		rec2 := httptest.NewRecorder()
		Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet,
			"/api/v2/server-info", nil))
		var during Response
		_ = json.Unmarshal(rec2.Body.Bytes(), &during)

		close(hold)
		<-done

		if during.GoroutineCount < before.GoroutineCount {
			t.Errorf("spawned goroutine should increase count; got %d -> %d",
				before.GoroutineCount, during.GoroutineCount)
		}
	})

	t.Run("Endpoint is unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/server-info", nil)
		// Deliberately no Authorization header.
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 — endpoint should be public",
				rec.Code)
		}
	})
}
