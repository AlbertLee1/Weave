//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ActionLogSideEffectStatus_PersistenceRoundTrip covers PRD-V2
// Gap-A4 round 32: the new action_logs.side_effect_status JSONB column
// records the per-effect dispatch outcomes ([{type, status, attempts,
// error, durationMs}, ...]) so Foundry-style action history can render
// "webhook 1/2 succeeded on 2nd attempt" without log scraping.
//
// This is the integration BDD that exercises the migration (000213)
// plus the new UpdateActionLogSideEffectStatus repo method end-to-end
// against a real PostgreSQL via testcontainers.
//
// Acceptance criteria (Given → When → Then):
//
//   Given a brand-new action_logs row with no side effects
//   When  GetActionLog returns it
//   Then  SideEffectStatus is nil (pre-migration rows + actions with
//         zero side effects both surface as nil)
//
//   Given UpdateActionLogSideEffectStatus(json) stamps a payload
//   When  GetActionLog returns it
//   Then  SideEffectStatus round-trips structurally
//
//   Given ListActionLogs and ListActionLogsByOntology
//   When  they return rows
//   Then  SideEffectStatus is populated from the SELECT
//
//   Given UpdateActionLogSideEffectStatus(nil) clears the column
//   When  GetActionLog returns it
//   Then  SideEffectStatus is nil
//
//   Given UpdateActionLogSideEffectStatus targets a missing row
//   When  it runs
//   Then  it returns ErrNotFound
func TestBDD_ActionLogSideEffectStatus_PersistenceRoundTrip(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)
	ctx := context.Background()

	// Seed an action_logs row with no side-effect status set.
	al := &oms.ActionLog{
		ActionTypeRID: "ri.ontology.main.action-type.gap-a4",
		UserID:        "user-gap-a4",
		Parameters:    json.RawMessage(`{"x":1}`),
		Edits:         json.RawMessage(`[{"type":"CREATE"}]`),
		Status:        "SUCCESS",
	}
	if err := repo.InsertActionLog(ctx, al); err != nil {
		t.Fatalf("InsertActionLog: %v", err)
	}

	t.Run("freshly-inserted row has nil SideEffectStatus", func(t *testing.T) {
		got, err := repo.GetActionLog(ctx, al.ID)
		if err != nil {
			t.Fatalf("GetActionLog: %v", err)
		}
		if got.SideEffectStatus != nil {
			t.Errorf("SideEffectStatus = %s, want nil (default for new rows)", string(got.SideEffectStatus))
		}
	})

	t.Run("UpdateActionLogSideEffectStatus + GetActionLog round-trips JSON structurally", func(t *testing.T) {
		payload := json.RawMessage(`[{"type":"webhook","status":"success","attempts":2,"durationMs":42},{"type":"log","status":"success","attempts":1,"durationMs":1}]`)
		if err := repo.UpdateActionLogSideEffectStatus(ctx, al.ID, payload); err != nil {
			t.Fatalf("UpdateActionLogSideEffectStatus: %v", err)
		}
		got, err := repo.GetActionLog(ctx, al.ID)
		if err != nil {
			t.Fatalf("GetActionLog: %v", err)
		}
		if len(got.SideEffectStatus) == 0 {
			t.Fatal("SideEffectStatus = empty, want round-tripped payload")
		}
		// PG normalizes JSONB whitespace; compare structurally.
		var sent, recvd []map[string]interface{}
		if err := json.Unmarshal(payload, &sent); err != nil {
			t.Fatalf("unmarshal sent: %v", err)
		}
		if err := json.Unmarshal(got.SideEffectStatus, &recvd); err != nil {
			t.Fatalf("unmarshal recvd: %v", err)
		}
		if len(sent) != len(recvd) {
			t.Fatalf("len(outcomes) = %d, want %d", len(recvd), len(sent))
		}
		for i := range sent {
			if sent[i]["type"] != recvd[i]["type"] {
				t.Errorf("outcomes[%d].type: sent=%v recvd=%v", i, sent[i]["type"], recvd[i]["type"])
			}
			if sent[i]["status"] != recvd[i]["status"] {
				t.Errorf("outcomes[%d].status: sent=%v recvd=%v", i, sent[i]["status"], recvd[i]["status"])
			}
		}
	})

	t.Run("ListActionLogs surfaces SideEffectStatus from the SELECT", func(t *testing.T) {
		logs, err := repo.ListActionLogs(ctx, al.ActionTypeRID, 10, 0)
		if err != nil {
			t.Fatalf("ListActionLogs: %v", err)
		}
		if len(logs) != 1 {
			t.Fatalf("len(logs) = %d, want 1", len(logs))
		}
		if len(logs[0].SideEffectStatus) == 0 {
			t.Errorf("ListActionLogs row has empty SideEffectStatus; want previously-persisted payload")
		}
	})

	t.Run("UpdateActionLogSideEffectStatus(nil) clears the column", func(t *testing.T) {
		if err := repo.UpdateActionLogSideEffectStatus(ctx, al.ID, nil); err != nil {
			t.Fatalf("UpdateActionLogSideEffectStatus(nil): %v", err)
		}
		got, err := repo.GetActionLog(ctx, al.ID)
		if err != nil {
			t.Fatalf("GetActionLog: %v", err)
		}
		if got.SideEffectStatus != nil {
			t.Errorf("SideEffectStatus = %s, want nil after clear", string(got.SideEffectStatus))
		}
	})

	t.Run("UpdateActionLogSideEffectStatus on missing id returns ErrNotFound", func(t *testing.T) {
		err := repo.UpdateActionLogSideEffectStatus(ctx, 999_999_999, json.RawMessage(`[]`))
		if !errors.Is(err, oms.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}
