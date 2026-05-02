package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// US-373: node-level dry-run handler. Runs a single in-flight node spec
// with a caller-provided state map and returns the trace entry without
// persisting a Run. Used by the SPA editor's "Run node" preview button.

func seedFlowForDryRun(t *testing.T, h *Handler) string {
	t.Helper()
	flow := &Flow{
		ID:   "flow.dryrun.test",
		Name: "dryrun",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		CreatedBy: "user:alice",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.store.CreateFlow(context.Background(), flow); err != nil {
		t.Fatalf("seed flow: %v", err)
	}
	return flow.ID
}

func postDryRun(t *testing.T, h *Handler, flowID string, body map[string]any, user string) *httptest.ResponseRecorder {
	t.Helper()
	r := newRouter(h)
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/aip/logic-flows/"+flowID+"/dry-run-node",
		bytes.NewReader(raw))
	req = withAuthContext(req, user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUS373_DryRunNode_LLMHappyPath(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	flowID := seedFlowForDryRun(t, h)

	body := map[string]any{
		"node": map[string]any{
			"id":   "preview",
			"type": "llm",
			"config": map[string]any{
				"provider":       "mock",
				"promptTemplate": "Hello {{input.name}}",
			},
		},
		"state": map[string]any{
			"input": map[string]any{"name": "Ada"},
		},
	}
	w := postDryRun(t, h, flowID, body, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp dryRunNodeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp.Trace.Status != TraceStatusSuccess {
		t.Errorf("status = %q, want success", resp.Trace.Status)
	}
	if resp.Trace.Output == nil {
		t.Errorf("expected output map, got nil")
	}
	if content, _ := resp.Trace.Output["content"].(string); !strings.Contains(content, "Ada") {
		t.Errorf("expected mock provider to echo 'Ada', got %v", resp.Trace.Output)
	}
}

func TestUS373_DryRunNode_ToolNode(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	flowID := seedFlowForDryRun(t, h)

	body := map[string]any{
		"node": map[string]any{
			"id":   "echo-preview",
			"type": "tool",
			"config": map[string]any{
				"tool":   "echo",
				"params": map[string]any{"value": "hi {{input.who}}"},
			},
		},
		"state": map[string]any{
			"input": map[string]any{"who": "world"},
		},
	}
	w := postDryRun(t, h, flowID, body, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp dryRunNodeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Trace.Status != TraceStatusSuccess {
		t.Errorf("status = %q, want success", resp.Trace.Status)
	}
}

func TestUS373_DryRunNode_FailureSurfacedInTrace(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	flowID := seedFlowForDryRun(t, h)

	// Reference an unregistered tool — runNode returns ErrToolNotFound,
	// which the handler folds into the trace entry as status=failed
	// rather than emitting a 5xx.
	body := map[string]any{
		"node": map[string]any{
			"id":   "missing-tool",
			"type": "tool",
			"config": map[string]any{
				"tool": "definitely-not-registered",
			},
		},
		"state": map[string]any{},
	}
	w := postDryRun(t, h, flowID, body, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp dryRunNodeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Trace.Status != TraceStatusFailed {
		t.Errorf("status = %q, want failed", resp.Trace.Status)
	}
	if resp.Trace.Error == "" {
		t.Errorf("expected non-empty trace.error")
	}
}

func TestUS373_DryRunNode_RejectsUnknownNodeType(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	flowID := seedFlowForDryRun(t, h)

	body := map[string]any{
		"node": map[string]any{
			"id":   "x",
			"type": "totally-bogus",
			"config": map[string]any{},
		},
	}
	w := postDryRun(t, h, flowID, body, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUS373_DryRunNode_RejectsMissingFlow(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	body := map[string]any{
		"node": map[string]any{
			"id":   "x",
			"type": "llm",
			"config": map[string]any{
				"provider": "mock",
			},
		},
	}
	w := postDryRun(t, h, "flow.does.not.exist", body, "user:alice")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUS373_DryRunNode_DoesNotPersistRun(t *testing.T) {
	h, store := newHandlerWithMockProvider()
	flowID := seedFlowForDryRun(t, h)

	body := map[string]any{
		"node": map[string]any{
			"id":   "preview",
			"type": "output",
			"config": map[string]any{
				"keys": []any{"input"},
			},
		},
		"state": map[string]any{"input": map[string]any{"k": "v"}},
	}
	if w := postDryRun(t, h, flowID, body, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("dry-run failed: %s", w.Body.String())
	}
	// Confirm no Run row was appended for the dry-run.
	runs, err := store.ListRuns(context.Background(), flowID, 50)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 runs after dry-run, got %d", len(runs))
	}
}

func TestUS373_DryRunNode_OwnershipEnforced(t *testing.T) {
	h, _ := newHandlerWithMockProvider()
	flowID := seedFlowForDryRun(t, h) // owned by user:alice

	body := map[string]any{
		"node": map[string]any{
			"id":   "preview",
			"type": "output",
			"config": map[string]any{},
		},
	}
	w := postDryRun(t, h, flowID, body, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
