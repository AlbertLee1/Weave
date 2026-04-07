package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ErrProbeUnconfigured is returned by a Probe* method when the corresponding
// dependency was not wired in (degraded mode). The readiness handler treats
// these as "skipped" and does NOT count them as failures, so a Weave instance
// running without PG / NATS / Bleve is still considered ready.
var ErrProbeUnconfigured = errors.New("probe: dependency not configured")

// HealthProbes is the surface ReadinessHandler depends on. It is satisfied by
// *ServerDeps in production and by test fakes in unit tests.
type HealthProbes interface {
	ProbePG(ctx context.Context) error
	ProbeNATS() error
	ProbeBleve() error
}

// LivenessHandler returns 200 with {"status":"alive"} unconditionally. It is
// the k8s liveness probe target — it does NOT check downstream dependencies.
// A passing liveness check means "this process is responsive"; an unready
// readiness check means "this process is responsive but should be removed
// from the load balancer pool".
func LivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
	})
}

// ReadinessHandler returns 200 with {"status":"ready", "checks":{...}} when
// every configured dependency probe succeeds, or 503 with
// {"status":"unready", "checks":{...}} when any one fails. Probes that
// return ErrProbeUnconfigured are recorded as "skipped" in the checks
// payload but do NOT cause readiness to fail (degraded mode is acceptable).
//
// The handler always emits a JSON body so clients can parse a structured
// response in either case.
func ReadinessHandler(deps HealthProbes) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{}
		ready := true

		if deps == nil {
			writeReadiness(w, true, checks)
			return
		}

		if err := deps.ProbePG(r.Context()); err != nil {
			if errors.Is(err, ErrProbeUnconfigured) {
				checks["postgres"] = "skipped"
			} else {
				checks["postgres"] = err.Error()
				ready = false
			}
		} else {
			checks["postgres"] = "ok"
		}

		if err := deps.ProbeNATS(); err != nil {
			if errors.Is(err, ErrProbeUnconfigured) {
				checks["nats"] = "skipped"
			} else {
				checks["nats"] = err.Error()
				ready = false
			}
		} else {
			checks["nats"] = "ok"
		}

		if err := deps.ProbeBleve(); err != nil {
			if errors.Is(err, ErrProbeUnconfigured) {
				checks["bleve"] = "skipped"
			} else {
				checks["bleve"] = err.Error()
				ready = false
			}
		} else {
			checks["bleve"] = "ok"
		}

		writeReadiness(w, ready, checks)
	})
}

func writeReadiness(w http.ResponseWriter, ready bool, checks map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	status := "ready"
	code := http.StatusOK
	if !ready {
		status = "unready"
		code = http.StatusServiceUnavailable
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"checks": checks,
	})
}
