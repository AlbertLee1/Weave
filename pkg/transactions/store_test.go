package transactions_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/transactions"
)

func TestMemoryStore_AppendAndList(t *testing.T) {
	ctx := context.Background()
	store := transactions.NewMemoryStore()

	key := transactions.Key{Ontology: "onto", TransactionID: "tx-1"}
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "User", PrimaryKey: "u1"},
		{Type: funnel.EditTypeModify, ObjectType: "User", PrimaryKey: "u1", Properties: map[string]interface{}{"name": "alice"}},
	}
	if err := store.AppendEdits(ctx, key, edits); err != nil {
		t.Fatalf("AppendEdits: %v", err)
	}

	got, err := store.ListEdits(ctx, key)
	if err != nil {
		t.Fatalf("ListEdits: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 edits, got %d", len(got))
	}
	if got[0].PrimaryKey != "u1" || got[1].Properties["name"] != "alice" {
		t.Fatalf("unexpected edits: %+v", got)
	}
}

func TestMemoryStore_AppendAccumulates(t *testing.T) {
	ctx := context.Background()
	store := transactions.NewMemoryStore()
	key := transactions.Key{Ontology: "onto", TransactionID: "tx-2"}

	if err := store.AppendEdits(ctx, key, []funnel.Edit{{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1"}}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := store.AppendEdits(ctx, key, []funnel.Edit{{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "2"}}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	got, err := store.ListEdits(ctx, key)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].PrimaryKey != "1" || got[1].PrimaryKey != "2" {
		t.Fatalf("order not preserved: %+v", got)
	}
}

func TestMemoryStore_IsolatesTransactions(t *testing.T) {
	ctx := context.Background()
	store := transactions.NewMemoryStore()
	a := transactions.Key{Ontology: "onto", TransactionID: "tx-a"}
	b := transactions.Key{Ontology: "onto", TransactionID: "tx-b"}

	if err := store.AppendEdits(ctx, a, []funnel.Edit{{Type: funnel.EditTypeCreate, ObjectType: "X", PrimaryKey: "1"}}); err != nil {
		t.Fatalf("append a: %v", err)
	}
	bEdits, err := store.ListEdits(ctx, b)
	if err != nil {
		t.Fatalf("list b: %v", err)
	}
	if len(bEdits) != 0 {
		t.Fatalf("transaction b should be empty, got %d", len(bEdits))
	}
}

func TestMemoryStore_DifferentOntologies(t *testing.T) {
	ctx := context.Background()
	store := transactions.NewMemoryStore()
	a := transactions.Key{Ontology: "onto-a", TransactionID: "tx-shared"}
	b := transactions.Key{Ontology: "onto-b", TransactionID: "tx-shared"}

	if err := store.AppendEdits(ctx, a, []funnel.Edit{{Type: funnel.EditTypeCreate, ObjectType: "X", PrimaryKey: "1"}}); err != nil {
		t.Fatalf("append a: %v", err)
	}
	bEdits, err := store.ListEdits(ctx, b)
	if err != nil {
		t.Fatalf("list b: %v", err)
	}
	if len(bEdits) != 0 {
		t.Fatalf("cross-ontology leak: got %d", len(bEdits))
	}
}
