package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServerState_Default_IsStarting verifies that the default zero-value
// ServerState is "starting" — readiness must NOT be reported until main.go
// explicitly calls MarkReady once the wiring is complete.
func TestServerState_Default_IsStarting(t *testing.T) {
	var s ServerState
	if got := s.Get(); got != StateStarting {
		t.Errorf("default ServerState should be %q, got %q", StateStarting, got)
	}
}

func TestServerState_Transitions(t *testing.T) {
	var s ServerState

	s.MarkReady()
	if got := s.Get(); got != StateReady {
		t.Errorf("after MarkReady: expected %q, got %q", StateReady, got)
	}

	s.MarkDraining()
	if got := s.Get(); got != StateDraining {
		t.Errorf("after MarkDraining: expected %q, got %q", StateDraining, got)
	}

	// MarkReady from draining is a no-op (one-way transition); once draining
	// you can't go back to ready.
	s.MarkReady()
	if got := s.Get(); got != StateDraining {
		t.Errorf("after MarkReady-from-draining: expected sticky %q, got %q", StateDraining, got)
	}
}

// TestReadinessHandler_StartingState_Returns503 verifies that a server
// whose state is still "starting" returns 503 from /healthz/ready even
// when every dependency probe would otherwise be healthy.
func TestReadinessHandler_StartingState_Returns503(t *testing.T) {
	deps := &healthDepsFake{} // all probes nil → healthy
	state := &ServerState{}   // default starting
	handler := ReadinessHandlerWithState(deps, state)

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 in starting state, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "starting" {
		t.Errorf("expected status=starting, got %v", body["status"])
	}
}

// TestReadinessHandler_DrainingState_Returns503 verifies that once
// MarkDraining has fired, /healthz/ready returns 503 with status=draining
// regardless of the underlying dependency health. This is the signal load
// balancers / k8s use to remove the pod from the rotation during graceful
// shutdown.
func TestReadinessHandler_DrainingState_Returns503(t *testing.T) {
	deps := &healthDepsFake{} // every probe healthy
	state := &ServerState{}
	state.MarkReady()
	state.MarkDraining()
	handler := ReadinessHandlerWithState(deps, state)

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when draining, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "draining" {
		t.Errorf("expected status=draining, got %v", body["status"])
	}
}

// TestReadinessHandler_ReadyState_RunsProbes verifies that once the state
// has flipped to ready, the dependency probes are evaluated normally.
func TestReadinessHandler_ReadyState_RunsProbes(t *testing.T) {
	deps := &healthDepsFake{}
	state := &ServerState{}
	state.MarkReady()
	handler := ReadinessHandlerWithState(deps, state)

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 in ready state with healthy probes, got %d body=%s",
			w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", body["status"])
	}
}

// TestHealthz_RoutesMounted verifies that /healthz/live and /healthz/ready
// are mounted on the full router as conventional kubernetes-style aliases.
func TestHealthz_RoutesMounted(t *testing.T) {
	deps := &ServerDeps{}
	// Mark ready so /healthz/ready can be exercised end-to-end (degraded
	// mode probes return ErrProbeUnconfigured → "skipped" → ready).
	deps.ServerState.MarkReady()
	router := NewFullRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/healthz/live", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/healthz/live: expected 200, got %d", w.Code)
	}
	var live map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &live); err != nil {
		t.Fatalf("/healthz/live body not JSON: %v body=%s", err, w.Body.String())
	}
	if live["status"] != "alive" {
		t.Errorf("/healthz/live: expected status=alive, got %q", live["status"])
	}

	req2 := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("/healthz/ready in degraded test mode should be 200, got %d body=%s",
			w2.Code, w2.Body.String())
	}
}

// TestHealthz_DrainingFlipsReadiness wires up a real ServerDeps and verifies
// that flipping the server's state to draining at runtime causes
// /healthz/ready to start returning 503 immediately, without rebuilding
// the router. This is the integration shape that gracefulShutdown depends on.
func TestHealthz_DrainingFlipsReadiness(t *testing.T) {
	deps := &ServerDeps{}
	deps.ServerState.MarkReady()
	router := NewFullRouter(deps)

	// Pre-condition: ready.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected ready before draining, got %d", w.Code)
	}

	// Flip to draining at runtime.
	deps.ServerState.MarkDraining()

	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
	if w2.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 once draining, got %d body=%s", w2.Code, w2.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "draining" {
		t.Errorf("expected status=draining, got %v", body["status"])
	}
}

// TestGracefulShutdown_MarksDrainingBeforeServerStop verifies that the
// shutdown sequence flips the readiness state to draining BEFORE waiting
// on the HTTP server to drain — otherwise k8s would keep routing requests
// to a pod that's about to terminate.
func TestGracefulShutdown_MarksDrainingBeforeServerStop(t *testing.T) {
	state := &ServerState{}
	state.MarkReady()

	srv := &capturingShutdownServer{
		stateAtShutdown: nil,
		state:           state,
	}

	if err := gracefulShutdownWithState(context.Background(), srv, nil, state); err != nil {
		t.Fatalf("gracefulShutdownWithState returned error: %v", err)
	}

	if srv.stateAtShutdown == nil {
		t.Fatal("Shutdown was never called")
	}
	if got := *srv.stateAtShutdown; got != StateDraining {
		t.Errorf("state at Shutdown call should be %q, got %q", StateDraining, got)
	}
	if got := state.Get(); got != StateDraining {
		t.Errorf("post-shutdown state should be %q, got %q", StateDraining, got)
	}
}

// capturingShutdownServer records the ServerState observed at the moment
// Shutdown is invoked so the test can assert on shutdown ordering.
type capturingShutdownServer struct {
	stateAtShutdown *string
	state           *ServerState
}

func (c *capturingShutdownServer) Shutdown(_ context.Context) error {
	s := c.state.Get()
	c.stateAtShutdown = &s
	return nil
}
