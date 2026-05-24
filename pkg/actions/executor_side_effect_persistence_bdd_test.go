package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_Executor_PersistsSideEffectOutcomesToActionLog covers the
// PRD-V2 Gap-A4 round-32 wiring: after an action commits, the executor
// calls ExecuteSideEffectsWithOutcomes and then stamps the per-effect
// outcomes onto action_logs.side_effect_status via the new
// UpdateActionLogSideEffectStatus repo method. Together with the
// pkg/oms PG integration test for the repo round-trip, this BDD proves
// the end-to-end persistence path: action → dispatcher → repo → row.
//
// Acceptance criteria (Given → When → Then):
//
//   Given an ActionType with a webhook side effect pointing at an
//         httptest.Server that returns 200
//   When  the executor applies the action
//   Then  the action_logs row's SideEffectStatus carries a single
//         outcome with status=success, attempts=1, type=webhook
//
//   Given an ActionType with NO side effects
//   When  the executor applies the action
//   Then  the action_logs row's SideEffectStatus is nil
//         (don't churn the column for actions with no side effects)
//
//   Given an ActionType with a webhook side effect that always
//         returns 500
//   When  the executor applies the action
//   Then  the action_logs row's SideEffectStatus carries
//         status=failed and the error message mentions "gave up"
func TestBDD_Executor_PersistsSideEffectOutcomesToActionLog(t *testing.T) {
	t.Run("webhook success outcome persisted to action_logs.side_effect_status", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		at := actionTypeWithWebhook(t, "wh-success", srv.URL, 3, 1)
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "wh-success",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Errorf("webhook hits = %d, want 1", atomic.LoadInt32(&calls))
		}

		// Locate the persisted action_logs row and inspect its
		// SideEffectStatus.
		assertActionLogSideEffectStatus(t, repo, result, []outcomeAssertion{
			{Type: "webhook", Status: SideEffectStatusSuccess, Attempts: 1, ErrorEmpty: true},
		})
	})

	t.Run("zero side effects leaves SideEffectStatus nil", func(t *testing.T) {
		// Use a real createObject action (which DOES persist an
		// action_logs row) but leave SideEffects unset. The executor
		// must NOT call UpdateActionLogSideEffectStatus for this action,
		// so the row's SideEffectStatus stays at its zero (nil) value.
		at := newTestActionType("createEmployeeNoEffects", []ParameterDef{
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
		// at.SideEffects intentionally left as zero-value (nil).
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "createEmployeeNoEffects",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if result.ActionLogID == 0 {
			t.Fatalf("action_log was not persisted (id=0); zero-side-effects path needs a real action log row to assert against")
		}
		al := repo.actionLogByID[result.ActionLogID]
		if al == nil {
			t.Fatalf("action_log id=%d missing from mock repo", result.ActionLogID)
		}
		if al.SideEffectStatus != nil {
			t.Errorf("SideEffectStatus = %s, want nil (no side effects, no UPDATE)", string(al.SideEffectStatus))
		}
	})

	t.Run("webhook persistent failure persists status=failed outcome", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		// MaxRetries=1 keeps total attempts to 2 — test stays fast.
		at := actionTypeWithWebhook(t, "wh-fail", srv.URL, 1, 1)
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "wh-fail",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v (action commits even when side effect fails)", err)
		}
		assertActionLogSideEffectStatus(t, repo, result, []outcomeAssertion{
			{Type: "webhook", Status: SideEffectStatusFailed, Attempts: 2, ErrorContains: "gave up"},
		})
	})
}

// actionTypeWithWebhook builds an oms.ActionType with one createObject
// rule and a single webhook side effect targeting url with the given
// retry config.
func actionTypeWithWebhook(t *testing.T, apiName, url string, maxRetries, backoffMs int) oms.ActionType {
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
	cfgJSON, err := json.Marshal(webhookConfig{
		URL: url, MaxRetries: maxRetries, RetryBackoffMilliseconds: backoffMs,
	})
	if err != nil {
		t.Fatalf("marshal webhookConfig: %v", err)
	}
	effects, err := json.Marshal([]SideEffect{{Type: "webhook", Config: cfgJSON}})
	if err != nil {
		t.Fatalf("marshal effects: %v", err)
	}
	at.SideEffects = effects
	return at
}

type outcomeAssertion struct {
	Type          string
	Status        string
	Attempts      int
	ErrorEmpty    bool
	ErrorContains string
}

func assertActionLogSideEffectStatus(t *testing.T, repo *mockOmsRepo, result *ApplyResult, want []outcomeAssertion) {
	t.Helper()
	al := repo.actionLogByID[result.ActionLogID]
	if al == nil {
		t.Fatalf("action_log id=%d missing from mock repo", result.ActionLogID)
	}
	if len(al.SideEffectStatus) == 0 {
		t.Fatalf("SideEffectStatus empty; want JSON outcome array on action_log %d", result.ActionLogID)
	}
	var got []SideEffectOutcome
	if err := json.Unmarshal(al.SideEffectStatus, &got); err != nil {
		t.Fatalf("unmarshal side_effect_status: %v; raw=%s", err, string(al.SideEffectStatus))
	}
	if len(got) != len(want) {
		t.Fatalf("len(outcomes) = %d, want %d; raw=%s", len(got), len(want), string(al.SideEffectStatus))
	}
	for i, w := range want {
		if got[i].Type != w.Type {
			t.Errorf("outcomes[%d].type = %q, want %q", i, got[i].Type, w.Type)
		}
		if got[i].Status != w.Status {
			t.Errorf("outcomes[%d].status = %q, want %q", i, got[i].Status, w.Status)
		}
		if got[i].Attempts != w.Attempts {
			t.Errorf("outcomes[%d].attempts = %d, want %d", i, got[i].Attempts, w.Attempts)
		}
		if w.ErrorEmpty && got[i].Error != "" {
			t.Errorf("outcomes[%d].error = %q, want empty", i, got[i].Error)
		}
		if w.ErrorContains != "" && !contains_(got[i].Error, w.ErrorContains) {
			t.Errorf("outcomes[%d].error = %q, want it to mention %q", i, got[i].Error, w.ErrorContains)
		}
	}
}

// contains_ is a local strings.Contains shim so this file doesn't have
// to import strings just for one call. Trailing underscore avoids any
// conflict with the existing `contains` helper in this package.
func contains_(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
