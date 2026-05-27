package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-209 Bidirectional Links: admin handlers must reject InverseLinkRID that
// point at partner LinkTypes whose endpoints do not mirror this row's, and
// accept the symmetric case.

func newLinkTypeRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/linkTypes", handler.CreateLinkType)
	r.Put("/api/admin/linkTypes/{linkTypeRid}", handler.UpdateLinkType)
	return r
}

func seedMockOntology(repo *mockRepo) string {
	const ontRID = "ri.ontology.main.ontology.1"
	repo.ontologies = append(repo.ontologies, oms.Ontology{
		RID: ontRID, APIName: "test", DisplayName: "Test",
	})
	return ontRID
}

func TestCreateLinkType_WithInverseLinkRID_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	// Pre-seed forward link A: employee -> department.
	forward := oms.LinkType{
		RID:              "ri.ontology.main.link-type.emp-dept",
		OntologyRID:      ontRID,
		APIName:          "employeeDepartment",
		SourceObjectType: "employee",
		TargetObjectType: "department",
		Cardinality:      "MANY_TO_ONE",
	}
	repo.linkTypes = append(repo.linkTypes, forward)

	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	// Create inverse B: department -> employee with inverseLinkRid=A.
	body := map[string]interface{}{
		"apiName":                 "departmentEmployees",
		"displayName":             "Department Employees",
		"objectTypeApiName":       "department",
		"linkedObjectTypeApiName": "employee",
		"cardinality":             "ONE_TO_MANY",
		"inverseLinkRid":          forward.RID,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["inverseLinkRid"] != forward.RID {
		t.Errorf("expected inverseLinkRid=%q, got %v", forward.RID, resp["inverseLinkRid"])
	}
	// Persistence: the mock repo received a record with InverseLinkRID set.
	if len(repo.linkTypes) != 2 {
		t.Fatalf("expected 2 link types after create, got %d", len(repo.linkTypes))
	}
	got := repo.linkTypes[1]
	if got.InverseLinkRID != forward.RID {
		t.Errorf("stored InverseLinkRID = %q, want %q", got.InverseLinkRID, forward.RID)
	}
}

func TestCreateLinkType_WithInverseLinkRID_NotFound(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"x","displayName":"X","objectTypeApiName":"a","linkedObjectTypeApiName":"b","cardinality":"MANY_TO_ONE","inverseLinkRid":"ri.ontology.main.link-type.missing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing inverse, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorName"] != "InverseLinkTypeNotFound" {
		t.Errorf("expected errorName=InverseLinkTypeNotFound, got %v", resp["errorName"])
	}
}

func TestCreateLinkType_WithInverseLinkRID_EndpointMismatch(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	// Partner link A: employee -> department. New B declares
	// employee -> employee, so partner.source (employee) == B.target (employee)
	// holds but partner.target (department) != B.source (employee).
	forward := oms.LinkType{
		RID:              "ri.ontology.main.link-type.emp-dept",
		OntologyRID:      ontRID,
		APIName:          "employeeDepartment",
		SourceObjectType: "employee",
		TargetObjectType: "department",
		Cardinality:      "MANY_TO_ONE",
	}
	repo.linkTypes = append(repo.linkTypes, forward)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := map[string]interface{}{
		"apiName":                 "selfLoop",
		"displayName":             "Self Loop",
		"objectTypeApiName":       "employee",
		"linkedObjectTypeApiName": "employee",
		"cardinality":             "ONE_TO_MANY",
		"inverseLinkRid":          forward.RID,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on endpoint mismatch, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if !strings.Contains(resp["errorName"].(string), "inverseLinkRid") {
		t.Errorf("expected errorName mentioning inverseLinkRid, got %v", resp["errorName"])
	}
	params, _ := resp["parameters"].(map[string]interface{})
	if params["expectedSourceObjectType"] != "employee" || params["expectedTargetObjectType"] != "employee" {
		t.Errorf("expected endpoint hints in parameters, got %v", params)
	}
	// Persistence must NOT have happened — only the pre-seeded row remains.
	if len(repo.linkTypes) != 1 {
		t.Errorf("expected mismatch to block persistence, stored %d rows", len(repo.linkTypes))
	}
}

func TestCreateLinkType_WithInverseLinkRID_CrossOntology(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	otherOnt := oms.Ontology{RID: "ri.ontology.other.ontology.2", APIName: "other", DisplayName: "Other"}
	repo.ontologies = append(repo.ontologies, otherOnt)
	forward := oms.LinkType{
		RID:              "ri.ontology.other.link-type.emp-dept",
		OntologyRID:      otherOnt.RID,
		APIName:          "employeeDepartment",
		SourceObjectType: "employee",
		TargetObjectType: "department",
		Cardinality:      "MANY_TO_ONE",
	}
	repo.linkTypes = append(repo.linkTypes, forward)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := map[string]interface{}{
		"apiName":                 "departmentEmployees",
		"displayName":             "Dept Employees",
		"objectTypeApiName":       "department",
		"linkedObjectTypeApiName": "employee",
		"cardinality":             "ONE_TO_MANY",
		"inverseLinkRid":          forward.RID,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on cross-ontology inverse, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	params, _ := resp["parameters"].(map[string]interface{})
	if !strings.Contains(params["reason"].(string), "same ontology") {
		t.Errorf("expected cross-ontology reason, got %v", params)
	}
}

func TestUpdateLinkType_WithInverseLinkRID_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	// Pair: A (emp -> dept) and B (dept -> emp), both already persisted.
	a := oms.LinkType{
		RID: "ri.ontology.main.link-type.emp-dept", OntologyRID: ontRID,
		APIName: "employeeDepartment", SourceObjectType: "employee", TargetObjectType: "department",
		Cardinality: "MANY_TO_ONE",
	}
	b := oms.LinkType{
		RID: "ri.ontology.main.link-type.dept-emp", OntologyRID: ontRID,
		APIName: "departmentEmployees", SourceObjectType: "department", TargetObjectType: "employee",
		Cardinality: "ONE_TO_MANY",
	}
	repo.linkTypes = append(repo.linkTypes, a, b)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	// Point B -> A.
	body := `{"displayName":"Department Employees","inverseLinkRid":"` + a.RID + `"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+b.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["inverseLinkRid"] != a.RID {
		t.Errorf("expected updated inverseLinkRid=%q, got %v", a.RID, resp["inverseLinkRid"])
	}
}

func TestUpdateLinkType_WithInverseLinkRID_ClearedByEmptyString(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	a := oms.LinkType{
		RID: "ri.ontology.main.link-type.emp-dept", OntologyRID: ontRID,
		APIName: "a", SourceObjectType: "x", TargetObjectType: "y",
		Cardinality: "MANY_TO_ONE",
	}
	b := oms.LinkType{
		RID: "ri.ontology.main.link-type.dept-emp", OntologyRID: ontRID,
		APIName: "b", SourceObjectType: "y", TargetObjectType: "x",
		Cardinality:    "ONE_TO_MANY",
		InverseLinkRID: a.RID,
	}
	repo.linkTypes = append(repo.linkTypes, a, b)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"cleared","inverseLinkRid":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+b.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on clear, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if _, present := resp["inverseLinkRid"]; present {
		t.Errorf("expected inverseLinkRid omitted after clear, got %v", resp["inverseLinkRid"])
	}
}

func TestUpdateLinkType_WithInverseLinkRID_EndpointMismatch(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	a := oms.LinkType{
		RID: "ri.ontology.main.link-type.emp-dept", OntologyRID: ontRID,
		APIName: "a", SourceObjectType: "x", TargetObjectType: "y",
		Cardinality: "MANY_TO_ONE",
	}
	// B has wrong endpoints for A's inverse.
	b := oms.LinkType{
		RID: "ri.ontology.main.link-type.bad", OntologyRID: ontRID,
		APIName: "b", SourceObjectType: "x", TargetObjectType: "z",
		Cardinality: "ONE_TO_MANY",
	}
	repo.linkTypes = append(repo.linkTypes, a, b)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"bad","inverseLinkRid":"` + a.RID + `"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+b.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on mismatch, got %d: %s", w.Code, w.Body.String())
	}
}
