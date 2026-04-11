package objectset_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss/objectset"
)

func TestLoadObjects_SelectRequired(t *testing.T) {
	// Minimal executor + handler; the select validation should fire before executor runs.
	store := objectset.NewStore(0)
	exec := objectset.NewExecutor(nil, nil, store)
	h := objectset.NewHandler(exec, nil, store)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", h.LoadObjects)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing select returns 400",
			body:       `{"objectSet":{"type":"base","objectType":"TestObj"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "SelectRequired",
		},
		{
			name:       "empty select array returns 400",
			body:       `{"objectSet":{"type":"base","objectType":"TestObj"},"select":[]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "SelectRequired",
		},
		{
			name:       "null select returns 400",
			body:       `{"objectSet":{"type":"base","objectType":"TestObj"},"select":null}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "SelectRequired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST",
				"/api/v2/ontologies/test/objectSets/loadObjects",
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
