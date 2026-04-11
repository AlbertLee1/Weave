package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

func TestListOntologies_NoPagination(t *testing.T) {
	repo := &mockRepo{}
	repo.ontologies = []oms.Ontology{
		{RID: "ri.ontology.main.ontology.1", APIName: "one", DisplayName: "One"},
		{RID: "ri.ontology.main.ontology.2", APIName: "two", DisplayName: "Two"},
	}

	h := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies", h.ListOntologies)

	req := httptest.NewRequest("GET", "/api/v2/ontologies", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Must have "data" array
	if _, ok := resp["data"]; !ok {
		t.Fatal("response missing 'data' key")
	}

	// Must NOT have pagination fields
	for _, key := range []string{"pageSize", "pageToken", "nextPageToken"} {
		if _, ok := resp[key]; ok {
			t.Errorf("response should NOT contain %q", key)
		}
	}
}

func TestObjectType_StatusEndorsed(t *testing.T) {
	// Verify ENDORSED is a valid ObjectType status in the model/wire format
	ot := oms.ObjectType{
		RID:         "ri.ontology.main.objectType.test",
		OntologyRID: "ri.ontology.main.ontology.test",
		APIName:     "TestObj",
		DisplayName: "Test Object",
		PrimaryKey:  "id",
		Status:      "ENDORSED",
	}

	data, err := ot.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON() error: %v", err)
	}

	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}

	if wire["status"] != "ENDORSED" {
		t.Errorf("status = %v, want ENDORSED", wire["status"])
	}

	// Verify all four valid statuses are representable
	for _, status := range []string{"ACTIVE", "ENDORSED", "EXPERIMENTAL", "DEPRECATED"} {
		ot.Status = status
		_, err := ot.ToWireJSON()
		if err != nil {
			t.Errorf("ToWireJSON() failed for status %q: %v", status, err)
		}
	}
}

func TestFullMetadata_RequiresPreview(t *testing.T) {
	repo := &mockRepo{}
	repo.ontologies = []oms.Ontology{
		{RID: "ri.ontology.main.ontology.test", APIName: "test", DisplayName: "Test"},
	}
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:         "ri.ontology.main.objectType.ot1",
		OntologyRID: "ri.ontology.main.ontology.test",
		APIName:     "TestObj",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
	})
	repo.actionTypes = append(repo.actionTypes, oms.ActionType{
		RID:         "ri.ontology.main.actionType.at1",
		OntologyRID: "ri.ontology.main.ontology.test",
		APIName:     "testAction",
		Status:      "ACTIVE",
	})

	h := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	// Register fullMetadata endpoints
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/fullMetadata", h.GetObjectTypeFullMetadataV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}/fullMetadata", h.GetActionTypeFullMetadataV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesFullMetadata", h.ListActionTypesFullMetadataV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/fullMetadata", h.GetFullMetadata)

	tests := []struct {
		name       string
		url        string
		wantStatus int
	}{
		{
			name:       "objectType fullMetadata without preview returns 400",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/objectTypes/TestObj/fullMetadata",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "objectType fullMetadata with preview returns 200",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/objectTypes/TestObj/fullMetadata?preview=true",
			wantStatus: http.StatusOK,
		},
		{
			name:       "actionType fullMetadata without preview returns 400",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/actionTypes/testAction/fullMetadata",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "actionType fullMetadata with preview returns 200",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/actionTypes/testAction/fullMetadata?preview=true",
			wantStatus: http.StatusOK,
		},
		{
			name:       "actionTypesFullMetadata without preview returns 400",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/actionTypesFullMetadata",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "actionTypesFullMetadata with preview returns 200",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/actionTypesFullMetadata?preview=true",
			wantStatus: http.StatusOK,
		},
		{
			name:       "ontology fullMetadata without preview returns 400",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/fullMetadata",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ontology fullMetadata with preview returns 200",
			url:        "/api/v2/ontologies/ri.ontology.main.ontology.test/fullMetadata?preview=true",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}
