package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ObjectTypeTypeGroups_V2_FoundryAlignment covers a 1:1
// Foundry alignment gap on the OMS reverse-lookup surface: given an
// ObjectType, what TypeGroups does it belong to? Weave's repo has
// ListTypeGroupsForObjectType wired (and the legacy
// /api/admin/objectTypes/{objectTypeRid}/groups handler reads it),
// but the V2 API surface — keyed by ObjectType API name — exposed
// no path. SDKs driving category-aware UIs (which chips to render
// on an ObjectType card) had to either pull every TypeGroup +
// every ObjectType's group assignments via /fullMetadata, or
// fall back to the deprecated admin RID-keyed route.
//
// The new endpoint:
//   - GET /api/v2/ontologies/{ontology}/objectTypes/{objectTypeApiName}/typeGroups
//     → {"data": [TypeGroup, ...]} for the groups this ObjectType
//     belongs to.
//
// Envelope NEVER null; ObjectType with no TypeGroup assignments
// returns {"data":[]}. Unknown ObjectType returns 404
// ObjectTypeNotFound (mirrors GetObjectType's error contract).
func TestBDD_ObjectTypeTypeGroups_V2_FoundryAlignment(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"
	const empRID = "ri.ontology.main.object-type.employee"
	const peopleRID = "ri.ontology.main.type-group.people"
	const financeRID = "ri.ontology.main.type-group.finance"

	newServer := func(t *testing.T, assignPeople, assignFinance bool) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
			RID:         empRID,
			OntologyRID: ontRID,
			APIName:     "employee",
			DisplayName: "Employee",
			PrimaryKey:  "employeeId",
			Status:      "ACTIVE",
		})
		repo.typeGroups = append(repo.typeGroups,
			oms.TypeGroup{
				RID: peopleRID, OntologyRID: ontRID,
				APIName: "people", DisplayName: "People",
			},
			oms.TypeGroup{
				RID: financeRID, OntologyRID: ontRID,
				APIName: "finance", DisplayName: "Finance",
			},
		)
		repo.typeGroupAssignments = map[string][]string{}
		if assignPeople {
			repo.typeGroupAssignments[empRID] = append(repo.typeGroupAssignments[empRID], peopleRID)
		}
		if assignFinance {
			repo.typeGroupAssignments[empRID] = append(repo.typeGroupAssignments[empRID], financeRID)
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/typeGroups",
			handler.ListTypeGroupsForObjectTypeV2)
		return r
	}

	t.Run("ObjectType with two assigned TypeGroups returns both", func(t *testing.T) {
		r := newServer(t, true, true)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/objectTypes/employee/typeGroups", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []oms.TypeGroup `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("data len=%d, want 2", len(resp.Data))
		}
		seen := map[string]bool{}
		for _, tg := range resp.Data {
			seen[tg.APIName] = true
		}
		if !seen["people"] || !seen["finance"] {
			t.Errorf("expected people + finance in data, got %v", resp.Data)
		}
	})

	t.Run("ObjectType with no TypeGroup assignments returns {data:[]} not null", func(t *testing.T) {
		r := newServer(t, false, false)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/objectTypes/employee/typeGroups", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !contains(body, `"data":[]`) {
			t.Errorf("body should serialize empty list as []; got %s", body)
		}
	})

	t.Run("Unknown ObjectType returns 404 ObjectTypeNotFound", func(t *testing.T) {
		r := newServer(t, true, false)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/objectTypes/doesNotExist/typeGroups", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "ObjectTypeNotFound" {
			t.Errorf("errorName: got %q, want ObjectTypeNotFound", env.ErrorName)
		}
	})

	t.Run("Partial assignment returns only the assigned TypeGroup", func(t *testing.T) {
		// Only `people` is assigned to employee; finance must not appear
		// in the result, even though it exists in the ontology.
		r := newServer(t, true, false)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/objectTypes/employee/typeGroups", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []oms.TypeGroup `json:"data"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if len(resp.Data) != 1 || resp.Data[0].APIName != "people" {
			t.Errorf("expected only people, got %v", resp.Data)
		}
	})
}
