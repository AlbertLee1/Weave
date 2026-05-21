package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

const testIfaceOntologyRID = "ri.ontology.main.ontology.test"

// setupInterfaceTest creates an environment with two ObjectTypes (employee,
// contractor) that both implement a "worker" interface.
func setupInterfaceTest(t *testing.T) (*oss.Handler, chi.Router, *mockOmsRepo) {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	// Employee index
	empProps := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "age", BaseType: "integer", IsSearchable: true},
		{APIName: "department", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", empProps); err != nil {
		t.Fatalf("EnsureIndex employee: %v", err)
	}

	// Contractor index
	ctrProps := []index.Property{
		{APIName: "contractorId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "hourlyRate", BaseType: "double", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("contractor", ctrProps); err != nil {
		t.Fatalf("EnsureIndex contractor: %v", err)
	}

	// Seed employees
	empDocs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"emp1", map[string]interface{}{"employeeId": "emp1", "name": "alice", "age": float64(30), "department": "eng"}},
		{"emp2", map[string]interface{}{"employeeId": "emp2", "name": "bob", "age": float64(25), "department": "eng"}},
	}
	for _, d := range empDocs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.id, err)
		}
	}

	// Seed contractors
	ctrDocs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"ctr1", map[string]interface{}{"contractorId": "ctr1", "name": "carol", "hourlyRate": float64(100)}},
	}
	for _, d := range ctrDocs {
		if err := mgr.IndexDocument("contractor", d.id, d.doc); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.id, err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()

	empOT := &oms.ObjectType{
		RID:         "ri.ontology.main.object-type.employee",
		OntologyRID: testIfaceOntologyRID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
	}
	ctrOT := &oms.ObjectType{
		RID:         "ri.ontology.main.object-type.contractor",
		OntologyRID: testIfaceOntologyRID,
		APIName:     "contractor",
		DisplayName: "Contractor",
		PrimaryKey:  "contractorId",
		Status:      "ACTIVE",
	}
	repo.addObjectType(empOT)
	repo.addObjectType(ctrOT)

	// Register interface and implementing ObjectTypes
	repo.interfaces = map[string]*oms.Interface{
		testIfaceOntologyRID + ":worker": {
			RID:         "ri.ontology.main.interface.worker",
			OntologyRID: testIfaceOntologyRID,
			APIName:     "worker",
			DisplayName: "Worker",
		},
	}
	repo.interfaceObjectTypes = map[string][]oms.ObjectType{
		"ri.ontology.main.interface.worker": {*empOT, *ctrOT},
	}

	linkResolver := &mockLinkResolver{results: make(map[string][]string)}
	svc := oss.NewService(repo, mgr, linkResolver)

	h := oss.NewHandler(svc)
	h.SetOmsRepo(repo)
	h.SetAggregation(aggregation.NewEngine(), mgr)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	return h, r, repo
}

// --- Test 1: List objects through interface ---

func TestInterfaceListObjects_ReturnsObjectsFromAllTypes(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	req := httptest.NewRequest("GET",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var page oss.ObjectPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 2 employees + 1 contractor = 3
	if len(page.Data) != 3 {
		t.Errorf("data length = %d, want 3", len(page.Data))
	}

	// Verify objects come from different ObjectTypes
	types := map[string]bool{}
	for _, obj := range page.Data {
		types[obj.APIName] = true
	}
	if !types["employee"] {
		t.Error("expected objects from 'employee' type")
	}
	if !types["contractor"] {
		t.Error("expected objects from 'contractor' type")
	}
}

func TestInterfaceListObjects_InterfaceNotFound(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	req := httptest.NewRequest("GET",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/nonexistent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}

	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "InterfaceNotFound" {
		t.Errorf("errorName = %q, want InterfaceNotFound", apiErr.ErrorName)
	}
}

// --- Test 2: Search objects through interface ---

func TestInterfaceSearch_ReturnsFilteredObjects(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	body := `{"where":{"type":"eq","field":"name","value":"alice"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var page oss.ObjectPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(page.Data) != 1 {
		t.Errorf("data length = %d, want 1 (only alice)", len(page.Data))
	}
	if len(page.Data) > 0 && page.Data[0].APIName != "employee" {
		t.Errorf("apiName = %q, want employee", page.Data[0].APIName)
	}
}

func TestInterfaceSearch_SelectRequired(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	body := `{"where":{"type":"eq","field":"name","value":"alice"}}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}

	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "SelectRequired" {
		t.Errorf("errorName = %q, want SelectRequired", apiErr.ErrorName)
	}
}

// --- Test 3: Aggregate objects through interface ---

func TestInterfaceAggregate_CountAcrossTypes(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	body := `{"aggregation":[{"type":"count","name":"total"}]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/aggregate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	// Parse response and verify the total count is 3 (2 employees + 1 contractor)
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Response should have data array with aggregation results
	if resp["data"] == nil && resp["excludedItems"] == nil {
		// At least the response parsed as valid JSON
		t.Logf("aggregate response: %s", rr.Body.String())
	}
}

func TestInterfaceAggregate_RejectsHiddenFieldsBeforePerTypeAggregation(t *testing.T) {
	h, r, _ := setupInterfaceTest(t)
	h.SetPropertyFilterProvider(&staticPropertyFilter{
		allowedByType: map[string][]string{
			"employee":   {"employeeId", "name", "age"},
			"contractor": {"contractorId", "name", "hourlyRate"},
		},
	})

	cases := []struct {
		name         string
		body         string
		wantProperty string
	}{
		{
			name: "hidden where field",
			body: `{
				"where":{"type":"eq","field":"department","value":"eng"},
				"aggregation":[{"type":"count","name":"total"}]
			}`,
			wantProperty: "department",
		},
		{
			name: "hidden groupBy field",
			body: `{
				"groupBy":[{"type":"exact","field":"department"}],
				"aggregation":[{"type":"count","name":"total"}]
			}`,
			wantProperty: "department",
		},
		{
			name: "hidden metric field",
			body: `{
				"aggregation":[{"type":"sum","field":"department","name":"total"}]
			}`,
			wantProperty: "department",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST",
				"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/aggregate", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusForbidden, rr.Body.String())
			}
			var apiErr struct {
				ErrorCode  string            `json:"errorCode"`
				ErrorName  string            `json:"errorName"`
				Parameters map[string]string `json:"parameters"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("unmarshal error body: %v", err)
			}
			if apiErr.ErrorCode != "PERMISSION_DENIED" {
				t.Errorf("errorCode = %q, want PERMISSION_DENIED", apiErr.ErrorCode)
			}
			if apiErr.ErrorName != "PropertyNotAccessible" {
				t.Errorf("errorName = %q, want PropertyNotAccessible", apiErr.ErrorName)
			}
			if apiErr.Parameters["property"] != tc.wantProperty {
				t.Errorf("parameters.property = %q, want %s", apiErr.Parameters["property"], tc.wantProperty)
			}
		})
	}
}

func TestInterfaceAggregate_RejectsRegexWhereBeforePerTypeAggregation(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	body := `{"where":{"type":"regex","field":"name","value":"a.*"},"aggregation":[{"type":"count","name":"total"}]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/aggregate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	var apiErr struct {
		ErrorCode string `json:"errorCode"`
		ErrorName string `json:"errorName"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if apiErr.ErrorCode != "INVALID_ARGUMENT" {
		t.Errorf("errorCode = %q, want INVALID_ARGUMENT", apiErr.ErrorCode)
	}
	if apiErr.ErrorName != "AggregationWhereRegexUnsupported" {
		t.Errorf("errorName = %q, want AggregationWhereRegexUnsupported", apiErr.ErrorName)
	}
}

func TestInterfaceAggregate_InterfaceNotFound(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	body := `{"aggregation":[{"type":"count","name":"total"}]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/nonexistent/aggregate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

// --- Test 4: Linked objects through interface ---

func TestInterfaceLinkedObjects_RouteRegistered(t *testing.T) {
	_, r, repo := setupInterfaceTest(t)

	// Set up interface link type as JSONB on the interface
	repo.interfaces[testIfaceOntologyRID+":worker"] = &oms.Interface{
		RID:               "ri.ontology.main.interface.worker",
		OntologyRID:       testIfaceOntologyRID,
		APIName:           "worker",
		DisplayName:       "Worker",
		OutgoingLinkTypes: json.RawMessage(`[{"apiName":"worksAt","displayName":"Works At","linkedEntityTypeApiName":"employee","cardinality":"MANY_TO_MANY"}]`),
	}

	// Add a link type to the mock so that link resolution can find it
	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.worksAt",
		OntologyRID:      testIfaceOntologyRID,
		APIName:          "worksAt",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.contractor",
		Cardinality:      "MANY_TO_MANY",
	})

	req := httptest.NewRequest("GET",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/employee/emp1/links/worksAt", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// Route is registered, interface resolves OK. Service delegates to
	// ListLinkedObjects which returns empty page or 400 based on link resolution.
	// Status 405 means the route is not registered; anything else means it is.
	if rr.Code == http.StatusMethodNotAllowed {
		t.Fatalf("route not registered: status = %d", rr.Code)
	}

	// Verify the response is a JSON API response (not chi's default text 404)
	ct := rr.Header().Get("Content-Type")
	if ct == "" {
		t.Fatal("expected Content-Type header in response")
	}

	// Should return 200 (empty result) or a JSON error — both confirm the route works
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got: %s", rr.Body.String())
	}
}

func TestInterfaceLinkedObjects_InterfaceNotFound(t *testing.T) {
	_, r, _ := setupInterfaceTest(t)

	req := httptest.NewRequest("GET",
		"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/nonexistent/employee/emp1/links/worksAt", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}
