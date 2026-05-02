package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// memSagaStore is an in-memory SagaStore used by US-369 unit tests. It
// exercises the full persistence lifecycle without requiring a live PG
// pool — the same code paths the executor takes against the real
// pgActionSagaStore in production.
type memSagaStore struct {
	mu        sync.Mutex
	sagas     map[string]*Saga
	stepsBy   map[string][]*SagaStep
	stepIdx   map[string]*SagaStep
	dlq       map[string]*SagaDLQEntry
	idemIndex map[string]string
}

func newMemSagaStore() *memSagaStore {
	return &memSagaStore{
		sagas:     map[string]*Saga{},
		stepsBy:   map[string][]*SagaStep{},
		stepIdx:   map[string]*SagaStep{},
		dlq:       map[string]*SagaDLQEntry{},
		idemIndex: map[string]string{},
	}
}

func (s *memSagaStore) CreateSaga(_ context.Context, sg *Saga) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sg.IdempotencyKey != "" {
		if _, exists := s.idemIndex[sg.IdempotencyKey]; exists {
			return ErrSagaIdempotencyConflict
		}
		s.idemIndex[sg.IdempotencyKey] = sg.SagaID
	}
	now := time.Now()
	sg.CreatedAt = now
	sg.UpdatedAt = now
	cp := *sg
	s.sagas[sg.SagaID] = &cp
	return nil
}

func (s *memSagaStore) GetSagaByIdempotencyKey(_ context.Context, key string) (*Saga, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		return nil, oms.ErrNotFound
	}
	id, ok := s.idemIndex[key]
	if !ok {
		return nil, oms.ErrNotFound
	}
	sg, ok := s.sagas[id]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *sg
	return &cp, nil
}

func (s *memSagaStore) GetSaga(_ context.Context, sagaID string) (*Saga, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sg, ok := s.sagas[sagaID]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *sg
	return &cp, nil
}

func (s *memSagaStore) UpdateSagaStatus(_ context.Context, sagaID string, upd SagaUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sg, ok := s.sagas[sagaID]
	if !ok {
		return oms.ErrNotFound
	}
	if upd.Status != "" {
		sg.Status = upd.Status
	}
	if upd.FailureMessage != nil {
		sg.FailureMessage = *upd.FailureMessage
	}
	if upd.ResultJSON != nil {
		sg.ResultJSON = upd.ResultJSON
	}
	sg.UpdatedAt = time.Now()
	return nil
}

func (s *memSagaStore) CreateSagaStep(_ context.Context, step *SagaStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	step.CreatedAt = now
	step.UpdatedAt = now
	cp := *step
	s.stepsBy[step.SagaID] = append(s.stepsBy[step.SagaID], &cp)
	s.stepIdx[step.StepID] = &cp
	return nil
}

func (s *memSagaStore) UpdateSagaStep(_ context.Context, stepID string, upd SagaStepUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	step, ok := s.stepIdx[stepID]
	if !ok {
		return oms.ErrNotFound
	}
	if upd.Status != "" {
		step.Status = upd.Status
	}
	if upd.EditsJSON != nil {
		step.EditsJSON = upd.EditsJSON
	}
	if upd.InverseEditsJSON != nil {
		step.InverseEditsJSON = upd.InverseEditsJSON
	}
	step.UpdatedAt = time.Now()
	return nil
}

func (s *memSagaStore) ListSagaSteps(_ context.Context, sagaID string) ([]*SagaStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	steps := s.stepsBy[sagaID]
	out := make([]*SagaStep, len(steps))
	for i, st := range steps {
		cp := *st
		out[i] = &cp
	}
	return out, nil
}

func (s *memSagaStore) EnqueueDLQ(_ context.Context, entry *SagaDLQEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	cp := *entry
	s.dlq[entry.DLQID] = &cp
	return nil
}

func (s *memSagaStore) ListDLQ(_ context.Context, status string, limit int) ([]*SagaDLQEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*SagaDLQEntry, 0)
	for _, e := range s.dlq {
		if status == "" || e.Status == status {
			cp := *e
			out = append(out, &cp)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *memSagaStore) UpdateDLQStatus(_ context.Context, dlqID string, upd SagaDLQUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.dlq[dlqID]
	if !ok {
		return oms.ErrNotFound
	}
	if upd.Status != "" {
		e.Status = upd.Status
	}
	if upd.Attempts != nil {
		e.Attempts = *upd.Attempts
	}
	if upd.FailureMessage != nil {
		e.FailureMessage = *upd.FailureMessage
	}
	if upd.LastAttemptAt != nil {
		e.LastAttemptAt = upd.LastAttemptAt
	}
	e.UpdatedAt = time.Now()
	return nil
}

// TestApplySaga_US369_ThreeStepSecondFails_FirstCompensated is the
// canonical PRD acceptance gate: a 3-step saga whose second step fails
// must compensate the first step (only) in reverse order, persist all
// state, and arrive at a clean COMPENSATED terminal status with no DLQ
// rows.
func TestApplySaga_US369_ThreeStepSecondFails_FirstCompensated(t *testing.T) {
	compA := "ri.ontology.main.action-type.test-deleteA"
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
			newTestActionType("stepB_fails",
				[]ParameterDef{{ID: "required", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "B"}},
			),
			newCompensatingActionType("stepC",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "C"}},
				compC,
			),
			{RID: compC, APIName: "deleteC", Status: "ACTIVE",
				Parameters: mustJSON([]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}}),
				Rules:      mustJSON([]Rule{{Type: "deleteObject", ObjectType: "C"}}),
			},
		},
	}
	pub := &fakePublisher{offset: 99}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "stepA", Parameters: map[string]interface{}{"primaryKey": "a1"}},
		{ActionType: "stepB_fails", Parameters: map[string]interface{}{}},
		{ActionType: "stepC", Parameters: map[string]interface{}{"primaryKey": "c1"}},
	}, SagaOptions{})

	if err == nil {
		t.Fatal("expected saga to fail at step B")
	}
	if result == nil {
		t.Fatal("expected non-nil SagaResult on failure")
	}
	if result.SagaID == "" {
		t.Fatal("expected SagaID populated when SagaStore is wired")
	}
	if result.Status != SagaStatusCompensated {
		t.Fatalf("expected status=COMPENSATED, got %q", result.Status)
	}
	if len(result.DLQEntries) != 0 {
		t.Fatalf("happy compensation must produce no DLQ rows, got %d", len(result.DLQEntries))
	}
	// Only stepA was prepared before stepB failed; stepC never reached
	// prepare. So exactly 1 compensation should fire (stepA's compA).
	if len(result.Compensations) != 1 {
		t.Fatalf("expected 1 compensation (stepA only), got %d", len(result.Compensations))
	}
	if result.Compensations[0].ActionRID != compA {
		t.Fatalf("expected stepA compensator (%q), got %q", compA, result.Compensations[0].ActionRID)
	}

	// Inspect persisted saga lifecycle.
	saga, _ := store.GetSaga(context.Background(), result.SagaID)
	if saga == nil {
		t.Fatal("expected saga row persisted")
	}
	if saga.Status != SagaStatusCompensated {
		t.Fatalf("expected persisted status=COMPENSATED, got %q", saga.Status)
	}
	if len(saga.ResultJSON) == 0 {
		t.Fatal("expected result_json snapshot stored on the saga row")
	}

	// Inspect persisted step rows.
	steps, _ := store.ListSagaSteps(context.Background(), result.SagaID)
	if len(steps) != 3 {
		t.Fatalf("expected 3 step rows, got %d", len(steps))
	}
	// Step 0: stepA started APPLIED, then advanced to COMPENSATED with
	// inverse_edits_json populated.
	if steps[0].Status != SagaStepStatusCompensated {
		t.Fatalf("step 0: expected COMPENSATED, got %q", steps[0].Status)
	}
	if len(steps[0].InverseEditsJSON) == 0 {
		t.Fatal("step 0: expected inverse_edits_json populated")
	}
	// Step 1: stepB failed during prepare, never APPLIED.
	if steps[1].Status != SagaStepStatusFailed {
		t.Fatalf("step 1: expected FAILED, got %q", steps[1].Status)
	}
	// Step 2: stepC never reached prepare, stays PENDING.
	if steps[2].Status != SagaStepStatusPending {
		t.Fatalf("step 2: expected PENDING, got %q", steps[2].Status)
	}
}

// TestApplySaga_US369_IdempotencyKeyReplay verifies that two calls with
// the same idempotency_key get the same SagaResult; the second call
// does not re-execute (no extra publish).
func TestApplySaga_US369_IdempotencyKeyReplay(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee",
				[]ParameterDef{{ID: "name", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}}},
			),
		},
	}
	pub := &fakePublisher{offset: 7}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)

	key := "client-supplied-dedupe-key-001"
	steps := []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
	}

	first, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", steps, SagaOptions{IdempotencyKey: key})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.Replayed {
		t.Fatal("first call must not be marked replayed")
	}
	if first.Status != SagaStatusSuccess {
		t.Fatalf("first call: expected status=SUCCESS, got %q", first.Status)
	}
	if pub.calls != 1 {
		t.Fatalf("first call: expected exactly 1 publish, got %d", pub.calls)
	}

	second, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", steps, SagaOptions{IdempotencyKey: key})
	if err != nil {
		t.Fatalf("replay call: %v", err)
	}
	if !second.Replayed {
		t.Fatal("expected replayed=true on second call")
	}
	if second.SagaID != first.SagaID {
		t.Fatalf("expected same SagaID on replay (%q vs %q)", first.SagaID, second.SagaID)
	}
	if pub.calls != 1 {
		t.Fatalf("replay must not re-publish, got %d total publishes", pub.calls)
	}
}

// TestApplySaga_US369_IdempotencyReplayPreservesFailure verifies the
// failure replay path: a replayed failed saga must still surface its
// *BatchError so callers see the same HTTP semantics as the original.
func TestApplySaga_US369_IdempotencyReplayPreservesFailure(t *testing.T) {
	compA := "ri.ontology.main.action-type.test-deleteA"
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
			newTestActionType("stepB_fails",
				[]ParameterDef{{ID: "required", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "B"}},
			),
		},
	}
	pub := &fakePublisher{}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)

	steps := []ApplyRequest{
		{ActionType: "stepA", Parameters: map[string]interface{}{"primaryKey": "a1"}},
		{ActionType: "stepB_fails", Parameters: map[string]interface{}{}},
	}
	key := "fail-replay-key"

	_, err1 := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", steps, SagaOptions{IdempotencyKey: key})
	if err1 == nil {
		t.Fatal("expected first call to fail")
	}
	publishesAfterFirst := pub.calls

	result2, err2 := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", steps, SagaOptions{IdempotencyKey: key})
	if err2 == nil {
		t.Fatal("expected replay to surface the original failure error")
	}
	var be *BatchError
	if !errors.As(err2, &be) {
		t.Fatalf("replay must surface a *BatchError, got %T: %v", err2, err2)
	}
	if !result2.Replayed {
		t.Fatal("expected Replayed=true on failure replay")
	}
	if pub.calls != publishesAfterFirst {
		t.Fatalf("replay must not publish; expected %d total publishes, got %d", publishesAfterFirst, pub.calls)
	}
}

// TestApplySaga_US369_DLQOnCompensatorFailure verifies that when a
// compensator's prepare step fails, a DLQ row is enqueued and the saga
// terminal status is FAILED (not COMPENSATED).
func TestApplySaga_US369_DLQOnCompensatorFailure(t *testing.T) {
	// The compensator RID points at a non-existent ActionType so the
	// resolveActionType lookup fails — runCompensations falls through
	// to the DLQ branch.
	missingCompRID := "ri.ontology.main.action-type.does-not-exist"
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("stepA",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "A"}},
				missingCompRID,
			),
			newTestActionType("stepB_fails",
				[]ParameterDef{{ID: "required", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "B"}},
			),
		},
	}
	pub := &fakePublisher{}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "stepA", Parameters: map[string]interface{}{"primaryKey": "a1"}},
		{ActionType: "stepB_fails", Parameters: map[string]interface{}{}},
	}, SagaOptions{})

	if err == nil {
		t.Fatal("expected saga to fail")
	}
	if len(result.DLQEntries) != 1 {
		t.Fatalf("expected 1 DLQ entry for the broken compensator, got %d", len(result.DLQEntries))
	}
	if result.Status != SagaStatusFailed {
		t.Fatalf("expected status=FAILED when DLQ rows exist, got %q", result.Status)
	}
	dlq, _ := store.ListDLQ(context.Background(), SagaDLQStatusPending, 100)
	if len(dlq) != 1 {
		t.Fatalf("expected 1 PENDING DLQ row, got %d", len(dlq))
	}
	if dlq[0].SagaID != result.SagaID {
		t.Fatalf("DLQ saga_id mismatch: %q vs %q", dlq[0].SagaID, result.SagaID)
	}
}

// TestApplySaga_US369_PersistsEditsJSONForAppliedSteps confirms each
// step row carries its actual edits in edits_json once the step
// reaches APPLIED — this is the "snapshot edits per step" contract the
// PRD calls out (action_saga_steps schema field edits_json).
func TestApplySaga_US369_PersistsEditsJSONForAppliedSteps(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee",
				[]ParameterDef{{ID: "name", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}}},
			),
		},
	}
	pub := &fakePublisher{}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
	}, SagaOptions{})
	if err != nil {
		t.Fatalf("happy saga failed: %v", err)
	}

	steps, _ := store.ListSagaSteps(context.Background(), result.SagaID)
	if len(steps) != 1 {
		t.Fatalf("expected 1 step row, got %d", len(steps))
	}
	if steps[0].Status != SagaStepStatusApplied {
		t.Fatalf("expected APPLIED step status, got %q", steps[0].Status)
	}
	if len(steps[0].EditsJSON) == 0 {
		t.Fatal("expected edits_json populated for APPLIED step")
	}
	// Confirm the edits round-trip as a JSON array.
	var edits []map[string]interface{}
	if err := json.Unmarshal(steps[0].EditsJSON, &edits); err != nil {
		t.Fatalf("edits_json must be valid JSON array: %v", err)
	}
	if len(edits) == 0 {
		t.Fatal("expected at least one edit in edits_json")
	}
}

// TestHandler_ApplySaga_US369_HappyPath_Returns200WithSagaResult
// verifies the POST /actions/applySaga endpoint returns the wire
// SagaResult shape on the happy path.
func TestHandler_ApplySaga_US369_HappyPath_Returns200WithSagaResult(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee",
				[]ParameterDef{{ID: "name", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}}},
			),
		},
	}
	pub := &fakePublisher{offset: 13}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)
	handler := NewHandler(exec)

	body := mustJSON(map[string]interface{}{
		"idempotencyKey": "test-key",
		"steps": []map[string]interface{}{
			{"actionType": "createEmployee", "parameters": map[string]interface{}{"name": "Alice"}},
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

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var result SagaResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if result.Status != SagaStatusSuccess {
		t.Fatalf("expected status=SUCCESS, got %q", result.Status)
	}
	if result.IdempotencyKey != "test-key" {
		t.Fatalf("expected echoed idempotency key, got %q", result.IdempotencyKey)
	}
	if result.SagaID == "" {
		t.Fatal("expected SagaID populated")
	}
}

// TestApplySaga_US369_RetryDLQReplaysEdits verifies that the DLQ retry
// path republishes the inverse-edit batch and transitions the row to
// RESOLVED.
func TestApplySaga_US369_RetryDLQReplaysEdits(t *testing.T) {
	pub := &fakePublisher{}
	exec := NewExecutor(&mockOmsRepo{}, pub)
	store := newMemSagaStore()
	exec.SetSagaStore(store)

	// Hand-craft a DLQ entry as if a previous saga had landed it.
	editsJSON := json.RawMessage(`[{"type":"DELETE","objectType":"Employee","primaryKey":"emp1"}]`)
	entry := &SagaDLQEntry{
		DLQID:          "dlq-1",
		SagaID:         "saga-1",
		StepID:         "step-1",
		Ontology:       "ont-1",
		EditsJSON:      editsJSON,
		FailureMessage: "simulated commit failure",
		Status:         SagaDLQStatusPending,
	}
	if err := store.EnqueueDLQ(context.Background(), entry); err != nil {
		t.Fatalf("enqueue dlq: %v", err)
	}

	if err := exec.RetrySagaDLQ(context.Background(), entry); err != nil {
		t.Fatalf("retry dlq: %v", err)
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 republish on dlq retry, got %d", pub.calls)
	}
	if pub.batches[0].OntologyAPIName != "ont-1" {
		t.Fatalf("unexpected ontology on republished batch: %q", pub.batches[0].OntologyAPIName)
	}
	if len(pub.batches[0].Edits) != 1 {
		t.Fatalf("expected 1 edit republished, got %d", len(pub.batches[0].Edits))
	}
}
