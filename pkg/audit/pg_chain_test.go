//go:build integration

package audit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

func TestPGStore_ChainIsContiguousAndVerifies(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	store := NewPGStore(pg.Pool)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		evt := AuditEvent{
			ActorID:      "user-1",
			Action:       "CREATE",
			ResourceType: "ObjectType",
			ResourceRID:  "ri.ontology.main.objectType.emp",
			DiffJSON:     json.RawMessage(`{"i":` + string(rune('0'+i)) + `}`),
		}
		if err := Record(ctx, store, evt); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	chain, err := store.ListChain(ctx)
	if err != nil {
		t.Fatalf("ListChain: %v", err)
	}
	if len(chain) != 5 {
		t.Fatalf("expected 5 chain rows, got %d", len(chain))
	}
	for i, e := range chain {
		if e.EntryHash == "" {
			t.Errorf("row %d has empty entry_hash", i)
		}
		if i == 0 && e.PrevHash != "" {
			t.Errorf("first row prev_hash = %q, want empty", e.PrevHash)
		}
		if i > 0 && e.PrevHash != chain[i-1].EntryHash {
			t.Errorf("row %d prev_hash = %q, want %q", i, e.PrevHash, chain[i-1].EntryHash)
		}
	}

	if err := VerifyChain(chain); err != nil {
		t.Fatalf("VerifyChain on untampered PG chain: %v", err)
	}
}

func TestPGStore_ListChainByDay(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	store := NewPGStore(pg.Pool)
	ctx := context.Background()

	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)

	evtYesterday := AuditEvent{
		ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		Timestamp: yesterday,
	}
	if err := Record(ctx, store, evtYesterday); err != nil {
		t.Fatalf("Record yesterday: %v", err)
	}
	if err := Record(ctx, store, AuditEvent{
		ActorID: "u", Action: "B", ResourceType: "T", ResourceRID: "ri.x",
	}); err != nil {
		t.Fatalf("Record today: %v", err)
	}

	yesterdayEvents, err := store.ListChainByDay(ctx, yesterday)
	if err != nil {
		t.Fatalf("ListChainByDay yesterday: %v", err)
	}
	if len(yesterdayEvents) != 1 {
		t.Fatalf("expected 1 event for yesterday, got %d", len(yesterdayEvents))
	}
	if yesterdayEvents[0].Action != "A" {
		t.Errorf("action = %q, want A", yesterdayEvents[0].Action)
	}

	todayEvents, err := store.ListChainByDay(ctx, now)
	if err != nil {
		t.Fatalf("ListChainByDay today: %v", err)
	}
	if len(todayEvents) != 1 || todayEvents[0].Action != "B" {
		t.Errorf("today events = %+v", todayEvents)
	}
}
