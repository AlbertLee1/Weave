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

// TestBDD_ActionLogSideEffectDLQ_PersistenceRoundTrip covers PRD-V2
// Gap-A4 round 33: the new action_log_side_effect_dlq table records
// failed-after-retries side-effect dispatches so an operator can
// review / replay them via the admin API (round 34).
//
// Acceptance criteria (Given → When → Then):
//
//   Given an action_logs row and an Insert with a failed outcome
//   When  ListSideEffectDLQByActionLog returns the rows
//   Then  the inserted row round-trips (action_log_id, effect_index,
//         effect_type, effect_config, outcome, replay_status='pending')
//
//   Given two effects on the same action both failed
//   When  ListSideEffectDLQByActionLog returns the rows
//   Then  rows are ordered by effect_index ascending
//
//   Given a duplicate insert on (action_log_id, effect_index)
//   When  Insert runs
//   Then  it returns ErrDuplicate (the unique constraint guards
//         against double-executor runs queueing twice)
//
//   Given an unrelated action_log_id
//   When  ListSideEffectDLQByActionLog runs
//   Then  it returns an empty (non-nil) slice
func TestBDD_ActionLogSideEffectDLQ_PersistenceRoundTrip(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)
	ctx := context.Background()

	// Seed an action_logs row to satisfy the FK.
	al := &oms.ActionLog{
		ActionTypeRID: "ri.ontology.main.action-type.gap-a4-dlq",
		UserID:        "user-dlq",
		Parameters:    json.RawMessage(`{}`),
		Edits:         json.RawMessage(`[]`),
		Status:        "SUCCESS",
	}
	if err := repo.InsertActionLog(ctx, al); err != nil {
		t.Fatalf("seed InsertActionLog: %v", err)
	}

	t.Run("Insert + List round-trips with all fields", func(t *testing.T) {
		row := &oms.SideEffectDLQRow{
			ActionLogID:  al.ID,
			EffectIndex:  0,
			EffectType:   "webhook",
			EffectConfig: json.RawMessage(`{"url":"https://example.com/hook"}`),
			Outcome:      json.RawMessage(`{"type":"webhook","status":"failed","attempts":4,"error":"gave up","durationMs":700}`),
		}
		if err := repo.InsertSideEffectDLQRow(ctx, row); err != nil {
			t.Fatalf("InsertSideEffectDLQRow: %v", err)
		}
		if row.ID == 0 {
			t.Error("ID was not back-filled by INSERT RETURNING")
		}
		if row.CreatedAt.IsZero() {
			t.Error("CreatedAt was not back-filled by INSERT RETURNING")
		}
		if row.ReplayStatus != oms.SideEffectDLQStatusPending {
			t.Errorf("ReplayStatus = %q, want pending (default)", row.ReplayStatus)
		}

		got, err := repo.ListSideEffectDLQByActionLog(ctx, al.ID)
		if err != nil {
			t.Fatalf("ListSideEffectDLQByActionLog: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(rows) = %d, want 1", len(got))
		}
		r := got[0]
		if r.ActionLogID != al.ID {
			t.Errorf("ActionLogID = %d, want %d", r.ActionLogID, al.ID)
		}
		if r.EffectIndex != 0 {
			t.Errorf("EffectIndex = %d, want 0", r.EffectIndex)
		}
		if r.EffectType != "webhook" {
			t.Errorf("EffectType = %q, want webhook", r.EffectType)
		}
		// EffectConfig + Outcome compare structurally (PG normalizes
		// JSONB whitespace).
		var cfg, recvCfg map[string]interface{}
		if err := json.Unmarshal(row.EffectConfig, &cfg); err != nil {
			t.Fatalf("unmarshal sent cfg: %v", err)
		}
		if err := json.Unmarshal(r.EffectConfig, &recvCfg); err != nil {
			t.Fatalf("unmarshal recvd cfg: %v", err)
		}
		if cfg["url"] != recvCfg["url"] {
			t.Errorf("EffectConfig URL: sent=%v recvd=%v", cfg["url"], recvCfg["url"])
		}
		var oc, recvOc map[string]interface{}
		_ = json.Unmarshal(row.Outcome, &oc)
		_ = json.Unmarshal(r.Outcome, &recvOc)
		if oc["status"] != recvOc["status"] {
			t.Errorf("Outcome status: sent=%v recvd=%v", oc["status"], recvOc["status"])
		}
	})

	t.Run("Insert multiple effects orders by effect_index ascending", func(t *testing.T) {
		// Seed a fresh action log so we don't collide with the previous
		// (action_log_id, effect_index) pair.
		al2 := &oms.ActionLog{
			ActionTypeRID: "ri.ontology.main.action-type.gap-a4-dlq",
			UserID:        "user-dlq-2",
			Parameters:    json.RawMessage(`{}`),
			Edits:         json.RawMessage(`[]`),
			Status:        "SUCCESS",
		}
		if err := repo.InsertActionLog(ctx, al2); err != nil {
			t.Fatalf("seed InsertActionLog: %v", err)
		}
		// Insert in reverse order to prove the list sort, not insert
		// order, drives the response.
		for _, idx := range []int{2, 0, 1} {
			row := &oms.SideEffectDLQRow{
				ActionLogID: al2.ID, EffectIndex: idx,
				EffectType:   "webhook",
				EffectConfig: json.RawMessage(`{}`),
				Outcome:      json.RawMessage(`{"status":"failed"}`),
			}
			if err := repo.InsertSideEffectDLQRow(ctx, row); err != nil {
				t.Fatalf("InsertSideEffectDLQRow idx=%d: %v", idx, err)
			}
		}
		got, err := repo.ListSideEffectDLQByActionLog(ctx, al2.ID)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("len(rows) = %d, want 3", len(got))
		}
		for i, want := range []int{0, 1, 2} {
			if got[i].EffectIndex != want {
				t.Errorf("rows[%d].EffectIndex = %d, want %d", i, got[i].EffectIndex, want)
			}
		}
	})

	t.Run("Duplicate (action_log_id, effect_index) insert returns ErrDuplicate", func(t *testing.T) {
		row := &oms.SideEffectDLQRow{
			ActionLogID: al.ID, EffectIndex: 0,
			EffectType:   "webhook",
			EffectConfig: json.RawMessage(`{}`),
			Outcome:      json.RawMessage(`{"status":"failed"}`),
		}
		err := repo.InsertSideEffectDLQRow(ctx, row)
		if !errors.Is(err, oms.ErrDuplicate) {
			t.Errorf("err = %v, want ErrDuplicate", err)
		}
	})

	t.Run("ListSideEffectDLQByActionLog on unrelated id returns empty slice", func(t *testing.T) {
		got, err := repo.ListSideEffectDLQByActionLog(ctx, 999_999_999)
		if err != nil {
			t.Fatalf("List unrelated: %v", err)
		}
		if got == nil {
			t.Error("want empty non-nil slice, got nil")
		}
		if len(got) != 0 {
			t.Errorf("len(rows) = %d, want 0", len(got))
		}
	})
}
