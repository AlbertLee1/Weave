package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// fakeAtomicActionLogStore records invocations and optionally fails to
// simulate a PG commit failure. It mirrors the narrow AtomicActionLogStore
// interface and is used exclusively by US-238 tests.
type fakeAtomicActionLogStore struct {
	calls int
	logs  [][]*oms.ActionLog
	err   error
}

func (f *fakeAtomicActionLogStore) WriteActionLogsAtomic(_ context.Context, logs []*oms.ActionLog) error {
	f.calls++
	// Capture a shallow copy so test assertions are stable even if the
	// caller later mutates the slice header.
	cp := make([]*oms.ActionLog, len(logs))
	copy(cp, logs)
	f.logs = append(f.logs, cp)
	return f.err
}

// TestApplyBatchAtomicTx_HappyPath_TxCommitThenPublish verifies US-238:
// in atomic-tx mode the PG action-log write runs first and NATS publish
// happens only after the tx commit (store call) succeeds.
func TestApplyBatchAtomicTx_HappyPath_TxCommitThenPublish(t *testing.T) {
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
	pub := &fakePublisher{offset: 7}
	store := &fakeAtomicActionLogStore{}
	exec := NewExecutor(repo, pub)
	exec.SetAtomicActionLogStore(store)

	reqs := []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Bob"}},
	}
	result, err := exec.ApplyBatchAtomicTx(context.Background(), "ont-1", reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.calls != 1 {
		t.Fatalf("expected exactly 1 atomic-store call, got %d", store.calls)
	}
	if got := len(store.logs[0]); got != 2 {
		t.Fatalf("expected 2 action logs written atomically, got %d", got)
	}
	// Atomic path must bypass the per-action fallback inserter so the same
	// log rows are not written twice.
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("atomic path must not fall through to InsertActionLog, got %d inserts", len(repo.insertedLogs))
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 NATS publish after commit, got %d", pub.calls)
	}
	if result.Offset != 7 {
		t.Fatalf("expected offset 7, got %d", result.Offset)
	}
	if result.Mode != "atomic" {
		t.Fatalf("expected mode=atomic, got %q", result.Mode)
	}
}

// TestApplyBatchAtomicTx_CommitFailure_NoPublish verifies that a PG commit
// failure rolls back all state and NATS is not called.
func TestApplyBatchAtomicTx_CommitFailure_NoPublish(t *testing.T) {
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
	store := &fakeAtomicActionLogStore{err: fmt.Errorf("pg rollback")}
	exec := NewExecutor(repo, pub)
	exec.SetAtomicActionLogStore(store)

	_, err := exec.ApplyBatchAtomicTx(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
	})
	if err == nil {
		t.Fatal("expected error from commit failure")
	}
	var be *BatchError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BatchError, got %T: %v", err, err)
	}
	if be.Phase != "commit" {
		t.Fatalf("expected phase=commit, got %q", be.Phase)
	}
	if pub.calls != 0 {
		t.Fatalf("commit failure must not publish, got %d publishes", pub.calls)
	}
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("commit failure must not leave action logs, got %d", len(repo.insertedLogs))
	}
}

// TestApplyBatchAtomicTx_PrepareFailure_NoCommit verifies that an invalid
// action aborts before the PG tx is even opened.
func TestApplyBatchAtomicTx_PrepareFailure_NoCommit(t *testing.T) {
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
	store := &fakeAtomicActionLogStore{}
	exec := NewExecutor(repo, pub)
	exec.SetAtomicActionLogStore(store)

	_, err := exec.ApplyBatchAtomicTx(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
		// missing required "name" → validation error, whole batch rolled back
		{ActionType: "createEmployee", Parameters: map[string]interface{}{}},
	})
	if err == nil {
		t.Fatal("expected *BatchError from prepare failure")
	}
	var be *BatchError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BatchError, got %T: %v", err, err)
	}
	if be.FailedActionIndex != 1 {
		t.Fatalf("expected FailedActionIndex=1, got %d", be.FailedActionIndex)
	}
	if store.calls != 0 {
		t.Fatalf("prepare failure must not open a tx, got %d", store.calls)
	}
	if pub.calls != 0 {
		t.Fatalf("prepare failure must not publish, got %d", pub.calls)
	}
}

// TestApplyBatchAtomicTx_NoStoreWired_FallsBackToCommitBatch verifies the
// degraded-mode path: without a store the executor falls back to the
// existing ApplyBatchAtomic commit flow so tests that don't wire a PG
// pool still work.
func TestApplyBatchAtomicTx_NoStoreWired_FallsBackToCommitBatch(t *testing.T) {
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
	pub := &fakePublisher{offset: 2}
	exec := NewExecutor(repo, pub)
	// Intentionally no SetAtomicActionLogStore.

	result, err := exec.ApplyBatchAtomicTx(context.Background(), "ont-1", []ApplyRequest{
		{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish even without store, got %d", pub.calls)
	}
	if len(repo.insertedLogs) != 1 {
		t.Fatalf("fallback path should write 1 action log, got %d", len(repo.insertedLogs))
	}
	if result.Offset != 2 {
		t.Fatalf("expected offset 2, got %d", result.Offset)
	}
}

// TestHandler_ApplyBatch_AtomicQuery_UsesTxPath verifies that the
// ?atomic=true query param routes through the atomic-tx commit path.
func TestHandler_ApplyBatch_AtomicQuery_UsesTxPath(t *testing.T) {
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
	store := &fakeAtomicActionLogStore{}
	exec := NewExecutor(repo, pub)
	exec.SetAtomicActionLogStore(store)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch?atomic=true", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("expected atomic store to be invoked once, got %d", store.calls)
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish after commit, got %d", pub.calls)
	}
}

// TestHandler_ApplyBatch_AtomicQuery_CommitFailure_500 verifies that a
// tx-commit failure surfaces as a 500 (or structured 400) without any
// publish side effect.
func TestHandler_ApplyBatch_AtomicQuery_CommitFailure(t *testing.T) {
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
	store := &fakeAtomicActionLogStore{err: fmt.Errorf("tx aborted")}
	exec := NewExecutor(repo, pub)
	exec.SetAtomicActionLogStore(store)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch?atomic=true", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected non-200 on commit failure, got 200: %s", w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("commit failure must not publish, got %d", pub.calls)
	}
}
