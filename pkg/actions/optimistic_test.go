package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// US-023: ApplyOptions.expectedVersion with 409 StaleObject
// ---------------------------------------------------------------------------

// renameEmployeeActionType is a shared fixture used by the optimistic
// concurrency tests. It defines a modifyObject rule against the "Employee"
// ObjectType so the executor emits a MODIFY edit whose version can be
// compared against ApplyOptions.ExpectedVersion.
func renameEmployeeActionType() oms.ActionType {
	return newTestActionType("renameEmployee", []ParameterDef{
		{ID: "primaryKey", Type: "string", Required: true},
		{ID: "name", Type: "string", Required: true},
	}, []Rule{
		{
			Type:       "modifyObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	})
}

// employeeOTRID is the canonical test ObjectType RID for "Employee".
const employeeOTRID = "ri.ontology.main.object-type.employee"

// newOptimisticRepo wires up the mock ObjectType + version counts used by
// the optimistic concurrency tests. currentVersion is the value returned by
// GetObjectVersionCount for (employeeOTRID, primaryKey).
func newOptimisticRepo(primaryKey string, currentVersion int64) *mockOmsRepo {
	return &mockOmsRepo{
		actionTypes: []oms.ActionType{renameEmployeeActionType()},
		objectTypesByAPIName: map[string]*oms.ObjectType{
			"Employee": {RID: employeeOTRID, APIName: "Employee"},
		},
		objectVersionCounts: map[string]int64{
			employeeOTRID + "|" + primaryKey: currentVersion,
		},
	}
}

// intPtr is a small helper to build *int literals in test payloads.
func intPtr(i int) *int { return &i }

// ---------------------------------------------------------------------------
// Executor-level unit tests
// ---------------------------------------------------------------------------

// TestExecutor_ExpectedVersion_Mismatch_ReturnsStaleObject locks in the
// failing-fast contract at the executor layer: if the caller passes a stale
// ExpectedVersion, CommitBatch must never run and Apply must return a
// *StaleObjectError carrying the current version.
func TestExecutor_ExpectedVersion_Mismatch_ReturnsStaleObject(t *testing.T) {
	repo := newOptimisticRepo("emp-1", 3)
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "renameEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice",
		},
		Options: &ApplyOptions{ExpectedVersion: intPtr(1)},
	})
	if err == nil {
		t.Fatal("expected StaleObjectError, got nil")
	}
	var stale *StaleObjectError
	if !errors.As(err, &stale) {
		t.Fatalf("expected *StaleObjectError, got %T: %v", err, err)
	}
	if stale.CurrentVersion != 3 {
		t.Fatalf("CurrentVersion = %d, want 3", stale.CurrentVersion)
	}
	if stale.ExpectedVersion != 1 {
		t.Fatalf("ExpectedVersion = %d, want 1", stale.ExpectedVersion)
	}
	if stale.PrimaryKey != "emp-1" {
		t.Fatalf("PrimaryKey = %q, want emp-1", stale.PrimaryKey)
	}
	if pub.calls != 0 {
		t.Fatalf("publisher must not be called on stale version, got %d calls", pub.calls)
	}
	if len(repo.insertedLogs) != 0 {
		t.Fatalf("action log must not be written on stale version, got %d", len(repo.insertedLogs))
	}
}

// TestExecutor_ExpectedVersion_Match_Succeeds confirms the happy path: when
// ExpectedVersion matches the current version, Apply runs to completion and
// the batch is published.
func TestExecutor_ExpectedVersion_Match_Succeeds(t *testing.T) {
	repo := newOptimisticRepo("emp-1", 3)
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "renameEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice",
		},
		Options: &ApplyOptions{ExpectedVersion: intPtr(3)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %+v", result)
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish, got %d", pub.calls)
	}
}

// TestExecutor_ExpectedVersion_Nil_BackwardsCompat guarantees that callers
// who never opt into optimistic concurrency see no behavioural change: no
// version lookup, no 409, no regression.
func TestExecutor_ExpectedVersion_Nil_BackwardsCompat(t *testing.T) {
	repo := newOptimisticRepo("emp-1", 7) // current version is high but it should not matter
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "renameEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice",
		},
		// Options == nil → ExpectedVersion not set → no check.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish, got %d", pub.calls)
	}
}

// TestExecutor_ExpectedVersion_CreateOnly_Ignored exercises the edge case
// where the action only creates new objects: there is no target object to
// check against, so ExpectedVersion is a no-op and the apply succeeds.
func TestExecutor_ExpectedVersion_CreateOnly_Ignored(t *testing.T) {
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

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Alice"},
		Options:    &ApplyOptions{ExpectedVersion: intPtr(1)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish, got %d", pub.calls)
	}
}

// ---------------------------------------------------------------------------
// Handler-level integration tests
// ---------------------------------------------------------------------------

// TestHandler_Apply_ExpectedVersionMismatch_Returns409 is the wire-format
// acceptance test for US-023: a stale ExpectedVersion must surface as an
// HTTP 409 with errorName=StaleObject and currentVersion in the parameters
// map so the frontend can show "reload" UX.
func TestHandler_Apply_ExpectedVersionMismatch_Returns409(t *testing.T) {
	repo := newOptimisticRepo("emp-1", 3)
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice",
		},
		"options": map[string]interface{}{
			"expectedVersion": 1,
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/renameEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 0 {
		t.Fatalf("publisher must not be called on 409, got %d", pub.calls)
	}

	var payload struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.ErrorCode != "CONFLICT" {
		t.Fatalf("errorCode = %q, want CONFLICT", payload.ErrorCode)
	}
	if payload.ErrorName != "StaleObject" {
		t.Fatalf("errorName = %q, want StaleObject", payload.ErrorName)
	}
	if payload.Parameters["currentVersion"] != "3" {
		t.Fatalf("currentVersion = %q, want 3", payload.Parameters["currentVersion"])
	}
}

// TestHandler_Apply_ExpectedVersionMatch_Returns200 is the happy-path wire
// test: matching version allows the apply to proceed and returns 200.
func TestHandler_Apply_ExpectedVersionMatch_Returns200(t *testing.T) {
	repo := newOptimisticRepo("emp-1", 3)
	pub := &fakePublisher{}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice",
		},
		"options": map[string]interface{}{
			"expectedVersion": 3,
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/renameEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("expected 1 publish, got %d", pub.calls)
	}
}
