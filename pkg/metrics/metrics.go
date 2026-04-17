// Package metrics defines the Prometheus metric registry for Weave and the
// helpers used by the rest of the codebase to report request, NATS, DB,
// search, and action telemetry.
//
// All metric names are prefixed `weave_` and the package owns one
// package-level Default() registry that is wired into the /metrics HTTP
// handler in cmd/server. Tests construct a fresh NewRegistry() so they do
// not pollute the default singleton.
package metrics

import (
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTP metrics.
var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_http_requests_total",
			Help: "Total HTTP requests handled by the Weave server, partitioned by method, route template, and status code.",
		},
		[]string{"method", "path", "status"},
	)
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weave_http_request_duration_seconds",
			Help:    "End-to-end HTTP request duration in seconds, partitioned by method and route template.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

// NATS metrics.
var (
	natsPublishTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_nats_publish_total",
			Help: "Total NATS publishes attempted, partitioned by subject and ok|error status.",
		},
		[]string{"subject", "status"},
	)
	natsConsumeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_nats_consume_total",
			Help: "Total NATS messages consumed, partitioned by subject and ok|error status.",
		},
		[]string{"subject", "status"},
	)
	natsConsumeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weave_nats_consume_duration_seconds",
			Help:    "Time spent processing a single NATS message in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"subject"},
	)
)

// Database metrics.
var (
	dbQueriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_db_queries_total",
			Help: "Total PostgreSQL queries issued by Weave, partitioned by logical operation and ok|error status.",
		},
		[]string{"operation", "status"},
	)
	dbQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weave_db_query_duration_seconds",
			Help:    "PostgreSQL query latency in seconds, partitioned by logical operation.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)
)

// Bleve search metrics.
var (
	bleveSearchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_bleve_search_total",
			Help: "Total Bleve searches executed, partitioned by object type and ok|error status.",
		},
		[]string{"object_type", "status"},
	)
	bleveSearchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weave_bleve_search_duration_seconds",
			Help:    "Bleve search latency in seconds, partitioned by object type.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"object_type"},
	)
	bleveIndexDocs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "weave_bleve_index_docs",
			Help: "Current Bleve document count, partitioned by object type.",
		},
		[]string{"object_type"},
	)
)

// Action metrics.
var (
	actionsAppliedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_actions_applied_total",
			Help: "Total actions applied, partitioned by action type and ok|error status.",
		},
		[]string{"action_type", "status"},
	)
	actionsDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weave_actions_duration_seconds",
			Help:    "Action execution latency in seconds, partitioned by action type.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"action_type"},
	)
)

// Build info metric. Always 1; the labels carry the version information.
var buildInfo = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "weave_build_info",
		Help: "Weave build information; value is always 1.",
	},
	[]string{"version", "commit", "go_version"},
)

// allCollectors returns every collector this package defines, in a stable
// order, so Register() can iterate them.
func allCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		httpRequestsTotal,
		httpRequestDuration,
		natsPublishTotal,
		natsConsumeTotal,
		natsConsumeDuration,
		dbQueriesTotal,
		dbQueryDuration,
		bleveSearchTotal,
		bleveSearchDuration,
		bleveIndexDocs,
		actionsAppliedTotal,
		actionsDuration,
		apiRequestsTotal,
		apiRequestDuration,
		buildInfo,
	}
}

// NewRegistry returns a fresh prometheus.Registry suitable for tests. Tests
// MUST use this rather than the package default so they do not pollute the
// global state shared by the running server.
func NewRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// Register registers every Weave metric on the given registry. It is
// idempotent: calling Register on the same registry twice is a no-op for the
// metrics that are already present. This matters because main() and tests
// both call Register and we do not want a panic-on-rerun.
func Register(r *prometheus.Registry) error {
	for _, c := range allCollectors() {
		if err := r.Register(c); err != nil {
			var already prometheus.AlreadyRegisteredError
			if errors.As(err, &already) {
				continue
			}
			return err
		}
	}
	return nil
}

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *prometheus.Registry
)

// Default returns the package-level Prometheus registry. Production code
// should call Default() once and feed the result into promhttp.HandlerFor()
// when wiring the /metrics endpoint.
func Default() *prometheus.Registry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = prometheus.NewRegistry()
	})
	return defaultRegistry
}

// SetBuildInfo records the build version, git commit, and Go runtime
// version on the weave_build_info gauge. The gauge value is always 1; the
// labels carry the metadata. Call this once during boot.
func SetBuildInfo(version, commit, goVersion string) {
	buildInfo.Reset()
	buildInfo.WithLabelValues(version, commit, goVersion).Set(1)
}

// observeDuration is a small helper used by the http/db/nats helpers to
// convert a time.Duration to seconds in a single place. The use of a
// helper keeps the math out of every call site so we can change the
// resolution unit (e.g. milliseconds) by editing one line.
func observeDuration(h prometheus.Observer, d time.Duration) {
	h.Observe(d.Seconds())
}
