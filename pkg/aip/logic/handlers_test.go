package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/aip"
	"github.com/liyang/weave/pkg/auth"
)

// newHandlerWithMockProvider wires a Handler against an in-memory store
// and the deterministic mock provider.
func newHandlerWithMockProvider() (*Handler, *MemoryStore) {
	store := NewMemoryStore()
	reg := aip.NewRegistry()
	reg.Register(aip.NewMockProvider())
	exec := NewExecutor(reg, NewMapToolRegistry())
	return NewHandler(store, exec), store
}

func withAuthContext(r *http.Request, userID string, roles ...string) *http.Request {
	user := &auth.User{ID: userID, Roles: roles}
	ctx := auth.WithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func newRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func sampleFlowBody(t *testing.T) string {
	t.Helper()
	body := map[string]any{
		"name": "summarise",
		"nodes": []any{
			map[string]any{
				"id":   "summary",
				"type": "llm",
				"config": map[string]any{
					"provider":       "mock",
					"promptTemplate": "Hi {{input.name}}",
				},
			},
			map[string]any{
				"id":   "out",
				"type": "output",
				"config": map[string]any{
					"keys": []any{"summary.content"},
				},
			},
		},
		"edges": []any{
			map[string]any{"from": "summary", "to": "out"},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestHandler_CreateFlow_Success(t *testing.T) {
	h, store := newHandlerWithMockProvider()
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows",
		strings.NewReader(sampleFlowBody(t)))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var flow Flow
	if err := json.Unmarshal(w.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(flow.ID, "flow_") {
		t.Errorf("expected flow_-prefixed id, got %q", flow.ID)
	}
	if flow.CreatedBy != "user:alice" {
		t.Errorf("CreatedBy = %q, want user:alice", flow.CreatedBy)
	}
	if _, err := store.GetFlow(context.Background(), flow.ID); err != nil {
		t.Fatalf("expected stored flow: %v", err)
	}
}

func TestHandler_CreateFlow_RejectsBadDefinition(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	r := newRouter(h)

	body := `{"name":"bad","nodes":[],"edges":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["errorName"].(string); got != "InvalidFlowDefinition" {
		t.Errorf("errorName=%q, want InvalidFlowDefinition", got)
	}
}

func TestHandler_GetFlow_OwnershipEnforced(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	r := newRouter(h)

	// Create as alice.
	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows",
		strings.NewReader(sampleFlowBody(t)))
	createReq = withAuthContext(createReq, "user:alice")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create failed: %s", createW.Body.String())
	}
	var flow Flow
	_ = json.Unmarshal(createW.Body.Bytes(), &flow)

	// Bob tries to read it.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/aip/logic-flows/"+flow.ID, nil)
	getReq = withAuthContext(getReq, "user:bob")
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body=%s)", getW.Code, getW.Body.String())
	}

	// Admin sees it.
	adminReq := httptest.NewRequest(http.MethodGet, "/api/v2/aip/logic-flows/"+flow.ID, nil)
	adminReq = withAuthContext(adminReq, "user:admin", auth.RoleAdmin)
	adminW := httptest.NewRecorder()
	r.ServeHTTP(adminW, adminReq)
	if adminW.Code != http.StatusOK {
		t.Fatalf("admin should see the flow, got %d", adminW.Code)
	}
}

func TestHandler_ExecuteFlow_PersistsRun(t *testing.T) {
	h, store := newHandlerWithMockProvider()
	r := newRouter(h)

	// Create flow.
	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows",
		strings.NewReader(sampleFlowBody(t)))
	createReq = withAuthContext(createReq, "user:alice")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create failed: %s", createW.Body.String())
	}
	var flow Flow
	_ = json.Unmarshal(createW.Body.Bytes(), &flow)

	// Execute.
	body := bytes.NewBufferString(`{"input":{"name":"world"}}`)
	exReq := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows/"+flow.ID+"/execute", body)
	exReq = withAuthContext(exReq, "user:alice")
	exW := httptest.NewRecorder()
	r.ServeHTTP(exW, exReq)
	if exW.Code != http.StatusOK {
		t.Fatalf("execute failed: status=%d body=%s", exW.Code, exW.Body.String())
	}
	var run Run
	if err := json.Unmarshal(exW.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.Status != RunStatusSuccess {
		t.Fatalf("expected success status, got %q (err=%s)", run.Status, run.Error)
	}
	if run.Output["summary.content"] == nil {
		t.Errorf("expected summary.content output, got %v", run.Output)
	}

	// Verify run row is stored.
	runs, err := store.ListRuns(context.Background(), flow.ID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 stored run, got %d", len(runs))
	}
}

func TestHandler_DeleteFlow_Success(t *testing.T) {
	h, store := newHandlerWithMockProvider()
	r := newRouter(h)

	// Create.
	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows",
		strings.NewReader(sampleFlowBody(t)))
	createReq = withAuthContext(createReq, "user:alice")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	var flow Flow
	_ = json.Unmarshal(createW.Body.Bytes(), &flow)

	// Delete.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v2/aip/logic-flows/"+flow.ID, nil)
	delReq = withAuthContext(delReq, "user:alice")
	delW := httptest.NewRecorder()
	r.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete failed: status=%d body=%s", delW.Code, delW.Body.String())
	}
	if _, err := store.GetFlow(context.Background(), flow.ID); err == nil {
		t.Fatalf("expected flow deleted, got nil error")
	}
}

func TestHandler_RequiresAuth(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/logic-flows", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestHandler_DegradedModeNoStore(t *testing.T) {
	h := NewHandler(nil, nil)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/logic-flows", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorName"] != "AIPLogicFlowsUnavailable" {
		t.Errorf("expected AIPLogicFlowsUnavailable, got %v", resp["errorName"])
	}
}

func TestHandler_ExecuteWithoutExecutorEmitsConfigured500(t *testing.T) {
	store := NewMemoryStore()
	flow := &Flow{
		ID: "flow_x", Name: "x",
		Nodes: []Node{{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}}},
	}
	if err := store.CreateFlow(context.Background(), flow); err != nil {
		t.Fatalf("seed flow: %v", err)
	}
	h := NewHandler(store, nil)
	r := newRouter(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows/flow_x/execute", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorName"] != "AIPLogicFlowExecutorUnavailable" {
		t.Errorf("expected AIPLogicFlowExecutorUnavailable, got %v", resp["errorName"])
	}
}
