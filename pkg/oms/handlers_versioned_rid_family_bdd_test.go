package oms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_VersionedRIDGuard_Family covers round 119: the
// rejectVersionedRID helper extracted from round-117 GetObjectType
// is now applied uniformly across the metadata-Get surface. Each
// of the 7 Get endpoints recognises @vN and returns 501
// VersionedLookupNotSupported.
//
// Table-driven so adding a new Get endpoint costs one row + one
// route registration in the harness (mirror of how the family
// actually grows in production).
//
// Each row:
//   - description: what the endpoint exposes
//   - route: chi pattern (must match cmd/server registration)
//   - identifierParam: the chi URLParam name carrying the
//     versioned RID candidate
//   - handlerFn: the OMSHandler method registered against the route
func TestBDD_VersionedRIDGuard_Family(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.northwind"

	cases := []struct {
		name            string
		route           string
		identifierParam string
		register        func(r *chi.Mux, h *oms.OMSHandler)
		// requestPath substitutes the versioned RID into the route
		// template (chi URLParams are already URL-decoded by the
		// time the handler reads them, so the test wire form uses
		// the raw `@v3` — http.server-test doesn't quote-encode
		// path params).
		requestPath string
	}{
		{
			name:            "GetObjectType (round 117 baseline)",
			route:           "/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}",
			identifierParam: "objectTypeApiName",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}",
					h.GetObjectType)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/objectTypes/ri.ontology.main.object-type.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
		{
			name:            "GetActionType",
			route:           "/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}",
			identifierParam: "actionTypeRid",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}",
					h.GetActionType)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/actionTypes/ri.ontology.main.action-type.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
		{
			name:            "GetLinkTypeByAPIName",
			route:           "/api/v2/ontologies/{ontologyApiName}/linkTypes/{linkType}",
			identifierParam: "linkType",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/linkTypes/{linkType}",
					h.GetLinkTypeByAPIName)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/linkTypes/ri.ontology.main.link-type.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
		{
			name:            "GetInterfaceTypeV2",
			route:           "/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}",
			identifierParam: "interfaceType",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}",
					h.GetInterfaceTypeV2)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/interfaceTypes/ri.ontology.main.interface.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
		{
			name:            "GetValueTypeV2",
			route:           "/api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}",
			identifierParam: "valueType",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}",
					h.GetValueTypeV2)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/valueTypes/ri.ontology.main.value-type.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
		{
			name:            "GetQueryTypeV2",
			route:           "/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}",
			identifierParam: "queryApiName",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}",
					h.GetQueryTypeV2)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/queryTypes/ri.ontology.main.query-type.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
		{
			name:            "GetSharedPropertyTypeByAPIName",
			route:           "/api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes/{sharedPropertyType}",
			identifierParam: "sharedPropertyType",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes/{sharedPropertyType}",
					h.GetSharedPropertyTypeByAPIName)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/sharedPropertyTypes/ri.ontology.main.shared-property.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
		{
			name:            "GetTypeGroupByAPIName",
			route:           "/api/v2/ontologies/{ontologyApiName}/typeGroups/{typeGroup}",
			identifierParam: "typeGroup",
			register: func(r *chi.Mux, h *oms.OMSHandler) {
				r.Get(
					"/api/v2/ontologies/{ontologyApiName}/typeGroups/{typeGroup}",
					h.GetTypeGroupByAPIName)
			},
			requestPath: "/api/v2/ontologies/" + ontRID + "/typeGroups/ri.ontology.main.type-group.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{}
			_ = repo.CreateOntology(context.Background(), &oms.Ontology{
				RID: ontRID, APIName: "northwind", DisplayName: "Northwind",
			})
			handler := oms.NewOMSHandler(repo)
			r := chi.NewRouter()
			tc.register(r, handler)

			req := httptest.NewRequest(http.MethodGet, tc.requestPath, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("%s status=%d, want 501; body=%s",
					tc.name, rec.Code, rec.Body.String())
			}
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body["errorName"] != "VersionedLookupNotSupported" {
				t.Errorf("%s errorName=%v, want VersionedLookupNotSupported",
					tc.name, body["errorName"])
			}
			if body["errorCode"] != "UNIMPLEMENTED" {
				t.Errorf("%s errorCode=%v, want UNIMPLEMENTED",
					tc.name, body["errorCode"])
			}
			params, _ := body["parameters"].(map[string]any)
			if params == nil {
				t.Fatalf("%s parameters absent; body=%s", tc.name, rec.Body.String())
			}
			if params["version"] != "3" {
				t.Errorf("%s parameters.version=%v, want \"3\"",
					tc.name, params["version"])
			}
			// The identifier field must echo the original input — proves
			// rejectVersionedRID was called with the right field name
			// (not just hard-coded "input").
			if _, ok := params[tc.identifierParam]; !ok {
				t.Errorf("%s parameters missing %q field; got %v",
					tc.name, tc.identifierParam, params)
			}
		})
	}
}
