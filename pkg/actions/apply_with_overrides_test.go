package actions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// setupRouterWithOverrides mirrors setupRouter but also wires the
// applyWithOverrides route introduced in US-030.
func setupRouterWithOverrides(handler *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", handler.Apply)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch", handler.ApplyBatch)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyWithOverrides", handler.ApplyWithOverrides)
	return r
}

// ---------------------------------------------------------------------------
// US-030: Action.applyWithOverrides endpoint
// ---------------------------------------------------------------------------

func TestHandler_ApplyWithOverrides_Success(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouterWithOverrides(handler)

	body := mustJSON(map[string]interface{}{
		"request": map[string]interface{}{
			"parameters": map[string]interface{}{"name": "Alice"},
		},
		"overrides": map[string]interface{}{},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyWithOverrides",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish, got %d", pub.calls)
	}

	var resp SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Edits == nil {
		t.Fatal("default returnEdits=ALL must include edits")
	}
	if resp.Edits.AddedObjectCount != 1 {
		t.Fatalf("expected 1 added object, got %d", resp.Edits.AddedObjectCount)
	}
}

func TestHandler_ApplyWithOverrides_ParameterOverridesWin(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouterWithOverrides(handler)

	// overrides.parameters.name = "Bob" must win over request.parameters.name = "Alice"
	body := mustJSON(map[string]interface{}{
		"request": map[string]interface{}{
			"parameters": map[string]interface{}{"name": "Alice"},
		},
		"overrides": map[string]interface{}{
			"parameters": map[string]interface{}{"name": "Bob"},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyWithOverrides",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(pub.batches) != 1 {
		t.Fatalf("expected 1 published batch, got %d", len(pub.batches))
	}
	lastBatch := pub.batches[0]
	if len(lastBatch.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(lastBatch.Edits))
	}
	// The final parameter value used by the create rule should be "Bob".
	if lastBatch.Edits[0].Properties["name"] != "Bob" {
		t.Fatalf("overrides.parameters must win; got name=%v", lastBatch.Edits[0].Properties["name"])
	}
}

func TestHandler_ApplyWithOverrides_MissingRequest_Returns400(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouterWithOverrides(handler)

	// No request field — just overrides.
	body := mustJSON(map[string]interface{}{
		"overrides": map[string]interface{}{
			"parameters": map[string]interface{}{"name": "Alice"},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyWithOverrides",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing request field must return 400, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("must not publish on rejected request, got %d calls", pub.calls)
	}
}

func TestHandler_ApplyWithOverrides_ReturnEdits_NONE(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouterWithOverrides(handler)

	body := mustJSON(map[string]interface{}{
		"request": map[string]interface{}{
			"parameters": map[string]interface{}{"name": "Alice"},
			"options": map[string]interface{}{
				"returnEdits": "NONE",
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyWithOverrides",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("returnEdits=NONE still executes, got %d calls", pub.calls)
	}

	var resp SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Edits != nil {
		t.Fatal("returnEdits=NONE must omit edits from response")
	}
}

func TestHandler_ApplyWithOverrides_ValidateOnly(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouterWithOverrides(handler)

	// VALIDATE_ONLY on the wrapped request should not publish, and overrides
	// must still be merged into the parameters used for validation.
	body := mustJSON(map[string]interface{}{
		"request": map[string]interface{}{
			"parameters": map[string]interface{}{}, // missing required name
			"options": map[string]interface{}{
				"mode": "VALIDATE_ONLY",
			},
		},
		"overrides": map[string]interface{}{
			"parameters": map[string]interface{}{"name": "Alice"},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyWithOverrides",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("VALIDATE_ONLY must not publish, got %d calls", pub.calls)
	}

	var resp SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Validation == nil || resp.Validation.Result != "VALID" {
		t.Fatalf("overrides should fill missing required param; expected VALID, got %+v", resp.Validation)
	}
}
