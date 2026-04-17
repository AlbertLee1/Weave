package metrics

import (
	"sort"
	"time"
)

// UsageSummary is the aggregated shape returned by the /usage endpoint.
// Counts and latency are computed per-window so the frontend can render
// 24h / 7d / 30d at a glance.
type UsageSummary struct {
	Window    string         `json:"window"`
	Since     time.Time      `json:"since"`
	Until     time.Time      `json:"until"`
	Total     int            `json:"total"`
	Errors    int            `json:"errors"`
	ByStatus  map[string]int `json:"byStatus"`
	ByMethod  map[string]int `json:"byMethod"`
	TopRoutes []RouteStat    `json:"topRoutes"`
	// Latency percentiles in milliseconds. Zero when Total == 0.
	P50 float64 `json:"p50Ms"`
	P95 float64 `json:"p95Ms"`
	P99 float64 `json:"p99Ms"`
}

// RouteStat is a single row in the TopRoutes list — one endpoint's usage.
type RouteStat struct {
	Endpoint string  `json:"endpoint"`
	Method   string  `json:"method"`
	Count    int     `json:"count"`
	Errors   int     `json:"errors"`
	P95Ms    float64 `json:"p95Ms"`
}

// Windows enumerates the standard aggregation buckets the /usage endpoint
// returns.
var UsageWindows = []struct {
	Name     string
	Duration time.Duration
}{
	{"24h", 24 * time.Hour},
	{"7d", 7 * 24 * time.Hour},
	{"30d", 30 * 24 * time.Hour},
}

// SummarizeAll returns a UsageSummary per window in UsageWindows. now is
// threaded through so tests can control the clock.
func SummarizeAll(samples []UsageSample, now time.Time) []UsageSummary {
	out := make([]UsageSummary, 0, len(UsageWindows))
	for _, w := range UsageWindows {
		out = append(out, Summarize(samples, w.Name, w.Duration, now))
	}
	return out
}

// Summarize reduces a raw sample log to a single UsageSummary over the
// [now-window, now] interval.
func Summarize(samples []UsageSample, windowName string, window time.Duration, now time.Time) UsageSummary {
	since := now.Add(-window)
	s := UsageSummary{
		Window:   windowName,
		Since:    since,
		Until:    now,
		ByStatus: map[string]int{},
		ByMethod: map[string]int{},
	}

	type routeKey struct{ endpoint, method string }
	perRoute := map[routeKey]*routeAgg{}
	durations := make([]float64, 0, len(samples))

	for _, v := range samples {
		if v.Timestamp.Before(since) || v.Timestamp.After(now) {
			continue
		}
		s.Total++
		if v.Status >= 400 {
			s.Errors++
		}
		s.ByStatus[statusBucket(v.Status)]++
		s.ByMethod[v.Method]++
		durations = append(durations, float64(v.Duration)/float64(time.Millisecond))

		key := routeKey{v.Endpoint, v.Method}
		agg := perRoute[key]
		if agg == nil {
			agg = &routeAgg{endpoint: v.Endpoint, method: v.Method}
			perRoute[key] = agg
		}
		agg.count++
		if v.Status >= 400 {
			agg.errors++
		}
		agg.durations = append(agg.durations, float64(v.Duration)/float64(time.Millisecond))
	}

	if s.Total > 0 {
		s.P50 = percentile(durations, 50)
		s.P95 = percentile(durations, 95)
		s.P99 = percentile(durations, 99)
	}

	top := make([]RouteStat, 0, len(perRoute))
	for _, a := range perRoute {
		top = append(top, RouteStat{
			Endpoint: a.endpoint,
			Method:   a.method,
			Count:    a.count,
			Errors:   a.errors,
			P95Ms:    percentile(a.durations, 95),
		})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Count != top[j].Count {
			return top[i].Count > top[j].Count
		}
		if top[i].Endpoint != top[j].Endpoint {
			return top[i].Endpoint < top[j].Endpoint
		}
		return top[i].Method < top[j].Method
	})
	if len(top) > 10 {
		top = top[:10]
	}
	s.TopRoutes = top
	return s
}

type routeAgg struct {
	endpoint  string
	method    string
	count     int
	errors    int
	durations []float64
}

// statusBucket groups raw HTTP status codes into "2xx"/"3xx"/"4xx"/"5xx"
// labels. Anything outside the standard families maps to "other".
func statusBucket(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

// percentile returns the p'th percentile of values in milliseconds using
// the nearest-rank method. Returns 0 for an empty slice.
func percentile(values []float64, p int) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
