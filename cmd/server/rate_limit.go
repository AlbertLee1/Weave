package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/auth"
	"golang.org/x/time/rate"
)

const defaultRateLimitIdleTTL = 5 * time.Minute

// KeyBy identifies how to partition rate limit buckets.
type KeyBy string

const (
	KeyByIP       KeyBy = "ip"
	KeyByUser     KeyBy = "user"
	KeyByOntology KeyBy = "ontology"
)

// RateLimitRule configures a per-endpoint token bucket rate limit.
type RateLimitRule struct {
	Method  string  // HTTP method (e.g. "POST")
	Pattern string  // URL path pattern with chi-style {param} placeholders
	RPS     float64 // Sustained requests per second
	Burst   int     // Maximum burst size (token bucket capacity)
	KeyBy   KeyBy   // How to partition buckets: "ip", "user", or "ontology"
	// IdleTTL optionally overrides how long an unused per-key bucket is kept.
	// Zero uses the middleware default.
	IdleTTL time.Duration
}

// compiledRule is the internal representation with a pre-compiled path matcher.
type compiledRule struct {
	method   string
	segments []string // split pattern segments; "{...}" is wildcard
	rps      float64
	burst    int
	keyBy    KeyBy
	idleTTL  time.Duration
	nowFunc  func() time.Time
	// limiters holds per-key token buckets for this rule.
	limiters sync.Map // map[string]*rateLimitBucket

	pruneMu   sync.Mutex
	lastPrune time.Time
}

type rateLimitBucket struct {
	limiter  *rate.Limiter
	mu       sync.Mutex
	lastSeen time.Time
}

func (b *rateLimitBucket) touch(now time.Time) {
	b.mu.Lock()
	b.lastSeen = now
	b.mu.Unlock()
}

func (b *rateLimitBucket) idleFor(now time.Time) time.Duration {
	b.mu.Lock()
	lastSeen := b.lastSeen
	b.mu.Unlock()
	return now.Sub(lastSeen)
}

// matchPath returns true if the given URL path matches this rule's pattern.
// Literal segments must match exactly; segments starting with "{" are wildcards.
func (cr *compiledRule) matchPath(path string) bool {
	pathSegs := splitPath(path)
	if len(pathSegs) != len(cr.segments) {
		return false
	}
	for i, seg := range cr.segments {
		if strings.HasPrefix(seg, "{") {
			continue // wildcard — matches any segment
		}
		if seg != pathSegs[i] {
			return false
		}
	}
	return true
}

// extractParam returns the value of the named {param} placeholder from the
// given URL path, using this rule's pattern as the template. Returns "" if
// the param name is not found or the path doesn't match.
func (cr *compiledRule) extractParam(path, paramName string) string {
	pathSegs := splitPath(path)
	if len(pathSegs) != len(cr.segments) {
		return ""
	}
	target := "{" + paramName + "}"
	for i, seg := range cr.segments {
		if seg == target {
			return pathSegs[i]
		}
	}
	return ""
}

// getLimiter returns (or lazily creates) the token bucket for the given key.
func (cr *compiledRule) getLimiter(key string) *rate.Limiter {
	now := cr.now()
	cr.pruneIdleLimiters(now)
	if v, ok := cr.limiters.Load(key); ok {
		bucket := v.(*rateLimitBucket)
		bucket.touch(now)
		return bucket.limiter
	}
	bucket := &rateLimitBucket{
		limiter:  rate.NewLimiter(rate.Limit(cr.rps), cr.burst),
		lastSeen: now,
	}
	actual, _ := cr.limiters.LoadOrStore(key, bucket)
	actualBucket := actual.(*rateLimitBucket)
	actualBucket.touch(now)
	return actualBucket.limiter
}

func (cr *compiledRule) now() time.Time {
	if cr.nowFunc != nil {
		return cr.nowFunc()
	}
	return time.Now()
}

func (cr *compiledRule) pruneIdleLimiters(now time.Time) {
	ttl := normalizeRateLimitIdleTTL(cr.idleTTL)
	interval := rateLimitPruneInterval(ttl)

	cr.pruneMu.Lock()
	if !cr.lastPrune.IsZero() && now.Sub(cr.lastPrune) < interval {
		cr.pruneMu.Unlock()
		return
	}
	cr.lastPrune = now
	cr.pruneMu.Unlock()

	cr.limiters.Range(func(key, value any) bool {
		bucket := value.(*rateLimitBucket)
		if bucket.idleFor(now) >= ttl {
			cr.limiters.Delete(key)
		}
		return true
	})
}

func normalizeRateLimitIdleTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultRateLimitIdleTTL
	}
	return ttl
}

func rateLimitPruneInterval(ttl time.Duration) time.Duration {
	if ttl <= time.Second {
		return ttl
	}
	interval := ttl / 2
	if interval < time.Second {
		return time.Second
	}
	return interval
}

// splitPath splits a URL path into non-empty segments.
func splitPath(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// DefaultRateLimitRules returns the production rate limit configuration table
// and a default fallback rule for unmatched requests.
//
// Endpoint-specific rules:
//   - POST /api/auth/login       → 5 rps / IP
//   - POST /api/auth/refresh     → 10 rps / user
//   - POST .../actions/{a}/apply → 100 rps / user
//   - POST .../streams/{ot}/ingest → 1000 rps / ontology
//
// Default (unmatched requests): 200 rps / user
func DefaultRateLimitRules() ([]RateLimitRule, *RateLimitRule) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/auth/login", RPS: 5, Burst: 5, KeyBy: KeyByIP},
		{Method: "POST", Pattern: "/api/auth/refresh", RPS: 10, Burst: 10, KeyBy: KeyByUser},
		{Method: "POST", Pattern: "/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", RPS: 100, Burst: 100, KeyBy: KeyByUser},
		{Method: "POST", Pattern: "/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", RPS: 1000, Burst: 1000, KeyBy: KeyByOntology},
	}
	defaultRule := &RateLimitRule{RPS: 200, Burst: 200, KeyBy: KeyByUser}
	return rules, defaultRule
}

// NewRateLimitMiddleware returns a chi-compatible middleware that enforces
// per-endpoint token bucket rate limits according to the given rules.
//
// Each rule matches requests by HTTP method and URL path pattern. When a
// request matches a rule, a key is derived (IP address, user ID, or ontology
// name) and a per-key token bucket is consulted. If the bucket is exhausted
// the middleware responds with 429 Too Many Requests and a Retry-After header.
//
// Requests that match no rule pass through without rate limiting.
func NewRateLimitMiddleware(rules []RateLimitRule) func(http.Handler) http.Handler {
	return NewRateLimitMiddlewareWithDefault(rules, nil)
}

// NewRateLimitMiddlewareWithDefault is like NewRateLimitMiddleware but accepts
// an optional default rule that applies to requests not matching any explicit
// rule. When defaultRule is nil, unmatched requests pass through freely.
func NewRateLimitMiddlewareWithDefault(rules []RateLimitRule, defaultRule *RateLimitRule) func(http.Handler) http.Handler {
	return newRateLimitMiddlewareWithClock(rules, defaultRule, time.Now)
}

func newRateLimitMiddlewareWithClock(rules []RateLimitRule, defaultRule *RateLimitRule, nowFunc func() time.Time) func(http.Handler) http.Handler {
	compiled := make([]*compiledRule, len(rules))
	for i, r := range rules {
		compiled[i] = &compiledRule{
			method:   r.Method,
			segments: splitPath(r.Pattern),
			rps:      r.RPS,
			burst:    r.Burst,
			keyBy:    r.KeyBy,
			idleTTL:  r.IdleTTL,
			nowFunc:  nowFunc,
		}
	}

	var compiledDefault *compiledRule
	if defaultRule != nil {
		compiledDefault = &compiledRule{
			rps:     defaultRule.RPS,
			burst:   defaultRule.Burst,
			keyBy:   defaultRule.KeyBy,
			idleTTL: defaultRule.IdleTTL,
			nowFunc: nowFunc,
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rule := matchRule(compiled, r.Method, r.URL.Path)
			if rule == nil {
				rule = compiledDefault
			}
			if rule == nil {
				next.ServeHTTP(w, r)
				return
			}

			key := extractKey(rule, r)
			lim := rule.getLimiter(key)

			if !lim.Allow() {
				retryAfter := int(math.Ceil(1.0 / rule.rps))
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

// matchRule finds the first compiled rule whose method and path pattern match
// the incoming request. Returns nil if no rule matches.
func matchRule(rules []*compiledRule, method, path string) *compiledRule {
	for _, cr := range rules {
		if cr.method != method {
			continue
		}
		if cr.matchPath(path) {
			return cr
		}
	}
	return nil
}

// extractKey derives the rate-limit bucket key from the request and the
// matched rule. The rule's pattern is used to extract URL params directly
// from the path (not chi.URLParam) so the middleware works as global
// r.Use() middleware where chi hasn't resolved route params yet.
// When user-keyed and no auth context exists (e.g. public endpoints like
// /api/auth/refresh), it falls back to IP.
func extractKey(rule *compiledRule, r *http.Request) string {
	switch rule.keyBy {
	case KeyByUser:
		if u := auth.UserFromContext(r.Context()); u != nil && u.ID != "" {
			return "user:" + u.ID
		}
		return "ip:" + clientIP(r)
	case KeyByOntology:
		ont := rule.extractParam(r.URL.Path, "ontologyApiName")
		if ont != "" {
			return "ont:" + ont
		}
		return "ip:" + clientIP(r)
	default: // KeyByIP
		return "ip:" + clientIP(r)
	}
}

// clientIP extracts the client IP from RemoteAddr, stripping the port.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
