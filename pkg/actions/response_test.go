package actions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// US-002: Single Apply — SyncApplyActionResponseV2 envelope
// ---------------------------------------------------------------------------

func TestHandler_Apply_ResponseEnvelope_HasEditsWithCounts(t *testing.T) {
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
		t.Fatalf("unmarshal SyncApplyActionResponseV2: %v", err)
	}

	// Must have edits with counts.
	if resp.Edits == nil {
		t.Fatal("expected edits field in response")
	}
	if resp.Edits.Type != "edits" {
		t.Fatalf("expected edits.type = \"edits\", got %q", resp.Edits.Type)
	}
	if resp.Edits.AddedObjectCount != 1 {
		t.Fatalf("expected addedObjectCount=1 for createObject, got %d", resp.Edits.AddedObjectCount)
	}
	if resp.Edits.ModifiedObjectsCount != 0 {
		t.Fatalf("expected modifiedObjectsCount=0, got %d", resp.Edits.ModifiedObjectsCount)
	}
	if resp.Edits.DeletedObjectsCount != 0 {
		t.Fatalf("expected deletedObjectsCount=0, got %d", resp.Edits.DeletedObjectsCount)
	}
}

func TestHandler_Apply_ResponseEnvelope_HasOperationId(t *testing.T) {
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
	if resp.OperationID == "" {
		t.Fatal("expected non-empty operationId in response")
	}
}

func TestHandler_Apply_ResponseEnvelope_NoOldFields(t *testing.T) {
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

	// Old fields must NOT appear in the response JSON.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, oldField := range []string{"actionRid", "batchId", "offset"} {
		if _, ok := raw[oldField]; ok {
			t.Errorf("old field %q must NOT appear in SyncApplyActionResponseV2", oldField)
		}
	}
}

func TestHandler_Apply_ResponseEnvelope_ReturnEditsNONE_OmitsEdits(t *testing.T) {
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
		"options":    map[string]interface{}{"returnEdits": "NONE"},
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
	if resp.Edits != nil {
		t.Fatal("returnEdits=NONE must omit edits from SyncApplyActionResponseV2")
	}
}

func TestHandler_Apply_ResponseEnvelope_ModifyObject_Counts(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("setSalary", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "salary", Type: "double", Required: true},
			}, []Rule{
				{Type: "modifyObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"salary": {Type: "parameter", Value: "salary"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"primaryKey": "emp-1", "salary": float64(100)},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/setSalary/apply",
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
		t.Fatal("expected edits in response")
	}
	if resp.Edits.ModifiedObjectsCount != 1 {
		t.Fatalf("expected modifiedObjectsCount=1, got %d", resp.Edits.ModifiedObjectsCount)
	}
	if resp.Edits.AddedObjectCount != 0 {
		t.Fatalf("expected addedObjectCount=0, got %d", resp.Edits.AddedObjectCount)
	}
}

// ---------------------------------------------------------------------------
// US-319: SyncApplyActionResponseV2.ActionLogID — surfaces the persisted
// action_logs row id so the toast Undo button can call POST /actions/revert
// with {actionLogId} during its 5-second window.
// ---------------------------------------------------------------------------

func TestHandler_Apply_ResponseEnvelope_HasActionLogId(t *testing.T) {
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
	if resp.ActionLogID == 0 {
		t.Fatalf("expected non-zero actionLogId in response, got %#v", resp)
	}
	if got, want := resp.ActionLogID, repo.insertedLogs[0].ID; got != want {
		t.Fatalf("actionLogId mismatch: response=%d, persisted=%d", got, want)
	}
}

// ---------------------------------------------------------------------------
// US-002: Batch Apply — BatchApplyActionResponseV2 envelope
// ---------------------------------------------------------------------------

func TestHandler_ApplyBatch_ResponseEnvelope_HasEditsWithCounts(t *testing.T) {
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

	var resp BatchApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal BatchApplyActionResponseV2: %v", err)
	}
	if resp.Edits == nil {
		t.Fatal("expected edits field in batch response")
	}
	if resp.Edits.Type != "edits" {
		t.Fatalf("expected edits.type = \"edits\", got %q", resp.Edits.Type)
	}
	if resp.Edits.AddedObjectCount != 2 {
		t.Fatalf("expected addedObjectCount=2 for 2 createObject actions, got %d", resp.Edits.AddedObjectCount)
	}
}

func TestHandler_ApplyBatch_ResponseEnvelope_NoOldFields(t *testing.T) {
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

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, oldField := range []string{"mode", "batchId", "offset", "results", "appliedEdits", "failures"} {
		if _, ok := raw[oldField]; ok {
			t.Errorf("old field %q must NOT appear in BatchApplyActionResponseV2", oldField)
		}
	}
}

func TestHandler_ApplyBatch_ResponseEnvelope_ReturnEditsNONE_OmitsEdits(t *testing.T) {
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
		"options": map[string]interface{}{"returnEdits": "NONE"},
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

	var resp BatchApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Edits != nil {
		t.Fatal("returnEdits=NONE must omit edits from BatchApplyActionResponseV2")
	}
}

// ---------------------------------------------------------------------------
// US-002: ActionResults helper — countEdits
// ---------------------------------------------------------------------------

func TestCountEdits_MixedTypes(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee"},
		{Type: funnel.EditTypeCreate, ObjectType: "Employee"},
		{Type: funnel.EditTypeModify, ObjectType: "Department"},
		{Type: funnel.EditTypeDelete, ObjectType: "Employee"},
	}
	result := countEdits(edits)
	if result.Type != "edits" {
		t.Fatalf("expected type=\"edits\", got %q", result.Type)
	}
	if result.AddedObjectCount != 2 {
		t.Fatalf("expected addedObjectCount=2, got %d", result.AddedObjectCount)
	}
	if result.ModifiedObjectsCount != 1 {
		t.Fatalf("expected modifiedObjectsCount=1, got %d", result.ModifiedObjectsCount)
	}
	if result.DeletedObjectsCount != 1 {
		t.Fatalf("expected deletedObjectsCount=1, got %d", result.DeletedObjectsCount)
	}
	if result.AddedLinksCount != 0 {
		t.Fatalf("expected addedLinksCount=0, got %d", result.AddedLinksCount)
	}
	if result.DeletedLinksCount != 0 {
		t.Fatalf("expected deletedLinksCount=0, got %d", result.DeletedLinksCount)
	}
}

func TestCountEdits_Empty(t *testing.T) {
	result := countEdits(nil)
	if result.Type != "edits" {
		t.Fatalf("expected type=\"edits\", got %q", result.Type)
	}
	if result.AddedObjectCount != 0 || result.ModifiedObjectsCount != 0 || result.DeletedObjectsCount != 0 {
		t.Fatal("expected all counts to be 0 for empty edits")
	}
}
