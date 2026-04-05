package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
