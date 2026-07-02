package actions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// Foundry SyncApplyActionResponseV2 parity — edit-count field names +
// returnEdits enum validation.
//
// Two deviations from the Foundry OSv2 wire contract are locked down here:
//
//  1. ActionResults previously serialized `modifiedObjectCount` /
//     `deletedObjectCount` (singular "Object"). Foundry's
//     SyncApplyActionResponseV2 uses `modifiedObjectsCount` /
//     `deletedObjectsCount` (plural "Objects" — matching the already-correct
//     `addedLinksCount` / `deletedLinksCount`; `addedObjectCount` IS singular
//     in Foundry and stays untouched). OSDK clients deserializing the old
//     names silently read 0 for both counts.
//
//  2. Apply validated options.mode against a whitelist but accepted ANY
//     options.returnEdits string, silently treating unknown values as "ALL".
//     Foundry's enum is ALL | ALL_V2_WITH_DELETIONS | NONE; anything else
//     must be rejected with 400 (mirroring applyBatch's InvalidReturnEdits).

// TestBDD_ApplyAction_EditCountsUseFoundryFieldNames
//
//	Given an ontology with a modifyObject action type
//	When  POST .../actions/{action}/apply with options.returnEdits=ALL
//	Then  the response edits object carries Foundry's field names
//	      `modifiedObjectsCount` / `deletedObjectsCount`
//	And   the pre-fix singular names `modifiedObjectCount` /
//	      `deletedObjectCount` do NOT appear anywhere in the payload
//
//	Given a deleteObject action type
//	When  the action deletes one object
//	Then  `deletedObjectsCount` is 1 under the Foundry field name
func TestBDD_ApplyAction_EditCountsUseFoundryFieldNames(t *testing.T) {
	t.Run("modifyObject → modifiedObjectsCount key (plural), old key absent", func(t *testing.T) {
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
		router := setupRouter(NewHandler(NewExecutor(repo, &fakePublisher{offset: 1})))

		body := mustJSON(map[string]interface{}{
			"parameters": map[string]interface{}{"primaryKey": "emp-1", "salary": float64(100)},
			"options":    map[string]interface{}{"returnEdits": "ALL"},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/setSalary/apply", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		edits := decodeEditsRaw(t, w.Body.Bytes())

		if got, ok := edits["modifiedObjectsCount"]; !ok {
			t.Errorf("edits must carry Foundry field name %q; payload = %s", "modifiedObjectsCount", w.Body.String())
		} else if got != float64(1) {
			t.Errorf("modifiedObjectsCount = %v, want 1", got)
		}
		if _, ok := edits["deletedObjectsCount"]; !ok {
			t.Errorf("edits must carry Foundry field name %q; payload = %s", "deletedObjectsCount", w.Body.String())
		}
		for _, oldKey := range []string{"modifiedObjectCount", "deletedObjectCount"} {
			if _, ok := edits[oldKey]; ok {
				t.Errorf("pre-fix field name %q must NOT appear in edits; payload = %s", oldKey, w.Body.String())
			}
		}
	})

	t.Run("deleteObject → deletedObjectsCount=1 under Foundry field name", func(t *testing.T) {
		repo := &mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("fireEmployee", []ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
				}, []Rule{
					{Type: "deleteObject", ObjectType: "Employee"},
				}),
			},
		}
		router := setupRouter(NewHandler(NewExecutor(repo, &fakePublisher{offset: 1})))

		body := mustJSON(map[string]interface{}{
			"parameters": map[string]interface{}{"primaryKey": "emp-1"},
			"options":    map[string]interface{}{"returnEdits": "ALL"},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/fireEmployee/apply", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		edits := decodeEditsRaw(t, w.Body.Bytes())
		if got := edits["deletedObjectsCount"]; got != float64(1) {
			t.Errorf("deletedObjectsCount = %v, want 1; payload = %s", got, w.Body.String())
		}
		if _, ok := edits["deletedObjectCount"]; ok {
			t.Errorf("pre-fix field name %q must NOT appear in edits; payload = %s", "deletedObjectCount", w.Body.String())
		}
	})
}

// TestBDD_ApplyAction_ReturnEditsWhitelist
//
//	Given a valid createObject action type
//	When  POST apply with options.returnEdits="BOGUS"
//	Then  HTTP 400 with errorName "InvalidReturnEdits" (mirrors InvalidMode)
//
//	When  options.returnEdits="ALL_V2_WITH_DELETIONS"
//	Then  HTTP 200 with an edits summary (currently identical to ALL)
//
//	When  options.returnEdits="none" (lowercase)
//	Then  HTTP 200 with edits omitted (case-insensitive normalization)
func TestBDD_ApplyAction_ReturnEditsWhitelist(t *testing.T) {
	newRouter := func() http.Handler {
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
		return setupRouter(NewHandler(NewExecutor(repo, &fakePublisher{offset: 1})))
	}

	postApply := func(t *testing.T, router http.Handler, returnEdits string) *httptest.ResponseRecorder {
		t.Helper()
		body := mustJSON(map[string]interface{}{
			"parameters": map[string]interface{}{"name": "Alice"},
			"options":    map[string]interface{}{"returnEdits": returnEdits},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/createEmployee/apply", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("BOGUS → 400 InvalidReturnEdits", func(t *testing.T) {
		w := postApply(t, newRouter(), "BOGUS")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal error envelope: %v", err)
		}
		if env.ErrorName != "InvalidReturnEdits" {
			t.Errorf("errorName = %q, want %q; body = %s", env.ErrorName, "InvalidReturnEdits", w.Body.String())
		}
	})

	t.Run("ALL_V2_WITH_DELETIONS → 200 with edits", func(t *testing.T) {
		w := postApply(t, newRouter(), "ALL_V2_WITH_DELETIONS")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		edits := decodeEditsRaw(t, w.Body.Bytes())
		if got := edits["addedObjectCount"]; got != float64(1) {
			t.Errorf("addedObjectCount = %v, want 1; payload = %s", got, w.Body.String())
		}
	})

	t.Run("lowercase none → 200 with edits omitted", func(t *testing.T) {
		w := postApply(t, newRouter(), "none")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, ok := resp["edits"]; ok {
			t.Errorf("returnEdits=none must omit edits; payload = %s", w.Body.String())
		}
	})
}

// decodeEditsRaw pulls the `edits` object out of a response body as a raw
// map so key NAMES (not just Go-side values) can be asserted.
func decodeEditsRaw(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var resp struct {
		Edits map[string]interface{} `json:"edits"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Edits == nil {
		t.Fatalf("expected edits object in response; body = %s", string(body))
	}
	return resp.Edits
}
