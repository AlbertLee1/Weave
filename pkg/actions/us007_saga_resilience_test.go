package actions

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// us007_saga_resilience_test.go — TDD coverage for the Saga rollback,
// approval gating, edit collapse, and async batch timeout paths called out
// in the US-007 acceptance criteria. The file lives next to saga_test.go
// and saga_us369_test.go and reuses their fixtures (mockOmsRepo,
// fakePublisher, newCompensatingActionType, memSagaStore,
// memActionApprovalStore, memActionJobStore, blockingFunnelPublisher).

// ---------------------------------------------------------------------------
// AC #1: 3-step saga where step3 fails → reverse compensation of step2/step1
// ---------------------------------------------------------------------------

// TestUS007_ThreeStepSaga_Step3FailsReverseCompensate is the canonical PRD
// acceptance test: stepA + stepB + stepC where stepC's parameters are
// invalid. Both stepA and stepB are prepared and must be compensated in
// REVERSE order (compB then compA). The primary edit batch never
// publishes — only the compensation batch does. No DLQ rows are written
// when both compensators succeed.
func TestUS007_ThreeStepSaga_Step3FailsReverseCompensate(t *testing.T) {
	compA := "ri.ontology.main.action-type.test-deleteA"
	compB := "ri.ontology.main.action-type.test-deleteB"

	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newCompensatingActionType("stepA",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "A",
					PropertyBindings: map[string]PropertyBinding{}}},
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
			newTestActionType("stepC_fails",
				[]ParameterDef{{ID: "required", Type: "string", Required: true}},
				[]Rule{{Type: "createObject", ObjectType: "C"}},
			),
		},
	}
	pub := &fakePublisher{offset: 7}
	store := newMemSagaStore()
	exec := NewExecutor(repo, pub)
	exec.SetSagaStore(store)

	reqs := []ApplyRequest{
		{ActionType: "stepA", Parameters: map[string]interface{}{"primaryKey": "a1"}},
		{ActionType: "stepB", Parameters: map[string]interface{}{"primaryKey": "b1"}},
		{ActionType: "stepC_fails", Parameters: map[string]interface{}{}},
	}

	result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", reqs, SagaOptions{})

	t.Run("FailureSurfacedAsBatchError_AtStepIndex2", func(t *testing.T) {
		if err == nil {
			t.Fatal("expected saga to surface a *BatchError when stepC fails")
		}
		var be *BatchError
		if !errors.As(err, &be) {
			t.Fatalf("expected *BatchError, got %T: %v", err, err)
		}
		if be.FailedActionIndex != 2 {
			t.Fatalf("expected FailedActionIndex=2, got %d", be.FailedActionIndex)
		}
		if be.ActionType != "stepC_fails" {
			t.Fatalf("expected failing action type=stepC_fails, got %q", be.ActionType)
		}
	})

	t.Run("CompensationsRunInReverseOrder_BThenA", func(t *testing.T) {
		if len(result.Compensations) != 2 {
			t.Fatalf("expected 2 compensations, got %d", len(result.Compensations))
		}
		if result.Compensations[0].ActionRID != compB {
			t.Fatalf("compensation #0 must be compB (%q), got %q", compB, result.Compensations[0].ActionRID)
		}
		if result.Compensations[1].ActionRID != compA {
			t.Fatalf("compensation #1 must be compA (%q), got %q", compA, result.Compensations[1].ActionRID)
		}
	})

	t.Run("PrimaryBatchNeverPublished_OnlyCompensationPublishedOnce", func(t *testing.T) {
		if pub.calls != 1 {
			t.Fatalf("expected exactly 1 publish (compensation only, primary must never publish), got %d", pub.calls)
		}
		batch := pub.batches[0]
		// Compensation batch should contain 2 DELETE edits — one each for A and B.
		var deleteATypes, deleteBTypes int
		for _, ed := range batch.Edits {
			if ed.Type != funnel.EditTypeDelete {
				t.Fatalf("compensation edit must be DELETE, got %q", ed.Type)
			}
			switch ed.ObjectType {
			case "A":
				deleteATypes++
			case "B":
				deleteBTypes++
			}
		}
		if deleteATypes != 1 || deleteBTypes != 1 {
			t.Fatalf("expected exactly 1 DELETE A + 1 DELETE B, got A=%d B=%d", deleteATypes, deleteBTypes)
		}
	})

	t.Run("StepRowsAdvanceToCompensatedFailedPending", func(t *testing.T) {
		if result.SagaID == "" {
			t.Fatal("expected SagaID populated when SagaStore is wired")
		}
		steps, _ := store.ListSagaSteps(context.Background(), result.SagaID)
		if len(steps) != 3 {
			t.Fatalf("expected 3 persisted step rows, got %d", len(steps))
		}
		// Step 0: stepA reached APPLIED then rolled back to COMPENSATED.
		if steps[0].Status != SagaStepStatusCompensated {
			t.Fatalf("step 0: expected COMPENSATED, got %q", steps[0].Status)
		}
		if len(steps[0].InverseEditsJSON) == 0 {
			t.Fatal("step 0: expected inverse_edits_json populated after compensation")
		}
		// Step 1: stepB reached APPLIED then rolled back to COMPENSATED.
		if steps[1].Status != SagaStepStatusCompensated {
			t.Fatalf("step 1: expected COMPENSATED, got %q", steps[1].Status)
		}
		// Step 2: stepC_fails never made it past prepare → FAILED.
		if steps[2].Status != SagaStepStatusFailed {
			t.Fatalf("step 2: expected FAILED, got %q", steps[2].Status)
		}
	})

	t.Run("TerminalStatusCompensatedAndNoDLQRows", func(t *testing.T) {
		if result.Status != SagaStatusCompensated {
			t.Fatalf("expected status=COMPENSATED on clean rollback, got %q", result.Status)
		}
		if len(result.DLQEntries) != 0 {
			t.Fatalf("clean rollback must not enqueue DLQ rows, got %d", len(result.DLQEntries))
		}
		dlq, _ := store.ListDLQ(context.Background(), "", 100)
		if len(dlq) != 0 {
			t.Fatalf("expected 0 DLQ rows on clean rollback, got %d", len(dlq))
		}
	})
}

// ---------------------------------------------------------------------------
// AC #2: Approval timeout — context.DeadlineExceeded surfaces on the
// approval store call and the row stays PENDING (not silently approved).
// ---------------------------------------------------------------------------

// slowApprovalStore wraps memActionApprovalStore with a configurable
// delay on UpdateActionApproval. Respects ctx so a caller that wraps the
// store with context.WithTimeout sees ctx.DeadlineExceeded instead of a
// late-completing write.
type slowApprovalStore struct {
	*memActionApprovalStore
	delay time.Duration
}

func (s *slowApprovalStore) UpdateActionApproval(ctx context.Context, id string, upd ActionApprovalUpdate) error {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.memActionApprovalStore.UpdateActionApproval(ctx, id, upd)
}

// TestUS007_ApprovalTimeout covers the approval-timeout AC: a slow
// approval store + a short request deadline must surface
// context.DeadlineExceeded and leave the row in PENDING so a retry is
// possible.
func TestUS007_ApprovalTimeout(t *testing.T) {
	t.Run("UpdateApproval_DeadlineExceeded_RowStaysPending", func(t *testing.T) {
		mem := newMemActionApprovalStore()
		store := &slowApprovalStore{memActionApprovalStore: mem, delay: 100 * time.Millisecond}

		approvalID := "approval-timeout-1"
		_ = mem.CreateActionApproval(context.Background(), &ActionApproval{
			ID:        approvalID,
			Status:    ActionApprovalStatusPending,
			Approvers: []string{"approver-1"},
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		reviewedBy := "u-7"
		err := store.UpdateActionApproval(ctx, approvalID, ActionApprovalUpdate{
			Status:     ActionApprovalStatusApproved,
			ReviewedBy: &reviewedBy,
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}

		// The row must remain PENDING — a slow-but-fired-after-deadline write
		// is a real failure mode in distributed systems and we surface it
		// honestly.
		saved, _ := mem.GetActionApproval(context.Background(), approvalID)
		if saved.Status != ActionApprovalStatusPending {
			t.Fatalf("expected approval to stay PENDING after timed-out write, got %q", saved.Status)
		}
	})

	t.Run("ApproveHandler_CtxAlreadyCanceled_ReturnsApprovalUpdateFailed", func(t *testing.T) {
		mem := newMemActionApprovalStore()
		store := &slowApprovalStore{memActionApprovalStore: mem, delay: 50 * time.Millisecond}

		repo := &mockOmsRepo{}
		exec := NewExecutor(repo, &fakePublisher{offset: 1})
		exec.SetActionApprovalStore(store)
		handler := NewHandler(exec)
		router := setupApprovalRouter(handler)

		approvalID := "approval-timeout-2"
		_ = mem.CreateActionApproval(context.Background(), &ActionApproval{
			ID:              approvalID,
			OntologyAPIName: "ont-1",
			ActionType:      "deleteAccount",
			Status:          ActionApprovalStatusPending,
			Approvers:       []string{"approver-1"},
		})

		body := mustJSON(map[string]interface{}{"reason": "LGTM"})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/approvals/"+approvalID+"/approve",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		ctx, cancel := context.WithTimeout(req.Context(), 1*time.Millisecond)
		defer cancel()
		ctx = auth.WithUser(ctx, &auth.User{ID: "u-7", Roles: []string{"approver-1"}})
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Handler's reviewApproval surfaces store errors as 500 ApprovalUpdateFailed.
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on store timeout, got %d body=%s", w.Code, w.Body.String())
		}
		// Approval row remains PENDING because the slow store respected ctx
		// and returned before mutating.
		saved, _ := mem.GetActionApproval(context.Background(), approvalID)
		if saved.Status != ActionApprovalStatusPending {
			t.Fatalf("expected PENDING after handler timeout, got %q", saved.Status)
		}
	})

	t.Run("CanceledCtxDuringDelayReturnsCancelErr", func(t *testing.T) {
		mem := newMemActionApprovalStore()
		store := &slowApprovalStore{memActionApprovalStore: mem, delay: 100 * time.Millisecond}

		approvalID := "approval-cancel-3"
		_ = mem.CreateActionApproval(context.Background(), &ActionApproval{
			ID:        approvalID,
			Status:    ActionApprovalStatusPending,
			Approvers: []string{"approver-1"},
		})

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()

		err := store.UpdateActionApproval(ctx, approvalID, ActionApprovalUpdate{
			Status: ActionApprovalStatusApproved,
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled when caller aborts mid-flight, got %v", err)
		}
		saved, _ := mem.GetActionApproval(context.Background(), approvalID)
		if saved.Status != ActionApprovalStatusPending {
			t.Fatalf("expected PENDING after canceled mid-flight write, got %q", saved.Status)
		}
	})
}

// ---------------------------------------------------------------------------
// AC #3: Concurrent actor conflict on the same approval row
// ---------------------------------------------------------------------------

// casApprovalStore extends memActionApprovalStore with PENDING→terminal
// compare-and-set semantics: an Update that supplies a non-empty Status
// is rejected with errStaleApproval when the current row is no longer
// PENDING. Mirrors what a PG row with WHERE status='PENDING' would do.
type casApprovalStore struct {
	*memActionApprovalStore
}

var errStaleApproval = errors.New("approval already in terminal state")

func (c *casApprovalStore) UpdateActionApproval(_ context.Context, id string, upd ActionApprovalUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.approvals[id]
	if !ok {
		return oms.ErrNotFound
	}
	if upd.Status != "" && a.Status != ActionApprovalStatusPending {
		return errStaleApproval
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

// TestUS007_ConcurrentApprovalActors covers the "并发 actor 冲突" AC.
// Two reviewers race for the same approval; only one terminal status
// must stick, and the second actor must see a 409-shaped conflict
// surfaced by the handler.
func TestUS007_ConcurrentApprovalActors(t *testing.T) {
	t.Run("SequentialApproveThenReject_SecondReturns409", func(t *testing.T) {
		mem := newMemActionApprovalStore()
		repo := &mockOmsRepo{}
		exec := NewExecutor(repo, &fakePublisher{offset: 1})
		exec.SetActionApprovalStore(mem)
		handler := NewHandler(exec)
		router := setupApprovalRouter(handler)

		approvalID := "approval-conflict-seq"
		_ = mem.CreateActionApproval(context.Background(), &ActionApproval{
			ID:              approvalID,
			OntologyAPIName: "ont-1",
			ActionType:      "deleteAccount",
			Status:          ActionApprovalStatusPending,
			Approvers:       []string{"approver-1", "approver-2"},
		})

		do := func(verb, userID string) *httptest.ResponseRecorder {
			body := mustJSON(map[string]interface{}{})
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/ont-1/actions/approvals/"+approvalID+"/"+verb,
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := auth.WithUser(req.Context(), &auth.User{ID: userID, Roles: []string{"approver-1", "approver-2"}})
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			return w
		}

		w1 := do("approve", "u-1")
		if w1.Code != http.StatusOK {
			t.Fatalf("first approve must succeed, got %d body=%s", w1.Code, w1.Body.String())
		}
		w2 := do("reject", "u-2")
		if w2.Code != http.StatusConflict {
			t.Fatalf("second reviewer must see 409 ApprovalAlreadyReviewed, got %d body=%s", w2.Code, w2.Body.String())
		}
		saved, _ := mem.GetActionApproval(context.Background(), approvalID)
		if saved.Status != ActionApprovalStatusApproved {
			t.Fatalf("expected APPROVED after sequential conflict, got %q", saved.Status)
		}
	})

	t.Run("ConcurrentApprove_OnlyOneStoreWriteSucceeds_Cas", func(t *testing.T) {
		mem := newMemActionApprovalStore()
		store := &casApprovalStore{memActionApprovalStore: mem}

		approvalID := "approval-cas"
		_ = mem.CreateActionApproval(context.Background(), &ActionApproval{
			ID:              approvalID,
			OntologyAPIName: "ont-1",
			Status:          ActionApprovalStatusPending,
			Approvers:       []string{"approver-1"},
		})

		var ok, conflict int64
		var wg sync.WaitGroup
		ctx := context.Background()
		for i := 0; i < 10; i++ {
			wg.Add(1)
			reviewedBy := "actor"
			go func() {
				defer wg.Done()
				err := store.UpdateActionApproval(ctx, approvalID, ActionApprovalUpdate{
					Status:     ActionApprovalStatusApproved,
					ReviewedBy: &reviewedBy,
				})
				if err == nil {
					atomic.AddInt64(&ok, 1)
				} else if errors.Is(err, errStaleApproval) {
					atomic.AddInt64(&conflict, 1)
				}
			}()
		}
		wg.Wait()

		if ok != 1 {
			t.Fatalf("expected exactly 1 winning writer, got %d", ok)
		}
		if conflict != 9 {
			t.Fatalf("expected 9 stale-conflict losers, got %d", conflict)
		}
		saved, _ := mem.GetActionApproval(ctx, approvalID)
		if saved.Status != ActionApprovalStatusApproved {
			t.Fatalf("expected APPROVED terminal state, got %q", saved.Status)
		}
	})

	t.Run("WrongActorWithoutApproverRole_403_NoStateMutation", func(t *testing.T) {
		mem := newMemActionApprovalStore()
		repo := &mockOmsRepo{}
		exec := NewExecutor(repo, &fakePublisher{offset: 1})
		exec.SetActionApprovalStore(mem)
		handler := NewHandler(exec)
		router := setupApprovalRouter(handler)

		approvalID := "approval-wrong-actor"
		_ = mem.CreateActionApproval(context.Background(), &ActionApproval{
			ID:              approvalID,
			OntologyAPIName: "ont-1",
			Status:          ActionApprovalStatusPending,
			Approvers:       []string{"approver-1"},
		})

		body := mustJSON(map[string]interface{}{})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/approvals/"+approvalID+"/approve",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := auth.WithUser(req.Context(), &auth.User{ID: "intruder", Roles: []string{"viewer"}})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for wrong actor, got %d body=%s", w.Code, w.Body.String())
		}
		saved, _ := mem.GetActionApproval(context.Background(), approvalID)
		if saved.Status != ActionApprovalStatusPending {
			t.Fatalf("approval must stay PENDING on forbidden actor, got %q", saved.Status)
		}
	})
}

// ---------------------------------------------------------------------------
// AC #4: Edit collapse boundaries inside a saga commit
// ---------------------------------------------------------------------------

// TestUS007_SagaEditCollapseBoundaries covers cross-action collapse
// interactions inside the saga primary commit path:
//   - A CREATE followed by a DELETE on the same PK across two actions
//     cancels into 0 published edits.
//   - A LINK_CREATE followed by a LINK_DELETE on the same triple cancels.
//   - A LINK_DELETE followed by a LINK_CREATE on the same triple resolves
//     to the LINK_CREATE (last write wins for link recreate).
//   - Two MODIFY actions on the same PK merge keys, with later writes
//     shadowing earlier ones.
func TestUS007_SagaEditCollapseBoundaries(t *testing.T) {
	mkSaga := func(actionTypes []oms.ActionType, reqs []ApplyRequest) (*SagaResult, *fakePublisher, error) {
		repo := &mockOmsRepo{actionTypes: actionTypes}
		pub := &fakePublisher{offset: 1}
		exec := NewExecutor(repo, pub)
		exec.SetSagaStore(newMemSagaStore())
		result, err := exec.ApplyBatchSagaWithOptions(context.Background(), "ont-1", reqs, SagaOptions{})
		return result, pub, err
	}

	t.Run("CreateThenDelete_SamePK_CollapsedToZeroEdits_NoPublish", func(t *testing.T) {
		// createOrModifyObject resolves through resolveUpsertEdits — with no
		// ObjectExistenceChecker wired the UPSERT degrades to a CREATE that
		// honours the parameter-supplied primaryKey. The plain `createObject`
		// rule generates a fresh RID per call which would defeat the
		// cross-action collapse we want to assert here.
		ats := []oms.ActionType{
			newTestActionType("upsertEmp",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "createOrModifyObject", ObjectType: "Employee"}},
			),
			newTestActionType("deleteEmp",
				[]ParameterDef{{ID: "primaryKey", Type: "string", Required: true}},
				[]Rule{{Type: "deleteObject", ObjectType: "Employee"}},
			),
		}
		reqs := []ApplyRequest{
			{ActionType: "upsertEmp", Parameters: map[string]interface{}{"primaryKey": "emp1"}},
			{ActionType: "deleteEmp", Parameters: map[string]interface{}{"primaryKey": "emp1"}},
		}
		result, pub, err := mkSaga(ats, reqs)
		if err != nil {
			t.Fatalf("unexpected saga error: %v", err)
		}
		if pub.calls != 0 {
			t.Fatalf("CREATE+DELETE on same PK must collapse to 0 edits — no publish expected, got %d", pub.calls)
		}
		if result.Status != SagaStatusSuccess {
			t.Fatalf("expected SUCCESS even with empty collapsed batch, got %q", result.Status)
		}
		if len(result.AppliedEdits) != 0 {
			t.Fatalf("expected AppliedEdits empty after collapse, got %d", len(result.AppliedEdits))
		}
	})

	t.Run("LinkCreateThenLinkDelete_SameTriple_CollapsedToZeroEdits", func(t *testing.T) {
		ats := []oms.ActionType{
			newTestActionType("linkAB",
				[]ParameterDef{
					{ID: "src", Type: "string", Required: true},
					{ID: "dst", Type: "string", Required: true},
				},
				[]Rule{{Type: "createLink", LinkTypeAPIName: "AtoB",
					SourceObjectPrimaryKey: "src", TargetObjectPrimaryKey: "dst"}},
			),
			newTestActionType("unlinkAB",
				[]ParameterDef{
					{ID: "src", Type: "string", Required: true},
					{ID: "dst", Type: "string", Required: true},
				},
				[]Rule{{Type: "deleteLink", LinkTypeAPIName: "AtoB",
					SourceObjectPrimaryKey: "src", TargetObjectPrimaryKey: "dst"}},
			),
		}
		reqs := []ApplyRequest{
			{ActionType: "linkAB", Parameters: map[string]interface{}{"src": "a1", "dst": "b1"}},
			{ActionType: "unlinkAB", Parameters: map[string]interface{}{"src": "a1", "dst": "b1"}},
		}
		_, pub, err := mkSaga(ats, reqs)
		if err != nil {
			t.Fatalf("unexpected saga error: %v", err)
		}
		if pub.calls != 0 {
			t.Fatalf("LINK_CREATE+LINK_DELETE on same triple must collapse — no publish, got %d", pub.calls)
		}
	})

	t.Run("LinkDeleteThenLinkCreate_SameTriple_ResolvesToCreate", func(t *testing.T) {
		ats := []oms.ActionType{
			newTestActionType("unlinkAB",
				[]ParameterDef{
					{ID: "src", Type: "string", Required: true},
					{ID: "dst", Type: "string", Required: true},
				},
				[]Rule{{Type: "deleteLink", LinkTypeAPIName: "AtoB",
					SourceObjectPrimaryKey: "src", TargetObjectPrimaryKey: "dst"}},
			),
			newTestActionType("linkAB",
				[]ParameterDef{
					{ID: "src", Type: "string", Required: true},
					{ID: "dst", Type: "string", Required: true},
				},
				[]Rule{{Type: "createLink", LinkTypeAPIName: "AtoB",
					SourceObjectPrimaryKey: "src", TargetObjectPrimaryKey: "dst"}},
			),
		}
		reqs := []ApplyRequest{
			{ActionType: "unlinkAB", Parameters: map[string]interface{}{"src": "a1", "dst": "b1"}},
			{ActionType: "linkAB", Parameters: map[string]interface{}{"src": "a1", "dst": "b1"}},
		}
		_, pub, err := mkSaga(ats, reqs)
		if err != nil {
			t.Fatalf("unexpected saga error: %v", err)
		}
		if pub.calls != 1 {
			t.Fatalf("expected 1 publish (final LINK_CREATE), got %d", pub.calls)
		}
		batch := pub.batches[0]
		if len(batch.Edits) != 1 {
			t.Fatalf("expected 1 collapsed edit, got %d", len(batch.Edits))
		}
		if batch.Edits[0].Type != funnel.EditTypeLinkCreate {
			t.Fatalf("expected LINK_CREATE survives DELETE→CREATE collapse, got %q", batch.Edits[0].Type)
		}
	})

	t.Run("TwoModifyActions_SamePK_MergesPropertiesLatestWins", func(t *testing.T) {
		ats := []oms.ActionType{
			newTestActionType("setName",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
				},
				[]Rule{{Type: "modifyObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}}},
			),
			newTestActionType("setNameAndAge",
				[]ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
					{ID: "age", Type: "integer", Required: true},
				},
				[]Rule{{Type: "modifyObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
						"age":  {Type: "parameter", Value: "age"},
					}}},
			),
		}
		reqs := []ApplyRequest{
			{ActionType: "setName", Parameters: map[string]interface{}{"primaryKey": "emp1", "name": "Alice"}},
			{ActionType: "setNameAndAge", Parameters: map[string]interface{}{"primaryKey": "emp1", "name": "Alicia", "age": float64(30)}},
		}
		_, pub, err := mkSaga(ats, reqs)
		if err != nil {
			t.Fatalf("unexpected saga error: %v", err)
		}
		if pub.calls != 1 {
			t.Fatalf("expected 1 publish (merged MODIFY), got %d", pub.calls)
		}
		batch := pub.batches[0]
		if len(batch.Edits) != 1 {
			t.Fatalf("expected 1 collapsed MODIFY edit, got %d", len(batch.Edits))
		}
		ed := batch.Edits[0]
		if ed.Type != funnel.EditTypeModify {
			t.Fatalf("expected MODIFY, got %q", ed.Type)
		}
		if got := ed.Properties["name"]; got != "Alicia" {
			t.Fatalf("expected later-write name=Alicia, got %v", got)
		}
		if _, ok := ed.Properties["age"]; !ok {
			t.Fatalf("expected merged-in age key, got properties=%v", ed.Properties)
		}
	})
}

// ---------------------------------------------------------------------------
// AC #5: Batch timeout — async batch runner respects ctx.Deadline and
//        cancels in-flight work, marking the job CANCELED.
// ---------------------------------------------------------------------------

// TestUS007_AsyncBatch_ContextDeadlineExceeded covers the "batch 超时" AC.
// runAsyncApplyBatch checks ctx.Err() at every iteration boundary; once
// the deadline trips, the worker stops, marks the job CANCELED, and
// emits a final 'canceled' progress event.
func TestUS007_AsyncBatch_ContextDeadlineExceeded(t *testing.T) {
	mkExec := func() (*Executor, *memActionJobStore, *recordingPublisher) {
		repo := &mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmp",
					[]ParameterDef{{ID: "name", Type: "string", Required: true}},
					[]Rule{{Type: "createObject", ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}}},
				),
			},
		}
		// Slow publisher so the worker spends real time inside Apply,
		// giving the deadline a chance to fire mid-loop.
		pub := &slowPublisher{delay: 40 * time.Millisecond}
		exec := NewExecutor(repo, pub)
		store := newMemActionJobStore()
		exec.SetActionJobStore(store)
		prog := &recordingPublisher{}
		exec.SetProgressPublisher(prog)
		return exec, store, prog
	}

	t.Run("DeadlineFiresMidBatch_JobMarkedCanceled", func(t *testing.T) {
		exec, store, prog := mkExec()
		reqs := []ApplyRequest{
			{ActionType: "createEmp", Parameters: map[string]interface{}{"name": "Alice"}},
			{ActionType: "createEmp", Parameters: map[string]interface{}{"name": "Bob"}},
			{ActionType: "createEmp", Parameters: map[string]interface{}{"name": "Carol"}},
			{ActionType: "createEmp", Parameters: map[string]interface{}{"name": "Dave"}},
			{ActionType: "createEmp", Parameters: map[string]interface{}{"name": "Eve"}},
		}
		jobID := "job-deadline-1"
		_ = store.CreateActionJob(context.Background(), &ActionJob{
			JobID:          jobID,
			OntologyAPI:    "ont-1",
			ActionTypeName: "createEmp",
			Status:         ActionJobStatusPending,
		})

		// 60ms deadline → 1 slow publish (~40ms) succeeds, the next
		// iteration's ctx.Err() gate trips.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		defer cancel()
		done := make(chan struct{})
		go func() {
			runAsyncApplyBatch(ctx, exec, store, jobID, "ont-1", "createEmp", reqs, "ALL", cancel)
			close(done)
		}()
		<-done

		final, _ := store.GetActionJob(context.Background(), jobID)
		if final.Status != ActionJobStatusCanceled {
			t.Fatalf("expected CANCELED on deadline exceed, got %q (err=%q)", final.Status, final.ErrorMessage)
		}
		if final.Progress >= 100 {
			t.Fatalf("expected partial progress (<100%%) on deadline, got %d", final.Progress)
		}

		// The terminal progress event must be the 'canceled' marker.
		events := prog.snapshot()
		if len(events) == 0 {
			t.Fatal("expected at least one progress event")
		}
		last := events[len(events)-1]
		if last.Message != "canceled" {
			t.Fatalf("last progress message must be 'canceled', got %q", last.Message)
		}
	})

	t.Run("AlreadyDeadlineExceeded_BeforeFirstApply_ZeroApplied", func(t *testing.T) {
		exec, store, _ := mkExec()
		// Reuse the slowPublisher but with an already-expired context so
		// the very first iteration's ctx.Err() gate trips before any Apply.
		reqs := []ApplyRequest{
			{ActionType: "createEmp", Parameters: map[string]interface{}{"name": "Alice"}},
			{ActionType: "createEmp", Parameters: map[string]interface{}{"name": "Bob"}},
		}
		jobID := "job-deadline-2"
		_ = store.CreateActionJob(context.Background(), &ActionJob{
			JobID:          jobID,
			OntologyAPI:    "ont-1",
			ActionTypeName: "createEmp",
			Status:         ActionJobStatusPending,
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already canceled

		runAsyncApplyBatch(ctx, exec, store, jobID, "ont-1", "createEmp", reqs, "ALL", cancel)

		final, _ := store.GetActionJob(context.Background(), jobID)
		if final.Status != ActionJobStatusCanceled {
			t.Fatalf("expected CANCELED when ctx is already done, got %q", final.Status)
		}
		// Slow publisher with 40ms delay × 2 actions would take 80ms+ if it
		// actually ran — confirm via the publisher that it never did.
		sp := exec.publisher.(*slowPublisher)
		if got := atomic.LoadInt64(&sp.calls); got != 0 {
			t.Fatalf("publisher must not be called when ctx is pre-canceled, got %d calls", got)
		}
	})

	t.Run("ZeroBatch_DeadlineIrrelevant_TerminalSucceeded", func(t *testing.T) {
		exec, store, _ := mkExec()
		jobID := "job-deadline-empty"
		_ = store.CreateActionJob(context.Background(), &ActionJob{
			JobID:          jobID,
			OntologyAPI:    "ont-1",
			ActionTypeName: "createEmp",
			Status:         ActionJobStatusPending,
		})
		// Even an already-canceled ctx must not turn an empty batch into a
		// failure — the worker short-circuits the empty case to SUCCEEDED.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		runAsyncApplyBatch(ctx, exec, store, jobID, "ont-1", "createEmp", nil, "ALL", cancel)

		final, _ := store.GetActionJob(context.Background(), jobID)
		if final.Status != ActionJobStatusSucceeded {
			t.Fatalf("empty batch must succeed even with canceled ctx, got %q", final.Status)
		}
		if final.Progress != 100 {
			t.Fatalf("empty batch progress must be 100, got %d", final.Progress)
		}
	})
}

// slowPublisher implements Publisher with a configurable per-call delay
// so async-batch deadline tests can deterministically expire mid-loop.
// Records call count atomically for race-free assertions.
type slowPublisher struct {
	delay  time.Duration
	calls  int64
	offset uint64
}

func (s *slowPublisher) Publish(_ *funnel.EditBatch) (uint64, error) {
	atomic.AddInt64(&s.calls, 1)
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.offset, nil
}
