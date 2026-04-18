package actions

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// newCompensatingActionType builds a primary ActionType that points at a
// compensating ActionType RID. Keeps the saga-specific shape close to the
// tests so the default newTestActionType helper stays uncluttered.
func newCompensatingActionType(apiName string, params []ParameterDef, rules []Rule, compensateRID string) oms.ActionType {
	at := newTestActionType(apiName, params, rules)
	at.CompensateActionRID = compensateRID
	return at
}

// TestApplyBatchSaga_SecondStepFails_FirstStepCompensated is the canonical
// two-step saga test from the US-239 acceptance criteria: action B fails
// during prepare, so action A's compensator fires in reverse order.
func TestApplyBatchSaga_SecondStepFails_FirstStepCompensated(t *testing.T) {
	compensatorRID := "ri.ontology.main.action-type.test-deleteEmployee"

	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("createEmployee",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
				},
				[]Rule{
					{Type: "createObject", ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				},
				compensatorRID,
			),
			{
				RID:     compensatorRID,
				APIName: "deleteEmployee",
				Status:  "ACTIVE",
				Parameters: mustJSON([]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
				}),
				Rules: mustJSON([]Rule{
					{Type: "deleteObject", ObjectType: "Employee"},
				}),
			},
			newTestActionType("bookResource",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "resourceId", Type: "string", Required: true},
				},
				[]Rule{
					{Type: "createObject", ObjectType: "Booking",
						PropertyBindings: map[string]PropertyBinding{
							"resourceId": {Type: "parameter", Value: "resourceId"},
						}},
				},
			),
		},
	}
	pub := &fakePublisher{offset: 42}
	exec := NewExecutor(repo, pub)

	reqs := []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{
			"primaryKey": "emp1",
			"name":       "Alice",
		}},
		// Missing required "resourceId" → prepare fails at step 2.
		{ActionType: "bookResource", Parameters: map[string]interface{}{
			"primaryKey": "book1",
		}},
	}

	result, err := exec.ApplyBatchSaga(context.Background(), "ont-1", reqs)
	if err == nil {
		t.Fatal("expected saga to surface a failure error")
	}
	var be *BatchError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BatchError, got %T: %v", err, err)
	}
	if be.FailedActionIndex != 1 {
		t.Fatalf("expected FailedActionIndex=1, got %d", be.FailedActionIndex)
	}
	if be.ActionType != "bookResource" {
		t.Fatalf("expected failing action type=bookResource, got %q", be.ActionType)
	}

	if result == nil {
		t.Fatal("expected non-nil SagaResult even on failure")
	}
	if len(result.Compensations) != 1 {
		t.Fatalf("expected 1 compensation, got %d", len(result.Compensations))
	}
	if result.Compensations[0].ActionRID != compensatorRID {
		t.Fatalf("expected compensator RID %q, got %q", compensatorRID, result.Compensations[0].ActionRID)
	}

	// Exactly one publish — the compensation batch. The primary batch must
	// never have been sent because step 2 failed before commit.
	if pub.calls != 1 {
		t.Fatalf("expected exactly 1 publish (compensation only), got %d", pub.calls)
	}
	batch := pub.batches[0]
	if len(batch.Edits) != 1 {
		t.Fatalf("expected 1 edit in compensation batch, got %d", len(batch.Edits))
	}
	edit := batch.Edits[0]
	if edit.Type != funnel.EditTypeDelete {
		t.Fatalf("expected compensation edit type=DELETE, got %q", edit.Type)
	}
	if edit.ObjectType != "Employee" || edit.PrimaryKey != "emp1" {
		t.Fatalf("expected compensation to target Employee/emp1, got %s/%s", edit.ObjectType, edit.PrimaryKey)
	}

	if result.Failure == nil {
		t.Fatal("expected SagaResult.Failure to be populated")
	}
	if result.Failure.FailedActionIndex != 1 {
		t.Fatalf("SagaResult.Failure.FailedActionIndex=1 expected, got %d", result.Failure.FailedActionIndex)
	}
}

// TestApplyBatchSaga_HappyPath_NoCompensation verifies the non-failure
// path: every action prepares and commits, the primary batch publishes, no
// compensator runs.
func TestApplyBatchSaga_HappyPath_NoCompensation(t *testing.T) {
	compensatorRID := "ri.ontology.main.action-type.test-deleteEmployee"
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("createEmployee",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
				},
				[]Rule{
					{Type: "createObject", ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				},
				compensatorRID,
			),
			{
				RID:     compensatorRID,
				APIName: "deleteEmployee",
				Status:  "ACTIVE",
				Parameters: mustJSON([]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
				}),
				Rules: mustJSON([]Rule{
					{Type: "deleteObject", ObjectType: "Employee"},
				}),
			},
		},
	}
	pub := &fakePublisher{offset: 7}
	exec := NewExecutor(repo, pub)

	result, err := exec.ApplyBatchSaga(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{
			"primaryKey": "emp1",
			"name":       "Alice",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Failure != nil {
		t.Fatalf("happy path must not populate Failure, got %+v", result.Failure)
	}
	if len(result.Compensations) != 0 {
		t.Fatalf("happy path must not run any compensations, got %d", len(result.Compensations))
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 primary publish, got %d", pub.calls)
	}
	if pub.batches[0].Edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected primary CREATE edit, got %q", pub.batches[0].Edits[0].Type)
	}
	if result.Offset != 7 {
		t.Fatalf("expected offset=7, got %d", result.Offset)
	}
}

// TestApplyBatchSaga_NoCompensatorDeclared_SkipsRollback verifies that an
// action without CompensateActionRID contributes no rollback edits. When
// ALL prior actions lack a compensator, a failure still aborts the batch
// but no compensation publish is emitted.
func TestApplyBatchSaga_NoCompensatorDeclared_SkipsRollback(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
				},
				[]Rule{
					{Type: "createObject", ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				},
			),
			newTestActionType("bookResource",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "resourceId", Type: "string", Required: true},
				},
				[]Rule{
					{Type: "createObject", ObjectType: "Booking"},
				},
			),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	_, err := exec.ApplyBatchSaga(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{
			"primaryKey": "emp1",
			"name":       "Alice",
		}},
		{ActionType: "bookResource", Parameters: map[string]interface{}{
			"primaryKey": "book1",
		}},
	})
	if err == nil {
		t.Fatal("expected saga failure from step 2 prepare error")
	}
	// No compensator ⇒ no publish at all.
	if pub.calls != 0 {
		t.Fatalf("expected 0 publishes (no compensator declared), got %d", pub.calls)
	}
}

// TestHandler_ApplyBatch_SagaQuery_RoutesToSaga verifies that
// ?saga=true on POST .../applyBatch routes through the saga coordinator
// and compensates previously-prepared actions when a later step fails.
func TestHandler_ApplyBatch_SagaQuery_RoutesToSaga(t *testing.T) {
	compensatorRID := "ri.ontology.main.action-type.test-deleteEmployee"
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("createEmployee",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
				},
				[]Rule{
					{Type: "createObject", ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				},
				compensatorRID,
			),
			{
				RID:     compensatorRID,
				APIName: "deleteEmployee",
				Status:  "ACTIVE",
				Parameters: mustJSON([]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
				}),
				Rules: mustJSON([]Rule{
					{Type: "deleteObject", ObjectType: "Employee"},
				}),
			},
		},
	}
	pub := &fakePublisher{offset: 11}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	// Same-action batch: the second item is missing the required "name"
	// parameter, so prepare fails at index 1 and step 0's compensator
	// must fire.
	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"primaryKey": "emp1", "name": "Alice"}},
			{"parameters": map[string]interface{}{"primaryKey": "emp2"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch?saga=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected non-200 response when saga hits a validation error, body=%s", w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("expected exactly 1 publish (compensation batch), got %d", pub.calls)
	}
	batch := pub.batches[0]
	if len(batch.Edits) != 1 {
		t.Fatalf("expected 1 compensation edit, got %d", len(batch.Edits))
	}
	if batch.Edits[0].Type != funnel.EditTypeDelete || batch.Edits[0].PrimaryKey != "emp1" {
		t.Fatalf("expected DELETE Employee/emp1 compensation, got %+v", batch.Edits[0])
	}
}

// TestApplyBatchSaga_ReverseOrder verifies that compensations run in
// reverse order of the prepared actions — action C's compensator fires
// before action A's when step D fails.
func TestApplyBatchSaga_ReverseOrder(t *testing.T) {
	compA := "ri.ontology.main.action-type.test-deleteA"
	compB := "ri.ontology.main.action-type.test-deleteB"
	compC := "ri.ontology.main.action-type.test-deleteC"

	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("stepA",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "A"}},
				compA,
			),
			{RID: compA, APIName: "deleteA", Status: "ACTIVE",
				Parameters: mustJSON([]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}}),
				Rules:      mustJSON([]Rule{{Type: "deleteObject", ObjectType: "A"}}),
			},
			newCompensatingActionType("stepB",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "B"}},
				compB,
			),
			{RID: compB, APIName: "deleteB", Status: "ACTIVE",
				Parameters: mustJSON([]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}}),
				Rules:      mustJSON([]Rule{{Type: "deleteObject", ObjectType: "B"}}),
			},
			newCompensatingActionType("stepC",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "C"}},
				compC,
			),
			{RID: compC, APIName: "deleteC", Status: "ACTIVE",
				Parameters: mustJSON([]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}}),
				Rules:      mustJSON([]Rule{{Type: "deleteObject", ObjectType: "C"}}),
			},
			newTestActionType("stepD_fails",
				[]ParameterDef{{ID: "required", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "D"}},
			),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.ApplyBatchSaga(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "stepA", Parameters: map[string]interface{}{"primaryKey": "a1"}},
		{ActionType: "stepB", Parameters: map[string]interface{}{"primaryKey": "b1"}},
		{ActionType: "stepC", Parameters: map[string]interface{}{"primaryKey": "c1"}},
		{ActionType: "stepD_fails", Parameters: map[string]interface{}{}},
	})
	if err == nil {
		t.Fatal("expected failure from step 4")
	}
	if len(result.Compensations) != 3 {
		t.Fatalf("expected 3 compensations, got %d", len(result.Compensations))
	}
	// Reverse order: C → B → A.
	want := []string{compC, compB, compA}
	for i, exp := range want {
		if result.Compensations[i].ActionRID != exp {
			t.Fatalf("compensation #%d: expected RID %q, got %q", i, exp, result.Compensations[i].ActionRID)
		}
	}
}
