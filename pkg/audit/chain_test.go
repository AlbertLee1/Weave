package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fixedEvent builds a deterministic AuditEvent for hash-stability tests.
// The ID and timestamp are explicit because the chain hash is sensitive to them.
func fixedEvent(id, actor, action string, ts time.Time, diff string) AuditEvent {
	evt := AuditEvent{
		ID:           id,
		ActorID:      actor,
		Action:       action,
		ResourceType: "ObjectType",
		ResourceRID:  "ri.ontology.main.objectType.emp",
		IP:           "10.0.0.1",
		UserAgent:    "test",
		Timestamp:    ts,
	}
	if diff != "" {
		evt.DiffJSON = json.RawMessage(diff)
	}
	return evt
}

func TestHashEvent_DeterministicAcrossKeyOrder(t *testing.T) {
	ts := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	// Two JSON-equal diffs with different key orders must produce the
	// same canonical hash — otherwise PG's JSONB whitespace / key-order
	// normalisation breaks the chain on read-back.
	a := fixedEvent("id-1", "user-1", "CREATE", ts, `{"b":2,"a":1,"nested":{"y":"y","x":"x"}}`)
	b := fixedEvent("id-1", "user-1", "CREATE", ts, `{"a":1,"b":2,"nested":{"x":"x","y":"y"}}`)

	ha, err := HashEvent("prev", a)
	if err != nil {
		t.Fatalf("HashEvent(a) error = %v", err)
	}
	hb, err := HashEvent("prev", b)
	if err != nil {
		t.Fatalf("HashEvent(b) error = %v", err)
	}
	if ha != hb {
		t.Fatalf("hash should be stable across key order: %s vs %s", ha, hb)
	}
	if !strings.HasPrefix(ha, "") || len(ha) != 64 {
		t.Fatalf("expected 64-char hex hash, got %q", ha)
	}
}

func TestHashEvent_ChangesOnEveryField(t *testing.T) {
	ts := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	base := fixedEvent("id-1", "user-1", "CREATE", ts, `{"x":1}`)
	baseHash, err := HashEvent("prev", base)
	if err != nil {
		t.Fatalf("HashEvent(base) error = %v", err)
	}

	cases := []struct {
		name string
		mut  func(e *AuditEvent)
	}{
		{"id", func(e *AuditEvent) { e.ID = "id-2" }},
		{"actor", func(e *AuditEvent) { e.ActorID = "user-2" }},
		{"action", func(e *AuditEvent) { e.Action = "UPDATE" }},
		{"resource_type", func(e *AuditEvent) { e.ResourceType = "Property" }},
		{"resource_rid", func(e *AuditEvent) { e.ResourceRID = "ri.other" }},
		{"diff", func(e *AuditEvent) { e.DiffJSON = json.RawMessage(`{"x":2}`) }},
		{"ip", func(e *AuditEvent) { e.IP = "10.0.0.2" }},
		{"user_agent", func(e *AuditEvent) { e.UserAgent = "other" }},
		{"timestamp", func(e *AuditEvent) { e.Timestamp = ts.Add(time.Second) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloned := base
			tc.mut(&cloned)
			h, err := HashEvent("prev", cloned)
			if err != nil {
				t.Fatalf("HashEvent error = %v", err)
			}
			if h == baseHash {
				t.Fatalf("mutating %s produced identical hash %s", tc.name, h)
			}
		})
	}
}

func TestHashEvent_ChangesOnPrevHash(t *testing.T) {
	ts := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	evt := fixedEvent("id-1", "user-1", "CREATE", ts, "")
	h1, _ := HashEvent("prev-a", evt)
	h2, _ := HashEvent("prev-b", evt)
	if h1 == h2 {
		t.Fatalf("different prev-hash should produce different entry hash: %s", h1)
	}
}

func TestHashEvent_NormalisesTimestampToUTC(t *testing.T) {
	// Same instant in UTC and a different zone should produce the same hash.
	utc := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	loc, _ := time.LoadLocation("America/Los_Angeles")
	local := utc.In(loc)

	a := fixedEvent("id-1", "user-1", "CREATE", utc, "")
	b := fixedEvent("id-1", "user-1", "CREATE", local, "")

	ha, _ := HashEvent("prev", a)
	hb, _ := HashEvent("prev", b)
	if ha != hb {
		t.Fatalf("timezone should not affect hash: %s vs %s", ha, hb)
	}
}

func TestHashEvent_RejectsInvalidDiffJSON(t *testing.T) {
	ts := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	evt := fixedEvent("id-1", "user-1", "CREATE", ts, `{not: "json"`)
	_, err := HashEvent("", evt)
	if err == nil {
		t.Fatal("expected error for malformed diff_json, got nil")
	}
}

func TestMemoryStore_LinksEntriesIntoChain(t *testing.T) {
	store := NewMemoryStore()

	for i := 0; i < 3; i++ {
		if err := Record(context.Background(), store, AuditEvent{
			ActorID:      "user-1",
			Action:       "CREATE",
			ResourceType: "ObjectType",
			ResourceRID:  "ri.ontology.main.objectType.x",
		}); err != nil {
			t.Fatalf("Record error = %v", err)
		}
	}

	chain, err := store.ListChain(context.Background())
	if err != nil {
		t.Fatalf("ListChain error = %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected 3 chained events, got %d", len(chain))
	}

	// First entry has empty prev_hash; each subsequent links to the previous.
	if chain[0].PrevHash != "" {
		t.Errorf("first entry PrevHash should be empty, got %q", chain[0].PrevHash)
	}
	for i := range chain {
		if chain[i].EntryHash == "" {
			t.Errorf("entry %d EntryHash empty", i)
		}
		if i > 0 && chain[i].PrevHash != chain[i-1].EntryHash {
			t.Errorf("entry %d PrevHash = %q, want %q (previous EntryHash)",
				i, chain[i].PrevHash, chain[i-1].EntryHash)
		}
	}
	if chain[0].ChainSeq != 1 || chain[1].ChainSeq != 2 || chain[2].ChainSeq != 3 {
		t.Errorf("ChainSeq should be monotonically increasing from 1, got %d,%d,%d",
			chain[0].ChainSeq, chain[1].ChainSeq, chain[2].ChainSeq)
	}
}

func TestVerifyChain_HappyPath(t *testing.T) {
	store := NewMemoryStore()
	for i := 0; i < 5; i++ {
		if err := Record(context.Background(), store, AuditEvent{
			ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		}); err != nil {
			t.Fatalf("Record error = %v", err)
		}
	}
	chain, _ := store.ListChain(context.Background())
	if err := VerifyChain(chain); err != nil {
		t.Fatalf("VerifyChain on untouched chain: %v", err)
	}
}

func TestVerifyChain_DetectsTamperedEntry(t *testing.T) {
	store := NewMemoryStore()
	for i := 0; i < 3; i++ {
		_ = Record(context.Background(), store, AuditEvent{
			ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		})
	}
	chain, _ := store.ListChain(context.Background())

	// Tamper with the middle event's data; EntryHash still points at the
	// original content, so re-hashing produces a mismatch.
	chain[1].ActorID = "attacker"

	err := VerifyChain(chain)
	if err == nil {
		t.Fatal("expected VerifyChain to detect tampering, got nil")
	}
	var vErr *ChainVerificationError
	if !errorAs(err, &vErr) {
		t.Fatalf("expected *ChainVerificationError, got %T: %v", err, err)
	}
	if vErr.Index != 1 {
		t.Errorf("Index = %d, want 1", vErr.Index)
	}
	if !strings.Contains(strings.ToLower(vErr.Reason), "hash mismatch") {
		t.Errorf("Reason = %q, expected to mention hash mismatch", vErr.Reason)
	}
}

func TestVerifyChain_DetectsBrokenPrevPointer(t *testing.T) {
	store := NewMemoryStore()
	for i := 0; i < 3; i++ {
		_ = Record(context.Background(), store, AuditEvent{
			ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		})
	}
	chain, _ := store.ListChain(context.Background())

	// Break the linkage. Recompute entry_hash for the tampered row so the
	// self-hash check still passes — this isolates the prev-pointer check.
	chain[2].PrevHash = "deadbeef"
	newHash, err := HashEvent(chain[2].PrevHash, chain[2])
	if err != nil {
		t.Fatalf("recompute hash: %v", err)
	}
	chain[2].EntryHash = newHash

	err = VerifyChain(chain)
	if err == nil {
		t.Fatal("expected VerifyChain to detect broken linkage")
	}
	var vErr *ChainVerificationError
	if !errorAs(err, &vErr) {
		t.Fatalf("expected *ChainVerificationError, got %T", err)
	}
	if vErr.Index != 2 {
		t.Errorf("Index = %d, want 2", vErr.Index)
	}
	if !strings.Contains(strings.ToLower(vErr.Reason), "prev_hash") {
		t.Errorf("Reason = %q, expected to mention prev_hash", vErr.Reason)
	}
}

func TestVerifyChain_DetectsSequenceGap(t *testing.T) {
	store := NewMemoryStore()
	for i := 0; i < 3; i++ {
		_ = Record(context.Background(), store, AuditEvent{
			ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		})
	}
	chain, _ := store.ListChain(context.Background())

	// Drop the middle entry to simulate a gap.
	tampered := []AuditEvent{chain[0], chain[2]}

	err := VerifyChain(tampered)
	if err == nil {
		t.Fatal("expected VerifyChain to detect sequence gap")
	}
	var vErr *ChainVerificationError
	if !errorAs(err, &vErr) {
		t.Fatalf("expected *ChainVerificationError, got %T", err)
	}
	if vErr.Index != 1 {
		t.Errorf("Index = %d, want 1", vErr.Index)
	}
}

func TestVerifyChain_EmptyOK(t *testing.T) {
	if err := VerifyChain(nil); err != nil {
		t.Fatalf("empty chain should verify: %v", err)
	}
	if err := VerifyChain([]AuditEvent{}); err != nil {
		t.Fatalf("empty chain should verify: %v", err)
	}
}

func TestComputeRootHash_StableAcrossOrdering(t *testing.T) {
	store := NewMemoryStore()
	for i := 0; i < 4; i++ {
		_ = Record(context.Background(), store, AuditEvent{
			ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		})
	}
	chain, _ := store.ListChain(context.Background())

	root1 := ComputeRootHash(chain)
	if len(root1) != 64 {
		t.Fatalf("root hash should be 64-char hex, got %d", len(root1))
	}
	// Root over the same events computed twice must be identical.
	root2 := ComputeRootHash(chain)
	if root1 != root2 {
		t.Fatalf("root hash not stable: %s vs %s", root1, root2)
	}
	// Root changes when the last entry changes.
	chain[3].EntryHash = "changed"
	root3 := ComputeRootHash(chain)
	if root1 == root3 {
		t.Fatal("root hash should change when a member entry hash changes")
	}
}

func TestComputeRootHash_EmptyIsEmpty(t *testing.T) {
	if got := ComputeRootHash(nil); got != "" {
		t.Errorf("ComputeRootHash(nil) = %q, want empty string", got)
	}
}

// errorAs is a tiny local wrapper around errors.As so callers can use `any`.
func errorAs(err error, target any) bool {
	return errors.As(err, target)
}
