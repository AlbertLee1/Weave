//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// US-477 — Automate 触发器 DAG 防环 (BDD).
//
// Three scenarios drive the PRD's acceptance verbatim against:
//   - real testcontainers PostgreSQL for both the ontology + action_type +
//     automation_rule rows;
//   - real *PGRepository so the cycle detector reads from the same table
//     production reads from;
//   - real chi router + real OMSHandler /automationRules POST / PUT
//     endpoints — i.e. the cycle check is exercised through the same wire
//     surface SDK / web clients hit.
//
// Scenario A (A→B→A) proves the back-edge rejection: pre-seeded rule with
// trigger A and effect writing B, then registration of a second rule whose
// trigger is B and effect writes A must 422 with WEAVE_AUTOMATION_RULE_CYCLE
// and a cycle parameter spelling out the path.
//
// Scenario B (A→B→C) is the positive control: extending the chain forward
// (trigger B, effect writes C) must succeed with 201 — without it, a
// regression that always returned 422 would falsely pass scenario A.
//
// Scenario C (PUT introducing cycle) covers the update path: two acyclic
// rules A→B and B→C in place, then updating r2 from B→C to B→A would close
// the loop. Hits the same DAG-check helper through the UpdateAutomationRule
// handler.

func setupUS477Fixture(t *testing.T) (router *chi.Mux, repo *oms.PGRepository, ontologyAPIName string) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo = oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "us477-bdd",
		DisplayName: "US-477 BDD",
	}
	if err := repo.CreateOntology(context.Background(), ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	// Seed ActionTypes whose Rules statically declare their target ObjectType
	// so the cycle detector can derive outgoing edges from the rule graph.
	for _, spec := range []struct {
		apiName, objectType, kind string
	}{
		{"writeB", "B", "createObject"},
		{"writeC", "C", "createObject"},
		{"writeA", "A", "modifyObject"},
	} {
		at := &oms.ActionType{
			RID:         rid.NewActionTypeRID(),
			OntologyRID: ont.RID,
			APIName:     spec.apiName,
			DisplayName: spec.apiName,
			Status:      "ACTIVE",
			Parameters:  json.RawMessage(`[]`),
			Rules:       json.RawMessage(`[{"type":"` + spec.kind + `","objectType":"` + spec.objectType + `"}]`),
		}
		if err := repo.CreateActionType(context.Background(), at); err != nil {
			t.Fatalf("create action type %s: %v", spec.apiName, err)
		}
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)
	r.Put("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.UpdateAutomationRule)
	return r, repo, ont.APIName
}

// TestBDD_US477_RegistrationRejectsBackEdgeCycle covers PRD acceptance:
// "A→B→A 环拒绝" — through real PG + the chi-routed POST handler.
func TestBDD_US477_RegistrationRejectsBackEdgeCycle(t *testing.T) {
	router, _, ontAPI := setupUS477Fixture(t)

	// --- Given: rule r1 listens on A and writes B (forward edge A→B).
	r1Body := `{"name":"r1-forward",
	            "triggerType":"dataChange",
	            "triggerConfig":{"objectType":"A"},
	            "effects":[{"type":"executeAction","actionTypeApiName":"writeB"}]}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/automationRules",
		bytes.NewBufferString(r1Body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("seed r1 failed: %d body=%s", w.Code, w.Body.String())
	}

	// --- When: operator registers r2 (B→A) that closes the cycle.
	r2Body := `{"name":"r2-cycle",
	            "triggerType":"dataChange",
	            "triggerConfig":{"objectType":"B"},
	            "effects":[{"type":"executeAction","actionTypeApiName":"writeA"}]}`
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/automationRules",
		bytes.NewBufferString(r2Body)))

	// --- Then: 422 WEAVE_AUTOMATION_RULE_CYCLE with the cycle path in params.
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on back-edge cycle, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, _ := resp["errorCode"].(string); got != "WEAVE_AUTOMATION_RULE_CYCLE" {
		t.Errorf("errorCode = %q, want WEAVE_AUTOMATION_RULE_CYCLE", got)
	}
	params, _ := resp["parameters"].(map[string]interface{})
	cyclePath, _ := params["cycle"].(string)
	if !strings.Contains(cyclePath, "A") || !strings.Contains(cyclePath, "B") {
		t.Errorf("cycle param should name A and B, got %q", cyclePath)
	}
}

// TestBDD_US477_RegistrationAcceptsLinearChain covers PRD acceptance:
// "A→B→C 合法" — the positive control proving the detector doesn't
// false-positive on every two-rule registration.
func TestBDD_US477_RegistrationAcceptsLinearChain(t *testing.T) {
	router, _, ontAPI := setupUS477Fixture(t)

	// --- Given: rule r1 listens on A and writes B (forward edge A→B).
	r1Body := `{"name":"r1-forward",
	            "triggerType":"dataChange",
	            "triggerConfig":{"objectType":"A"},
	            "effects":[{"type":"executeAction","actionTypeApiName":"writeB"}]}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/automationRules",
		bytes.NewBufferString(r1Body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("seed r1 failed: %d body=%s", w.Code, w.Body.String())
	}

	// --- When: operator registers r2 (B→C) extending the chain forward.
	r2Body := `{"name":"r2-forward",
	            "triggerType":"dataChange",
	            "triggerConfig":{"objectType":"B"},
	            "effects":[{"type":"executeAction","actionTypeApiName":"writeC"}]}`
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/automationRules",
		bytes.NewBufferString(r2Body)))

	// --- Then: 201 Created — linear A→B→C is acyclic.
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for acyclic A→B→C, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestBDD_US477_UpdatePathRejectsCycleClosure covers the update branch of
// the same DAG check: two pre-existing acyclic rules (A→B and B→C) and then
// an update that mutates r2's effect from "writes C" to "writes A". After
// the update the graph would close into A→B→A; the handler must reject 422
// before the row is persisted, so a fresh GetAutomationRule confirms the
// rule still points at writeC.
func TestBDD_US477_UpdatePathRejectsCycleClosure(t *testing.T) {
	router, repo, ontAPI := setupUS477Fixture(t)

	// --- Given: r1 = A→B (creates B).
	r1Body := `{"name":"r1",
	            "triggerType":"dataChange",
	            "triggerConfig":{"objectType":"A"},
	            "effects":[{"type":"executeAction","actionTypeApiName":"writeB"}]}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/automationRules",
		bytes.NewBufferString(r1Body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("seed r1 failed: %d body=%s", w.Code, w.Body.String())
	}

	// --- Given: r2 = B→C (creates C) — acyclic chain.
	r2Body := `{"name":"r2",
	            "triggerType":"dataChange",
	            "triggerConfig":{"objectType":"B"},
	            "effects":[{"type":"executeAction","actionTypeApiName":"writeC"}]}`
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/automationRules",
		bytes.NewBufferString(r2Body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("seed r2 failed: %d body=%s", w.Code, w.Body.String())
	}

	var r2Created map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &r2Created); err != nil {
		t.Fatalf("unmarshal r2 create response: %v", err)
	}
	r2ID, _ := r2Created["id"].(string)
	if r2ID == "" {
		t.Fatalf("r2 create did not return id, body=%s", w.Body.String())
	}

	// --- When: operator PUTs r2 to swap writeC → writeA, which would close
	// the cycle A→B→A.
	updateBody := `{"effects":[{"type":"executeAction","actionTypeApiName":"writeA"}]}`
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/"+ontAPI+"/automationRules/"+r2ID,
		bytes.NewBufferString(updateBody)))

	// --- Then: 422 — and the persisted row must still target writeC,
	// proving the rejection happened before any DB write.
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on update that closes cycle, got %d body=%s", w.Code, w.Body.String())
	}
	persisted, err := repo.GetAutomationRule(context.Background(), r2ID)
	if err != nil {
		t.Fatalf("get r2 after rejected update: %v", err)
	}
	if !strings.Contains(string(persisted.Effects), `"writeC"`) {
		t.Errorf("expected r2 effects to still target writeC after rejected update, got %s",
			string(persisted.Effects))
	}
}
