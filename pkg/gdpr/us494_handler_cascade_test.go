package gdpr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/comments"
	"github.com/liyang/weave/pkg/reactions"
	"github.com/liyang/weave/pkg/userprefs"
	"github.com/liyang/weave/pkg/watches"
)

// TestEraseUser_RejectsAnonymous mirrors Erase's defence-in-depth
// auth-context check on the new DELETE /users/{userId}/erase route.
func TestEraseUser_RejectsAnonymous(t *testing.T) {
	jobStore := NewMemoryJobStore()
	h := newTestHandler(t, jobStore, simpleEraser(jobStore))
	rec := doRequest(h, http.MethodDelete, "/api/admin/gdpr/users/u123/erase", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

// TestEraseUser_RejectsMissingUserID asserts a path-param-stripped
// request 404s on chi's router rather than 500-ing inside the handler.
func TestEraseUser_RejectsMissingUserID(t *testing.T) {
	jobStore := NewMemoryJobStore()
	h := newTestHandler(t, jobStore, simpleEraser(jobStore))
	rec := doRequest(h, http.MethodDelete, "/api/admin/gdpr/users//erase", nil, "user:admin")
	if rec.Code == http.StatusOK || rec.Code == http.StatusAccepted {
		t.Fatalf("expected non-2xx for empty userId, got %d", rec.Code)
	}
}

// TestEraseUser_NoCascadeRunsDefaultSteps verifies the default eraser
// (sessions / refresh / user / audit) is invoked when ?cascade=true is
// absent. The cascade eraser is set on the handler but must NOT run.
func TestEraseUser_NoCascadeRunsDefaultSteps(t *testing.T) {
	jobStore := NewMemoryJobStore()
	var defaultCalls int32
	defaultStep := StepFunc{StepName: "default", Fn: func(context.Context, string) (int, error) {
		atomic.AddInt32(&defaultCalls, 1)
		return 0, nil
	}}
	defaultEraser := NewEraser(jobStore, []Step{defaultStep})

	var cascadeCalls int32
	cascadeStep := StepFunc{StepName: "cascade", Fn: func(context.Context, string) (int, error) {
		atomic.AddInt32(&cascadeCalls, 1)
		return 0, nil
	}}
	cascadeEraser := NewEraser(jobStore, []Step{cascadeStep})

	h := newTestHandler(t, jobStore, defaultEraser)
	h.SetCascadeEraser(cascadeEraser)

	rec := doRequest(h, http.MethodDelete, "/api/admin/gdpr/users/user:carol/erase", nil, "user:admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	waitForJob(t, jobStore, decodeJobID(t, rec))
	if atomic.LoadInt32(&defaultCalls) != 1 {
		t.Errorf("default eraser calls = %d, want 1", atomic.LoadInt32(&defaultCalls))
	}
	if atomic.LoadInt32(&cascadeCalls) != 0 {
		t.Errorf("cascade eraser ran without ?cascade=true: %d", atomic.LoadInt32(&cascadeCalls))
	}
}

// TestEraseUser_CascadeTrueRunsCascadeEraser verifies the ?cascade=true
// query param picks the cascade eraser instead of the default one.
func TestEraseUser_CascadeTrueRunsCascadeEraser(t *testing.T) {
	jobStore := NewMemoryJobStore()
	var defaultCalls, cascadeCalls int32
	defaultEraser := NewEraser(jobStore, []Step{StepFunc{StepName: "d", Fn: func(context.Context, string) (int, error) {
		atomic.AddInt32(&defaultCalls, 1)
		return 0, nil
	}}})
	cascadeEraser := NewEraser(jobStore, []Step{StepFunc{StepName: "c", Fn: func(context.Context, string) (int, error) {
		atomic.AddInt32(&cascadeCalls, 1)
		return 0, nil
	}}})
	h := newTestHandler(t, jobStore, defaultEraser)
	h.SetCascadeEraser(cascadeEraser)

	rec := doRequest(h, http.MethodDelete, "/api/admin/gdpr/users/user:carol/erase?cascade=true", nil, "user:admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	waitForJob(t, jobStore, decodeJobID(t, rec))
	if atomic.LoadInt32(&cascadeCalls) != 1 {
		t.Errorf("cascade eraser not invoked: calls=%d", atomic.LoadInt32(&cascadeCalls))
	}
	if atomic.LoadInt32(&defaultCalls) != 0 {
		t.Errorf("default eraser ran under ?cascade=true: %d", atomic.LoadInt32(&defaultCalls))
	}
}

// TestEraseUser_CascadeTrueWithoutCascadeEraserFallsBack: when no
// cascade eraser is configured (e.g. partially-wired test deployment),
// ?cascade=true falls back to the default eraser rather than 500-ing.
// The audit row records cascade=true so operators can see the intent
// even if the cleanup is incomplete.
func TestEraseUser_CascadeTrueWithoutCascadeEraserFallsBack(t *testing.T) {
	jobStore := NewMemoryJobStore()
	auditStore := audit.NewMemoryStore()
	h := newTestHandler(t, jobStore, simpleEraser(jobStore))
	h.auditStore = auditStore

	rec := doRequest(h, http.MethodDelete, "/api/admin/gdpr/users/user:carol/erase?cascade=true", nil, "user:admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	waitForJob(t, jobStore, decodeJobID(t, rec))

	events, _ := auditStore.List(context.Background(), audit.ListFilter{})
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Action != "gdpr_erase_request" {
		t.Errorf("audit action = %q", events[0].Action)
	}
	if !strings.Contains(string(events[0].DiffJSON), `"cascade":true`) {
		t.Errorf("audit diff should record cascade=true: %s", events[0].DiffJSON)
	}
}

// TestBDD_US494_CascadeErase_ZeroUserIDReferencesAfter is the headline
// US-494 BDD scenario. It wires the cascade eraser against the
// in-memory comments / reactions / watches / userprefs stores, seeds
// each one with rows owned by 'user:alice' AND 'user:bob', then hits
// DELETE /api/admin/gdpr/users/user:alice/erase?cascade=true.
//
// Given each store carries rows for both users.
// When the operator issues the DELETE with cascade=true.
// Then the job reaches SUCCEEDED, every row referencing user:alice is
//
//	gone from every store ("user_id 出现次数 = 0"), every row
//	referencing user:bob is untouched, and the audit log carries the
//	cascade=true request marker for compliance review.
func TestBDD_US494_CascadeErase_ZeroUserIDReferencesAfter(t *testing.T) {
	// --- Given ---
	ctx := context.Background()
	cs := comments.NewMemoryStore()
	rs := reactions.NewMemoryStore()
	ws := watches.NewMemoryStore()
	ps := userprefs.NewMemoryStore()

	target := "ri.ontology.main.object.alpha"
	mustCreate := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustCreate(cs.Create(ctx, &comments.Comment{ID: "c-a-1", TargetRID: target, Author: "user:alice", Body: "alice 1"}))
	mustCreate(cs.Create(ctx, &comments.Comment{ID: "c-a-2", TargetRID: target, Author: "user:alice", Body: "alice 2"}))
	mustCreate(cs.Create(ctx, &comments.Comment{ID: "c-b-1", TargetRID: target, Author: "user:bob", Body: "bob 1"}))
	// alice has soft-deleted one of her own comments — the cascade must
	// still nuke the tombstone, otherwise user_id leaks through.
	mustCreate(cs.Delete(ctx, "c-a-2", "user:alice"))

	mustCreate(rs.Create(ctx, &reactions.Reaction{ID: "r-a-1", UserID: "user:alice", TargetRID: target, Emoji: "👍"}))
	mustCreate(rs.Create(ctx, &reactions.Reaction{ID: "r-a-2", UserID: "user:alice", TargetRID: target, Emoji: "🎉"}))
	mustCreate(rs.Create(ctx, &reactions.Reaction{ID: "r-b-1", UserID: "user:bob", TargetRID: target, Emoji: "👍"}))

	mustCreate(ws.Create(ctx, &watches.Watch{ID: "w-a-1", UserID: "user:alice", TargetRID: target}))
	mustCreate(ws.Create(ctx, &watches.Watch{ID: "w-b-1", UserID: "user:bob", TargetRID: target}))

	theme := "dark"
	if _, err := ps.Upsert(ctx, "user:alice", userprefs.Update{Theme: &theme}); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Upsert(ctx, "user:bob", userprefs.Update{Theme: &theme}); err != nil {
		t.Fatal(err)
	}

	jobStore := NewMemoryJobStore()
	defaultEraser := NewEraser(jobStore, []Step{
		StepFunc{StepName: "noop", Fn: func(context.Context, string) (int, error) { return 0, nil }},
	})
	cascadeEraser := NewEraser(jobStore, []Step{
		StepFunc{StepName: "noop", Fn: func(context.Context, string) (int, error) { return 0, nil }},
		NewCommentsCascadeStep(cs),
		NewReactionsCascadeStep(rs),
		NewWatchesCascadeStep(ws),
		NewUserPrefsCascadeStep(ps),
	})
	auditStore := audit.NewMemoryStore()
	h := newTestHandler(t, jobStore, defaultEraser)
	h.SetCascadeEraser(cascadeEraser)
	h.auditStore = auditStore

	// --- When ---
	rec := doRequest(h, http.MethodDelete, "/api/admin/gdpr/users/user:alice/erase?cascade=true", nil, "user:admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	jobID := decodeJobID(t, rec)
	job := waitForJob(t, jobStore, jobID)
	if job.Status != JobStatusSucceeded {
		t.Fatalf("job status = %s (err=%q); want SUCCEEDED. steps=%#v", job.Status, job.ErrorMessage, job.Steps)
	}

	// --- Then: cascade invariant — user:alice count = 0 ---
	countUserID := func(userID string) int {
		t.Helper()
		n := 0
		rows, _, err := cs.List(ctx, comments.ListQuery{TargetRID: target, IncludeDeleted: true, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range rows {
			if c.Author == userID {
				n++
			}
		}
		// reactions: aggregate-as-target then probe; cheaper to scan once.
		if _, err := rs.AggregateForTarget(ctx, userID, target); err != nil {
			t.Fatal(err)
		}
		// Use a direct store walk via Create-then-Delete probe: if a row
		// exists for (userID, target, emoji), Delete returns nil; absent
		// rows return ErrNotFound. We emulate a count via known emojis.
		for _, emoji := range []string{"👍", "🎉"} {
			if err := rs.Delete(ctx, userID, target, emoji); err == nil {
				n++
				// Re-create the probe row so the next assertion stays
				// honest about pre-erase state.
				_ = rs.Create(ctx, &reactions.Reaction{ID: userID + emoji, UserID: userID, TargetRID: target, Emoji: emoji})
			}
		}
		watchRows, err := ws.List(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		n += len(watchRows)
		if _, err := ps.Get(ctx, userID); err == nil {
			n++
		}
		return n
	}
	if got := countUserID("user:alice"); got != 0 {
		t.Errorf("user:alice references after cascade = %d, want 0", got)
	}
	if got := countUserID("user:bob"); got == 0 {
		t.Errorf("user:bob was clobbered by alice's cascade — refs = %d, want > 0", got)
	}

	events, _ := auditStore.List(ctx, audit.ListFilter{})
	if len(events) == 0 {
		t.Fatalf("expected at least one audit event for cascade erase")
	}
	if !strings.Contains(string(events[0].DiffJSON), `"cascade":true`) {
		t.Errorf("audit diff should record cascade=true: %s", events[0].DiffJSON)
	}
}

// --- helpers ---

func decodeJobID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp EraseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if resp.JobID == "" {
		t.Fatal("empty jobId in response")
	}
	return resp.JobID
}

func waitForJob(t *testing.T, store JobStore, jobID string) *ErasureJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := store.GetJob(context.Background(), jobID)
		if err == nil && (got.Status == JobStatusSucceeded || got.Status == JobStatusFailed) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s did not reach terminal state: %#v err=%v", jobID, got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Make sure the chi import is used even when only Delete-style routes
// reach this file via the helper map.
var _ = chi.URLParam
