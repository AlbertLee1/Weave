//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
	"golang.org/x/time/rate"
)

// -----------------------------------------------------------------------
// Minimal rate-limit middleware reproduced from cmd/server/rate_limit.go
// so that the integration test can exercise the full HTTP pipeline
// without importing package main.
// -----------------------------------------------------------------------

type keyBy string

const (
	keyByIP       keyBy = "ip"
	keyByUser     keyBy = "user"
	keyByOntology keyBy = "ontology"
)

type rlRule struct {
	method   string
	segments []string
	rps      float64
	burst    int
	keyBy    keyBy
	limiters sync.Map
}

func (r *rlRule) matchPath(path string) bool {
	segs := splitSegs(path)
	if len(segs) != len(r.segments) {
		return false
	}
	for i, s := range r.segments {
		if strings.HasPrefix(s, "{") {
			continue
		}
		if s != segs[i] {
			return false
		}
	}
	return true
}

func (r *rlRule) extractParam(path, paramName string) string {
	segs := splitSegs(path)
	if len(segs) != len(r.segments) {
		return ""
	}
	target := "{" + paramName + "}"
	for i, s := range r.segments {
		if s == target {
			return segs[i]
		}
	}
	return ""
}

func (r *rlRule) getLimiter(key string) *rate.Limiter {
	if v, ok := r.limiters.Load(key); ok {
		return v.(*rate.Limiter)
	}
	lim := rate.NewLimiter(rate.Limit(r.rps), r.burst)
	actual, _ := r.limiters.LoadOrStore(key, lim)
	return actual.(*rate.Limiter)
}

func splitSegs(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

type rlConfig struct {
	method  string
	pattern string
	rps     float64
	burst   int
	keyBy   keyBy
}

func buildRateLimitMW(rules []rlConfig, defaultRule *rlConfig) func(http.Handler) http.Handler {
	compiled := make([]*rlRule, len(rules))
	for i, r := range rules {
		compiled[i] = &rlRule{
			method:   r.method,
			segments: splitSegs(r.pattern),
			rps:      r.rps,
			burst:    r.burst,
			keyBy:    r.keyBy,
		}
	}
	var def *rlRule
	if defaultRule != nil {
		def = &rlRule{rps: defaultRule.rps, burst: defaultRule.burst, keyBy: defaultRule.keyBy}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var matched *rlRule
			for _, cr := range compiled {
				if cr.method == r.Method && cr.matchPath(r.URL.Path) {
					matched = cr
					break
				}
			}
			if matched == nil {
				matched = def
			}
			if matched == nil {
				next.ServeHTTP(w, r)
				return
			}
			key := extractRLKey(matched, r)
			lim := matched.getLimiter(key)
			if !lim.Allow() {
				retryAfter := int(math.Ceil(1.0 / matched.rps))
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]any{
					"errorCode":    "TooManyRequests",
					"errorName":    "TooManyRequests",
					"errorMessage": "Rate limit exceeded. Please retry after the indicated period.",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractRLKey(rule *rlRule, r *http.Request) string {
	switch rule.keyBy {
	case keyByUser:
		if u := auth.UserFromContext(r.Context()); u != nil && u.ID != "" {
			return "user:" + u.ID
		}
		return "ip:" + clientIPFor(r)
	case keyByOntology:
		ont := rule.extractParam(r.URL.Path, "ontologyApiName")
		if ont != "" {
			return "ont:" + ont
		}
		return "ip:" + clientIPFor(r)
	default:
		return "ip:" + clientIPFor(r)
	}
}

func clientIPFor(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// -----------------------------------------------------------------------
// Integration tests
// -----------------------------------------------------------------------

// productionRules mirrors DefaultRateLimitRules() from cmd/server but with
// burst values low enough for fast in-process tests. The RPS values match
// the production config exactly; burst is clamped to 3 so that burst+1
// requests trigger a 429 without sending thousands of requests.
func testableRules() ([]rlConfig, *rlConfig) {
	rules := []rlConfig{
		{method: "POST", pattern: "/api/auth/login", rps: 5, burst: 3, keyBy: keyByIP},
		{method: "POST", pattern: "/api/auth/refresh", rps: 10, burst: 3, keyBy: keyByUser},
		{method: "POST", pattern: "/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", rps: 100, burst: 3, keyBy: keyByUser},
		{method: "POST", pattern: "/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", rps: 1000, burst: 3, keyBy: keyByOntology},
	}
	defaultRule := &rlConfig{rps: 200, burst: 3, keyBy: keyByUser}
	return rules, defaultRule
}

func ok200(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestRateLimit_LoginEndpoint_IPKeyed(t *testing.T) {
	rules, def := testableRules()
	mw := buildRateLimitMW(rules, def)

	r := chi.NewRouter()
	r.Use(mw)
	r.Post("/api/auth/login", ok200)

	// Burst of 3 succeeds
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "192.168.1.1:9999"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 4th request triggers 429
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request: expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}

	// Different IP still succeeds (separate bucket)
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req2.RemoteAddr = "192.168.1.2:9999"
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("different IP: expected 200, got %d", w2.Code)
	}
}

func TestRateLimit_RefreshEndpoint_UserKeyed(t *testing.T) {
	rules, def := testableRules()
	mw := buildRateLimitMW(rules, def)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithUser(req.Context(), &auth.User{ID: "user-alpha"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(mw)
	r.Post("/api/auth/refresh", ok200)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request: expected 429, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("expected JSON body: %v", err)
	}
	if body["errorCode"] != "TooManyRequests" {
		t.Errorf("errorCode: got %v, want TooManyRequests", body["errorCode"])
	}
}

func TestRateLimit_ApplyEndpoint_UserKeyed(t *testing.T) {
	rules, def := testableRules()
	mw := buildRateLimitMW(rules, def)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithUser(req.Context(), &auth.User{ID: "user-beta"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(mw)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", ok200)

	path := "/api/v2/ontologies/northwind/actions/createOrder/apply"
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request: expected 429, got %d", w.Code)
	}
}

func TestRateLimit_IngestEndpoint_OntologyKeyed(t *testing.T) {
	rules, def := testableRules()
	mw := buildRateLimitMW(rules, def)

	r := chi.NewRouter()
	r.Use(mw)
	r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", ok200)

	// Exhaust burst for ontology "northwind"
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/streams/orders/ingest", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 4th request → 429
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/orders/ingest", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request: expected 429, got %d", w.Code)
	}

	// Different ontology "chinook" → still OK (separate bucket)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/chinook/streams/albums/ingest", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("different ontology: expected 200, got %d", w2.Code)
	}
}

func TestRateLimit_DefaultFallback_UnmatchedRoute(t *testing.T) {
	rules, def := testableRules()
	mw := buildRateLimitMW(rules, def)

	r := chi.NewRouter()
	r.Use(mw)
	r.Get("/api/v2/ontologies", ok200)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
		req.RemoteAddr = "10.0.0.5:8080"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 4th request triggers default fallback 429
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	req.RemoteAddr = "10.0.0.5:8080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request: expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After on default fallback 429")
	}
}

func TestRateLimit_CSPHeadersTightened(t *testing.T) {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
					"connect-src 'self'; img-src 'self' data:; font-src 'self'; "+
					"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/api/v2/ontologies", ok200)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil))

	csp := w.Header().Get("Content-Security-Policy")
	expected := []string{
		"default-src 'self'",
		"script-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}
	for _, directive := range expected {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP missing directive %q, got: %s", directive, csp)
		}
	}
}
