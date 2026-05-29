package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_AdminActionType_CriteriaValidation covers PRD-V2 Gap-A3
// round 135: admin CreateActionType / UpdateActionType handlers
// must validate SubmissionCriteria STRUCTURE at save time so
// authoring mistakes surface as 422 instead of as confusing
// "submission criteria not met: unknown type X" runtime errors at
// the first apply hours later.
//
// The handler accepts arbitrary criteria JSON, but when an injected
// criteriaValidator is wired (cmd/server uses
// actions.ValidateCriteriaSchema), the handler must call it before
// persisting. A nil validator disables enforcement — degraded-mode
// test routers that don't wire one preserve the legacy behavior.
//
// Acceptance criteria (Given → When → Then):
//
//	Given an admin POST CreateActionType with valid criteria
//	      and a wired ValidateCriteriaSchema
//	When  the handler runs
//	Then  the action type is created (201) and persisted with the
//	      criteria intact
//
//	Given an admin POST CreateActionType with an unknown
//	      submissionCriteria type
//	When  the handler runs
//	Then  the response is 400 InvalidParameter naming the bad type
//	      AND no row is persisted
//
//	Given an admin POST CreateActionType with a parameterMatch
//	      missing the parameter field
//	When  the handler runs
//	Then  the response is 400 InvalidParameter mentioning
//	      "parameter" AND no row is persisted
//
//	Given an admin PUT UpdateActionType with a group(not) holding
//	      two children (invalid NOT)
//	When  the handler runs
//	Then  the response is 400 InvalidParameter mentioning NOT
//	      AND the persisted row is unchanged
//
//	Given an admin POST CreateActionType WITHOUT a wired validator
//	When  the handler runs with bad criteria
//	Then  the row is created with the bad criteria (degraded mode
//	      preserves legacy behavior — no implicit enforcement)
//
// Tests written FIRST (RED) before the handler is wired to call
// the validator.
func newAdminActionTypeRouterWithValidator(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes", handler.CreateActionType)
	r.Put("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", handler.UpdateActionType)
	return r
}

func TestBDD_AdminActionType_CriteriaValidation(t *testing.T) {
	t.Run("Create with valid criteria persists 201", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		h := oms.NewOMSHandler(repo)
		h.SetCriteriaValidator(actions.ValidateCriteriaSchema)
		r := newAdminActionTypeRouterWithValidator(h)

		body, _ := json.Marshal(map[string]interface{}{
			"apiName":     "approve",
			"displayName": "Approve",
			"status":      "ACTIVE",
			"submissionCriteria": map[string]interface{}{
				"type": "parameterMatch",
				"value": map[string]interface{}{
					"parameter": "status",
					"operator":  "eq",
					"value":     "active",
				},
			},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/actionTypes",
			strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		if len(repo.actionTypes) != 1 {
			t.Fatalf("expected 1 persisted row, got %d", len(repo.actionTypes))
		}
		if len(repo.actionTypes[0].SubmissionCriteria) == 0 {
			t.Errorf("persisted row missing SubmissionCriteria")
		}
	})

	t.Run("Create with unknown criteria type rejected 422 + no persist", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		h := oms.NewOMSHandler(repo)
		h.SetCriteriaValidator(actions.ValidateCriteriaSchema)
		r := newAdminActionTypeRouterWithValidator(h)

		body, _ := json.Marshal(map[string]interface{}{
			"apiName":     "approve",
			"displayName": "Approve",
			"status":      "ACTIVE",
			"submissionCriteria": map[string]interface{}{
				"type": "totallyMadeUpType",
			},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/actionTypes",
			strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 InvalidParameter, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "totallyMadeUpType") {
			t.Errorf("expected error body to mention 'totallyMadeUpType', got: %s", w.Body.String())
		}
		if len(repo.actionTypes) != 0 {
			t.Errorf("expected no persisted row, got %d", len(repo.actionTypes))
		}
	})

	t.Run("Create with parameterMatch missing parameter rejected 422", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		h := oms.NewOMSHandler(repo)
		h.SetCriteriaValidator(actions.ValidateCriteriaSchema)
		r := newAdminActionTypeRouterWithValidator(h)

		body, _ := json.Marshal(map[string]interface{}{
			"apiName":     "approve",
			"displayName": "Approve",
			"status":      "ACTIVE",
			"submissionCriteria": map[string]interface{}{
				"type": "parameterMatch",
				"value": map[string]interface{}{
					"operator": "eq",
					"value":    "x",
				},
			},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/actionTypes",
			strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 InvalidParameter, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "parameter") {
			t.Errorf("expected error body to mention 'parameter', got: %s", w.Body.String())
		}
		if len(repo.actionTypes) != 0 {
			t.Errorf("expected no persisted row, got %d", len(repo.actionTypes))
		}
	})

	t.Run("Update with invalid NOT group rejected 422 + row unchanged", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		// Pre-seed a clean action type so we can attempt to update it.
		existing := oms.ActionType{
			RID:                "ri.ontology.main.action-type.preexist",
			OntologyRID:        ontRID,
			APIName:            "approve",
			DisplayName:        "Approve",
			Status:             "ACTIVE",
			SubmissionCriteria: json.RawMessage(`{"type":"always"}`),
		}
		repo.actionTypes = append(repo.actionTypes, existing)

		h := oms.NewOMSHandler(repo)
		h.SetCriteriaValidator(actions.ValidateCriteriaSchema)
		r := newAdminActionTypeRouterWithValidator(h)

		body, _ := json.Marshal(map[string]interface{}{
			"displayName": "Approve",
			"status":      "ACTIVE",
			"submissionCriteria": map[string]interface{}{
				"type": "group",
				"value": map[string]interface{}{
					"operator": "not",
					"criteria": []interface{}{
						map[string]interface{}{"type": "always"},
						map[string]interface{}{"type": "always"},
					},
				},
			},
		})
		req := httptest.NewRequest(http.MethodPut,
			"/api/v2/ontologies/"+ontRID+"/actionTypes/byRid/"+existing.RID,
			strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 InvalidParameter, got %d: %s", w.Code, w.Body.String())
		}
		low := strings.ToLower(w.Body.String())
		if !strings.Contains(low, "not") {
			t.Errorf("expected error body to mention NOT, got: %s", w.Body.String())
		}
		// Row's criteria must be unchanged (still the pre-existing
		// {"type":"always"}). We assert by re-reading repo state.
		if string(repo.actionTypes[0].SubmissionCriteria) != `{"type":"always"}` {
			t.Errorf("expected persisted criteria unchanged, got: %s",
				string(repo.actionTypes[0].SubmissionCriteria))
		}
	})

	t.Run("Create without wired validator accepts bad criteria (degraded mode)", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		h := oms.NewOMSHandler(repo) // NO SetCriteriaValidator
		r := newAdminActionTypeRouterWithValidator(h)

		body, _ := json.Marshal(map[string]interface{}{
			"apiName":     "approve",
			"displayName": "Approve",
			"status":      "ACTIVE",
			"submissionCriteria": map[string]interface{}{
				"type": "totallyMadeUpType",
			},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/actionTypes",
			strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 (degraded mode preserves legacy), got %d: %s", w.Code, w.Body.String())
		}
		if len(repo.actionTypes) != 1 {
			t.Errorf("expected 1 row, got %d", len(repo.actionTypes))
		}
	})
}
