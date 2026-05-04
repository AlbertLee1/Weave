package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// healthDeps is the test surface for the readiness handler. It mirrors the
// methods exposed on ServerDeps.
type healthDepsFake struct {
	pgErr    error
	natsErr  error
	bleveErr error
	pgNil    bool // when true, ProbePG returns ErrProbeUnconfigured-style nil signaling no PG
	natsNil  bool
	bleveNil bool
}

func (h *healthDepsFake) ProbePG(_ context.Context) error {
	if h.pgNil {
		return ErrProbeUnconfigured
	}
	return h.pgErr
}

func (h *healthDepsFake) ProbeNATS() error {
	if h.natsNil {
		return ErrProbeUnconfigured
	}
	return h.natsErr
}

func (h *healthDepsFake) ProbeBleve() error {
	if h.bleveNil {
		return ErrProbeUnconfigured
	}
	return h.bleveErr
}

func TestHealth_Liveness_AlwaysOK(t *testing.T) {
	// Even with broken deps, /health (liveness) returns 200.
	deps := &healthDepsFake{
		pgErr:    errors.New("postgres down"),
		natsErr:  errors.New("nats down"),
		bleveErr: errors.New("bleve down"),
	}

	handler := LivenessHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected liveness 200 even with deps broken, got %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "alive" {
		t.Errorf("expected status=alive, got %q", body["status"])
	}
	// deps reference for compiler — used by other tests in this file.
	_ = deps
}

func TestHealthReady_AllHealthy_Returns200(t *testing.T) {
	deps := &healthDepsFake{} // all probes return nil → healthy
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when all probes pass, got %d, body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", body["status"])
	}
	if _, ok := body["checks"]; !ok {
		t.Error("expected checks object in body")
	}
}

func TestHealthReady_PGFails_Returns503(t *testing.T) {
	deps := &healthDepsFake{pgErr: errors.New("connection refused")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when PG probe fails, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "unready" {
		t.Errorf("expected status=unready, got %v", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatal("expected checks object in body")
	}
	pgVal, ok := checks["postgres"].(string)
	if !ok || !strings.Contains(pgVal, "connection refused") {
		t.Errorf("expected postgres check to mention error, got %v", checks["postgres"])
	}
}

func TestHealthReady_NATSFails_Returns503(t *testing.T) {
	deps := &healthDepsFake{natsErr: errors.New("disconnected")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when NATS probe fails, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	checks := body["checks"].(map[string]any)
	natsVal, ok := checks["nats"].(string)
	if !ok || !strings.Contains(natsVal, "disconnected") {
		t.Errorf("expected nats check to mention error, got %v", checks["nats"])
	}
}

func TestHealthReady_BleveFails_Returns503(t *testing.T) {
	deps := &healthDepsFake{bleveErr: errors.New("index corrupt")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when Bleve probe fails, got %d", w.Code)
	}
}

func TestHealthReady_DegradedMode_Returns200(t *testing.T) {
	// All probes report unconfigured → considered healthy (degraded mode).
	deps := &healthDepsFake{pgNil: true, natsNil: true, bleveNil: true}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 in fully degraded mode, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready in degraded mode, got %v", body["status"])
	}
	checks := body["checks"].(map[string]any)
	if checks["postgres"] != "skipped" {
		t.Errorf("expected postgres=skipped, got %v", checks["postgres"])
	}
	if checks["nats"] != "skipped" {
		t.Errorf("expected nats=skipped, got %v", checks["nats"])
	}
	if checks["bleve"] != "skipped" {
		t.Errorf("expected bleve=skipped, got %v", checks["bleve"])
	}
}

func TestHealthReady_PartialDegraded_StillRequiresHealthyConfigured(t *testing.T) {
	// PG configured and healthy; NATS skipped; Bleve configured but failing.
	deps := &healthDepsFake{natsNil: true, bleveErr: errors.New("io error")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when configured-but-failing probe present, got %d", w.Code)
	}
}

func TestHealthReady_NilDeps_Returns200(t *testing.T) {
	// Defensive: nil deps means there is nothing to check, treat as healthy.
	handler := ReadinessHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when deps is nil, got %d", w.Code)
	}
}

// TestHealth_RoutesMounted verifies that NewFullRouter mounts /health,
// /health/live (alias for liveness), and /health/ready (readiness) and
// that the liveness paths return the alive payload.
func TestHealth_RoutesMounted(t *testing.T) {
	deps := &ServerDeps{}
	// Mark ready so /health/ready can be exercised end-to-end (degraded
	// mode probes return ErrProbeUnconfigured → "skipped" → ready).
	deps.ServerState.MarkReady()
	router := NewFullRouter(deps)

	// /health and /health/live both serve the liveness payload.
	for _, path := range []string{"/health", "/health/live"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
		var live map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &live); err != nil {
			t.Fatalf("%s body not JSON: %v body=%s", path, err, w.Body.String())
		}
		if live["status"] != "alive" {
			t.Errorf("%s: expected status=alive, got %q", path, live["status"])
		}
	}

	// /health/ready
	req2 := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("/health/ready in degraded test mode should be 200, got %d body=%s",
			w2.Code, w2.Body.String())
	}
}
