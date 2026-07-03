package actions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// Foundry SyncApplyActionResponseV2 `edits.edits[]` detail-array parity.
//
// Prior to this change ActionResults carried only the five counts + type:
// "edits" with no per-edit detail. Foundry's ObjectEdits model nests an
// `edits: ObjectEdit[]` array (osdk-ts PR #1271 / foundry-platform-python):
//
//   - returnEdits=ALL                    → addObject / modifyObject / addLink
//   - returnEdits=ALL_V2_WITH_DELETIONS  → + deleteObject / deleteLink
//   - returnEdits=NONE                   → whole edits object omitted
//
// deletedObjectsCount / deletedLinksCount stay in the summary regardless; only
// the detail array gates on the deletions flag.

// decodeEditsDetail pulls the nested `edits.edits[]` detail array out of a
// SyncApplyActionResponseV2 body as raw maps so wire field NAMES (not just
// Go values) can be asserted.
func decodeEditsDetail(t *testing.T, body []byte) []map[string]interface{} {
	t.Helper()
	var resp struct {
		Edits struct {
			Edits []map[string]interface{} `json:"edits"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body = %s", err, string(body))
	}
	return resp.Edits.Edits
}

// TestBDD_ApplyAction_EditsDetailArray_ObjectVariants
//
//	Given a createOrModifyObject action type resolving to CREATE (no existence
//	      checker) so the addObject variant carries the supplied primary key
//	When  POST .../apply with options.returnEdits=ALL
//	Then  edits.edits[] carries one {type:addObject, primaryKey, objectType}
func TestBDD_ApplyAction_EditsDetailArray_ObjectVariants(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createOrModifyObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	router := setupRouter(NewHandler(NewExecutor(repo, &fakePublisher{offset: 1})))

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"primaryKey": "emp-1", "name": "Alice"},
		"options":    map[string]interface{}{"returnEdits": "ALL"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	detail := decodeEditsDetail(t, w.Body.Bytes())
	if len(detail) != 1 {
		t.Fatalf("expected 1 detail edit, got %d; body = %s", len(detail), w.Body.String())
	}
	e := detail[0]
	if e["type"] != "addObject" {
		t.Errorf("detail[0].type = %v, want addObject; body = %s", e["type"], w.Body.String())
	}
	if e["primaryKey"] != "emp-1" {
		t.Errorf("detail[0].primaryKey = %v, want emp-1", e["primaryKey"])
	}
	if e["objectType"] != "Employee" {
		t.Errorf("detail[0].objectType = %v, want Employee", e["objectType"])
	}
}

// TestBDD_ApplyAction_EditsDetailArray_LinkVariant
//
//	Given a createLink action type and a LinkType with an inverse partner
//	When  POST .../apply with options.returnEdits=ALL
//	Then  edits.edits[] carries one addLink variant with linkTypeApiNameAtoB /
//	      linkTypeApiNameBtoA and aSideObject / bSideObject descriptors
func TestBDD_ApplyAction_EditsDetailArray_LinkVariant(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("assignEmployee", []ParameterDef{
				{ID: "empId", Type: "string", Required: true},
				{ID: "deptId", Type: "string", Required: true},
			}, []Rule{
				{Type: "createLink",
					LinkTypeAPIName:        "worksIn",
					SourceObjectPrimaryKey: "empId",
					TargetObjectPrimaryKey: "deptId"},
			}),
		},
		linkTypesByAPIName: map[string]*oms.LinkType{
			"worksIn": {
				RID:              "ri.ontology.main.link-type.worksIn",
				APIName:          "worksIn",
				SourceObjectType: "Employee",
				TargetObjectType: "Department",
				InverseLinkRID:   "ri.ontology.main.link-type.employees",
			},
		},
		linkTypesByRID: map[string]*oms.LinkType{
			"ri.ontology.main.link-type.employees": {
				RID:     "ri.ontology.main.link-type.employees",
				APIName: "employees",
			},
		},
	}
	router := setupRouter(NewHandler(NewExecutor(repo, &fakePublisher{offset: 1})))

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"empId": "emp-1", "deptId": "dept-9"},
		"options":    map[string]interface{}{"returnEdits": "ALL"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/assignEmployee/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	detail := decodeEditsDetail(t, w.Body.Bytes())
	if len(detail) != 1 {
		t.Fatalf("expected 1 detail edit, got %d; body = %s", len(detail), w.Body.String())
	}
	e := detail[0]
	if e["type"] != "addLink" {
		t.Fatalf("detail[0].type = %v, want addLink; body = %s", e["type"], w.Body.String())
	}
	if e["linkTypeApiNameAtoB"] != "worksIn" {
		t.Errorf("linkTypeApiNameAtoB = %v, want worksIn", e["linkTypeApiNameAtoB"])
	}
	if e["linkTypeApiNameBtoA"] != "employees" {
		t.Errorf("linkTypeApiNameBtoA = %v, want employees", e["linkTypeApiNameBtoA"])
	}
	aSide, _ := e["aSideObject"].(map[string]interface{})
	if aSide == nil || aSide["primaryKey"] != "emp-1" || aSide["objectType"] != "Employee" {
		t.Errorf("aSideObject = %v, want emp-1/Employee", e["aSideObject"])
	}
	bSide, _ := e["bSideObject"].(map[string]interface{})
	if bSide == nil || bSide["primaryKey"] != "dept-9" || bSide["objectType"] != "Department" {
		t.Errorf("bSideObject = %v, want dept-9/Department", e["bSideObject"])
	}
}

// TestBDD_ApplyAction_EditsDetailArray_DeletionGating
//
//	Given a deleteObject action type
//	When  returnEdits=ALL                   → edits[] omits deleteObject
//	                                          (deletedObjectsCount still 1)
//	When  returnEdits=ALL_V2_WITH_DELETIONS → edits[] includes deleteObject
func TestBDD_ApplyAction_EditsDetailArray_DeletionGating(t *testing.T) {
	newRouter := func() http.Handler {
		repo := &mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("fireEmployee", []ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
				}, []Rule{
					{Type: "deleteObject", ObjectType: "Employee"},
				}),
			},
		}
		return setupRouter(NewHandler(NewExecutor(repo, &fakePublisher{offset: 1})))
	}

	post := func(t *testing.T, router http.Handler, returnEdits string) *httptest.ResponseRecorder {
		t.Helper()
		body := mustJSON(map[string]interface{}{
			"parameters": map[string]interface{}{"primaryKey": "emp-1"},
			"options":    map[string]interface{}{"returnEdits": returnEdits},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/fireEmployee/apply", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("ALL omits deleteObject from detail but keeps the count", func(t *testing.T) {
		w := post(t, newRouter(), "ALL")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if got := decodeEditsRaw(t, w.Body.Bytes())["deletedObjectsCount"]; got != float64(1) {
			t.Errorf("deletedObjectsCount = %v, want 1", got)
		}
		for _, e := range decodeEditsDetail(t, w.Body.Bytes()) {
			if e["type"] == "deleteObject" {
				t.Errorf("returnEdits=ALL must omit deleteObject from edits[]; body = %s", w.Body.String())
			}
		}
	})

	t.Run("ALL_V2_WITH_DELETIONS includes deleteObject", func(t *testing.T) {
		w := post(t, newRouter(), "ALL_V2_WITH_DELETIONS")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		detail := decodeEditsDetail(t, w.Body.Bytes())
		if len(detail) != 1 || detail[0]["type"] != "deleteObject" || detail[0]["primaryKey"] != "emp-1" {
			t.Errorf("V2 detail = %v, want one deleteObject/emp-1; body = %s", detail, w.Body.String())
		}
	})
}
