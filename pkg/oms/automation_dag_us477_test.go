package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-477 — Automate 触发器 DAG 防环.
//
// PRD acceptance:
//   - 注册期走拓扑排序，环检测失败拒绝 + 422
//   - 测试：A→B→A 环拒绝；A→B→C 合法
//
// The cycle detector treats each automation rule as a directed edge in the
// action→event→action graph:
//   - Source node: the trigger config's ObjectType (dataChange triggers only;
//     schedule / manual triggers don't observe an ObjectType so they emit no
//     incoming edge).
//   - Target nodes: ObjectTypes that the rule's executeAction effects write,
//     derived statically from the referenced ActionType's Rules JSON
//     (createObject / modifyObject / deleteObject / createOrModifyObject).
//
// A cycle in that graph means firing one rule's effect produces an event
// that triggers another rule whose effect eventually produces an event the
// first rule listens for — the classic action→event→action loop the PRD
// targets.

func us477Ontology() *oms.Ontology {
	return &oms.Ontology{RID: "ri.ontology.main.ontology.us477", APIName: "us477"}
}

func us477ActionType(apiName, objectType, kind string) oms.ActionType {
	return oms.ActionType{
		RID:         "ri.action.main.action-type." + apiName,
		OntologyRID: "ri.ontology.main.ontology.us477",
		APIName:     apiName,
		Status:      "active",
		Rules:       json.RawMessage(`[{"type":"` + kind + `","objectType":"` + objectType + `"}]`),
	}
}

func us477Rule(id, triggerObjectType, actionAPIName string) oms.AutomationRule {
	return oms.AutomationRule{
		ID:            id,
		OntologyRID:   "ri.ontology.main.ontology.us477",
		Name:          id,
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: json.RawMessage(`{"objectType":"` + triggerObjectType + `"}`),
		Effects:       json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"` + actionAPIName + `"}]`),
	}
}

// --- ValidateAutomationDAG unit tests ---

func TestValidateAutomationDAG_ScheduleTrigger_NoIncomingEdge_Acyclic(t *testing.T) {
	repo := &mockRepo{
		ontologies:  []oms.Ontology{*us477Ontology()},
		actionTypes: []oms.ActionType{us477ActionType("writeA", "A", "createObject")},
	}
	candidate := &oms.AutomationRule{
		ID: "r-sched", OntologyRID: "ri.ontology.main.ontology.us477",
		Status: "active", TriggerType: "schedule",
		TriggerConfig: json.RawMessage(`{"cron":"0 * * * *"}`),
		Effects:       json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"writeA"}]`),
	}
	cycle, err := oms.ValidateAutomationDAG(context.Background(), repo, "ri.ontology.main.ontology.us477", candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycle != nil {
		t.Fatalf("schedule-trigger rule should not introduce a cycle, got %v", cycle)
	}
}

func TestValidateAutomationDAG_LinearAtoBtoC_Acyclic(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{*us477Ontology()},
		actionTypes: []oms.ActionType{
			us477ActionType("writeB", "B", "createObject"),
			us477ActionType("writeC", "C", "createObject"),
		},
		automationRules: []oms.AutomationRule{us477Rule("r1", "A", "writeB")},
	}
	candidate := us477Rule("r2", "B", "writeC")
	cycle, err := oms.ValidateAutomationDAG(context.Background(), repo, "ri.ontology.main.ontology.us477", &candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycle != nil {
		t.Fatalf("linear A→B→C must not cycle, got %v", cycle)
	}
}

func TestValidateAutomationDAG_BackEdgeBtoA_DetectsCycle(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{*us477Ontology()},
		actionTypes: []oms.ActionType{
			us477ActionType("writeB", "B", "createObject"),
			us477ActionType("writeA", "A", "modifyObject"),
		},
		automationRules: []oms.AutomationRule{us477Rule("r1", "A", "writeB")},
	}
	candidate := us477Rule("r2", "B", "writeA")
	cycle, err := oms.ValidateAutomationDAG(context.Background(), repo, "ri.ontology.main.ontology.us477", &candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycle == nil {
		t.Fatalf("expected cycle A→B→A, got nil")
	}
	seen := map[string]bool{}
	for _, n := range cycle {
		seen[n] = true
	}
	if !seen["A"] || !seen["B"] {
		t.Errorf("cycle path must contain A and B, got %v", cycle)
	}
}

func TestValidateAutomationDAG_SelfLoopAtoA_DetectsCycle(t *testing.T) {
	repo := &mockRepo{
		ontologies:  []oms.Ontology{*us477Ontology()},
		actionTypes: []oms.ActionType{us477ActionType("writeA", "A", "modifyObject")},
	}
	candidate := us477Rule("r1", "A", "writeA")
	cycle, err := oms.ValidateAutomationDAG(context.Background(), repo, "ri.ontology.main.ontology.us477", &candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycle == nil {
		t.Fatalf("expected self-loop cycle on A, got nil")
	}
}

func TestValidateAutomationDAG_UpdateIgnoresOldVersionOfSameRule(t *testing.T) {
	// Existing state: r1 = A→B, r2 = B→A. (Hypothetical cycle, pretend it
	// was created before the validator existed.) The operator updates r1
	// from A→B to A→C — the new graph (A→C, B→A) is acyclic. The validator
	// must compare against the post-update graph, i.e. ignore the existing
	// r1 row when looking up edges.
	repo := &mockRepo{
		ontologies: []oms.Ontology{*us477Ontology()},
		actionTypes: []oms.ActionType{
			us477ActionType("writeB", "B", "createObject"),
			us477ActionType("writeC", "C", "createObject"),
			us477ActionType("writeA", "A", "modifyObject"),
		},
		automationRules: []oms.AutomationRule{
			us477Rule("r1", "A", "writeB"),
			us477Rule("r2", "B", "writeA"),
		},
	}
	candidate := us477Rule("r1", "A", "writeC")
	cycle, err := oms.ValidateAutomationDAG(context.Background(), repo, "ri.ontology.main.ontology.us477", &candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycle != nil {
		t.Fatalf("updating r1 to break the cycle should yield acyclic graph, got %v", cycle)
	}
}

func TestValidateAutomationDAG_DisabledRule_DoesNotContributeEdge(t *testing.T) {
	// Existing rule r1: A→B but its status is disabled — it should be off
	// the graph. Candidate r2 = B→A should NOT cycle with a disabled r1.
	repo := &mockRepo{
		ontologies: []oms.Ontology{*us477Ontology()},
		actionTypes: []oms.ActionType{
			us477ActionType("writeB", "B", "createObject"),
			us477ActionType("writeA", "A", "modifyObject"),
		},
		automationRules: []oms.AutomationRule{
			func() oms.AutomationRule {
				r := us477Rule("r1", "A", "writeB")
				r.Status = "disabled"
				return r
			}(),
		},
	}
	candidate := us477Rule("r2", "B", "writeA")
	cycle, err := oms.ValidateAutomationDAG(context.Background(), repo, "ri.ontology.main.ontology.us477", &candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycle != nil {
		t.Fatalf("disabled rule must not contribute an edge, got cycle %v", cycle)
	}
}

func TestValidateAutomationDAG_NotificationOnlyEffect_NoOutgoingEdge(t *testing.T) {
	// A rule whose only effect is a notification (not executeAction) has no
	// outgoing edge — adding it cannot create a cycle.
	repo := &mockRepo{
		ontologies: []oms.Ontology{*us477Ontology()},
	}
	candidate := oms.AutomationRule{
		ID: "r-notify", OntologyRID: "ri.ontology.main.ontology.us477",
		Status: "active", TriggerType: "dataChange",
		TriggerConfig: json.RawMessage(`{"objectType":"A"}`),
		Effects:       json.RawMessage(`[{"type":"notification","channel":"platform","template":"hi","recipients":["u1"]}]`),
	}
	cycle, err := oms.ValidateAutomationDAG(context.Background(), repo, "ri.ontology.main.ontology.us477", &candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cycle != nil {
		t.Fatalf("notification-only effect cannot cycle, got %v", cycle)
	}
}

// --- Handler-level tests (registration path) ---

func TestCreateAutomationRule_CycleRejected_422(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.us477", APIName: "us477"},
		},
		actionTypes: []oms.ActionType{
			us477ActionType("writeB", "B", "createObject"),
			us477ActionType("writeA", "A", "modifyObject"),
		},
		automationRules: []oms.AutomationRule{us477Rule("r1", "A", "writeB")},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)

	body := `{"name":"cycle-back","triggerType":"dataChange","triggerConfig":{"objectType":"B"},` +
		`"effects":[{"type":"executeAction","actionTypeApiName":"writeA"}]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/us477/automationRules",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if code, _ := resp["errorCode"].(string); code != "WEAVE_AUTOMATION_RULE_CYCLE" {
		t.Errorf("errorCode = %q, want WEAVE_AUTOMATION_RULE_CYCLE", code)
	}
	params, _ := resp["parameters"].(map[string]interface{})
	cyclePath, _ := params["cycle"].(string)
	if !strings.Contains(cyclePath, "A") || !strings.Contains(cyclePath, "B") {
		t.Errorf("cycle parameter should mention A and B, got %q", cyclePath)
	}
}

func TestCreateAutomationRule_LinearABtoBC_Accepted(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.us477", APIName: "us477"},
		},
		actionTypes: []oms.ActionType{
			us477ActionType("writeB", "B", "createObject"),
			us477ActionType("writeC", "C", "createObject"),
		},
		automationRules: []oms.AutomationRule{us477Rule("r1", "A", "writeB")},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)

	body := `{"name":"forward-bc","triggerType":"dataChange","triggerConfig":{"objectType":"B"},` +
		`"effects":[{"type":"executeAction","actionTypeApiName":"writeC"}]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/us477/automationRules",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for acyclic A→B→C, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateAutomationRule_IntroducingCycle_Rejected_422(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.us477", APIName: "us477"},
		},
		actionTypes: []oms.ActionType{
			us477ActionType("writeB", "B", "createObject"),
			us477ActionType("writeC", "C", "createObject"),
			us477ActionType("writeA", "A", "modifyObject"),
		},
		automationRules: []oms.AutomationRule{
			us477Rule("r1", "A", "writeB"),
			us477Rule("r2", "B", "writeC"),
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.UpdateAutomationRule)

	// Updating r2 from B→C to B→A would close the cycle A→B→A.
	body := `{"triggerConfig":{"objectType":"B"},` +
		`"effects":[{"type":"executeAction","actionTypeApiName":"writeA"}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/us477/automationRules/r2",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on update introducing cycle, got %d body=%s", w.Code, w.Body.String())
	}
}
