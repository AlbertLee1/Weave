package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_Executor_RoutesFailedSideEffectsToDLQ covers PRD-V2 Gap-A4
// round 33: after the round-30 retry loop exhausts its budget on a
// transient failure (status=failed), the executor must persist a
// row to action_log_side_effect_dlq so operators can review / replay
// via the admin surface (round 34).
//
// Acceptance criteria (Given → When → Then):
//
//   Given a webhook that always returns 500 (exhausts retries)
//   When  the executor applies the action
//   Then  ONE DLQ row is inserted with:
//           action_log_id matching the action's log id
//           effect_index = 0
//           effect_type  = "webhook"
//           effect_config carrying the original SideEffect.Config blob
//                         (so a future replay can dispatch without re-
//                         reading the ActionType)
//           outcome      = the SideEffectOutcome JSON (status=failed,
//                          attempts=2, error mentions "gave up")
//           replay_status = "pending"
//
//   Given an action with both a successful and a failing webhook
//   When  the executor applies the action
//   Then  ONLY the failing webhook produces a DLQ row;
//         the successful webhook's outcome stays out of the DLQ.
//
//   Given an action with NO side effects
//   When  the executor applies the action
//   Then  NO DLQ rows are inserted (we don't churn the table for
//         zero-effects actions)
//
//   Given an action whose only side effect succeeds on 1st attempt
//   When  the executor applies the action
//   Then  NO DLQ rows are inserted (success path never queues)
func TestBDD_Executor_RoutesFailedSideEffectsToDLQ(t *testing.T) {
	t.Run("exhausted-retry webhook lands one DLQ row with effect_config snapshot", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		// MaxRetries=1 → total attempts=2 (test stays fast).
		at := actionTypeWithWebhook(t, "wh-dlq-fail", srv.URL, 1, 1)
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "wh-dlq-fail",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(repo.dlqRows) != 1 {
			t.Fatalf("len(dlqRows) = %d, want 1 (one failed webhook)", len(repo.dlqRows))
		}
		row := repo.dlqRows[0]
		if row.ActionLogID != result.ActionLogID {
			t.Errorf("DLQ row action_log_id = %d, want %d", row.ActionLogID, result.ActionLogID)
		}
		if row.EffectIndex != 0 {
			t.Errorf("DLQ row effect_index = %d, want 0", row.EffectIndex)
		}
		if row.EffectType != "webhook" {
			t.Errorf("DLQ row effect_type = %q, want webhook", row.EffectType)
		}
		if row.ReplayStatus != oms.SideEffectDLQStatusPending {
			t.Errorf("DLQ row replay_status = %q, want pending", row.ReplayStatus)
		}
		// effect_config should round-trip the original URL so a replay
		// can dispatch without re-reading the ActionType.
		var cfg webhookConfig
		if err := json.Unmarshal(row.EffectConfig, &cfg); err != nil {
			t.Fatalf("unmarshal effect_config: %v; raw=%s", err, string(row.EffectConfig))
		}
		if cfg.URL != srv.URL {
			t.Errorf("effect_config URL = %q, want %q", cfg.URL, srv.URL)
		}
		// outcome should carry the SideEffectOutcome with status=failed.
		var oc SideEffectOutcome
		if err := json.Unmarshal(row.Outcome, &oc); err != nil {
			t.Fatalf("unmarshal outcome: %v; raw=%s", err, string(row.Outcome))
		}
		if oc.Status != SideEffectStatusFailed || oc.Attempts != 2 {
			t.Errorf("outcome = %+v, want {status=failed attempts=2}", oc)
		}
	})

	t.Run("mixed success+failure: only the failing effect queues", func(t *testing.T) {
		okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer okSrv.Close()
		failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer failSrv.Close()

		at := actionTypeWithTwoWebhooks(t, "wh-mixed", okSrv.URL, failSrv.URL)
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "wh-mixed",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(repo.dlqRows) != 1 {
			t.Fatalf("len(dlqRows) = %d, want 1 (only the failing webhook queues)", len(repo.dlqRows))
		}
		row := repo.dlqRows[0]
		if row.EffectIndex != 1 {
			t.Errorf("DLQ row effect_index = %d, want 1 (failing effect is at index 1)", row.EffectIndex)
		}
		if row.ActionLogID != result.ActionLogID {
			t.Errorf("DLQ row action_log_id = %d, want %d", row.ActionLogID, result.ActionLogID)
		}
		// Verify the action_logs.side_effect_status still reflects both
		// outcomes (success + failed) — DLQ is parallel, not a
		// replacement.
		al := repo.actionLogByID[result.ActionLogID]
		if al == nil || len(al.SideEffectStatus) == 0 {
			t.Fatalf("action_log %d SideEffectStatus missing", result.ActionLogID)
		}
		var outcomes []SideEffectOutcome
		if err := json.Unmarshal(al.SideEffectStatus, &outcomes); err != nil {
			t.Fatalf("unmarshal side_effect_status: %v", err)
		}
		if len(outcomes) != 2 ||
			outcomes[0].Status != SideEffectStatusSuccess ||
			outcomes[1].Status != SideEffectStatusFailed {
			t.Errorf("side_effect_status outcomes = %+v, want [success, failed]", outcomes)
		}
	})

	t.Run("action with no side effects: no DLQ rows queued", func(t *testing.T) {
		at := newTestActionType("createNoEffects", []ParameterDef{
			{ID: "name", Type: "string", Required: true},
		}, []Rule{
			{
				Type:       "createObject",
				ObjectType: "Employee",
				PropertyBindings: map[string]PropertyBinding{
					"name": {Type: "parameter", Value: "name"},
				},
			},
		})
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "createNoEffects",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(repo.dlqRows) != 0 {
			t.Errorf("len(dlqRows) = %d, want 0 (no side effects, no DLQ)", len(repo.dlqRows))
		}
	})

	t.Run("successful webhook: no DLQ row (success path doesn't queue)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		at := actionTypeWithWebhook(t, "wh-ok", srv.URL, 3, 1)
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "wh-ok",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(repo.dlqRows) != 0 {
			t.Errorf("len(dlqRows) = %d, want 0 (success outcomes don't queue)", len(repo.dlqRows))
		}
	})
}

// actionTypeWithTwoWebhooks creates an action with two webhooks:
// index 0 points at the always-200 server (success path), index 1
// at the always-500 server (DLQ candidate).
func actionTypeWithTwoWebhooks(t *testing.T, apiName, okURL, failURL string) oms.ActionType {
	t.Helper()
	at := newTestActionType(apiName, []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}, []Rule{
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	})
	okCfg, _ := json.Marshal(webhookConfig{URL: okURL, RetryBackoffMilliseconds: 1})
	failCfg, _ := json.Marshal(webhookConfig{URL: failURL, MaxRetries: 1, RetryBackoffMilliseconds: 1})
	effects, err := json.Marshal([]SideEffect{
		{Type: "webhook", Config: okCfg},
		{Type: "webhook", Config: failCfg},
	})
	if err != nil {
		t.Fatalf("marshal effects: %v", err)
	}
	at.SideEffects = effects
	return at
}
