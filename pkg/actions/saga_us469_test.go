package actions

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

// saga_us469_test.go — TDD coverage for US-469: explicit
// CompensationStrategy (best-effort | stop-on-first), FailedCompensations
// []FailedCompensationRef field on SagaResult, and DLQ entries for failed
// compensation attempts.
//
// The PRD acceptance gate:
//   - SagaResult exposes CompensationStrategy + FailedCompensations.
//   - 失败补偿写入 DLQ 表 (saga's existing action_saga_dlq).
//   - "3 步全成功 → 中间步失败 → 后续步仍补偿" — under best-effort, a
//     broken middle compensator must not block the surrounding
//     compensators from running.
//
// Fixtures reuse the package-local helpers introduced in saga_test.go +
// saga_us369_test.go (mockOmsRepo, fakePublisher, memSagaStore,
// newCompensatingActionType, newTestActionType, mustJSON).

// us469Fixture builds a 4-step saga where stepC's compensator points at
// a non-existent ActionType RID — prepareCompensator therefore fails for
// stepC during the rollback walk. stepD fails to prepare so the rollback
// is triggered with stepA / stepB / stepC all already prepared and
// stepC's compensator pre-wired to break.
//
// Reverse compensation order: stepD (no comp → skip), stepC (broken
// comp → MUST fail), stepB (good comp → MUST still run), stepA (good
// comp → MUST still run) — that is the "middle compensator fails, rest
// still compensate" PRD case.
func us469Fixture() (*Executor, *fakePublisher, *memSagaStore, []string) {
	compA := "ri.ontology.main.action-type.us469-deleteA"
	compB := "ri.ontology.main.action-type.us469-deleteB"
	missingCompC := "ri.ontology.main.action-type.us469-does-not-exist"

	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("us469stepA",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "A"}},
				compA,
			),
			{RID: compA, APIName: "us469deleteA", Status: "ACTIVE",
				Parameters: mustJSON([]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}}),
				Rules:      mustJSON([]Rule{{Type: "deleteObject", ObjectType: "A"}}),
			},
			newCompensatingActionType("us469stepB",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "B"}},
				compB,
			),
			{RID: compB, APIName: "us469deleteB", Status: "ACTIVE",
				Parameters: mustJSON([]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}}),
				Rules:      mustJSON([]Rule{{Type: "deleteObject", ObjectType: "B"}}),
			},
			newCompensatingActionType("us469stepC",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "C"}},
				missingCompC,
			),
			newTestActionType("us469stepD_fails",
				[]ParameterDef{{ID: "required", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "D"}},
			),
		},
	}
	pub := &fakePublisher{offset: 1}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)
	return exec, pub, store, []string{compA, compB, missingCompC}
}

// us469Requests returns the 4 saga requests for the fixture. stepD has
// no required parameter so Prepare fails at index 3, triggering rollback.
func us469Requests() []ApplyRequest {
	return []ApplyRequest{
		{ActionType: "us469stepA", Parameters: map[string]interface{}{"primaryKey": "a1"}},
		{ActionType: "us469stepB", Parameters: map[string]interface{}{"primaryKey": "b1"}},
		{ActionType: "us469stepC", Parameters: map[string]interface{}{"primaryKey": "c1"}},
		{ActionType: "us469stepD_fails", Parameters: map[string]interface{}{}},
	}
}

// TestUS469_BestEffort_BrokenMiddleCompensator_RestStillCompensate is
// the canonical PRD acceptance gate: under best-effort, a broken middle
// compensator must NOT block compensators around it from running.
func TestUS469_BestEffort_BrokenMiddleCompensator_RestStillCompensate(t *testing.T) {
	exec, pub, store, rids := us469Fixture()
	compA, compB := rids[0], rids[1]

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1",
		us469Requests(),
		SagaOptions{CompensationStrategy: CompensationStrategyBestEffort})

	if err == nil {
		t.Fatal("expected saga to fail at stepD prepare")
	}
	if result == nil {
		t.Fatal("expected non-nil SagaResult on failure")
	}

	t.Run("CompensationStrategy_EchoedInResult", func(t *testing.T) {
		if result.CompensationStrategy != CompensationStrategyBestEffort {
			t.Fatalf("expected CompensationStrategy=%q, got %q",
				CompensationStrategyBestEffort, result.CompensationStrategy)
		}
	})

	t.Run("CompensationsRun_For_StepB_And_StepA_Only", func(t *testing.T) {
		if len(result.Compensations) != 2 {
			t.Fatalf("expected 2 compensations (stepB + stepA), got %d", len(result.Compensations))
		}
		// Reverse order: stepC's comp is broken (skipped to DLQ);
		// stepB runs first, then stepA.
		if result.Compensations[0].ActionRID != compB {
			t.Fatalf("compensation[0]: expected stepB compensator %q, got %q",
				compB, result.Compensations[0].ActionRID)
		}
		if result.Compensations[1].ActionRID != compA {
			t.Fatalf("compensation[1]: expected stepA compensator %q, got %q",
				compA, result.Compensations[1].ActionRID)
		}
	})

	t.Run("FailedCompensations_HasOneEntry_ForStepC", func(t *testing.T) {
		if len(result.FailedCompensations) != 1 {
			t.Fatalf("expected 1 FailedCompensations entry (stepC), got %d: %+v",
				len(result.FailedCompensations), result.FailedCompensations)
		}
		fc := result.FailedCompensations[0]
		if fc.StepIndex != 2 {
			t.Fatalf("FailedCompensations[0].StepIndex=2 expected, got %d", fc.StepIndex)
		}
		if fc.ActionType != "us469stepC" {
			t.Fatalf("FailedCompensations[0].ActionType=us469stepC expected, got %q", fc.ActionType)
		}
		if fc.Phase != FailedCompensationPhasePrepare {
			t.Fatalf("FailedCompensations[0].Phase=%q expected, got %q",
				FailedCompensationPhasePrepare, fc.Phase)
		}
		if fc.Reason == "" {
			t.Fatal("FailedCompensations[0].Reason expected non-empty")
		}
		if fc.DLQID == "" {
			t.Fatal("FailedCompensations[0].DLQID expected populated (DLQ row enqueued)")
		}
		if fc.StepID == "" {
			t.Fatal("FailedCompensations[0].StepID expected populated when SagaStore is wired")
		}
	})

	t.Run("DLQEntries_HasOne_MatchingFailedCompensations", func(t *testing.T) {
		if len(result.DLQEntries) != 1 {
			t.Fatalf("expected 1 DLQ entry, got %d", len(result.DLQEntries))
		}
		if result.DLQEntries[0] != result.FailedCompensations[0].DLQID {
			t.Fatalf("DLQEntries[0]=%q must equal FailedCompensations[0].DLQID=%q",
				result.DLQEntries[0], result.FailedCompensations[0].DLQID)
		}
		dlq, _ := store.ListDLQ(context.Background(), SagaDLQStatusPending, 100)
		if len(dlq) != 1 {
			t.Fatalf("expected 1 PENDING DLQ row in store, got %d", len(dlq))
		}
	})

	t.Run("TerminalStatus_FailedWhenAnyDLQRowExists", func(t *testing.T) {
		if result.Status != SagaStatusFailed {
			t.Fatalf("expected status=FAILED (DLQ rows exist), got %q", result.Status)
		}
	})

	t.Run("Publisher_SawExactlyOneCompensationBatch", func(t *testing.T) {
		// stepC's compensator never made it past prepare → it does not
		// contribute edits. stepB + stepA produce a single merged
		// compensation batch published once. The primary commit never
		// fires because stepD failed during prepare.
		if pub.calls != 1 {
			t.Fatalf("expected exactly 1 publish (compensation batch, B+A merged), got %d", pub.calls)
		}
		batch := pub.batches[0]
		// 2 DELETE edits — one for B, one for A.
		var deleteA, deleteB int
		for _, ed := range batch.Edits {
			switch ed.ObjectType {
			case "A":
				deleteA++
			case "B":
				deleteB++
			}
		}
		if deleteA != 1 || deleteB != 1 {
			t.Fatalf("expected 1 DELETE A + 1 DELETE B, got A=%d B=%d", deleteA, deleteB)
		}
	})
}

// TestUS469_StopOnFirst_BrokenMiddleCompensator_SkipsLaterSteps verifies
// the stop-on-first strategy: walking stops as soon as the first
// compensator fails. Remaining (unwalked) prepared steps are listed in
// FailedCompensations with Phase="skipped" and do NOT get their
// compensators tried.
func TestUS469_StopOnFirst_BrokenMiddleCompensator_SkipsLaterSteps(t *testing.T) {
	exec, pub, store, _ := us469Fixture()

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1",
		us469Requests(),
		SagaOptions{CompensationStrategy: CompensationStrategyStopOnFirst})

	if err == nil {
		t.Fatal("expected saga to fail")
	}

	t.Run("CompensationStrategy_StopOnFirst_Echoed", func(t *testing.T) {
		if result.CompensationStrategy != CompensationStrategyStopOnFirst {
			t.Fatalf("expected strategy=%q, got %q",
				CompensationStrategyStopOnFirst, result.CompensationStrategy)
		}
	})

	t.Run("NoCompensationsRun_StopOnFirstHaltsImmediately", func(t *testing.T) {
		// Reverse walk hits stepC first (broken), stop immediately. stepB
		// and stepA never get compensated even though they were
		// successfully prepared.
		if len(result.Compensations) != 0 {
			t.Fatalf("stop-on-first: expected 0 compensations after broken stepC trigger, got %d (%+v)",
				len(result.Compensations), result.Compensations)
		}
	})

	t.Run("FailedCompensations_ListsTriggerPlusSkippedSteps", func(t *testing.T) {
		// 3 entries: stepC (prepare-failed), stepB (skipped), stepA (skipped).
		// stepD has no compensator so it must NOT appear.
		if len(result.FailedCompensations) != 3 {
			t.Fatalf("expected 3 FailedCompensations (C trigger + B+A skipped), got %d: %+v",
				len(result.FailedCompensations), result.FailedCompensations)
		}
		// Reverse walk: index 2 (C) first, then 1 (B), then 0 (A).
		want := []struct {
			idx   int
			phase string
		}{
			{2, FailedCompensationPhasePrepare},
			{1, FailedCompensationPhaseSkipped},
			{0, FailedCompensationPhaseSkipped},
		}
		for i, w := range want {
			fc := result.FailedCompensations[i]
			if fc.StepIndex != w.idx {
				t.Fatalf("FailedCompensations[%d].StepIndex=%d want %d", i, fc.StepIndex, w.idx)
			}
			if fc.Phase != w.phase {
				t.Fatalf("FailedCompensations[%d].Phase=%q want %q", i, fc.Phase, w.phase)
			}
		}
	})

	t.Run("DLQOnlyForActualFailure_NotForSkippedSteps", func(t *testing.T) {
		// stop-on-first DLQs ONLY the actual failure (stepC). Skipped
		// steps are listed in FailedCompensations for operator visibility
		// but do not enter DLQ — they were never attempted.
		if len(result.DLQEntries) != 1 {
			t.Fatalf("expected 1 DLQ entry (stepC failure only), got %d", len(result.DLQEntries))
		}
		dlq, _ := store.ListDLQ(context.Background(), SagaDLQStatusPending, 100)
		if len(dlq) != 1 {
			t.Fatalf("expected 1 PENDING DLQ row, got %d", len(dlq))
		}
	})

	t.Run("TerminalStatus_Failed", func(t *testing.T) {
		if result.Status != SagaStatusFailed {
			t.Fatalf("expected status=FAILED, got %q", result.Status)
		}
	})

	t.Run("Publisher_NeverPublishedCompensationBatch", func(t *testing.T) {
		// Stop-on-first halts before any compensator commits.
		if pub.calls != 0 {
			t.Fatalf("stop-on-first must not publish compensation, got %d publishes", pub.calls)
		}
	})
}

// TestUS469_DefaultStrategyIsBestEffort verifies that a SagaOptions with
// an empty CompensationStrategy defaults to best-effort (matches the
// pre-US-469 behaviour so existing callers see no change).
func TestUS469_DefaultStrategyIsBestEffort(t *testing.T) {
	exec, _, _, _ := us469Fixture()

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1",
		us469Requests(),
		SagaOptions{}) // strategy left empty
	if err == nil {
		t.Fatal("expected saga to fail")
	}
	if result.CompensationStrategy != CompensationStrategyBestEffort {
		t.Fatalf("empty CompensationStrategy must default to %q, got %q",
			CompensationStrategyBestEffort, result.CompensationStrategy)
	}
	// And under default best-effort, both surviving compensators ran.
	if len(result.Compensations) != 2 {
		t.Fatalf("default best-effort: expected 2 compensations, got %d", len(result.Compensations))
	}
}

// TestUS469_FailedCompensations_EmptyOnHappyRollback verifies that a
// fully successful compensation walk (every prepared step's compensator
// runs cleanly) leaves FailedCompensations empty.
func TestUS469_FailedCompensations_EmptyOnHappyRollback(t *testing.T) {
	// Reuse the US-369 canonical fixture: stepA + stepC have compensators,
	// stepB_fails breaks during prepare. No broken compensators.
	compA := "ri.ontology.main.action-type.us469-clean-deleteA"

	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("clnStepA",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "A"}},
				compA,
			),
			{RID: compA, APIName: "clnDeleteA", Status: "ACTIVE",
				Parameters: mustJSON([]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}}),
				Rules:      mustJSON([]Rule{{Type: "deleteObject", ObjectType: "A"}}),
			},
			newTestActionType("clnStepB_fails",
				[]ParameterDef{{ID: "required", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "B"}},
			),
		},
	}
	pub := &fakePublisher{offset: 1}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1",
		[]ApplyRequest{
			{ActionType: "clnStepA", Parameters: map[string]interface{}{"primaryKey": "a1"}},
			{ActionType: "clnStepB_fails", Parameters: map[string]interface{}{}},
		},
		SagaOptions{CompensationStrategy: CompensationStrategyBestEffort})

	if err == nil {
		t.Fatal("expected saga failure")
	}
	if result.Status != SagaStatusCompensated {
		t.Fatalf("expected COMPENSATED on clean rollback, got %q", result.Status)
	}
	if len(result.FailedCompensations) != 0 {
		t.Fatalf("clean rollback must produce zero FailedCompensations, got %d: %+v",
			len(result.FailedCompensations), result.FailedCompensations)
	}
	if len(result.DLQEntries) != 0 {
		t.Fatalf("clean rollback must produce zero DLQ entries, got %d", len(result.DLQEntries))
	}
	if result.CompensationStrategy != CompensationStrategyBestEffort {
		t.Fatalf("strategy must still be echoed on clean rollback, got %q",
			result.CompensationStrategy)
	}
}

// TestUS469_StrategyNormalization verifies the strategy value is
// accepted in common variant casings (uppercase, surrounding
// whitespace) so SDK clients have a forgiving wire format.
func TestUS469_StrategyNormalization(t *testing.T) {
	cases := []struct {
		in   string
		want SagaCompensationStrategy
	}{
		{"", CompensationStrategyBestEffort},
		{"best-effort", CompensationStrategyBestEffort},
		{"BEST-EFFORT", CompensationStrategyBestEffort},
		{" Best-Effort ", CompensationStrategyBestEffort},
		{"stop-on-first", CompensationStrategyStopOnFirst},
		{"STOP-ON-FIRST", CompensationStrategyStopOnFirst},
		{" stop-on-first\n", CompensationStrategyStopOnFirst},
	}
	for _, tc := range cases {
		got, err := NormalizeCompensationStrategy(tc.in)
		if err != nil {
			t.Fatalf("NormalizeCompensationStrategy(%q): unexpected err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeCompensationStrategy(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// TestUS469_StrategyRejectsUnknownValue verifies that an unknown
// strategy keyword surfaces as a clean validation error rather than
// silently mis-routing.
func TestUS469_StrategyRejectsUnknownValue(t *testing.T) {
	if _, err := NormalizeCompensationStrategy("yolo"); err == nil {
		t.Fatal("expected error for unknown strategy value")
	}
}

// TestUS469_Handler_ApplyStrategyFromRequestBody verifies that the
// /actions/applySaga handler honours the compensationStrategy field on
// the request body and echoes it back on the SagaResult.
func TestUS469_Handler_ApplyStrategyFromRequestBody(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("hdlNoop",
				[]ParameterDef{{ID: "name", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}}},
			),
		},
	}
	pub := &fakePublisher{offset: 1}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)
	handler := NewHandler(exec)

	body := mustJSON(map[string]interface{}{
		"compensationStrategy": "stop-on-first",
		"steps": []map[string]interface{}{
			{"actionType": "hdlNoop", "parameters": map[string]interface{}{"name": "Alice"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/applySaga",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/applySaga", handler.ApplySaga)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var result SagaResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if result.CompensationStrategy != CompensationStrategyStopOnFirst {
		t.Fatalf("handler must surface CompensationStrategy=%q, got %q",
			CompensationStrategyStopOnFirst, result.CompensationStrategy)
	}
}

// TestUS469_Handler_RejectsUnknownStrategyWith400 verifies the
// applySaga handler returns a 400-shaped InvalidParameter error when
// the body declares an unknown compensationStrategy.
func TestUS469_Handler_RejectsUnknownStrategyWith400(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, &fakePublisher{})
	handler := NewHandler(exec)

	body := mustJSON(map[string]interface{}{
		"compensationStrategy": "blast-radius-everywhere",
		"steps": []map[string]interface{}{
			{"actionType": "x", "parameters": map[string]interface{}{}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/applySaga",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/applySaga", handler.ApplySaga)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on unknown strategy, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "compensationStrategy") {
		t.Fatalf("body should mention compensationStrategy field, got %s", w.Body.String())
	}
}

// TestBDD_US469_ThreeStepMiddleFails_BestEffortStillCompensatesRest is
// the US-469 BDD integration scenario:
//
//   Given: 4-step saga with stepC's compensator pointing at a non-existent
//          ActionType RID (broken middle compensator) wired against a
//          real chi router + memSagaStore
//   When:  applySaga POST with compensationStrategy=best-effort and
//          stepD missing its required parameter (forces rollback)
//   Then:  HTTP non-200; response body's saga.compensations has B and A
//          (in that order); saga.failedCompensations identifies stepC
//          with phase=prepare and a DLQ id; saga.dlqEntries matches;
//          the SagaStore's action_saga_dlq has 1 PENDING row; step rows
//          show 0/1 COMPENSATED, 2 COMPENSATION_FAILED, 3 FAILED.
//
// Uses the existing US-369 sagaResponseEnvelope handler shape (errors
// wrap the SagaResult in a {saga: ...} body). This is the BDD-required
// "走真实 chi router" end-to-end scenario for this story.
func TestBDD_US469_ThreeStepMiddleFails_BestEffortStillCompensatesRest(t *testing.T) {
	exec, pub, store, _ := us469Fixture()
	handler := NewHandler(exec)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/applySaga", handler.ApplySaga)

	body := mustJSON(map[string]interface{}{
		"compensationStrategy": "best-effort",
		"steps": []map[string]interface{}{
			{"actionType": "us469stepA", "parameters": map[string]interface{}{"primaryKey": "a1"}},
			{"actionType": "us469stepB", "parameters": map[string]interface{}{"primaryKey": "b1"}},
			{"actionType": "us469stepC", "parameters": map[string]interface{}{"primaryKey": "c1"}},
			{"actionType": "us469stepD_fails", "parameters": map[string]interface{}{}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/applySaga",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// THEN HTTP is non-200 (BatchError surfaced via 4xx).
	if w.Code == http.StatusOK {
		t.Fatalf("expected non-200 status; got 200 body=%s", w.Body.String())
	}

	// THEN response body wraps the SagaResult under {saga: ...} (error envelope shape).
	var envelope struct {
		Saga *SagaResult `json:"saga"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Saga == nil {
		t.Fatalf("expected {saga: ...} envelope; got body=%s err=%v", w.Body.String(), err)
	}
	got := envelope.Saga

	// THEN saga.compensationStrategy is echoed.
	if got.CompensationStrategy != CompensationStrategyBestEffort {
		t.Fatalf("saga.compensationStrategy=%q want best-effort", got.CompensationStrategy)
	}

	// THEN compensations: stepB + stepA both ran (in that reverse order).
	if len(got.Compensations) != 2 {
		t.Fatalf("expected 2 compensations (B+A), got %d: %+v",
			len(got.Compensations), got.Compensations)
	}

	// THEN failedCompensations: exactly 1 entry for stepC with phase=prepare + DLQID.
	if len(got.FailedCompensations) != 1 {
		t.Fatalf("expected 1 failedCompensations entry, got %d", len(got.FailedCompensations))
	}
	fc := got.FailedCompensations[0]
	if fc.StepIndex != 2 || fc.ActionType != "us469stepC" {
		t.Fatalf("failedCompensations[0]: wrong identity StepIndex=%d ActionType=%q",
			fc.StepIndex, fc.ActionType)
	}
	if fc.Phase != FailedCompensationPhasePrepare {
		t.Fatalf("failedCompensations[0].Phase=%q want %q", fc.Phase, FailedCompensationPhasePrepare)
	}
	if fc.DLQID == "" {
		t.Fatal("failedCompensations[0].DLQID expected populated")
	}

	// THEN dlqEntries matches the DLQ row enqueued.
	if len(got.DLQEntries) != 1 || got.DLQEntries[0] != fc.DLQID {
		t.Fatalf("dlqEntries=%+v must contain the same id as failedCompensations[0].DLQID=%q",
			got.DLQEntries, fc.DLQID)
	}

	// THEN SagaStore has the DLQ row.
	dlq, _ := store.ListDLQ(context.Background(), SagaDLQStatusPending, 100)
	if len(dlq) != 1 || dlq[0].DLQID != fc.DLQID {
		t.Fatalf("SagaStore DLQ rows: expected 1 with id=%q, got %+v", fc.DLQID, dlq)
	}

	// THEN per-step rows reflect lifecycle: A=COMPENSATED, B=COMPENSATED,
	// C=COMPENSATION_FAILED, D=FAILED.
	steps, _ := store.ListSagaSteps(context.Background(), got.SagaID)
	if len(steps) != 4 {
		t.Fatalf("expected 4 step rows, got %d", len(steps))
	}
	want := []string{
		SagaStepStatusCompensated,
		SagaStepStatusCompensated,
		SagaStepStatusCompensationFailed,
		SagaStepStatusFailed,
	}
	for i, w := range want {
		if steps[i].Status != w {
			t.Fatalf("step[%d].Status=%q want %q", i, steps[i].Status, w)
		}
	}

	// THEN publisher saw exactly 1 batch (B+A merged compensation; no primary commit).
	if pub.calls != 1 {
		t.Fatalf("expected exactly 1 compensation publish, got %d", pub.calls)
	}
}
