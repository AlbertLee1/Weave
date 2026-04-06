package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

func newTestIndexManager(t *testing.T, dir string) *index.Manager {
	t.Helper()
	return index.NewManager(dir)
}

func newTestAggEngine() *aggregation.Engine {
	return aggregation.NewEngine()
}

func TestNewServer_Timeouts(t *testing.T) {
	router := NewRouter()
	srv := NewServer(router, 9999)

	if srv.ReadTimeout != 30*time.Second {
		t.Errorf("expected ReadTimeout 30s, got %v", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 60*time.Second {
		t.Errorf("expected WriteTimeout 60s, got %v", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("expected IdleTimeout 120s, got %v", srv.IdleTimeout)
	}
	if srv.Addr != ":9999" {
		t.Errorf("expected Addr ':9999', got %q", srv.Addr)
	}
}

func TestAggregationEndpoint_ErrorsAreJSON(t *testing.T) {
	deps := &ServerDeps{
		AggEngine: nil, // will be set per-subtest
		IndexMgr:  nil,
	}

	// Test with nil engine — the route won't even be registered, so we test
	// with a minimal setup that exercises the error paths inside the handler.
	// We need real AggEngine and IndexMgr to register the route.
	// Use a real aggregation engine and index manager with a temp dir.
	tmpDir := t.TempDir()
	idxMgr := newTestIndexManager(t, tmpDir)
	defer idxMgr.Close()

	deps.AggEngine = newTestAggEngine()
	deps.IndexMgr = idxMgr

	router := NewFullRouter(deps)

	tests := []struct {
		name       string
		body       string
		objectType string
		wantStatus int
		wantJSON   bool
	}{
		{
			name:       "invalid JSON body",
			body:       `{invalid`,
			objectType: "Employee",
			wantStatus: http.StatusBadRequest,
			wantJSON:   true,
		},
		{
			name:       "index not found",
			body:       `{"aggregations":[]}`,
			objectType: "NonExistent",
			wantStatus: http.StatusNotFound,
			wantJSON:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/v2/ontologies/test-ontology/objects/" + tt.objectType + "/aggregate"
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}

			ct := w.Header().Get("Content-Type")
			if tt.wantJSON && !strings.HasPrefix(ct, "application/json") {
				t.Errorf("expected JSON Content-Type, got %q", ct)
			}

			// Verify it's valid JSON
			if tt.wantJSON {
				var jsonBody map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &jsonBody); err != nil {
					t.Errorf("response body is not valid JSON: %v; body: %s", err, w.Body.String())
				}
			}
		})
	}
}

func TestHealthEndpoint_Returns200(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
}

func TestHealthEndpoint_ContentType(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", ct)
	}
}

// fakeFunnelConsumer captures Stop() calls so the shutdown sequence test can
// assert that the funnel consumer is stopped during graceful shutdown.
type fakeFunnelConsumer struct {
	stopCalls int
	stopErr   error
}

func (f *fakeFunnelConsumer) Stop() error {
	f.stopCalls++
	return f.stopErr
}

// fakeShutdownableServer captures Shutdown() calls and the order in which
// things happen, so we can assert HTTP shutdown happens before consumer stop.
type fakeShutdownableServer struct {
	shutdownCalls int
	shutdownErr   error
	stoppedAfter  bool // set true if consumer was stopped after server shutdown
	consumer      *fakeFunnelConsumer
}

func (f *fakeShutdownableServer) Shutdown(ctx context.Context) error {
	f.shutdownCalls++
	if f.consumer != nil && f.consumer.stopCalls > 0 {
		// consumer was stopped before us — order violation
		f.stoppedAfter = false
	} else {
		f.stoppedAfter = true
	}
	return f.shutdownErr
}

func TestGracefulShutdown_StopsFunnelConsumer(t *testing.T) {
	consumer := &fakeFunnelConsumer{}
	srv := &fakeShutdownableServer{consumer: consumer}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := gracefulShutdown(ctx, srv, consumer); err != nil {
		t.Fatalf("gracefulShutdown returned error: %v", err)
	}

	if srv.shutdownCalls != 1 {
		t.Errorf("expected srv.Shutdown to be called once, got %d", srv.shutdownCalls)
	}
	if consumer.stopCalls != 1 {
		t.Errorf("expected consumer.Stop to be called once, got %d", consumer.stopCalls)
	}
	if !srv.stoppedAfter {
		t.Errorf("expected consumer.Stop to be called AFTER srv.Shutdown")
	}
}

func TestGracefulShutdown_NilConsumer(t *testing.T) {
	srv := &fakeShutdownableServer{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := gracefulShutdown(ctx, srv, nil); err != nil {
		t.Fatalf("gracefulShutdown with nil consumer returned error: %v", err)
	}
	if srv.shutdownCalls != 1 {
		t.Errorf("expected srv.Shutdown to be called once, got %d", srv.shutdownCalls)
	}
}

func TestGracefulShutdown_PropagatesShutdownError(t *testing.T) {
	consumer := &fakeFunnelConsumer{}
	srv := &fakeShutdownableServer{
		shutdownErr: errors.New("shutdown failed"),
		consumer:    consumer,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := gracefulShutdown(ctx, srv, consumer)
	if err == nil {
		t.Fatal("expected shutdown error to propagate")
	}
	// Even on shutdown error, the consumer should still be stopped to release
	// the NATS subscription.
	if consumer.stopCalls != 1 {
		t.Errorf("expected consumer.Stop to be called once even on shutdown error, got %d", consumer.stopCalls)
	}
}

// TestNATSBootstrap_UsesFunnelConnect locks the wiring path: main.go must
// route through funnel.Connect (which sets reconnect options) and NOT use
// the bare nats.Connect helper, otherwise production NATS reconnect handling
// is silently disabled.
func TestNATSBootstrap_UsesFunnelConnect(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	body := string(src)
	if strings.Contains(body, "nats.Connect(") {
		t.Errorf("main.go must not use bare nats.Connect; use funnel.Connect to enable reconnect handling")
	}
	if !strings.Contains(body, "funnel.Connect(") {
		t.Errorf("main.go must call funnel.Connect to bootstrap NATS with reconnect options")
	}
}
