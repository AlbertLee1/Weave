package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// setupSearchRouter creates a minimal OSS handler router for search-related tests.
func setupSearchRouter() (chi.Router, *mockOmsRepo) {
	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.objectType.test",
		OntologyRID: "ri.ontology.main.ontology.test",
		APIName:     "TestObj",
		PrimaryKey:  "id",
	})

	svc := oss.NewService(repo, nil, nil)
	h := oss.NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, repo
}

func TestSearchObjects_SelectRequired(t *testing.T) {
	r, _ := setupSearchRouter()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing select returns 400",
			body:       `{"where":{"type":"eq","field":"id","value":"1"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "SelectRequired",
		},
		{
			name:       "empty select array returns 400",
			body:       `{"where":{"type":"eq","field":"id","value":"1"},"select":[]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "SelectRequired",
		},
		{
			name:       "null select returns 400",
			body:       `{"where":{"type":"eq","field":"id","value":"1"},"select":null}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "SelectRequired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST",
				"/api/v2/ontologies/ri.ontology.main.ontology.test/objects/TestObj/search",
				strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			r.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.wantCode != "" {
				var apiErr struct {
					ErrorName string `json:"errorName"`
				}
				json.Unmarshal(rr.Body.Bytes(), &apiErr)
				if apiErr.ErrorName != tt.wantCode {
					t.Errorf("errorName = %q, want %q", apiErr.ErrorName, tt.wantCode)
				}
			}
		})
	}
}
