package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_GetOutgoingLinkTypeV2 covers the Foundry single-get sibling
// of the outgoing link-type list:
//
//	GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes/{linkType}
//
// Foundry ships this endpoint (getOutgoingLinkType) next to the list;
// Weave only had the list, so SDK callers resolving one link had to
// pull the whole list and filter client-side. {linkType} is the link
// api name, mirroring the interfaceTypes single-get
// (GetInterfaceOutgoingLinkTypeV2) and GET /linkTypes/{linkType}.
//
// Scenarios (Given → When → Then):
//
//   - Given Employee --worksIn--> Department, When GET
//     /objectTypes/Employee/outgoingLinkTypes/worksIn, Then 200 with
//     the Foundry LinkTypeSideV2 shape (objectTypeApiName = the far
//     end, linkTypeRid, status, foreignKeyPropertyApiName).
//
//   - Unknown link api name → 404 LinkTypeNotFound.
//
//   - Link exists but is not OUTGOING for the queried type (worksIn
//     starts at Employee, not Department) → 404 LinkTypeNotFound.
//
//   - Unknown object type → 404 ObjectTypeNotFound (matches the list
//     endpoint's error taxonomy).
func TestBDD_GetOutgoingLinkTypeV2(t *testing.T) {
	const (
		ontRID  = "ri.ontology.main.ontology.1"
		otEmp   = "ri.ontology.main.object-type.emp"
		otDept  = "ri.ontology.main.object-type.dept"
		ltWorks = "ri.ontology.main.link-type.worksIn"
	)

	newServer := func(t *testing.T) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "hr", DisplayName: "HR",
		})
		repo.objectTypes = append(repo.objectTypes,
			oms.ObjectType{RID: otEmp, OntologyRID: ontRID, APIName: "Employee", PrimaryKey: "id"},
			oms.ObjectType{RID: otDept, OntologyRID: ontRID, APIName: "Department", PrimaryKey: "id"},
		)
		repo.linkTypes = append(repo.linkTypes,
			oms.LinkType{
				RID: ltWorks, OntologyRID: ontRID, APIName: "worksIn",
				DisplayName:      "Works In",
				SourceObjectType: otEmp, TargetObjectType: otDept,
				Cardinality:      "MANY_TO_ONE",
				ForeignKeyConfig: json.RawMessage(`{"sourceProperty":"departmentId","targetProperty":"id"}`),
				IsRequired:       true,
			},
		)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		// Mirror cmd/server/routes.go registration order: list + single-get.
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes",
			handler.ListOutgoingLinkTypes)
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes/{linkType}",
			handler.GetOutgoingLinkTypeV2)
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, otAPIName, linkAPIName string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/objectTypes/"+otAPIName+"/outgoingLinkTypes/"+linkAPIName, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("known link returns 200 with the LinkTypeSideV2 shape", func(t *testing.T) {
		r := newServer(t)
		rec := doGet(t, r, "Employee", "worksIn")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var lt map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &lt); err != nil {
			t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
		}
		if got := lt["apiName"]; got != "worksIn" {
			t.Errorf("apiName=%v, want worksIn", got)
		}
		if got := lt["objectTypeApiName"]; got != "Department" {
			t.Errorf("objectTypeApiName=%v, want Department (the linked side)", got)
		}
		if got := lt["linkTypeRid"]; got != ltWorks {
			t.Errorf("linkTypeRid=%v, want %s", got, ltWorks)
		}
		if got := lt["status"]; got != "ACTIVE" {
			t.Errorf("status=%v, want ACTIVE", got)
		}
		if got := lt["foreignKeyPropertyApiName"]; got != "departmentId" {
			t.Errorf("foreignKeyPropertyApiName=%v, want departmentId", got)
		}
		if got := lt["cardinality"]; got != "MANY_TO_ONE" {
			t.Errorf("cardinality=%v, want MANY_TO_ONE", got)
		}
	})

	t.Run("unknown link api name returns 404 LinkTypeNotFound", func(t *testing.T) {
		r := newServer(t)
		rec := doGet(t, r, "Employee", "ghostLink")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "LinkTypeNotFound" {
			t.Errorf("errorName=%v, want LinkTypeNotFound", body["errorName"])
		}
	})

	t.Run("link that is not outgoing for the queried type returns 404", func(t *testing.T) {
		// worksIn starts at Employee; asking for it under Department's
		// outgoing list must miss.
		r := newServer(t)
		rec := doGet(t, r, "Department", "worksIn")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "LinkTypeNotFound" {
			t.Errorf("errorName=%v, want LinkTypeNotFound", body["errorName"])
		}
	})

	t.Run("unknown object type returns 404 ObjectTypeNotFound", func(t *testing.T) {
		r := newServer(t)
		rec := doGet(t, r, "GhostType", "worksIn")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "ObjectTypeNotFound" {
			t.Errorf("errorName=%v, want ObjectTypeNotFound", body["errorName"])
		}
	})
}
