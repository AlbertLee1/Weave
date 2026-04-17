package metrics

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
	"github.com/prometheus/client_golang/prometheus"
)

// Per-application API usage metrics (US-144). The labels mirror the
// counters spec in the user story: endpoint (chi route template), method,
// status code, and app_id (OAuth client_id, or "anonymous" when the
// caller did not authenticate via an OAuth bearer).
var (
	apiRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_api_requests_total",
			Help: "Total API requests partitioned by chi route template, method, status, and OAuth client_id (app_id).",
		},
		[]string{"endpoint", "method", "status", "app_id"},
	)
	apiRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weave_api_request_duration_seconds",
			Help:    "API request latency in seconds, partitioned by chi route template, method, and OAuth client_id (app_id).",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"endpoint", "method", "app_id"},
	)
)

// UsageSampleStore keeps a bounded in-memory log of per-app samples so the
// GET /api/v2/developer/applications/{id}/usage endpoint can aggregate 24h
// / 7d / 30d windows without a Prometheus TSDB backend. Samples older than
// retention are lazily evicted on append.
type UsageSampleStore struct {
	mu        sync.Mutex
	samples   map[string][]UsageSample
	retention time.Duration
	// maxPerApp caps the in-memory sample log per app — protects against a
	// runaway client blowing up process memory. Oldest samples evict when
	// the cap is reached.
	maxPerApp int
	now       func() time.Time
}

// UsageSample is a single request observation. Pointer-free struct so slice
// mutation is cheap and serialisation stays cheap for the /usage endpoint.
type UsageSample struct {
	Timestamp time.Time
	Endpoint  string
	Method    string
	Status    int
	Duration  time.Duration
}

// NewUsageSampleStore builds a sample store. retention defaults to 30d and
// maxPerApp defaults to 10_000 when the inputs are zero — both are the
// values the /usage endpoint needs to answer the 30d window without
// unbounded growth.
func NewUsageSampleStore(retention time.Duration, maxPerApp int) *UsageSampleStore {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	if maxPerApp <= 0 {
		maxPerApp = 10000
	}
	return &UsageSampleStore{
		samples:   make(map[string][]UsageSample),
		retention: retention,
		maxPerApp: maxPerApp,
		now:       time.Now,
	}
}

// Record appends a sample for an app. Called from the usage middleware
// after the handler returns.
func (s *UsageSampleStore) Record(appID string, sample UsageSample) {
	if s == nil || appID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := append(s.samples[appID], sample)
	cutoff := s.now().Add(-s.retention)
	// Evict in place: samples are appended in time order, so a single scan
	// from the front is enough to drop anything older than the retention
	// window. Cap-based eviction drops the oldest tail of the slice.
	trimmed := samples[:0]
	for _, v := range samples {
		if v.Timestamp.Before(cutoff) {
			continue
		}
		trimmed = append(trimmed, v)
	}
	if len(trimmed) > s.maxPerApp {
		trimmed = trimmed[len(trimmed)-s.maxPerApp:]
	}
	s.samples[appID] = trimmed
}

// Snapshot returns a defensive copy of all samples for an app within the
// window ending at now. The caller is free to mutate the returned slice.
func (s *UsageSampleStore) Snapshot(appID string, window time.Duration) []UsageSample {
	if s == nil || appID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.samples[appID]
	if len(all) == 0 {
		return nil
	}
	cutoff := s.now().Add(-window)
	out := make([]UsageSample, 0, len(all))
	for _, v := range all {
		if v.Timestamp.Before(cutoff) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// UsageMiddleware records per-request Prometheus metrics and (optionally)
// appends to an in-memory UsageSampleStore so the /usage endpoint has
// something to aggregate in environments without PromQL.
//
// The middleware MUST be mounted AFTER auth middleware so User.Attributes
// is populated; when the caller did not authenticate via an OAuth bearer
// the app_id label falls back to "anonymous".
func UsageMiddleware(store *UsageSampleStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			capture := &statusCapturingResponseWriter{ResponseWriter: w, status: 0}

			next.ServeHTTP(capture, r)

			endpoint := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pat := rctx.RoutePattern(); pat != "" {
					endpoint = pat
				}
			}
			if capture.status == 0 {
				capture.status = http.StatusOK
			}
			method := r.Method
			status := strconv.Itoa(capture.status)
			appID := ClientIDFromContext(r.Context())
			elapsed := time.Since(start)

			apiRequestsTotal.WithLabelValues(endpoint, method, status, appID).Inc()
			observeDuration(apiRequestDuration.WithLabelValues(endpoint, method, appID), elapsed)

			// Only record samples for real apps — skipping "anonymous" keeps
			// the in-memory store bounded and makes the /usage endpoint's
			// per-app queries cheap.
			if store != nil && appID != AnonymousAppID {
				store.Record(appID, UsageSample{
					Timestamp: start,
					Endpoint:  endpoint,
					Method:    method,
					Status:    capture.status,
					Duration:  elapsed,
				})
			}
		})
	}
}

// AnonymousAppID is the app_id label used for requests that did not carry
// an OAuth bearer. Exported so handlers and tests can reference the same
// constant string.
const AnonymousAppID = "anonymous"

// ClientIDFromContext extracts the OAuth client_id from the request
// context's User.Attributes. Returns AnonymousAppID when no client_id is
// present (non-OAuth request, or user attributes missing entirely).
func ClientIDFromContext(ctx context.Context) string {
	u := auth.UserFromContext(ctx)
	if u == nil || u.Attributes == nil {
		return AnonymousAppID
	}
	raw, ok := u.Attributes[auth.OAuthClientIDAttributeKey]
	if !ok {
		return AnonymousAppID
	}
	if s, ok := raw.(string); ok && s != "" {
		return s
	}
	return AnonymousAppID
}
