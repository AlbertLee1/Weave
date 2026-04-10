package oss_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// TestLinkCRUD_RoutesRemoved verifies that the link write endpoints
// (POST/DELETE /api/v2/ontologies/{o}/links/{linkType}) have been removed
// from the Foundry-aligned API surface. Foundry requires all link mutations
// to go through Actions, not REST endpoints. These routes must return 404
// (Method Not Allowed) or 405 — never a success status.
func TestLinkCRUD_RoutesRemoved(t *testing.T) {
	svc, _, mockRepo, _ := setupOSSTest(t)
	handler := oss.NewHandler(svc)

	// Register a valid M2M link type so the old handler would return 201/200
	// if the route were still active. This makes the test fail in the Red
	// phase (routes still registered) and pass in the Green phase (routes removed).
	mockRepo.addLinkTypeByAPIName(testOntologyRID, &oms.LinkType{
		RID:              "ri.ontology.main.linkType.empProj",
		APIName:          "employeeProjects",
		Cardinality:      "MANY_TO_MANY",
		SourceObjectType: "ri.ontology.main.objectType.employee",
		TargetObjectType: "ri.ontology.main.objectType.project",
	})

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int // 404 after route removal
	}{
		{
			name:       "POST /links/{linkType} removed",
			method:     http.MethodPost,
			path:       "/api/v2/ontologies/" + testOntologyRID + "/links/employeeProjects",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "DELETE /links/{linkType} removed",
			method:     http.MethodDelete,
			path:       "/api/v2/ontologies/" + testOntologyRID + "/links/employeeProjects",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReader(`{"sourcePk":"emp1","targetPk":"proj1"}`)
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// After removal, the route must return 404 (chi default for unmatched paths).
			// Before removal the handlers would return 201 (create) or 200 (delete)
			// with the configured mock link type — so this test FAILS if routes exist.
			if w.Code == http.StatusOK || w.Code == http.StatusCreated {
				t.Errorf("%s: expected route to be removed (404), but got success status %d — route is still registered", tc.name, w.Code)
			}
		})
	}
}
