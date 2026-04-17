package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// usageTestRouter mounts UsageMiddleware on a chi router with a couple of
// simple routes. The OAuth identity is injected via a tiny helper
// middleware that populates auth.UserFromContext the same way
// auth.MiddlewareFull would in production.
func usageTestRouter(t *testing.T, store *UsageSampleStore, u *auth.User) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u != nil {
				ctx := auth.WithUser(r.Context(), u)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Use(UsageMiddleware(store))
	r.Get("/api/v2/things/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	return r
}

func TestUsageMiddleware_IncrementsCounterWithAppID(t *testing.T) {
	apiRequestsTotal.Reset()
	apiRequestDuration.Reset()

	u := &auth.User{
		ID: "user:alice",
		Attributes: map[string]any{
			auth.OAuthClientIDAttributeKey: "wapp_123",
		},
	}
	store := NewUsageSampleStore(time.Hour, 100)
	r := usageTestRouter(t, store, u)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/things/42", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}

	got := testutil.ToFloat64(apiRequestsTotal.WithLabelValues(
		"/api/v2/things/{id}", "GET", "200", "wapp_123"))
	if got != 3 {
		t.Fatalf("expected 3 GET 200 requests for wapp_123, got %v", got)
	}
}

func TestUsageMiddleware_AnonymousFallback(t *testing.T) {
	apiRequestsTotal.Reset()

	r := usageTestRouter(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/things/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	got := testutil.ToFloat64(apiRequestsTotal.WithLabelValues(
		"/api/v2/things/{id}", "GET", "200", AnonymousAppID))
	if got != 1 {
		t.Fatalf("anonymous path: expected 1 counter, got %v", got)
	}
}

func TestUsageMiddleware_CapturesErrorStatus(t *testing.T) {
	apiRequestsTotal.Reset()

	u := &auth.User{
		ID: "user:alice",
		Attributes: map[string]any{
			auth.OAuthClientIDAttributeKey: "wapp_err",
		},
	}
	r := usageTestRouter(t, nil, u)

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	got := testutil.ToFloat64(apiRequestsTotal.WithLabelValues(
		"/boom", "GET", "500", "wapp_err"))
	if got != 1 {
		t.Fatalf("500 path: expected 1 counter, got %v", got)
	}
}

func TestUsageMiddleware_RecordsSamplesPerApp(t *testing.T) {
	store := NewUsageSampleStore(time.Hour, 100)

	alice := &auth.User{ID: "user:alice", Attributes: map[string]any{
		auth.OAuthClientIDAttributeKey: "wapp_alice",
	}}
	bob := &auth.User{ID: "user:bob", Attributes: map[string]any{
		auth.OAuthClientIDAttributeKey: "wapp_bob",
	}}

	runRequest := func(u *auth.User, path string) {
		r := usageTestRouter(t, store, u)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}

	runRequest(alice, "/api/v2/things/1")
	runRequest(alice, "/api/v2/things/2")
	runRequest(bob, "/api/v2/things/3")

	if got := len(store.Snapshot("wapp_alice", time.Hour)); got != 2 {
		t.Fatalf("alice should have 2 samples, got %d", got)
	}
	if got := len(store.Snapshot("wapp_bob", time.Hour)); got != 1 {
		t.Fatalf("bob should have 1 sample, got %d", got)
	}
	// Anonymous requests should not populate the sample store even though
	// their Prometheus counters still fire.
	if got := len(store.Snapshot(AnonymousAppID, time.Hour)); got != 0 {
		t.Fatalf("anonymous samples should not be recorded, got %d", got)
	}
}

func TestUsageSampleStore_EvictsOlderThanRetention(t *testing.T) {
	store := NewUsageSampleStore(time.Hour, 100)
	anchor := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return anchor }

	// Stale sample (older than retention window) should be dropped on
	// append.
	store.Record("wapp_a", UsageSample{
		Timestamp: anchor.Add(-2 * time.Hour),
		Endpoint:  "/old",
		Method:    "GET",
		Status:    200,
		Duration:  time.Millisecond,
	})
	store.Record("wapp_a", UsageSample{
		Timestamp: anchor.Add(-10 * time.Minute),
		Endpoint:  "/fresh",
		Method:    "GET",
		Status:    200,
		Duration:  time.Millisecond,
	})

	got := store.Snapshot("wapp_a", time.Hour)
	if len(got) != 1 {
		t.Fatalf("expected 1 fresh sample after eviction, got %d", len(got))
	}
	if got[0].Endpoint != "/fresh" {
		t.Fatalf("expected /fresh survivor, got %q", got[0].Endpoint)
	}
}

func TestUsageSampleStore_CapsPerApp(t *testing.T) {
	store := NewUsageSampleStore(time.Hour, 3)
	anchor := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return anchor }

	for i := 0; i < 5; i++ {
		store.Record("wapp_a", UsageSample{
			Timestamp: anchor.Add(-time.Duration(i) * time.Second),
			Endpoint:  "/x",
			Method:    "GET",
			Status:    200,
			Duration:  time.Millisecond,
		})
	}
	got := store.Snapshot("wapp_a", time.Hour)
	if len(got) != 3 {
		t.Fatalf("expected cap=3 to hold, got %d", len(got))
	}
}

func TestUsageSampleStore_SnapshotWindow(t *testing.T) {
	store := NewUsageSampleStore(30*24*time.Hour, 1000)
	anchor := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return anchor }

	store.Record("wapp_a", UsageSample{Timestamp: anchor.Add(-25 * time.Hour), Endpoint: "/x", Method: "GET", Status: 200, Duration: time.Millisecond})
	store.Record("wapp_a", UsageSample{Timestamp: anchor.Add(-10 * time.Hour), Endpoint: "/x", Method: "GET", Status: 200, Duration: time.Millisecond})
	store.Record("wapp_a", UsageSample{Timestamp: anchor.Add(-1 * time.Hour), Endpoint: "/x", Method: "GET", Status: 200, Duration: time.Millisecond})

	got24h := store.Snapshot("wapp_a", 24*time.Hour)
	if len(got24h) != 2 {
		t.Fatalf("24h window expected 2 samples, got %d", len(got24h))
	}
	got7d := store.Snapshot("wapp_a", 7*24*time.Hour)
	if len(got7d) != 3 {
		t.Fatalf("7d window expected 3 samples, got %d", len(got7d))
	}
}

func TestSummarize_GroupsAndPercentiles(t *testing.T) {
	anchor := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	samples := []UsageSample{
		{Timestamp: anchor.Add(-10 * time.Minute), Endpoint: "/a", Method: "GET", Status: 200, Duration: 10 * time.Millisecond},
		{Timestamp: anchor.Add(-9 * time.Minute), Endpoint: "/a", Method: "GET", Status: 200, Duration: 20 * time.Millisecond},
		{Timestamp: anchor.Add(-8 * time.Minute), Endpoint: "/a", Method: "GET", Status: 500, Duration: 30 * time.Millisecond},
		{Timestamp: anchor.Add(-7 * time.Minute), Endpoint: "/b", Method: "POST", Status: 201, Duration: 5 * time.Millisecond},
	}

	s := Summarize(samples, "24h", 24*time.Hour, anchor)
	if s.Total != 4 {
		t.Fatalf("total: expected 4, got %d", s.Total)
	}
	if s.Errors != 1 {
		t.Fatalf("errors: expected 1, got %d", s.Errors)
	}
	if s.ByStatus["2xx"] != 3 || s.ByStatus["5xx"] != 1 {
		t.Fatalf("status buckets: got %v", s.ByStatus)
	}
	if s.ByMethod["GET"] != 3 || s.ByMethod["POST"] != 1 {
		t.Fatalf("method buckets: got %v", s.ByMethod)
	}
	if len(s.TopRoutes) != 2 {
		t.Fatalf("top routes: expected 2, got %d", len(s.TopRoutes))
	}
	if s.TopRoutes[0].Endpoint != "/a" || s.TopRoutes[0].Count != 3 {
		t.Fatalf("top route: expected /a count=3, got %+v", s.TopRoutes[0])
	}
	if s.TopRoutes[0].Errors != 1 {
		t.Fatalf("top route errors: expected 1, got %d", s.TopRoutes[0].Errors)
	}
	if s.P50 == 0 || s.P95 == 0 || s.P99 == 0 {
		t.Fatalf("percentiles should be non-zero on 4 samples: %+v", s)
	}
}

func TestSummarize_WindowExcludesOldSamples(t *testing.T) {
	anchor := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	samples := []UsageSample{
		{Timestamp: anchor.Add(-25 * time.Hour), Endpoint: "/a", Method: "GET", Status: 200, Duration: time.Millisecond},
		{Timestamp: anchor.Add(-10 * time.Hour), Endpoint: "/a", Method: "GET", Status: 200, Duration: time.Millisecond},
	}
	s24 := Summarize(samples, "24h", 24*time.Hour, anchor)
	if s24.Total != 1 {
		t.Fatalf("24h window: expected 1, got %d", s24.Total)
	}
	s7d := Summarize(samples, "7d", 7*24*time.Hour, anchor)
	if s7d.Total != 2 {
		t.Fatalf("7d window: expected 2, got %d", s7d.Total)
	}
}

func TestSummarizeAll_ReturnsThreeWindows(t *testing.T) {
	anchor := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	summaries := SummarizeAll(nil, anchor)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(summaries))
	}
	want := []string{"24h", "7d", "30d"}
	for i, s := range summaries {
		if s.Window != want[i] {
			t.Errorf("windows[%d]: want %q, got %q", i, want[i], s.Window)
		}
	}
}

func TestClientIDFromContext(t *testing.T) {
	ctxAnon := context.Background()
	if got := ClientIDFromContext(ctxAnon); got != AnonymousAppID {
		t.Errorf("empty ctx: expected anonymous, got %q", got)
	}

	u := &auth.User{ID: "user:a", Attributes: map[string]any{
		auth.OAuthClientIDAttributeKey: "wapp_xyz",
	}}
	ctx := auth.WithUser(context.Background(), u)
	if got := ClientIDFromContext(ctx); got != "wapp_xyz" {
		t.Errorf("oauth ctx: expected wapp_xyz, got %q", got)
	}

	noAttr := &auth.User{ID: "user:b"}
	ctxNoAttr := auth.WithUser(context.Background(), noAttr)
	if got := ClientIDFromContext(ctxNoAttr); got != AnonymousAppID {
		t.Errorf("no attrs: expected anonymous, got %q", got)
	}
}

func TestStatusBucket(t *testing.T) {
	cases := map[int]string{
		200: "2xx",
		204: "2xx",
		302: "3xx",
		404: "4xx",
		500: "5xx",
		100: "other",
		700: "other",
	}
	for code, want := range cases {
		if got := statusBucket(code); got != want {
			t.Errorf("statusBucket(%d): want %q, got %q", code, want, got)
		}
	}
}
