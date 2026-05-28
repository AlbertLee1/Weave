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

// TestBDD_SharedPropertyTypes_V2_FoundryAlignment covers the
// Foundry-1:1 alignment gap on sharedPropertyTypes read surface:
// Weave's repo has full SharedProperty CRUD (CreateSharedProperty /
// GetSharedProperty / ListSharedProperties / UpdateSharedProperty /
// DeleteSharedProperty), but routes.go exposes NONE of them on the
// V2 API surface — they were only accessible via the legacy
// /api/admin/shared-properties/* paths that US-006 removed. SDKs
// could see SharedProperties only by parsing the bulky
// /fullMetadata response, even when they wanted just one entry.
//
// Foundry's surface is two endpoints:
//
//   - GET /api/v2/ontologies/{ontology}/sharedPropertyTypes
//     → {"data": [SharedProperty, ...]}
//   - GET /api/v2/ontologies/{ontology}/sharedPropertyTypes/{spt}
//     → SharedProperty wire object (404 if api-name unknown)
//
// Both are scoped to one ontology. The single-get path mirrors the
// linkTypes/{linkType} round-7 contract: 200 + body, 404 +
// SharedPropertyTypeNotFound on unknown slug, 400 on empty path
// segment.
func TestBDD_SharedPropertyTypes_V2_FoundryAlignment(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"

	newServer := func(t *testing.T) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.sharedProperties = append(repo.sharedProperties,
			oms.SharedProperty{
				RID:         "ri.ontology.main.shared-property.email",
				OntologyRID: ontRID,
				APIName:     "email",
				DisplayName: "Email",
				BaseType:    "string",
			},
			oms.SharedProperty{
				RID:         "ri.ontology.main.shared-property.priority",
				OntologyRID: ontRID,
				APIName:     "priority",
				DisplayName: "Priority",
				BaseType:    "integer",
			},
		)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes", handler.ListSharedPropertyTypesV2)
		r.Get("/api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes/{sharedPropertyType}", handler.GetSharedPropertyTypeByAPIName)
		return r, repo
	}

	t.Run("List returns {data:[...]} with every SharedProperty", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/sharedPropertyTypes", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []oms.SharedProperty `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("data len=%d, want 2", len(resp.Data))
		}
		// Names captured so the assertion survives map-iteration
		// reordering in any future repo implementation.
		seen := map[string]bool{}
		for _, sp := range resp.Data {
			seen[sp.APIName] = true
		}
		if !seen["email"] || !seen["priority"] {
			t.Errorf("expected email + priority in data, got %v", resp.Data)
		}
	})

	t.Run("List returns empty {data:[]} for an ontology with no SharedProperties (never nil)", func(t *testing.T) {
		// A fresh ontology with no shared properties must still
		// return `{"data": []}` — SDKs that iterate without a nil
		// check would otherwise NPE on the first call.
		repo := &mockRepo{}
		const emptyOnt = "ri.ontology.main.ontology.empty"
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: emptyOnt, APIName: "empty", DisplayName: "Empty",
		})
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes", handler.ListSharedPropertyTypesV2)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+emptyOnt+"/sharedPropertyTypes", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		// Crucial: data must be `[]` in the JSON, not `null`, even
		// when no shared properties exist for this ontology.
		body := rec.Body.String()
		if got := body; !contains(got, `"data":[]`) {
			t.Errorf("body should serialize empty list as []; got %s", got)
		}
	})

	t.Run("Get by API name returns the SharedProperty wire object", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/sharedPropertyTypes/email", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got oms.SharedProperty
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.APIName != "email" {
			t.Errorf("apiName: got %q, want email", got.APIName)
		}
		if got.DisplayName != "Email" {
			t.Errorf("displayName: got %q, want Email", got.DisplayName)
		}
		if got.BaseType != "string" {
			t.Errorf("baseType: got %q, want string", got.BaseType)
		}
	})

	t.Run("Unknown api name returns 404 SharedPropertyTypeNotFound", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/sharedPropertyTypes/doesNotExist", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "SharedPropertyTypeNotFound" {
			t.Errorf("errorName: got %q, want SharedPropertyTypeNotFound", env.ErrorName)
		}
	})

	t.Run("Empty path segment returns 400 MissingSharedPropertyType", func(t *testing.T) {
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		handler := oms.NewOMSHandler(repo)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/sharedPropertyTypes/", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("ontologyApiName", ontRID)
		rctx.URLParams.Add("sharedPropertyType", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		handler.GetSharedPropertyTypeByAPIName(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})
}

// contains is a strings.Contains shim; inline so the test file's
// import block stays minimal in the BDD-focused diff.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
