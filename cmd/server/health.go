package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
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

// Server lifecycle states reported by /healthz/ready (US-446). The default
// zero value is StateStarting so a freshly-constructed ServerDeps is NOT
// reported as ready until main.go has finished wiring every subsystem and
// explicitly called MarkReady. Transitions are one-way: starting → ready →
// draining; once gracefulShutdown flips the state to draining the readiness
// handler stays at 503 until process exit.
const (
	StateStarting = "starting"
	StateReady    = "ready"
	StateDraining = "draining"
)

// ServerState is the atomic readiness/lifecycle marker consulted by the
// readiness handler. Stored as an int32 internally so concurrent
// shutdown signals + healthcheck reads don't race. The zero value
// represents StateStarting so a default-constructed ServerDeps is honest
// about the fact that wiring is not done yet.
type ServerState struct {
	v atomic.Int32
}

// state encoding (kept private). 0=starting, 1=ready, 2=draining. The
// numeric ordering is the canonical monotonic transition order so a
// CompareAndSwap-style upgrade in the future stays trivial.
const (
	stateStartingCode int32 = 0
	stateReadyCode    int32 = 1
	stateDrainingCode int32 = 2
)

// Get returns the current state name as a string suitable for the
// readiness wire response.
func (s *ServerState) Get() string {
	switch s.v.Load() {
	case stateReadyCode:
		return StateReady
	case stateDrainingCode:
		return StateDraining
	default:
		return StateStarting
	}
}

// MarkReady transitions the state from starting to ready. Calling
// MarkReady once already-draining is a no-op — graceful shutdown is
// terminal.
func (s *ServerState) MarkReady() {
	s.v.CompareAndSwap(stateStartingCode, stateReadyCode)
}

// MarkDraining transitions the state to draining unconditionally. After
// this call /healthz/ready returns 503 with status=draining so the
// orchestrator (k8s, load balancer) removes the instance from rotation
// before the HTTP server starts refusing connections.
func (s *ServerState) MarkDraining() {
	s.v.Store(stateDrainingCode)
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
//
// Lifecycle short-circuits: if a non-nil ServerState is supplied via
// ReadinessHandlerWithState, requests received before MarkReady fires
// return 503 with status=starting; requests received after MarkDraining
// fires return 503 with status=draining. Both bypass the dependency
// probes entirely.
func ReadinessHandler(deps HealthProbes) http.Handler {
	return ReadinessHandlerWithState(deps, nil)
}

// ReadinessHandlerWithState extends ReadinessHandler with a lifecycle
// state gate. See ReadinessHandler for the dependency-probe contract.
func ReadinessHandlerWithState(deps HealthProbes, state *ServerState) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if state != nil {
			switch state.Get() {
			case StateStarting:
				writeReadinessLifecycle(w, http.StatusServiceUnavailable, StateStarting)
				return
			case StateDraining:
				writeReadinessLifecycle(w, http.StatusServiceUnavailable, StateDraining)
				return
			}
		}

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

func writeReadinessLifecycle(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"checks": map[string]string{},
	})
}
