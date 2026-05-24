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

// TestBDD_SideEffectDLQReplay_RepoMethods covers the PG-side
// behaviour of the round-35 repo methods that back the admin replay
// endpoint: GetSideEffectDLQRow + UpdateSideEffectDLQAfterReplay.
//
// Acceptance criteria (Given → When → Then):
//
//   Given a row exists
//   When  GetSideEffectDLQRow runs
//   Then  it returns the row with all fields populated
//
//   Given a missing id
//   When  GetSideEffectDLQRow runs
//   Then  it returns ErrNotFound
//
//   Given a pending row + UpdateSideEffectDLQAfterReplay(success=true)
//   When  GetSideEffectDLQRow runs
//   Then  replay_status='replayed', replay_count=1, replayed_at set,
//         outcome updated to the new payload
//
//   Given a pending row + UpdateSideEffectDLQAfterReplay(success=false)
//   When  GetSideEffectDLQRow runs
//   Then  replay_status STAYS 'pending', replay_count=1, replayed_at
//         set, outcome updated
//
//   Given a row already in 'replayed' status
//   When  UpdateSideEffectDLQAfterReplay runs
//   Then  it returns ErrInvalidState
//
//   Given a missing id
//   When  UpdateSideEffectDLQAfterReplay runs
//   Then  it returns ErrNotFound
func TestBDD_SideEffectDLQReplay_RepoMethods(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)
	ctx := context.Background()

	al := &oms.ActionLog{
		ActionTypeRID: "ri.ontology.main.action-type.gap-a4-r35",
		UserID:        "user-r35",
		Parameters:    json.RawMessage(`{}`),
		Edits:         json.RawMessage(`[]`),
		Status:        "SUCCESS",
	}
	if err := repo.InsertActionLog(ctx, al); err != nil {
		t.Fatalf("seed InsertActionLog: %v", err)
	}

	mkRow := func(idx int, status string) *oms.SideEffectDLQRow {
		t.Helper()
		row := &oms.SideEffectDLQRow{
			ActionLogID:  al.ID,
			EffectIndex:  idx,
			EffectType:   "webhook",
			EffectConfig: json.RawMessage(`{"url":"https://example.com"}`),
			Outcome:      json.RawMessage(`{"status":"failed","attempts":4}`),
			ReplayStatus: status,
		}
		if err := repo.InsertSideEffectDLQRow(ctx, row); err != nil {
			t.Fatalf("seed InsertSideEffectDLQRow(idx=%d): %v", idx, err)
		}
		return row
	}

	t.Run("GetSideEffectDLQRow returns existing row", func(t *testing.T) {
		row := mkRow(0, oms.SideEffectDLQStatusPending)
		got, err := repo.GetSideEffectDLQRow(ctx, row.ID)
		if err != nil {
			t.Fatalf("GetSideEffectDLQRow: %v", err)
		}
		if got.ID != row.ID || got.ActionLogID != al.ID || got.EffectIndex != 0 ||
			got.EffectType != "webhook" || got.ReplayStatus != oms.SideEffectDLQStatusPending ||
			got.ReplayCount != 0 {
			t.Errorf("got = %+v, want canonical seed", got)
		}
	})

	t.Run("GetSideEffectDLQRow on missing id returns ErrNotFound", func(t *testing.T) {
		_, err := repo.GetSideEffectDLQRow(ctx, 999_999_999)
		if !errors.Is(err, oms.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("UpdateAfterReplay(success=true) flips to replayed + bumps count", func(t *testing.T) {
		row := mkRow(1, oms.SideEffectDLQStatusPending)
		newOutcome := json.RawMessage(`{"status":"success","attempts":1}`)
		if err := repo.UpdateSideEffectDLQAfterReplay(ctx, row.ID, newOutcome, true); err != nil {
			t.Fatalf("UpdateSideEffectDLQAfterReplay: %v", err)
		}
		got, err := repo.GetSideEffectDLQRow(ctx, row.ID)
		if err != nil {
			t.Fatalf("Get post-replay: %v", err)
		}
		if got.ReplayStatus != oms.SideEffectDLQStatusReplayed {
			t.Errorf("ReplayStatus = %q, want replayed", got.ReplayStatus)
		}
		if got.ReplayCount != 1 {
			t.Errorf("ReplayCount = %d, want 1", got.ReplayCount)
		}
		if got.ReplayedAt == nil || got.ReplayedAt.IsZero() {
			t.Errorf("ReplayedAt = %v, want a non-zero timestamp", got.ReplayedAt)
		}
		var outcome map[string]interface{}
		_ = json.Unmarshal(got.Outcome, &outcome)
		if outcome["status"] != "success" {
			t.Errorf("Outcome status = %v, want success", outcome["status"])
		}
	})

	t.Run("UpdateAfterReplay(success=false) keeps pending + bumps count", func(t *testing.T) {
		row := mkRow(2, oms.SideEffectDLQStatusPending)
		newOutcome := json.RawMessage(`{"status":"failed","attempts":2,"error":"still bad"}`)
		if err := repo.UpdateSideEffectDLQAfterReplay(ctx, row.ID, newOutcome, false); err != nil {
			t.Fatalf("UpdateSideEffectDLQAfterReplay: %v", err)
		}
		got, err := repo.GetSideEffectDLQRow(ctx, row.ID)
		if err != nil {
			t.Fatalf("Get post-failed-replay: %v", err)
		}
		if got.ReplayStatus != oms.SideEffectDLQStatusPending {
			t.Errorf("ReplayStatus = %q, want pending (failure stays pending)", got.ReplayStatus)
		}
		if got.ReplayCount != 1 {
			t.Errorf("ReplayCount = %d, want 1", got.ReplayCount)
		}
		var outcome map[string]interface{}
		_ = json.Unmarshal(got.Outcome, &outcome)
		if outcome["status"] != "failed" {
			t.Errorf("Outcome status = %v, want failed (updated to latest)", outcome["status"])
		}
	})

	t.Run("UpdateAfterReplay on already-replayed row returns ErrInvalidState", func(t *testing.T) {
		row := mkRow(3, oms.SideEffectDLQStatusPending)
		// Flip to replayed first.
		if err := repo.UpdateSideEffectDLQAfterReplay(ctx, row.ID, json.RawMessage(`{}`), true); err != nil {
			t.Fatalf("first UpdateAfterReplay: %v", err)
		}
		// Second replay attempt — should reject.
		err := repo.UpdateSideEffectDLQAfterReplay(ctx, row.ID, json.RawMessage(`{}`), true)
		if !errors.Is(err, oms.ErrInvalidState) {
			t.Errorf("second UpdateAfterReplay err = %v, want ErrInvalidState", err)
		}
	})

	t.Run("UpdateAfterReplay on missing id returns ErrNotFound", func(t *testing.T) {
		err := repo.UpdateSideEffectDLQAfterReplay(ctx, 999_999_999, json.RawMessage(`{}`), true)
		if !errors.Is(err, oms.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}
