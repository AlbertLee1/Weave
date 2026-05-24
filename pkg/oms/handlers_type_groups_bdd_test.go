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

// TestBDD_TypeGroups_V2_FoundryAlignment closes the Foundry-OSv2
// alignment gap on the TypeGroup read surface. Same shape as round
// 8's sharedPropertyTypes closure: the repo (CreateTypeGroup /
// GetTypeGroup / ListTypeGroups / UpdateTypeGroup / DeleteTypeGroup
// + AssignTypeGroup / RemoveTypeGroup / ListTypeGroupsForObjectType)
// has been wired for many rounds, but the V2 API surface exposed
// NONE of it — TypeGroups were only visible by parsing the bulky
// /fullMetadata bundle.
//
// Two endpoints, exact parallel to sharedPropertyTypes:
//   - GET /api/v2/ontologies/{ontology}/typeGroups
//     → {"data": [TypeGroup, ...]}
//   - GET /api/v2/ontologies/{ontology}/typeGroups/{typeGroup}
//     → TypeGroup wire object
//
// Both keyed by API name (not RID). Envelope NEVER null; empty
// ontology returns {"data":[]}. Error contract: 404 TypeGroupNotFound
// on unknown slug, 400 MissingTypeGroup on empty path segment.
func TestBDD_TypeGroups_V2_FoundryAlignment(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"

	newServer := func(t *testing.T) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.typeGroups = append(repo.typeGroups,
			oms.TypeGroup{
				RID:         "ri.ontology.main.type-group.people",
				OntologyRID: ontRID,
				APIName:     "people",
				DisplayName: "People",
				Color:       "#3b82f6",
			},
			oms.TypeGroup{
				RID:         "ri.ontology.main.type-group.finance",
				OntologyRID: ontRID,
				APIName:     "finance",
				DisplayName: "Finance",
				Color:       "#10b981",
			},
		)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/typeGroups", handler.ListTypeGroupsV2)
		r.Get("/api/v2/ontologies/{ontologyApiName}/typeGroups/{typeGroup}", handler.GetTypeGroupByAPIName)
		return r, repo
	}

	t.Run("List returns {data:[...]} with every TypeGroup", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/typeGroups", nil)
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

	t.Run("List returns empty {data:[]} for an ontology with no TypeGroups (never nil)", func(t *testing.T) {
		repo := &mockRepo{}
		const emptyOnt = "ri.ontology.main.ontology.empty"
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: emptyOnt, APIName: "empty", DisplayName: "Empty",
		})
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/typeGroups", handler.ListTypeGroupsV2)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+emptyOnt+"/typeGroups", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !contains(body, `"data":[]`) {
			t.Errorf("body should serialize empty list as []; got %s", body)
		}
	})

	t.Run("Get by API name returns the TypeGroup wire object", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/typeGroups/people", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got oms.TypeGroup
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.APIName != "people" {
			t.Errorf("apiName: got %q, want people", got.APIName)
		}
		if got.DisplayName != "People" {
			t.Errorf("displayName: got %q, want People", got.DisplayName)
		}
		if got.Color != "#3b82f6" {
			t.Errorf("color: got %q, want #3b82f6", got.Color)
		}
	})

	t.Run("Unknown api name returns 404 TypeGroupNotFound", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/typeGroups/doesNotExist", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "TypeGroupNotFound" {
			t.Errorf("errorName: got %q, want TypeGroupNotFound", env.ErrorName)
		}
	})

	t.Run("Empty path segment returns 400 MissingTypeGroup", func(t *testing.T) {
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		handler := oms.NewOMSHandler(repo)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/typeGroups/", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("ontologyApiName", ontRID)
		rctx.URLParams.Add("typeGroup", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		handler.GetTypeGroupByAPIName(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})
}
