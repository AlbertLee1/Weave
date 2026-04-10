package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// Fake publisher (records calls, can return errors)
// ---------------------------------------------------------------------------

type fakePublisher struct {
	calls   int
	batches []*funnel.EditBatch
	err     error
	offset  uint64
}

func (f *fakePublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	f.calls++
	f.batches = append(f.batches, batch)
	if f.err != nil {
		return 0, f.err
	}
	return f.offset, nil
}

// ---------------------------------------------------------------------------
// Phase 1: Prepare tests
// ---------------------------------------------------------------------------

func TestPrepare_ValidationError_NoPublish(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	// Missing required "name" -> validation must fail and publisher must NOT be called.
	_, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected validation error from Prepare")
	}
	if pub.calls != 0 {
		t.Fatalf("publisher must not be called on prepare failure, got %d calls", pub.calls)
	}
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("action log must not be written on prepare failure, got %d", len(repo.insertedLogs))
	}
}

func TestPrepare_Success_NoSideEffects(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	prep, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prep.Edits) != 1 {
		t.Fatalf("expected 1 edit from Prepare, got %d", len(prep.Edits))
	}
	// Prepare is pure: no publish, no log, no side effects.
	if pub.calls != 0 {
		t.Fatalf("Prepare must not publish, got %d calls", pub.calls)
	}
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("Prepare must not write action log, got %d", len(repo.insertedLogs))
	}
}

// ---------------------------------------------------------------------------
// Phase 2: ApplyBatchAtomic tests
// ---------------------------------------------------------------------------

func TestApplyBatchAtomic_HappyPath_SinglePublish(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 42}
	exec := NewExecutor(repo, pub)

	reqs := []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Bob"}},
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Cara"}},
	}
	result, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Single publish with 3 combined edits.
	if pub.calls != 1 {
		t.Fatalf("expected exactly 1 publish, got %d", pub.calls)
	}
	if len(pub.batches) != 1 {
		t.Fatalf("expected 1 batch captured, got %d", len(pub.batches))
	}
	if len(pub.batches[0].Edits) != 3 {
		t.Fatalf("expected 3 edits in the single batch, got %d", len(pub.batches[0].Edits))
	}

	if result.Mode != "atomic" {
		t.Fatalf("expected mode=atomic, got %q", result.Mode)
	}
	if result.Offset != 42 {
		t.Fatalf("expected offset 42, got %d", result.Offset)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 per-action results, got %d", len(result.Results))
	}
	// Each per-action result should share the same BatchID and Offset.
	batchID := result.BatchID
	if batchID == "" {
		t.Fatal("expected non-empty BatchID")
	}
	for i, r := range result.Results {
		if r.BatchID != batchID {
			t.Fatalf("result[%d]: BatchID mismatch: got %q, want %q", i, r.BatchID, batchID)
		}
		if r.Offset != 42 {
			t.Fatalf("result[%d]: Offset mismatch: got %d, want 42", i, r.Offset)
		}
	}

	// Action logs are written once per successful action (3 total).
	if len(repo.insertedLogs) != 3 {
		t.Fatalf("expected 3 action logs after commit, got %d", len(repo.insertedLogs))
	}
}

func TestApplyBatchAtomic_FirstActionFails_NoneCommitted(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	reqs := []ApplyRequest{
		// Action[0] fails validation (missing name).
		{ActionType: "createEmployee", Parameters: map[string]interface{}{}},
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Bob"}},
	}
	_, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", reqs)
	if err == nil {
		t.Fatal("expected error when first action fails validation")
	}
	var bErr *BatchError
	if !errorsAs(err, &bErr) {
		t.Fatalf("expected *BatchError, got %T: %v", err, err)
	}
	if bErr.FailedActionIndex != 0 {
		t.Fatalf("expected FailedActionIndex=0, got %d", bErr.FailedActionIndex)
	}
	if bErr.Phase != "validation" {
		t.Fatalf("expected phase=validation, got %q", bErr.Phase)
	}
	// Atomicity: nothing published, nothing logged.
	if pub.calls != 0 {
		t.Fatalf("atomicity violated: publisher called %d times on prepare failure", pub.calls)
	}
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("atomicity violated: %d action logs on prepare failure", len(repo.insertedLogs))
	}
}

func TestApplyBatchAtomic_MiddleActionFails_NoneCommitted(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	reqs := []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
		// Action[1] fails validation.
		{ActionType: "createEmployee", Parameters: map[string]interface{}{}},
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Cara"}},
	}
	_, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", reqs)
	if err == nil {
		t.Fatal("expected error when middle action fails validation")
	}
	var bErr *BatchError
	if !errorsAs(err, &bErr) {
		t.Fatalf("expected *BatchError, got %T: %v", err, err)
	}
	if bErr.FailedActionIndex != 1 {
		t.Fatalf("expected FailedActionIndex=1, got %d", bErr.FailedActionIndex)
	}
	if pub.calls != 0 {
		t.Fatalf("atomicity violated: publisher called %d times", pub.calls)
	}
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("atomicity violated: %d action logs on failure", len(repo.insertedLogs))
	}
}

func TestApplyBatchAtomic_CollapseAcrossActions(t *testing.T) {
	// Two actions both modify the same Employee; collapse should fuse them
	// into a single MODIFY with the later action's value winning.
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("setSalary", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "salary", Type: "double", Required: true},
			}, []Rule{
				{
					Type:       "modifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"salary": {Type: "parameter", Value: "salary"},
					},
				},
			}),
		},
	}
	pub := &fakePublisher{offset: 7}
	exec := NewExecutor(repo, pub)

	reqs := []ApplyRequest{
		{ActionType: "setSalary", Parameters: map[string]interface{}{"primaryKey": "emp-1", "salary": float64(100)}},
		{ActionType: "setSalary", Parameters: map[string]interface{}{"primaryKey": "emp-1", "salary": float64(200)}},
	}
	result, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pub.calls != 1 {
		t.Fatalf("expected 1 publish, got %d", pub.calls)
	}
	batch := pub.batches[0]
	if len(batch.Edits) != 1 {
		t.Fatalf("expected collapse to 1 edit, got %d", len(batch.Edits))
	}
	if batch.Edits[0].Type != funnel.EditTypeModify {
		t.Fatalf("expected MODIFY, got %s", batch.Edits[0].Type)
	}
	if got, want := batch.Edits[0].Properties["salary"], float64(200); got != want {
		t.Fatalf("expected later salary %v to win, got %v", want, got)
	}
	// AppliedEdits reflects the post-collapse list.
	if len(result.AppliedEdits) != 1 {
		t.Fatalf("expected AppliedEdits length 1, got %d", len(result.AppliedEdits))
	}
}

func TestApplyBatchAtomic_PublishFailure_NoLogs(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{err: fmt.Errorf("nats down")}
	exec := NewExecutor(repo, pub)

	_, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
	})
	if err == nil {
		t.Fatal("expected publish error")
	}
	var bErr *BatchError
	if !errorsAs(err, &bErr) {
		t.Fatalf("expected *BatchError, got %T: %v", err, err)
	}
	if bErr.Phase != "publish" {
		t.Fatalf("expected phase=publish, got %q", bErr.Phase)
	}
	// No action logs on publish failure.
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("no action logs should be written on publish failure, got %d", len(repo.insertedLogs))
	}
}

func TestApplyBatchAtomic_Empty(t *testing.T) {
	repo := &mockOmsRepo{}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 0 {
		t.Fatalf("empty batch must not publish, got %d calls", pub.calls)
	}
	if result.Mode != "atomic" {
		t.Fatalf("expected mode=atomic, got %q", result.Mode)
	}
	if len(result.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result.Results))
	}
}

// ---------------------------------------------------------------------------
// Phase 2: ApplyBatchBestEffort tests
// ---------------------------------------------------------------------------

func TestApplyBatchBestEffort_MixedSuccessFailure(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 99}
	exec := NewExecutor(repo, pub)

	reqs := []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
		// Invalid: missing "name".
		{ActionType: "createEmployee", Parameters: map[string]interface{}{}},
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Cara"}},
	}
	result, err := exec.ApplyBatchBestEffort(context.Background(), "ont-1", reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Mode != "bestEffort" {
		t.Fatalf("expected mode=bestEffort, got %q", result.Mode)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 committed results, got %d", len(result.Results))
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(result.Failures))
	}
	if result.Failures[0].Index != 1 {
		t.Fatalf("expected failure at index 1, got %d", result.Failures[0].Index)
	}
	if result.Failures[0].Phase != "validation" {
		t.Fatalf("expected failure phase=validation, got %q", result.Failures[0].Phase)
	}
	// Single commit with 2 surviving actions' edits.
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish, got %d", pub.calls)
	}
	if len(pub.batches[0].Edits) != 2 {
		t.Fatalf("expected 2 edits in commit, got %d", len(pub.batches[0].Edits))
	}
}

func TestApplyBatchBestEffort_AllFail_NoPublish(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.ApplyBatchBestEffort(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{}},
		{ActionType: "createEmployee", Parameters: map[string]interface{}{}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 0 {
		t.Fatalf("no publish expected when all actions fail, got %d", pub.calls)
	}
	if len(result.Failures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(result.Failures))
	}
	if len(result.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result.Results))
	}
}

// ---------------------------------------------------------------------------
// Phase 3: Handler tests (atomic is the default)
// ---------------------------------------------------------------------------

func TestHandler_ApplyBatch_DefaultAtomic_SingleCommit(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 5}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	// Batch items only need parameters; action is in the path.
	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
			{"parameters": map[string]interface{}{"name": "Bob"}},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("expected exactly 1 publish for atomic batch, got %d", pub.calls)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// Backwards-compat: "results" array still present.
	var results []ApplyResult
	if err := json.Unmarshal(resp["results"], &results); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestHandler_ApplyBatch_AtomicValidationFailure_400(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
			{"parameters": map[string]interface{}{}}, // missing required "name"
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("atomic-mode failure must not publish anything, got %d calls", pub.calls)
	}
}

// TestHandler_ApplyBatch_BestEffortMode_Rejected verifies that the old
// mode=bestEffort field is rejected with 400 after the Foundry OSv2
// options schema rewrite (US-001 / PR-03).
func TestHandler_ApplyBatch_BestEffortMode_Rejected(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 11}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"mode": "bestEffort",
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("old mode=bestEffort must return 400, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("must not publish on rejected old mode, got %d calls", pub.calls)
	}
}

// ---------------------------------------------------------------------------
// Apply backwards compatibility
// ---------------------------------------------------------------------------

func TestExecutor_Apply_BackwardsCompatible(t *testing.T) {
	// The legacy Apply should continue to work and route through the new
	// Prepare + CommitBatch pipeline without changing its observable result.
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish for single Apply, got %d", pub.calls)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Offset != 1 {
		t.Fatalf("expected offset 1, got %d", result.Offset)
	}
	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
}

// ---------------------------------------------------------------------------
// errors.As wrapper (test-only) so callers read naturally.
// ---------------------------------------------------------------------------

func errorsAs(err error, target interface{}) bool {
	return errors.As(err, target)
}
