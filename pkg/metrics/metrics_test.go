package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegister_IdempotentOnRepeat(t *testing.T) {
	r := NewRegistry()

	// First registration must succeed.
	if err := Register(r); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Second call on the same registry must NOT panic and must NOT return
	// an AlreadyRegisteredError. Idempotent re-registration is required so
	// callers can call Register() multiple times during boot/test setup
	// without crashing.
	if err := Register(r); err != nil {
		t.Fatalf("second Register: %v", err)
	}
}

func TestRegister_FreshRegistryHasWeaveCounters(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Touch every metric so they show up in Gather (Prometheus *Vec
	// metrics with no observed labels are not surfaced by Gather).
	httpRequestsTotal.WithLabelValues("GET", "/x", "200").Inc()
	httpRequestDuration.WithLabelValues("GET", "/x").Observe(0.001)
	natsPublishTotal.WithLabelValues("subj", "ok").Inc()
	natsConsumeTotal.WithLabelValues("subj", "ok").Inc()
	natsConsumeDuration.WithLabelValues("subj").Observe(0.001)
	dbQueriesTotal.WithLabelValues("op", "ok").Inc()
	dbQueryDuration.WithLabelValues("op").Observe(0.001)
	bleveSearchTotal.WithLabelValues("ot", "ok").Inc()
	bleveSearchDuration.WithLabelValues("ot").Observe(0.001)
	bleveIndexDocs.WithLabelValues("ot").Set(0)
	actionsAppliedTotal.WithLabelValues("at", "ok").Inc()
	actionsDuration.WithLabelValues("at").Observe(0.001)
	SetBuildInfo("v0", "c0", "go0")

	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	have := map[string]bool{}
	for _, mf := range mfs {
		have[mf.GetName()] = true
	}

	required := []string{
		"weave_http_requests_total",
		"weave_http_request_duration_seconds",
		"weave_nats_publish_total",
		"weave_nats_consume_total",
		"weave_nats_consume_duration_seconds",
		"weave_db_queries_total",
		"weave_db_query_duration_seconds",
		"weave_bleve_search_total",
		"weave_bleve_search_duration_seconds",
		"weave_bleve_index_docs",
		"weave_actions_applied_total",
		"weave_actions_duration_seconds",
		"weave_build_info",
	}
	for _, name := range required {
		if !have[name] {
			t.Errorf("expected metric %q to be registered, missing", name)
		}
	}
}

func TestBuildInfo_Exposed(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	SetBuildInfo("v0.1.0-test", "abc1234", "go1.25.0")

	// We need to call Register again to ensure SetBuildInfo took effect on
	// THIS registry. SetBuildInfo writes to a package-level gauge, but the
	// gauge is registered into r above.
	expected := `
# HELP weave_build_info Weave build information; value is always 1.
# TYPE weave_build_info gauge
weave_build_info{commit="abc1234",go_version="go1.25.0",version="v0.1.0-test"} 1
`
	if err := testutil.GatherAndCompare(r, strings.NewReader(expected), "weave_build_info"); err != nil {
		t.Fatalf("build info compare: %v", err)
	}
}

func TestDefault_ReturnsSameRegistry(t *testing.T) {
	r1 := Default()
	r2 := Default()
	if r1 != r2 {
		t.Fatalf("Default() must return the same registry instance on every call")
	}
	// And it must be a *prometheus.Registry (compile-time check).
	var _ *prometheus.Registry = r1
}
