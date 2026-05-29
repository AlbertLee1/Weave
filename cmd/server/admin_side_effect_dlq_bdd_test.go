package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_AdminSideEffectDLQ_ListAndAbandon covers PRD-V2 Gap-A4 round
// 34: the admin surface that lets operators inspect and dismiss
// failed-after-retries side-effect dispatches sitting in the round-33
// action_log_side_effect_dlq table.
//
// Acceptance criteria (Given → When → Then):
//
//	Given the DLQ has 2 pending rows
//	When  GET /api/admin/side-effect-dlq
//	Then  it returns 200 with both entries in the JSON `entries`
//	      array, newest-first ordering (DESC by created_at)
//
//	Given a degraded boot where deps.Repo is nil
//	When  GET /api/admin/side-effect-dlq
//	Then  it returns 503 SERVICE_UNAVAILABLE with errorName
//	      SideEffectDLQNotConfigured
//
//	Given ?limit=2 query parameter
//	When  GET /api/admin/side-effect-dlq?limit=2
//	Then  the response truncates to 2 entries (limit is honored)
//
//	Given a pending row with id 42
//	When  POST /api/admin/side-effect-dlq/42/abandon
//	Then  it returns 200 {id:42, abandoned:true, status:"abandoned"}
//	      AND the underlying row's replay_status flipped
//
//	Given an already-abandoned row
//	When  POST /api/admin/side-effect-dlq/{id}/abandon
//	Then  it returns 200 (idempotent on rows already in abandoned)
//
//	Given a replayed row (Status="replayed")
//	When  POST /api/admin/side-effect-dlq/{id}/abandon
//	Then  it returns 409 CONFLICT with errorName
//	      SideEffectDLQCannotAbandonReplayed (can't mask a
//	      successful replay)
//
//	Given a missing id
//	When  POST /api/admin/side-effect-dlq/{id}/abandon
//	Then  it returns 404 NOT_FOUND with errorName
//	      SideEffectDLQEntryNotFound
//
//	Given a non-numeric id
//	When  POST /api/admin/side-effect-dlq/abc/abandon
//	Then  it returns 400 INVALID_ARGUMENT
func TestBDD_AdminSideEffectDLQ_ListAndAbandon(t *testing.T) {
	t.Run("list returns pending rows newest-first", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{
			pending: []oms.SideEffectDLQRow{
				{ID: 11, ActionLogID: 100, EffectIndex: 0, EffectType: "webhook",
					ReplayStatus: oms.SideEffectDLQStatusPending,
					Outcome:      json.RawMessage(`{"status":"failed"}`),
					CreatedAt:    time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				{ID: 12, ActionLogID: 101, EffectIndex: 0, EffectType: "webhook",
					ReplayStatus: oms.SideEffectDLQStatusPending,
					Outcome:      json.RawMessage(`{"status":"failed"}`),
					CreatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/admin/side-effect-dlq", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var resp adminSideEffectDLQListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
		}
		if len(resp.Entries) != 2 {
			t.Fatalf("entries = %d, want 2", len(resp.Entries))
		}
		// The repo determines ordering; this test just verifies the
		// handler forwards the rows unchanged.
		if resp.Entries[0].ID != 11 || resp.Entries[1].ID != 12 {
			t.Errorf("entry ids = [%d, %d], want [11, 12] (repo order preserved)",
				resp.Entries[0].ID, resp.Entries[1].ID)
		}
	})

	t.Run("degraded boot (Repo nil) returns 503 SideEffectDLQNotConfigured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/side-effect-dlq", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(nil).ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "SideEffectDLQNotConfigured" {
			t.Errorf("errorName = %q, want SideEffectDLQNotConfigured", env.ErrorName)
		}
	})

	t.Run("limit query parameter caps the response", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{
			pending: []oms.SideEffectDLQRow{
				{ID: 1, ReplayStatus: oms.SideEffectDLQStatusPending},
				{ID: 2, ReplayStatus: oms.SideEffectDLQStatusPending},
				{ID: 3, ReplayStatus: oms.SideEffectDLQStatusPending},
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/admin/side-effect-dlq?limit=2", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if fake.lastListLimit != 2 {
			t.Errorf("repo got limit = %d, want 2", fake.lastListLimit)
		}
	})

	t.Run("invalid limit query parameter returns 400", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{}
		req := httptest.NewRequest(http.MethodGet, "/api/admin/side-effect-dlq?limit=-3", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("abandon flips pending row to abandoned and returns 200", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{
			byID: map[int64]string{42: oms.SideEffectDLQStatusPending},
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/42/abandon", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var resp adminSideEffectDLQAbandonResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.ID != 42 || !resp.Abandoned || resp.Status != oms.SideEffectDLQStatusAbandoned {
			t.Errorf("resp = %+v, want {id:42, abandoned:true, status:abandoned}", resp)
		}
		// And the fake repo's underlying state flipped.
		if fake.byID[42] != oms.SideEffectDLQStatusAbandoned {
			t.Errorf("repo state[42] = %q, want abandoned", fake.byID[42])
		}
	})

	t.Run("abandon on already-abandoned row is idempotent (200)", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{
			byID: map[int64]string{42: oms.SideEffectDLQStatusAbandoned},
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/42/abandon", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (idempotent); body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("abandon on replayed row returns 409 CONFLICT", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{
			byID: map[int64]string{42: oms.SideEffectDLQStatusReplayed},
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/42/abandon", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "SideEffectDLQCannotAbandonReplayed" {
			t.Errorf("errorName = %q, want SideEffectDLQCannotAbandonReplayed", env.ErrorName)
		}
	})

	t.Run("abandon on missing id returns 404 NOT_FOUND", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{byID: map[int64]string{}}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/999/abandon", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "SideEffectDLQEntryNotFound" {
			t.Errorf("errorName = %q, want SideEffectDLQEntryNotFound", env.ErrorName)
		}
	})

	t.Run("abandon with non-numeric id returns 400", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{byID: map[int64]string{}}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/abc/abandon", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
		}
	})
}

// fakeSideEffectDLQRepo is a minimal in-memory test double for
// SideEffectDLQRepo. byID drives MarkSideEffectDLQAbandoned;
// pending drives ListPendingSideEffectDLQRows. rowByID + actionLogByID
// support the round-35 replay path. updateReplayCalls captures replay
// outcome persistence so tests can assert what was written.
type fakeSideEffectDLQRepo struct {
	pending          []oms.SideEffectDLQRow
	byID             map[int64]string // id → current replay_status
	rowByID          map[int64]*oms.SideEffectDLQRow
	actionLogByID    map[int64]*oms.ActionLog
	updateReplayLast struct {
		id      int64
		outcome json.RawMessage
		success bool
		called  bool
	}
	updateReplayErr error
	lastListLimit   int
}

func (f *fakeSideEffectDLQRepo) ListPendingSideEffectDLQRows(_ context.Context, limit int) ([]oms.SideEffectDLQRow, error) {
	f.lastListLimit = limit
	if limit > 0 && limit < len(f.pending) {
		return f.pending[:limit], nil
	}
	return f.pending, nil
}

func (f *fakeSideEffectDLQRepo) MarkSideEffectDLQAbandoned(_ context.Context, id int64) error {
	cur, ok := f.byID[id]
	if !ok {
		return oms.ErrNotFound
	}
	if cur == oms.SideEffectDLQStatusAbandoned {
		return nil
	}
	if cur == oms.SideEffectDLQStatusReplayed {
		return oms.ErrInvalidState
	}
	f.byID[id] = oms.SideEffectDLQStatusAbandoned
	return nil
}

func (f *fakeSideEffectDLQRepo) GetSideEffectDLQRow(_ context.Context, id int64) (*oms.SideEffectDLQRow, error) {
	if row, ok := f.rowByID[id]; ok {
		// Return a copy so handler mutations don't leak.
		cp := *row
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (f *fakeSideEffectDLQRepo) UpdateSideEffectDLQAfterReplay(_ context.Context, id int64, outcome json.RawMessage, success bool) error {
	f.updateReplayLast.id = id
	f.updateReplayLast.outcome = outcome
	f.updateReplayLast.success = success
	f.updateReplayLast.called = true
	if f.updateReplayErr != nil {
		return f.updateReplayErr
	}
	if row, ok := f.rowByID[id]; ok {
		row.ReplayCount++
		if success {
			row.ReplayStatus = oms.SideEffectDLQStatusReplayed
		}
	}
	return nil
}

func (f *fakeSideEffectDLQRepo) GetActionLog(_ context.Context, id int64) (*oms.ActionLog, error) {
	if al, ok := f.actionLogByID[id]; ok {
		return al, nil
	}
	return nil, oms.ErrNotFound
}

// newSideEffectDLQRouter wires the round-34/35 admin handlers on a
// fresh chi router. Mirrors the production wire-up in main.go minus
// the auth middleware (these tests focus on handler contract).
func newSideEffectDLQRouter(repo SideEffectDLQRepo) http.Handler {
	r := chi.NewRouter()
	deps := AdminSideEffectDLQDeps{Repo: repo}
	r.Method(http.MethodGet, "/api/admin/side-effect-dlq", NewAdminSideEffectDLQListHandler(deps))
	r.Method(http.MethodPost, "/api/admin/side-effect-dlq/{id}/abandon", NewAdminSideEffectDLQAbandonHandler(deps))
	r.Method(http.MethodPost, "/api/admin/side-effect-dlq/{id}/replay", NewAdminSideEffectDLQReplayHandler(deps))
	return r
}

// keep imports honest if other test files in the package don't need them
var _ = errors.Is
var _ = strconv.Atoi
