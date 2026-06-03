package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_AdminActionType_ApprovalFields covers Unit-4 B1: the admin
// CreateActionType / UpdateActionType handlers must accept and persist
// the `requiresApproval` and `approvers` fields so the ApprovalsPage can
// surface gated actions. Before this change the ActionType model carried
// both fields (US-242) but the request structs dropped them, leaving the
// fields unsettable from the Ontology Manager builder.
//
// The same scenario also asserts the admin list serializer
// (ToFullMetadataJSON) echoes the fields back so an edit round-trip can
// pre-populate the builder.
//
// Acceptance criteria (Given → When → Then):
//
//	Given an admin POST CreateActionType with requiresApproval=true
//	      and approvers=["role:approver","alice"]
//	When  the handler runs
//	Then  the action type is created (201) and the persisted row carries
//	      RequiresApproval=true and Approvers=[...]
//
//	Given that persisted ActionType
//	When  ListActionTypesForOntologyAdmin serializes it
//	Then  the wire JSON carries requiresApproval=true and approvers=[...]
//
//	Given an admin PUT UpdateActionType setting requiresApproval=true
//	When  the handler runs
//	Then  the persisted row reflects the new RequiresApproval/Approvers
func newAdminActionTypeApprovalRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes", handler.CreateActionType)
	r.Put("/api/admin/actionTypes/{actionTypeRid}", handler.UpdateActionType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesAdmin", handler.ListActionTypesForOntologyAdmin)
	return r
}

func TestBDD_AdminActionType_ApprovalFields(t *testing.T) {
	t.Run("Create persists requiresApproval + approvers", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		h := oms.NewOMSHandler(repo)
		r := newAdminActionTypeApprovalRouter(h)

		body, _ := json.Marshal(map[string]interface{}{
			"apiName":          "approve",
			"displayName":      "Approve",
			"status":           "ACTIVE",
			"requiresApproval": true,
			"approvers":        []string{"role:approver", "alice"},
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
		got := repo.actionTypes[0]
		if !got.RequiresApproval {
			t.Errorf("persisted row RequiresApproval=false, want true")
		}
		if len(got.Approvers) != 2 || got.Approvers[0] != "role:approver" || got.Approvers[1] != "alice" {
			t.Errorf("persisted approvers = %v, want [role:approver alice]", got.Approvers)
		}
	})

	t.Run("Admin list serializer echoes requiresApproval + approvers", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		repo.actionTypes = append(repo.actionTypes, oms.ActionType{
			RID:              "ri.ontology.main.action-type.gated",
			OntologyRID:      ontRID,
			APIName:          "gated",
			DisplayName:      "Gated",
			Status:           "ACTIVE",
			RequiresApproval: true,
			Approvers:        []string{"role:approver"},
		})
		h := oms.NewOMSHandler(repo)
		r := newAdminActionTypeApprovalRouter(h)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/actionTypesAdmin", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data []struct {
				RequiresApproval bool     `json:"requiresApproval"`
				Approvers        []string `json:"approvers"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 row, got %d", len(resp.Data))
		}
		if !resp.Data[0].RequiresApproval {
			t.Errorf("wire requiresApproval=false, want true")
		}
		if len(resp.Data[0].Approvers) != 1 || resp.Data[0].Approvers[0] != "role:approver" {
			t.Errorf("wire approvers = %v, want [role:approver]", resp.Data[0].Approvers)
		}
	})

	t.Run("Update toggles requiresApproval + approvers", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		repo.actionTypes = append(repo.actionTypes, oms.ActionType{
			RID:         "ri.ontology.main.action-type.upd",
			OntologyRID: ontRID,
			APIName:     "upd",
			DisplayName: "Upd",
			Status:      "ACTIVE",
		})
		h := oms.NewOMSHandler(repo)
		r := newAdminActionTypeApprovalRouter(h)

		body, _ := json.Marshal(map[string]interface{}{
			"displayName":      "Upd",
			"status":           "ACTIVE",
			"requiresApproval": true,
			"approvers":        []string{"role:approver"},
		})
		req := httptest.NewRequest(http.MethodPut,
			"/api/admin/actionTypes/ri.ontology.main.action-type.upd",
			strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		got := repo.actionTypes[0]
		if !got.RequiresApproval {
			t.Errorf("after update RequiresApproval=false, want true")
		}
		if len(got.Approvers) != 1 || got.Approvers[0] != "role:approver" {
			t.Errorf("after update approvers = %v, want [role:approver]", got.Approvers)
		}
	})
}
