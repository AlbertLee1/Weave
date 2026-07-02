package actions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// US-001: Single Apply — options.mode tests
// ---------------------------------------------------------------------------

func TestHandler_Apply_VALIDATE_ONLY_ValidParams_ReturnsValid(t *testing.T) {
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
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Alice"},
		"options": map[string]interface{}{
			"mode": "VALIDATE_ONLY",
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Must NOT have published anything.
	if pub.calls != 0 {
		t.Fatalf("VALIDATE_ONLY must not publish, got %d calls", pub.calls)
	}

	// Response must have validation.result = "VALID", no edits.
	var resp SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Validation == nil {
		t.Fatal("expected validation field in response")
	}
	if resp.Validation.Result != "VALID" {
		t.Fatalf("expected VALID, got %q", resp.Validation.Result)
	}
	if resp.Edits != nil {
		t.Fatal("VALIDATE_ONLY must omit edits from SyncApplyActionResponseV2")
	}
}

func TestHandler_Apply_VALIDATE_ONLY_InvalidParams_ReturnsInvalid(t *testing.T) {
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
	router := setupRouter(handler)

	// Missing required "name" parameter.
	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{},
		"options": map[string]interface{}{
			"mode": "VALIDATE_ONLY",
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("VALIDATE_ONLY with invalid params should still return 200, got %d: %s", w.Code, w.Body.String())
	}

	if pub.calls != 0 {
		t.Fatalf("VALIDATE_ONLY must not publish, got %d calls", pub.calls)
	}

	var resp SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Validation == nil {
		t.Fatal("expected validation field in response")
	}
	if resp.Validation.Result != "INVALID" {
		t.Fatalf("expected INVALID, got %q", resp.Validation.Result)
	}
}

func TestHandler_Apply_VALIDATE_AND_EXECUTE_ExplicitOption(t *testing.T) {
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
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Alice"},
		"options": map[string]interface{}{
			"mode": "VALIDATE_AND_EXECUTE",
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("VALIDATE_AND_EXECUTE must publish, got %d calls", pub.calls)
	}
}

// ---------------------------------------------------------------------------
// US-001: Single Apply — options.returnEdits tests
// ---------------------------------------------------------------------------

func TestHandler_Apply_ReturnEdits_NONE(t *testing.T) {
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
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Alice"},
		"options": map[string]interface{}{
			"returnEdits": "NONE",
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Action MUST still be executed (published).
	if pub.calls != 1 {
		t.Fatalf("returnEdits=NONE still executes, expected 1 publish, got %d", pub.calls)
	}

	// Response edits must be nil (omitted).
	var resp SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Edits != nil {
		t.Fatal("returnEdits=NONE must omit edits from SyncApplyActionResponseV2")
	}
}

func TestHandler_Apply_ReturnEdits_ALL_Default(t *testing.T) {
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
	router := setupRouter(handler)

	// No options at all — default is returnEdits=ALL.
	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Alice"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Edits == nil {
		t.Fatal("default (ALL) must include edits")
	}
}

// ---------------------------------------------------------------------------
// US-001: Batch Apply — options.returnEdits tests
// ---------------------------------------------------------------------------

func TestHandler_ApplyBatch_Options_ReturnEdits_ALL(t *testing.T) {
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
	pub := &fakePublisher{offset: 5}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
			{"parameters": map[string]interface{}{"name": "Bob"}},
		},
		"options": map[string]interface{}{
			"returnEdits": "ALL",
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch",
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

	var br BatchApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &br); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if br.Edits == nil {
		t.Fatal("returnEdits=ALL must include edits in BatchApplyActionResponseV2")
	}
}

func TestHandler_ApplyBatch_Options_ReturnEdits_NONE(t *testing.T) {
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
	pub := &fakePublisher{offset: 5}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
		"options": map[string]interface{}{
			"returnEdits": "NONE",
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Still executes.
	if pub.calls != 1 {
		t.Fatalf("returnEdits=NONE still executes, expected 1 publish, got %d", pub.calls)
	}

	var br BatchApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &br); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if br.Edits != nil {
		t.Fatal("returnEdits=NONE must omit edits from BatchApplyActionResponseV2")
	}
}

// ---------------------------------------------------------------------------
// US-001: Batch Apply — old mode values return 400
// ---------------------------------------------------------------------------

func TestHandler_ApplyBatch_OldModeAtomic_Returns400(t *testing.T) {
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
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
		"mode": "atomic", // OLD field — must be rejected
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("old mode=atomic must return 400, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("must not publish on rejected old mode, got %d calls", pub.calls)
	}
}

func TestHandler_ApplyBatch_OldModeBestEffort_Returns400(t *testing.T) {
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
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
		"mode": "bestEffort", // OLD field — must be rejected
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("old mode=bestEffort must return 400, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("must not publish on rejected old mode, got %d calls", pub.calls)
	}
}

func TestHandler_ApplyBatch_DefaultOptions_AtomicWithEdits(t *testing.T) {
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
	pub := &fakePublisher{offset: 5}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	// No mode, no options — default is atomic + returnEdits=ALL.
	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch",
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

	var br BatchApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &br); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if br.Edits == nil {
		t.Fatal("default options must include edits in BatchApplyActionResponseV2")
	}
}
