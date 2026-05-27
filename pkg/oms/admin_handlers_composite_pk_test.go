package oms_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-211 Composite Primary Keys (admin CRUD): the create handler must accept
// either the legacy `primaryKey` string field or the new `primaryKeys` array.
// At least one must be supplied. Both single-element and multi-element forms
// round-trip through Get/Update/Delete.

func newObjectTypeAdminRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)
	r.Put("/api/admin/objectTypes/{objectTypeRid}", handler.UpdateObjectType)
	r.Delete("/api/admin/objectTypes/{objectTypeRid}", handler.DeleteObjectType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)
	return r
}

func TestCreateObjectType_WithCompositePrimaryKeys_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newObjectTypeAdminRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"orderDetail","displayName":"Order Detail","primaryKeys":["orderId","lineNumber"],"status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())

	pkArr, ok := resp["primaryKeys"].([]interface{})
	if !ok {
		t.Fatalf("expected primaryKeys array in response, got %v", resp["primaryKeys"])
	}
	if len(pkArr) != 2 || pkArr[0] != "orderId" || pkArr[1] != "lineNumber" {
		t.Errorf("expected [orderId lineNumber], got %v", pkArr)
	}
	// The legacy primaryKey field must be set to the FIRST element so single-PK
	// consumers (Bleve doc IDs, link FK lookups) keep functioning unchanged.
	if resp["primaryKey"] != "orderId" {
		t.Errorf("expected primaryKey='orderId' (first element), got %v", resp["primaryKey"])
	}

	// Persistence: the mock repo received both fields.
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 object type after create, got %d", len(repo.objectTypes))
	}
	got := repo.objectTypes[0]
	if got.PrimaryKey != "orderId" {
		t.Errorf("stored PrimaryKey = %q, want orderId", got.PrimaryKey)
	}
	if len(got.PrimaryKeys) != 2 || got.PrimaryKeys[0] != "orderId" || got.PrimaryKeys[1] != "lineNumber" {
		t.Errorf("stored PrimaryKeys = %v, want [orderId lineNumber]", got.PrimaryKeys)
	}
	if !got.IsCompositeKey() {
		t.Error("stored ObjectType.IsCompositeKey() should be true")
	}
}

func TestCreateObjectType_LegacyPrimaryKeyStillWorks(t *testing.T) {
	// Backward compat: senders that only know about the singular `primaryKey`
	// field continue to succeed; the handler synthesises a single-element
	// PrimaryKeys list so wire format is consistent.
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newObjectTypeAdminRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"employee","displayName":"Employee","primaryKey":"employeeId","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["primaryKey"] != "employeeId" {
		t.Errorf("expected primaryKey='employeeId', got %v", resp["primaryKey"])
	}
	pkArr, ok := resp["primaryKeys"].([]interface{})
	if !ok || len(pkArr) != 1 || pkArr[0] != "employeeId" {
		t.Errorf("expected primaryKeys=[employeeId], got %v", resp["primaryKeys"])
	}
	if repo.objectTypes[0].IsCompositeKey() {
		t.Error("single-element PK should not report composite")
	}
}

func TestCreateObjectType_MissingBothPrimaryKeyFields(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newObjectTypeAdminRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"employee","displayName":"Employee","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorName"] != "InvalidParameter:primaryKey" {
		t.Errorf("expected errorName=InvalidParameter:primaryKey, got %v", resp["errorName"])
	}
}

func TestCreateObjectType_RejectsEmptyPrimaryKeysElement(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newObjectTypeAdminRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"x","displayName":"X","primaryKeys":["orderId",""],"status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty element, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorName"] != "InvalidParameter:primaryKeys" {
		t.Errorf("expected errorName=InvalidParameter:primaryKeys, got %v", resp["errorName"])
	}
}

func TestObjectTypeWireFormat_IncludesPrimaryKeysOnGet(t *testing.T) {
	// Simulates a row loaded from a post-migration DB: PrimaryKey + PrimaryKeys
	// both populated. The V2 wire format must include both fields.
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.od", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "orderDetail", DisplayName: "Order Detail",
				PrimaryKey:  "orderId",
				PrimaryKeys: []string{"orderId", "lineNumber"},
				Status:      "ACTIVE", Visibility: "NORMAL",
			},
		},
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	r := newObjectTypeAdminRouter(oms.NewOMSHandler(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/orderDetail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["primaryKey"] != "orderId" {
		t.Errorf("expected primaryKey='orderId', got %v", resp["primaryKey"])
	}
	pkArr, ok := resp["primaryKeys"].([]interface{})
	if !ok || len(pkArr) != 2 || pkArr[0] != "orderId" || pkArr[1] != "lineNumber" {
		t.Errorf("expected primaryKeys=[orderId lineNumber], got %v", resp["primaryKeys"])
	}
}

// Full CRUD round trip for a composite-PK ObjectType:
//
//	create → get → update (display only) → delete.
//
// Acceptance criterion mandates "创建+get+update+delete 全流程".
func TestObjectType_CompositePK_FullCRUDFlow(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	repo.ontologies[0].APIName = "test"
	r := newObjectTypeAdminRouter(oms.NewOMSHandler(repo))

	// CREATE
	createBody := `{"apiName":"orderDetail","displayName":"Order Detail","primaryKeys":["orderId","lineNumber"],"status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CREATE: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w.Body.Bytes())
	otRID, _ := created["rid"].(string)
	if otRID == "" {
		t.Fatal("CREATE: missing rid in response")
	}

	// GET
	req = httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+ontRID+"/objectTypes/orderDetail", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := parseJSON(t, w.Body.Bytes())
	if got["apiName"] != "orderDetail" {
		t.Errorf("GET: expected apiName='orderDetail', got %v", got["apiName"])
	}
	pkArr, ok := got["primaryKeys"].([]interface{})
	if !ok || len(pkArr) != 2 {
		t.Errorf("GET: expected primaryKeys array of length 2, got %v", got["primaryKeys"])
	}

	// UPDATE displayName + deprecate (DELETE refuses ACTIVE/PROMOTED rows so
	// the deprecation has to happen before the final cleanup step).
	updateBody := `{"displayName":"Updated Order Detail","status":"DEPRECATED","visibility":"NORMAL"}`
	req = httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+otRID, strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UPDATE: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.objectTypes[0].DisplayName != "Updated Order Detail" {
		t.Errorf("UPDATE: stored displayName=%q, want 'Updated Order Detail'", repo.objectTypes[0].DisplayName)
	}
	if len(repo.objectTypes[0].PrimaryKeys) != 2 {
		t.Errorf("UPDATE: PrimaryKeys lost after update, got %v", repo.objectTypes[0].PrimaryKeys)
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/objectTypes/"+otRID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.objectTypes) != 0 {
		t.Errorf("DELETE: expected 0 object types after delete, got %d", len(repo.objectTypes))
	}
}

// Composite-key URL routing: chi treats `:` as an opaque path character, so
// the `/objects/{objectType}/{key1}:{key2}` shape requires no router changes.
// The handler reads the raw segment and downstream code (oms.ParseCompositeKey)
// is what splits it. This test asserts the chi-level capture works.
func TestCompositePKRoute_ChiCapturesColonDelimitedSegment(t *testing.T) {
	captured := ""
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}", func(w http.ResponseWriter, req *http.Request) {
		captured = chi.URLParam(req, "primaryKey")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/main/objects/orderDetail/10248:11", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured != "10248:11" {
		t.Fatalf("expected captured PK='10248:11', got %q", captured)
	}
	// And the helper unpacks it to the declared shape:
	parts, err := oms.ParseCompositeKey(captured, 2)
	if err != nil {
		t.Fatalf("ParseCompositeKey unexpected error: %v", err)
	}
	if len(parts) != 2 || parts[0] != "10248" || parts[1] != "11" {
		t.Errorf("expected [10248 11], got %v", parts)
	}

	// JoinCompositeKey is the inverse — the canonical string the route accepted
	// is what we'd surface back to the client.
	if got := oms.JoinCompositeKey(parts); got != "10248:11" {
		t.Errorf("JoinCompositeKey round trip mismatch: got %q", got)
	}
}
