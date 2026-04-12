package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

// staticPropertyFilter is a test double for oss.PropertyFilterProvider. It
// returns the allow list keyed on objectType; a nil entry means "no
// PROPERTY-scope policy attached" and every field is allowed.
type staticPropertyFilter struct {
	allowedByType map[string][]string
}

func (s *staticPropertyFilter) AllowedProperties(_ context.Context, objectType string) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	return s.allowedByType[objectType], nil
}

// TestAggregationRejectsFilteredField is the US-049 RED test. It wires a
// PropertyFilterProvider that hides `age` from the caller and verifies that
// the /objects/{objectType}/aggregate handler returns 403 + errorName
// "PropertyNotAccessible" when a request references a filtered field in
// either groupBy.field or metric.field. The same provider is then exercised
// with an allowed field (`name`) to confirm the happy path still returns 200.
func TestAggregationRejectsFilteredField(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)
	h.SetPropertyFilterProvider(&staticPropertyFilter{
		allowedByType: map[string][]string{
			// Caller can see name/deptId/active but NOT age.
			"employee": {"employeeId", "name", "deptId", "active"},
		},
	})

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantName   string
	}{
		{
			name: "groupBy on hidden field → 403 PropertyNotAccessible",
			body: `{
				"groupBy":[{"type":"exact","field":"age"}],
				"aggregation":[{"type":"count","name":"c"}]
			}`,
			wantStatus: http.StatusForbidden,
			wantName:   "PropertyNotAccessible",
		},
		{
			name: "metric on hidden field → 403 PropertyNotAccessible",
			body: `{
				"aggregation":[{"type":"sum","field":"age","name":"total"}]
			}`,
			wantStatus: http.StatusForbidden,
			wantName:   "PropertyNotAccessible",
		},
		{
			name: "groupBy on allowed field → 200 OK",
			body: `{
				"groupBy":[{"type":"exact","field":"deptId"}],
				"aggregation":[{"type":"count","name":"c"}]
			}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST",
				"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantName == "" {
				return
			}
			var apiErr struct {
				ErrorCode  string            `json:"errorCode"`
				ErrorName  string            `json:"errorName"`
				Parameters map[string]string `json:"parameters"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("unmarshal error body: %v (body=%s)", err, rr.Body.String())
			}
			if apiErr.ErrorName != tc.wantName {
				t.Errorf("errorName = %q, want %q", apiErr.ErrorName, tc.wantName)
			}
			if apiErr.ErrorCode != "PERMISSION_DENIED" {
				t.Errorf("errorCode = %q, want PERMISSION_DENIED", apiErr.ErrorCode)
			}
			if apiErr.Parameters["property"] != "age" {
				t.Errorf("parameters.property = %q, want age", apiErr.Parameters["property"])
			}
		})
	}
}

// TestAggregationNilFilterAllowsEverything guards the nil / empty-engine
// back-compat contract: un-policied callers never pay the filter cost and the
// handler returns 200 for any aggregation request.
func TestAggregationNilFilterAllowsEverything(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)
	// No SetPropertyFilterProvider call — provider is nil.

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body := `{
		"groupBy":[{"type":"exact","field":"age"}],
		"aggregation":[{"type":"sum","field":"age","name":"total"}]
	}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
}

// TestAggregationNilAllowListFromPolicyEngineBlocksAll ensures that a
// PROPERTY-scope policy that grants nothing (allow list = [] — explicit
// empty) still rejects any field reference. The nil convention means
// "no policy attached"; an explicit zero-length slice is the strictest
// possible allow list and must reject every groupBy.field / metric.field.
func TestAggregationEmptyAllowListRejectsEverything(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)
	h.SetPropertyFilterProvider(&staticPropertyFilter{
		allowedByType: map[string][]string{
			"employee": {}, // explicit empty allow list
		},
	})

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body := `{
		"groupBy":[{"type":"exact","field":"deptId"}],
		"aggregation":[{"type":"count","name":"c"}]
	}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rr.Code, rr.Body.String())
	}
}
