package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// memActionApprovalStore is an in-memory ActionApprovalStore used by
// approval-workflow tests. Mirrors memActionJobStore — PG is covered by
// integration tests.
type memActionApprovalStore struct {
	mu        sync.Mutex
	approvals map[string]*ActionApproval
}

func newMemActionApprovalStore() *memActionApprovalStore {
	return &memActionApprovalStore{approvals: make(map[string]*ActionApproval)}
}

func (m *memActionApprovalStore) CreateActionApproval(_ context.Context, a *ActionApproval) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.approvals[a.ID]; ok {
		return errors.New("duplicate approval id")
	}
	copy := *a
	m.approvals[a.ID] = &copy
	return nil
}

func (m *memActionApprovalStore) GetActionApproval(_ context.Context, id string) (*ActionApproval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.approvals[id]
	if !ok {
		return nil, oms.ErrNotFound
	}
	copy := *a
	return &copy, nil
}

func (m *memActionApprovalStore) ListActionApprovals(_ context.Context, filter ActionApprovalListFilter) ([]*ActionApproval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*ActionApproval, 0, len(m.approvals))
	for _, a := range m.approvals {
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		if filter.OntologyAPIName != "" && a.OntologyAPIName != filter.OntologyAPIName {
			continue
		}
		if filter.Approver != "" {
			match := false
			for _, approver := range a.Approvers {
				if approver == filter.Approver {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		copy := *a
		out = append(out, &copy)
	}
	// Deterministic ordering by ID for tests — production PG store orders by
	// created_at DESC.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (m *memActionApprovalStore) UpdateActionApproval(_ context.Context, id string, upd ActionApprovalUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.approvals[id]
	if !ok {
		return oms.ErrNotFound
	}
	if upd.Status != "" {
		a.Status = upd.Status
	}
	if upd.ReviewedBy != nil {
		a.ReviewedBy = *upd.ReviewedBy
	}
	if upd.Reason != nil {
		a.Reason = *upd.Reason
	}
	a.UpdatedAt = time.Now()
	return nil
}

func setupApprovalRouter(handler *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", handler.Apply)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/approvals", handler.ListApprovals)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/approvals/{approvalId}/approve", handler.ApproveAction)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/approvals/{approvalId}/reject", handler.RejectAction)
	return r
}

// gatedActionType is a helper producing an ActionType with approval gating on.
func gatedActionType(apiName string, params []ParameterDef, rules []Rule, approvers []string) oms.ActionType {
	at := newTestActionType(apiName, params, rules)
	at.RequiresApproval = true
	at.Approvers = approvers
	return at
}

// TestApply_GatedAction_CreatesPendingApproval verifies that when the
// ActionType is flagged RequiresApproval AND an approval store is wired, the
// handler enqueues an approval row and returns 202 {approvalId, status} —
// without executing any rules.
func TestApply_GatedAction_CreatesPendingApproval(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			gatedActionType("deleteAccount",
				[]ParameterDef{{ID: "id", Type: "string", Required: true}},
				[]Rule{{Type: "deleteObject", ObjectType: "Account"}},
				[]string{"approver-1"},
			),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"id": "acct-1"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/deleteAccount/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 PendingApproval, got %d: %s", w.Code, w.Body.String())
	}
	var resp PendingApprovalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ApprovalID == "" {
		t.Fatal("expected non-empty approvalId")
	}
	if resp.Status != ActionApprovalStatusPending {
		t.Fatalf("expected status PENDING, got %q", resp.Status)
	}

	// Verify store row was created with the expected shape.
	saved, err := store.GetActionApproval(context.Background(), resp.ApprovalID)
	if err != nil {
		t.Fatalf("approval row not persisted: %v", err)
	}
	if saved.ActionType != "deleteAccount" {
		t.Fatalf("unexpected action type %q", saved.ActionType)
	}
	if saved.Status != ActionApprovalStatusPending {
		t.Fatalf("expected stored status PENDING, got %q", saved.Status)
	}
	if len(saved.Approvers) != 1 || saved.Approvers[0] != "approver-1" {
		t.Fatalf("unexpected approvers snapshot: %+v", saved.Approvers)
	}
	// No rules should have executed — publisher must not have fired.
	if pub.calls != 0 {
		t.Fatalf("expected no edit batch to be published while action is pending approval, got %d calls", pub.calls)
	}
}

// TestApply_GatedAction_NoStore_DegradesToSync verifies that when no store is
// wired the RequiresApproval flag is ignored and the sync Apply path runs as
// usual. Matches the degraded-mode pattern used elsewhere (ActionJobStore,
// MediaCatalog, etc.).
func TestApply_GatedAction_NoStore_DegradesToSync(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			gatedActionType("deleteAccount",
				[]ParameterDef{{ID: "id", Type: "string", Required: true}},
				[]Rule{{Type: "deleteObject", ObjectType: "Account"}},
				[]string{"approver-1"},
			),
		},
	}
	pub := &fakePublisher{offset: 2}
	exec := NewExecutor(repo, pub)
	// Intentionally do NOT wire SetActionApprovalStore.
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"id": "acct-1"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/deleteAccount/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected sync 200 in degraded mode, got %d: %s", w.Code, w.Body.String())
	}
}

// TestApproveAction_ByApprover_TransitionsApproved verifies that a caller
// whose user.Roles intersect the approval's approvers list can flip the
// status to APPROVED and record their ID as reviewer.
func TestApproveAction_ByApprover_TransitionsApproved(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			gatedActionType("deleteAccount",
				[]ParameterDef{{ID: "id", Type: "string", Required: true}},
				[]Rule{{Type: "deleteObject", ObjectType: "Account"}},
				[]string{"approver-1"},
			),
		},
	}
	exec := NewExecutor(repo, &fakePublisher{offset: 1})
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	// Seed a pending approval.
	approvalID := "approval-abc"
	_ = store.CreateActionApproval(context.Background(), &ActionApproval{
		ID:              approvalID,
		ActionTypeRID:   "ri.ontology.main.action-type.test-deleteAccount",
		OntologyAPIName: "ont-1",
		ActionType:      "deleteAccount",
		Parameters:      mustJSON(map[string]interface{}{"id": "acct-1"}),
		Approvers:       []string{"approver-1"},
		Status:          ActionApprovalStatusPending,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	})

	body := mustJSON(map[string]interface{}{"reason": "LGTM"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/approvals/"+approvalID+"/approve",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	saved, err := store.GetActionApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("load approval: %v", err)
	}
	if saved.Status != ActionApprovalStatusApproved {
		t.Fatalf("expected APPROVED, got %q", saved.Status)
	}
	if saved.ReviewedBy != "u-7" {
		t.Fatalf("expected ReviewedBy=u-7, got %q", saved.ReviewedBy)
	}
	if saved.Reason != "LGTM" {
		t.Fatalf("expected reason LGTM, got %q", saved.Reason)
	}
}

// TestRejectAction_ByApprover_TransitionsRejected verifies the reject flow.
func TestRejectAction_ByApprover_TransitionsRejected(t *testing.T) {
	repo := &mockOmsRepo{actionTypes: []oms.ActionType{}}
	exec := NewExecutor(repo, &fakePublisher{offset: 1})
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	approvalID := "approval-rej"
	_ = store.CreateActionApproval(context.Background(), &ActionApproval{
		ID:              approvalID,
		ActionTypeRID:   "ri.ontology.main.action-type.test-deleteAccount",
		OntologyAPIName: "ont-1",
		ActionType:      "deleteAccount",
		Parameters:      mustJSON(map[string]interface{}{}),
		Approvers:       []string{"approver-1"},
		Status:          ActionApprovalStatusPending,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	})

	body := mustJSON(map[string]interface{}{"reason": "not safe"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/approvals/"+approvalID+"/reject",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-9", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	saved, _ := store.GetActionApproval(context.Background(), approvalID)
	if saved.Status != ActionApprovalStatusRejected {
		t.Fatalf("expected REJECTED, got %q", saved.Status)
	}
	if saved.Reason != "not safe" {
		t.Fatalf("expected reason, got %q", saved.Reason)
	}
}

// TestApproveAction_NonApprover_Forbidden verifies that a caller without
// matching role membership can't approve the request.
func TestApproveAction_NonApprover_Forbidden(t *testing.T) {
	repo := &mockOmsRepo{actionTypes: []oms.ActionType{}}
	exec := NewExecutor(repo, &fakePublisher{offset: 1})
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	approvalID := "approval-forbidden"
	_ = store.CreateActionApproval(context.Background(), &ActionApproval{
		ID:              approvalID,
		OntologyAPIName: "ont-1",
		ActionType:      "deleteAccount",
		Parameters:      mustJSON(map[string]interface{}{}),
		Approvers:       []string{"approver-1"},
		Status:          ActionApprovalStatusPending,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/approvals/"+approvalID+"/approve",
		bytes.NewReader(mustJSON(map[string]interface{}{})))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "intruder", Roles: []string{"viewer"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	saved, _ := store.GetActionApproval(context.Background(), approvalID)
	if saved.Status != ActionApprovalStatusPending {
		t.Fatalf("expected status to stay PENDING after forbidden attempt, got %q", saved.Status)
	}
}

// TestApproveAction_AlreadyTerminal_Conflict verifies that approving an
// already-APPROVED/REJECTED row returns a 409 and does not re-trigger the
// status transition.
func TestApproveAction_AlreadyTerminal_Conflict(t *testing.T) {
	repo := &mockOmsRepo{actionTypes: []oms.ActionType{}}
	exec := NewExecutor(repo, &fakePublisher{offset: 1})
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	approvalID := "approval-terminal"
	_ = store.CreateActionApproval(context.Background(), &ActionApproval{
		ID:              approvalID,
		OntologyAPIName: "ont-1",
		ActionType:      "deleteAccount",
		Parameters:      mustJSON(map[string]interface{}{}),
		Approvers:       []string{"approver-1"},
		Status:          ActionApprovalStatusApproved,
		ReviewedBy:      "u-1",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/approvals/"+approvalID+"/approve",
		bytes.NewReader(mustJSON(map[string]interface{}{})))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on terminal approval, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListApprovals_DefaultsToPendingForCaller verifies that GET
// /actions/approvals returns only rows whose snapshotted approvers list
// intersects the caller's identity AND whose status is PENDING (default).
// Rows the caller cannot approve are filtered out even when their own
// request is in the queue.
func TestListApprovals_DefaultsToPendingForCaller(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	// Seed: PENDING for approver-1, PENDING for data-gov, APPROVED (terminal),
	// and a PENDING the caller can't approve.
	seed := func(a *ActionApproval) { _ = store.CreateActionApproval(context.Background(), a) }
	now := time.Now()
	seed(&ActionApproval{
		ID: "a-01", OntologyAPIName: "ont-1", ActionType: "deleteAccount",
		Approvers: []string{"approver-1"}, Status: ActionApprovalStatusPending,
		CreatedAt: now, UpdatedAt: now,
	})
	seed(&ActionApproval{
		ID: "a-02", OntologyAPIName: "ont-1", ActionType: "dropTable",
		Approvers: []string{"data-gov"}, Status: ActionApprovalStatusPending,
		CreatedAt: now, UpdatedAt: now,
	})
	seed(&ActionApproval{
		ID: "a-03", OntologyAPIName: "ont-1", ActionType: "resetPassword",
		Approvers: []string{"approver-1"}, Status: ActionApprovalStatusApproved,
		CreatedAt: now, UpdatedAt: now,
	})
	seed(&ActionApproval{
		ID: "a-04", OntologyAPIName: "ont-1", ActionType: "archive",
		Approvers: []string{"intruder"}, Status: ActionApprovalStatusPending,
		CreatedAt: now, UpdatedAt: now,
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/approvals", nil)
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []ActionApproval `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 approval visible to approver-1, got %d: %+v", len(resp.Data), resp.Data)
	}
	if resp.Data[0].ID != "a-01" {
		t.Fatalf("expected a-01, got %q", resp.Data[0].ID)
	}
}

// TestListApprovals_MineFalseListsAllStatuses verifies that ?mine=false
// lifts the approver filter; callers can see every PENDING row in the
// ontology (useful for admin dashboards / audit views).
func TestListApprovals_MineFalseListsAllStatuses(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	now := time.Now()
	seed := func(a *ActionApproval) { _ = store.CreateActionApproval(context.Background(), a) }
	seed(&ActionApproval{ID: "b-01", OntologyAPIName: "ont-1", ActionType: "a",
		Approvers: []string{"x"}, Status: ActionApprovalStatusPending,
		CreatedAt: now, UpdatedAt: now})
	seed(&ActionApproval{ID: "b-02", OntologyAPIName: "ont-1", ActionType: "b",
		Approvers: []string{"y"}, Status: ActionApprovalStatusPending,
		CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/approvals?mine=false", nil)
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []ActionApproval `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 approvals without mine filter, got %d", len(resp.Data))
	}
}

// TestListApprovals_StatusFilter verifies that ?status=APPROVED narrows the
// result set to terminal-approved rows (inspection / audit pattern).
func TestListApprovals_StatusFilter(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	store := newMemActionApprovalStore()
	exec.SetActionApprovalStore(store)
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	now := time.Now()
	seed := func(a *ActionApproval) { _ = store.CreateActionApproval(context.Background(), a) }
	seed(&ActionApproval{ID: "c-01", OntologyAPIName: "ont-1", ActionType: "a",
		Approvers: []string{"approver-1"}, Status: ActionApprovalStatusPending,
		CreatedAt: now, UpdatedAt: now})
	seed(&ActionApproval{ID: "c-02", OntologyAPIName: "ont-1", ActionType: "b",
		Approvers: []string{"approver-1"}, Status: ActionApprovalStatusApproved,
		CreatedAt: now, UpdatedAt: now})
	seed(&ActionApproval{ID: "c-03", OntologyAPIName: "ont-1", ActionType: "c",
		Approvers: []string{"approver-1"}, Status: ActionApprovalStatusRejected,
		CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/approvals?status=APPROVED", nil)
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data []ActionApproval `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 1 || resp.Data[0].ID != "c-02" {
		t.Fatalf("expected only c-02, got %+v", resp.Data)
	}
}

// TestListApprovals_NoStoreDegradedMode verifies that without an approval
// store the endpoint returns an empty list rather than 500 — same degraded
// shape used by GetJob when no ActionJobStore is wired.
func TestListApprovals_NoStoreDegradedMode(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	// Intentionally do NOT wire SetActionApprovalStore.
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/approvals", nil)
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 empty-list in degraded mode, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []ActionApproval `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty list, got %+v", resp.Data)
	}
}

// TestApproveAction_NotFound verifies that approving a missing approval id
// returns 404.
func TestApproveAction_NotFound(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	exec.SetActionApprovalStore(newMemActionApprovalStore())
	handler := NewHandler(exec)
	router := setupApprovalRouter(handler)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/approvals/does-not-exist/approve", nil)
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
