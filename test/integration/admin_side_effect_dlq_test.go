//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_AdminSideEffectDLQRepoMethods covers the PG-side acceptance
// criteria for the Gap-A4 round-34 admin endpoints: the two new
// repo methods ListPendingSideEffectDLQRows + MarkSideEffectDLQ
// Abandoned must behave correctly against real PostgreSQL.
//
// Acceptance criteria (Given → When → Then):
//
//   Given two pending DLQ rows + one abandoned + one replayed
//   When  ListPendingSideEffectDLQRows runs
//   Then  it returns ONLY the pending rows (filters by replay_status)
//
//   Given pending rows inserted with different created_at
//   When  ListPendingSideEffectDLQRows runs
//   Then  ordering is newest-first (DESC by created_at)
//
//   Given ListPendingSideEffectDLQRows(limit=2) with 3 pending rows
//   When  it runs
//   Then  it returns exactly 2 rows
//
//   Given a pending row
//   When  MarkSideEffectDLQAbandoned runs
//   Then  the row's replay_status flips to 'abandoned'
//
//   Given an already-abandoned row
//   When  MarkSideEffectDLQAbandoned runs
//   Then  it returns nil (idempotent on rows already in abandoned)
//
//   Given a row in 'replayed' status
//   When  MarkSideEffectDLQAbandoned runs
//   Then  it returns ErrInvalidState (can't abandon a successful
//         replay — would mask the dispatch)
//
//   Given a missing id
//   When  MarkSideEffectDLQAbandoned runs
//   Then  it returns ErrNotFound
func TestBDD_AdminSideEffectDLQRepoMethods(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)
	ctx := context.Background()

	// Seed: action_log row to satisfy the FK
	al := &oms.ActionLog{
		ActionTypeRID: "ri.ontology.main.action-type.gap-a4-r34",
		UserID:        "user-r34",
		Parameters:    json.RawMessage(`{}`),
		Edits:         json.RawMessage(`[]`),
		Status:        "SUCCESS",
	}
	if err := repo.InsertActionLog(ctx, al); err != nil {
		t.Fatalf("seed InsertActionLog: %v", err)
	}

	// Helper: insert a DLQ row with a specific status and effect_index.
	mkRow := func(idx int, status string) *oms.SideEffectDLQRow {
		t.Helper()
		row := &oms.SideEffectDLQRow{
			ActionLogID:  al.ID,
			EffectIndex:  idx,
			EffectType:   "webhook",
			EffectConfig: json.RawMessage(`{}`),
			Outcome:      json.RawMessage(`{"status":"failed"}`),
			ReplayStatus: status,
		}
		if err := repo.InsertSideEffectDLQRow(ctx, row); err != nil {
			t.Fatalf("seed InsertSideEffectDLQRow(idx=%d, status=%q): %v", idx, status, err)
		}
		return row
	}

	pendingA := mkRow(0, oms.SideEffectDLQStatusPending)
	pendingB := mkRow(1, oms.SideEffectDLQStatusPending)
	abandoned := mkRow(2, oms.SideEffectDLQStatusAbandoned)
	// Insert a row then UPDATE to 'replayed' status — we don't have a
	// MarkReplayed method yet (round 35), so use raw SQL through the
	// pool to simulate the replayed state for this test.
	replayedRow := mkRow(3, oms.SideEffectDLQStatusPending)
	if _, err := pg.Pool.Exec(ctx,
		`UPDATE action_log_side_effect_dlq SET replay_status = $1 WHERE id = $2`,
		oms.SideEffectDLQStatusReplayed, replayedRow.ID); err != nil {
		t.Fatalf("seed replayed state: %v", err)
	}

	t.Run("ListPendingSideEffectDLQRows filters by replay_status='pending'", func(t *testing.T) {
		got, err := repo.ListPendingSideEffectDLQRows(ctx, 100)
		if err != nil {
			t.Fatalf("ListPendingSideEffectDLQRows: %v", err)
		}
		// Pending count = pendingA + pendingB = 2. abandoned + replayed excluded.
		pendingIDs := map[int64]bool{pendingA.ID: false, pendingB.ID: false}
		excludedIDs := map[int64]bool{abandoned.ID: false, replayedRow.ID: false}
		for _, r := range got {
			if _, ok := pendingIDs[r.ID]; ok {
				pendingIDs[r.ID] = true
			}
			if _, ok := excludedIDs[r.ID]; ok {
				t.Errorf("row id=%d (status %q) MUST NOT appear in pending list", r.ID, r.ReplayStatus)
			}
		}
		for id, seen := range pendingIDs {
			if !seen {
				t.Errorf("pending row id=%d MUST appear in list, got missing", id)
			}
		}
	})

	t.Run("ListPendingSideEffectDLQRows orders newest-first (DESC by created_at)", func(t *testing.T) {
		// Sleep then insert a fresh pending row to guarantee a strictly-
		// newer created_at than pendingA/B.
		time.Sleep(50 * time.Millisecond)
		newest := mkRow(4, oms.SideEffectDLQStatusPending)

		got, err := repo.ListPendingSideEffectDLQRows(ctx, 100)
		if err != nil {
			t.Fatalf("ListPendingSideEffectDLQRows: %v", err)
		}
		if len(got) < 1 {
			t.Fatalf("len(rows) = %d, want >= 1", len(got))
		}
		if got[0].ID != newest.ID {
			t.Errorf("got[0].ID = %d, want %d (newest first)", got[0].ID, newest.ID)
		}
	})

	t.Run("ListPendingSideEffectDLQRows honors the limit", func(t *testing.T) {
		got, err := repo.ListPendingSideEffectDLQRows(ctx, 1)
		if err != nil {
			t.Fatalf("ListPendingSideEffectDLQRows limit=1: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("limit=1 returned %d rows, want 1", len(got))
		}
	})

	t.Run("MarkSideEffectDLQAbandoned flips pending → abandoned", func(t *testing.T) {
		if err := repo.MarkSideEffectDLQAbandoned(ctx, pendingA.ID); err != nil {
			t.Fatalf("MarkSideEffectDLQAbandoned: %v", err)
		}
		// Confirm by reading the row back via direct SQL (no GetSideEffectDLQRow yet).
		var status string
		if err := pg.Pool.QueryRow(ctx,
			`SELECT replay_status FROM action_log_side_effect_dlq WHERE id = $1`,
			pendingA.ID).Scan(&status); err != nil {
			t.Fatalf("post-abandon select: %v", err)
		}
		if status != oms.SideEffectDLQStatusAbandoned {
			t.Errorf("post-abandon status = %q, want %q", status, oms.SideEffectDLQStatusAbandoned)
		}
	})

	t.Run("MarkSideEffectDLQAbandoned is idempotent on already-abandoned rows", func(t *testing.T) {
		if err := repo.MarkSideEffectDLQAbandoned(ctx, pendingA.ID); err != nil {
			t.Errorf("second abandon: err = %v, want nil (idempotent)", err)
		}
	})

	t.Run("MarkSideEffectDLQAbandoned on replayed row returns ErrInvalidState", func(t *testing.T) {
		err := repo.MarkSideEffectDLQAbandoned(ctx, replayedRow.ID)
		if !errors.Is(err, oms.ErrInvalidState) {
			t.Errorf("err = %v, want ErrInvalidState", err)
		}
	})

	t.Run("MarkSideEffectDLQAbandoned on missing id returns ErrNotFound", func(t *testing.T) {
		err := repo.MarkSideEffectDLQAbandoned(ctx, 999_999_999)
		if !errors.Is(err, oms.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}
