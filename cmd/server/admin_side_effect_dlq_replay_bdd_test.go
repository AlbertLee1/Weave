package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_AdminSideEffectDLQ_Replay covers PRD-V2 Gap-A4 round 35:
// the admin replay endpoint re-dispatches a failed-after-retries
// side-effect via the round-30 retry loop, persists the new outcome,
// bumps replay_count, stamps replayed_at, and flips replay_status to
// 'replayed' on success (kept 'pending' on failure so the operator
// can try again).
//
// Acceptance criteria (Given → When → Then):
//
//	Given a pending DLQ row whose snapshotted webhook target is now
//	      responding 200
//	When  POST /api/admin/side-effect-dlq/{id}/replay
//	Then  HTTP 200 with replayed:true, status:"replayed",
//	      outcome.status:"success", replayCount=1
//	      AND UpdateSideEffectDLQAfterReplay was called with success=true
//
//	Given a pending DLQ row whose webhook still returns 500
//	When  POST /api/admin/side-effect-dlq/{id}/replay
//	Then  HTTP 200 with replayed:false, status:"pending" (NOT
//	      replayed), outcome.status:"failed", replayCount=1
//	      AND UpdateSideEffectDLQAfterReplay was called with success=false
//
//	Given a row already in 'replayed' status
//	When  POST .../replay
//	Then  HTTP 409 with errorName SideEffectDLQNotReplayable
//
//	Given a missing DLQ row id
//	When  POST .../replay
//	Then  HTTP 404 SideEffectDLQEntryNotFound
//
//	Given a DLQ row whose linked action_log_id no longer exists
//	When  POST .../replay
//	Then  HTTP 404 SideEffectDLQActionLogMissing
//
//	Given a non-numeric id
//	When  POST .../replay
//	Then  HTTP 400 InvalidParameter:id
//
//	Given a degraded boot (Repo nil)
//	When  POST .../replay
//	Then  HTTP 503 SideEffectDLQNotConfigured
func TestBDD_AdminSideEffectDLQ_Replay(t *testing.T) {
	t.Run("pending row + now-healthy webhook → 200, replayed, status=replayed, count=1", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cfg := mustWebhookCfg(t, srv.URL, 0, 1) // 0 retries, 1ms backoff (fast)
		row := &oms.SideEffectDLQRow{
			ID: 42, ActionLogID: 100, EffectIndex: 0, EffectType: "webhook",
			EffectConfig: cfg,
			Outcome:      json.RawMessage(`{"status":"failed"}`),
			ReplayStatus: oms.SideEffectDLQStatusPending,
			CreatedAt:    time.Now(),
		}
		fake := &fakeSideEffectDLQRepo{
			rowByID: map[int64]*oms.SideEffectDLQRow{42: row},
			actionLogByID: map[int64]*oms.ActionLog{100: {
				ID: 100, ActionTypeRID: "ri.ontology.main.action-type.replay",
				Edits: json.RawMessage(`[{"type":"CREATE"}]`),
			}},
		}

		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/42/replay", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp adminSideEffectDLQReplayResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !resp.Replayed || resp.Status != oms.SideEffectDLQStatusReplayed ||
			resp.Outcome.Status != actions.SideEffectStatusSuccess || resp.ReplayCount != 1 {
			t.Errorf("resp = %+v, want {replayed:true, status:replayed, outcome.status:success, replayCount:1}", resp)
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Errorf("webhook hits = %d, want 1", atomic.LoadInt32(&calls))
		}
		if !fake.updateReplayLast.called || !fake.updateReplayLast.success {
			t.Errorf("UpdateSideEffectDLQAfterReplay: called=%v success=%v, want true/true",
				fake.updateReplayLast.called, fake.updateReplayLast.success)
		}
	})

	t.Run("pending row + still-broken webhook → 200, NOT replayed, status=pending", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		cfg := mustWebhookCfg(t, srv.URL, 0, 1)
		row := &oms.SideEffectDLQRow{
			ID: 43, ActionLogID: 101, EffectIndex: 0, EffectType: "webhook",
			EffectConfig: cfg, Outcome: json.RawMessage(`{}`),
			ReplayStatus: oms.SideEffectDLQStatusPending,
		}
		fake := &fakeSideEffectDLQRepo{
			rowByID:       map[int64]*oms.SideEffectDLQRow{43: row},
			actionLogByID: map[int64]*oms.ActionLog{101: {ID: 101, ActionTypeRID: "ri.x"}},
		}

		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/43/replay", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp adminSideEffectDLQReplayResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Replayed {
			t.Errorf("replayed = true, want false (webhook still broken)")
		}
		if resp.Status != oms.SideEffectDLQStatusPending {
			t.Errorf("status = %q, want pending (kept for retry)", resp.Status)
		}
		if resp.Outcome.Status != actions.SideEffectStatusFailed {
			t.Errorf("outcome.status = %q, want failed", resp.Outcome.Status)
		}
		if !fake.updateReplayLast.called || fake.updateReplayLast.success {
			t.Errorf("UpdateSideEffectDLQAfterReplay: called=%v success=%v, want true/false",
				fake.updateReplayLast.called, fake.updateReplayLast.success)
		}
	})

	t.Run("already-replayed row → 409 SideEffectDLQNotReplayable", func(t *testing.T) {
		row := &oms.SideEffectDLQRow{
			ID: 44, ActionLogID: 102, ReplayStatus: oms.SideEffectDLQStatusReplayed,
		}
		fake := &fakeSideEffectDLQRepo{
			rowByID: map[int64]*oms.SideEffectDLQRow{44: row},
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/44/replay", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "SideEffectDLQNotReplayable" {
			t.Errorf("errorName = %q, want SideEffectDLQNotReplayable", env.ErrorName)
		}
		if fake.updateReplayLast.called {
			t.Errorf("UpdateSideEffectDLQAfterReplay was called; should be skipped for non-pending rows")
		}
	})

	t.Run("missing dlq id → 404 SideEffectDLQEntryNotFound", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{rowByID: map[int64]*oms.SideEffectDLQRow{}}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/999/replay", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "SideEffectDLQEntryNotFound" {
			t.Errorf("errorName = %q, want SideEffectDLQEntryNotFound", env.ErrorName)
		}
	})

	t.Run("missing linked action_log → 404 SideEffectDLQActionLogMissing", func(t *testing.T) {
		row := &oms.SideEffectDLQRow{
			ID: 45, ActionLogID: 999_999, EffectType: "webhook",
			EffectConfig: json.RawMessage(`{}`),
			Outcome:      json.RawMessage(`{}`),
			ReplayStatus: oms.SideEffectDLQStatusPending,
		}
		fake := &fakeSideEffectDLQRepo{
			rowByID:       map[int64]*oms.SideEffectDLQRow{45: row},
			actionLogByID: map[int64]*oms.ActionLog{}, // empty
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/45/replay", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "SideEffectDLQActionLogMissing" {
			t.Errorf("errorName = %q, want SideEffectDLQActionLogMissing", env.ErrorName)
		}
	})

	t.Run("non-numeric id → 400", func(t *testing.T) {
		fake := &fakeSideEffectDLQRepo{}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/abc/replay", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(fake).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("degraded boot (Repo nil) → 503", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/side-effect-dlq/1/replay", nil)
		rec := httptest.NewRecorder()
		newSideEffectDLQRouter(nil).ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rec.Code)
		}
	})
}

// mustWebhookCfg marshals a webhookConfig for the BDD test fixtures.
// MaxRetries=0 keeps the retry loop to a single attempt — round-30
// retry semantics are already covered elsewhere, this BDD focuses on
// the round-35 wiring.
func mustWebhookCfg(t *testing.T, url string, maxRetries, backoffMs int) json.RawMessage {
	t.Helper()
	// Embed the raw JSON so we don't import pkg/actions internals.
	body := map[string]interface{}{
		"url":                      url,
		"maxRetries":               maxRetries,
		"retryBackoffMilliseconds": backoffMs,
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal webhookCfg: %v", err)
	}
	return b
}

// Force-use to keep import.
var _ = strconv.Itoa
