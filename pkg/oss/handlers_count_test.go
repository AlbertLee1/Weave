package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

func TestCountObjects_ReturnsCount(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	h := oss.NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/count", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// setupOSSTest seeds 3 employees
	if resp.Count != 3 {
		t.Errorf("count = %d, want 3", resp.Count)
	}
}

func TestCountObjects_ObjectTypeNotFound(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	h := oss.NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testOntologyRID+"/objects/nonexistent/count", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}

	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "ObjectTypeNotFound" {
		t.Errorf("errorName = %q, want ObjectTypeNotFound", apiErr.ErrorName)
	}
}

func TestCountObjects_EmptyIndex(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)

	// Add an object type with no indexed documents
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.widget",
		OntologyRID: testOntologyRID,
		APIName:     "widget",
		DisplayName: "Widget",
		PrimaryKey:  "widgetId",
		Status:      "ACTIVE",
	})

	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testOntologyRID+"/objects/widget/count", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	// Object type exists but has no index — should return 0 or a reasonable error.
	// The indexMgr.DocCount will fail for missing index; service should return count=0
	// for valid object types that happen to have no data.
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 0 {
		t.Errorf("count = %d, want 0", resp.Count)
	}
}
