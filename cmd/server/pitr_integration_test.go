package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/cliclient"
	"github.com/liyang/weave/pkg/oms"
)

// combinedDatasetRouter mounts both the history GET handler and the
// rollback POST handler on one chi router so the integration test can
// drive the canonical CLI workflow (list history → pick a target → roll
// back) in a single httptest.Server.
func newCombinedDatasetRouter(
	historyH *datasetHistoryHandler,
	rollbackR http.Handler,
) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v2/datasets/{rid}/history", historyH.History)
	r.Mount("/", rollbackR)
	return r
}

// TestPITR_ThreeWritesRestoreToSecondDropsThirdWrite is the US-390 AC
// chokepoint: 3 writes (t1, t2, t3) → restore to tx-2 (the second tx) →
// the third write must disappear from the live index, while everything up
// to t2 is preserved. The test drives the real dataset_rollback_handler
// through the cliclient so the CLI HTTP wire shape is also exercised.
//
// Layout:
//
//	t1: pk=A {price:100}                      (state at tx-1)
//	t2: pk=A {price:200}, pk=B {price:50}     (state at tx-2 — the restore target)
//	t3: pk=A {price:300}, pk=C {price:9}      (state at tx-3 — must disappear)
//
// Expectations after `weave pitr restore --to-tx=tx-2`:
//   - tx-3 is marked rolled-back (RolledBackToTxID = tx-2).
//   - pk=A is re-indexed to {price:200} — its state at t2.
//   - pk=B is re-indexed to {price:50}  — its state at t2.
//   - pk=C is deleted from the live index — it did not exist at t2.
//   - A bookkeeping rollback tx is recorded with parent_tx_id = tx-2.
func TestPITR_ThreeWritesRestoreToSecondDropsThirdWrite(t *testing.T) {
	store := newFakeDatasetTxWriter()

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	store.seed(&oms.DatasetTransaction{TxID: "tx-1", OntologyAPIName: "shop", CommittedAt: t1, EditsCount: 1})
	store.seed(&oms.DatasetTransaction{TxID: "tx-2", ParentTxID: "tx-1", OntologyAPIName: "shop", CommittedAt: t2, EditsCount: 2})
	store.seed(&oms.DatasetTransaction{TxID: "tx-3", ParentTxID: "tx-2", OntologyAPIName: "shop", CommittedAt: t3, EditsCount: 2})

	productOT := "ri.objecttype.main.objecttype.product"
	repo := &rollbackOntologyResolver{
		byOntologyInput: map[string]*oms.Ontology{
			"shop": {RID: "ri.ontology.main.ontology.shop", APIName: "shop"},
		},
		byObjectTypeRID: map[string]*oms.ObjectType{
			productOT: {RID: productOT, APIName: "Product"},
		},
	}

	// Affected keys after t2: pk=A (modified at t3) + pk=C (created at t3).
	// pk=B was last touched at t2, so it is NOT affected and the replay
	// leaves it alone.
	affected := &fakeAffectedStore{
		byOntologyAfter: map[string][]oms.AffectedKey{
			"ri.ontology.main.ontology.shop": {
				{ObjectTypeRID: productOT, PrimaryKey: "A"},
				{ObjectTypeRID: productOT, PrimaryKey: "C"},
			},
		},
	}

	// Snapshot at t2 (the restore target): pk=A had {price:200}; pk=B had
	// {price:50}; pk=C did NOT exist yet.
	history := &fakeHistorySnapshot{
		byObjectType: map[string][]oms.LatestObjectState{
			productOT: {
				{PrimaryKey: "A", NewState: json.RawMessage(`{"price":200}`)},
				{PrimaryKey: "B", NewState: json.RawMessage(`{"price":50}`)},
			},
		},
	}

	idx := newRecordingIndex()

	srv := httptest.NewServer(newDatasetRollbackRouter(repo, store, affected, history, idx))
	t.Cleanup(srv.Close)

	c := cliclient.NewClient(srv.URL, "tok")
	resp, err := c.RollbackDataset(context.Background(), "shop", "tx-2")
	if err != nil {
		t.Fatalf("RollbackDataset: %v", err)
	}

	// pk=A is restored from snapshot, pk=C is deleted from live index.
	if resp.RestoredObjects != 1 {
		t.Errorf("RestoredObjects = %d, want 1 (pk=A re-indexed to t2 state)", resp.RestoredObjects)
	}
	if resp.DeletedObjects != 1 {
		t.Errorf("DeletedObjects = %d, want 1 (pk=C purged)", resp.DeletedObjects)
	}

	// Only tx-3 is newer than t2 → exactly one entry in the rolled-back
	// list. tx-2 is the target itself and stays canonical.
	if len(resp.RolledBackTxIDs) != 1 || resp.RolledBackTxIDs[0] != "tx-3" {
		t.Errorf("RolledBackTxIDs = %v, want [tx-3]", resp.RolledBackTxIDs)
	}
	if call, ok := store.markCalls["tx-3"]; !ok {
		t.Errorf("tx-3 was not marked rolled-back")
	} else if call.rolledBackToTxID != "tx-2" {
		t.Errorf("tx-3 RolledBackToTxID = %q, want tx-2", call.rolledBackToTxID)
	}
	if _, ok := store.markCalls["tx-2"]; ok {
		t.Errorf("tx-2 should NOT be rolled-back; it is the restore target")
	}
	if _, ok := store.markCalls["tx-1"]; ok {
		t.Errorf("tx-1 should NOT be rolled-back; it pre-dates the target")
	}

	// New chain head: parent_tx_id = tx-2 (the restore target).
	if resp.NewTransaction == nil {
		t.Fatal("expected bookkeeping transaction in response")
	}
	if resp.NewTransaction.ParentTxID != "tx-2" {
		t.Errorf("bookkeeping ParentTxID = %q, want tx-2", resp.NewTransaction.ParentTxID)
	}
	if resp.NewTransaction.RolledBackToTxID != "tx-2" {
		t.Errorf("bookkeeping RolledBackToTxID = %q, want tx-2", resp.NewTransaction.RolledBackToTxID)
	}

	// Index assertions: pk=A is re-indexed to {price:200}; pk=C is deleted.
	scoped := "shop__Product"
	if doc, ok := idx.indexed[scoped]["A"]; !ok {
		t.Errorf("pk=A was not re-indexed")
	} else if v, _ := doc["price"].(float64); v != 200 {
		t.Errorf("pk=A re-indexed price = %v, want 200 (t2 state)", doc["price"])
	}
	if !idx.deleted[scoped]["C"] {
		t.Errorf("pk=C should be deleted from live index (third write disappears)")
	}
	// pk=B was not in the affected set so the replay does not touch it —
	// the live index is left as-is, which is the correct behaviour because
	// pk=B's last write IS the t2 state.
	if _, indexed := idx.indexed[scoped]["B"]; indexed {
		t.Errorf("pk=B was not affected and should not be re-indexed")
	}
	if idx.deleted[scoped]["B"] {
		t.Errorf("pk=B was not affected and should not be deleted")
	}

	// Echo back the target tx for client convenience.
	if resp.TargetTx == nil || resp.TargetTx.TxID != "tx-2" {
		t.Errorf("TargetTx echo = %+v, want tx-2", resp.TargetTx)
	}
}

// TestPITR_HistoryThenRestoreFlow exercises the canonical CLI workflow:
// (1) GET /history to discover transaction ids, (2) POST /rollback?to=...
// against one of them. This reproduces what an operator would do
// interactively.
func TestPITR_HistoryThenRestoreFlow(t *testing.T) {
	store := newFakeDatasetTxWriter()
	t1 := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	store.seed(&oms.DatasetTransaction{TxID: "tx-a", OntologyAPIName: "shop", CommittedAt: t1, EditsCount: 1})
	store.seed(&oms.DatasetTransaction{TxID: "tx-b", ParentTxID: "tx-a", OntologyAPIName: "shop", CommittedAt: t2, EditsCount: 1})

	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"shop": {RID: "ri.ontology.main.ontology.shop", APIName: "shop"},
	}}

	historyH := newDatasetHistoryHandler(repo, store)
	rollbackR := newDatasetRollbackRouter(repo, store, nil, nil, nil)

	combined := newCombinedDatasetRouter(historyH, rollbackR)
	srv := httptest.NewServer(combined)
	t.Cleanup(srv.Close)

	c := cliclient.NewClient(srv.URL, "tok")

	// Step 1: list history.
	hist, err := c.DatasetHistory(context.Background(), "shop")
	if err != nil {
		t.Fatalf("DatasetHistory: %v", err)
	}
	if len(hist.Transactions) != 2 {
		t.Fatalf("history len = %d, want 2", len(hist.Transactions))
	}
	// Ordering is newest-first per ListByOntology contract.
	target := hist.Transactions[1].TxID // older tx — the rollback target.
	if target != "tx-a" {
		t.Fatalf("oldest tx = %q, want tx-a", target)
	}

	// Step 2: roll back to it.
	resp, err := c.RollbackDataset(context.Background(), "shop", target)
	if err != nil {
		t.Fatalf("RollbackDataset: %v", err)
	}
	if len(resp.RolledBackTxIDs) != 1 || resp.RolledBackTxIDs[0] != "tx-b" {
		t.Errorf("RolledBackTxIDs = %v, want [tx-b]", resp.RolledBackTxIDs)
	}
}
