package audit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRedactActorID_StableHash(t *testing.T) {
	a := RedactActorID("user:alice@example.com")
	b := RedactActorID("user:alice@example.com")
	if a != b {
		t.Fatalf("expected identical hash, got %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, RedactedActorPrefix) {
		t.Fatalf("expected redacted prefix, got %q", a)
	}
	if a == RedactActorID("user:bob@example.com") {
		t.Fatal("distinct inputs collided to same redacted value")
	}
}

func TestRedactActorID_EmptyInput(t *testing.T) {
	got := RedactActorID("")
	if got != RedactedActorPrefix+"anonymous" {
		t.Fatalf("expected sentinel for empty input, got %q", got)
	}
}

func TestApplyRedaction_ZeroesPIIPreservesChain(t *testing.T) {
	evt := &AuditEvent{
		ID:           "evt-1",
		ActorID:      "user:alice@example.com",
		Action:       "object.read",
		ResourceType: "object_instance",
		IP:           "10.0.0.1",
		UserAgent:    "Mozilla/5.0",
		DiffJSON:     json.RawMessage(`{"name":"Alice"}`),
		ChainSeq:     42,
		PrevHash:     "deadbeef",
		EntryHash:    "feedface",
		Timestamp:    time.Now(),
	}

	ApplyRedaction(evt)

	if !strings.HasPrefix(evt.ActorID, RedactedActorPrefix) {
		t.Errorf("ActorID not redacted: %q", evt.ActorID)
	}
	if evt.IP != "" {
		t.Errorf("IP not cleared: %q", evt.IP)
	}
	if evt.UserAgent != "" {
		t.Errorf("UserAgent not cleared: %q", evt.UserAgent)
	}
	if evt.DiffJSON != nil {
		t.Errorf("DiffJSON not cleared: %s", string(evt.DiffJSON))
	}
	// Chain fields preserved so VerifyChain still succeeds against the
	// underlying ListChain output.
	if evt.ChainSeq != 42 || evt.PrevHash != "deadbeef" || evt.EntryHash != "feedface" {
		t.Errorf("chain fields mutated: seq=%d prev=%s entry=%s",
			evt.ChainSeq, evt.PrevHash, evt.EntryHash)
	}
	if evt.Action != "object.read" || evt.ResourceType != "object_instance" {
		t.Errorf("non-PII fields mutated: action=%s rt=%s",
			evt.Action, evt.ResourceType)
	}
}

func TestRedactingStore_RewritesMatchingActor(t *testing.T) {
	ctx := context.Background()
	inner := NewMemoryStore()
	for _, e := range []AuditEvent{
		{ID: "e1", ActorID: "user:alice", Action: "x", IP: "1.1.1.1", Timestamp: time.Now()},
		{ID: "e2", ActorID: "user:bob", Action: "x", IP: "2.2.2.2", Timestamp: time.Now().Add(time.Second)},
	} {
		if err := inner.Insert(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	red := NewMemoryRedactionStore()
	if err := red.Add(ctx, "user:alice", "gdpr_erase"); err != nil {
		t.Fatal(err)
	}

	store := NewRedactingStore(inner, red)
	got, err := store.List(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	for _, e := range got {
		switch e.ID {
		case "e1":
			if !strings.HasPrefix(e.ActorID, RedactedActorPrefix) {
				t.Errorf("e1 actor not redacted: %q", e.ActorID)
			}
			if e.IP != "" {
				t.Errorf("e1 IP not cleared: %q", e.IP)
			}
		case "e2":
			if e.ActorID != "user:bob" {
				t.Errorf("e2 actor unexpectedly mutated: %q", e.ActorID)
			}
			if e.IP != "2.2.2.2" {
				t.Errorf("e2 IP unexpectedly cleared: %q", e.IP)
			}
		}
	}
}

func TestRedactingStore_NilRedactionsIsPassthrough(t *testing.T) {
	ctx := context.Background()
	inner := NewMemoryStore()
	if err := inner.Insert(ctx, AuditEvent{ID: "e1", ActorID: "user:alice", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	store := NewRedactingStore(inner, nil)
	got, err := store.List(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ActorID != "user:alice" {
		t.Fatalf("expected passthrough, got %#v", got)
	}
}

func TestRedactingStore_InsertDelegated(t *testing.T) {
	ctx := context.Background()
	inner := NewMemoryStore()
	store := NewRedactingStore(inner, NewMemoryRedactionStore())
	if err := store.Insert(ctx, AuditEvent{ID: "e1", ActorID: "u1", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if len(inner.Events()) != 1 {
		t.Fatalf("expected inner to receive 1 event, got %d", len(inner.Events()))
	}
}

func TestRedactingStore_FailClosedOnLookupError(t *testing.T) {
	ctx := context.Background()
	inner := NewMemoryStore()
	if err := inner.Insert(ctx, AuditEvent{ID: "e1", ActorID: "user:carol", IP: "9.9.9.9", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	store := NewRedactingStore(inner, errRedactionStore{})
	got, err := store.List(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if !strings.HasPrefix(got[0].ActorID, RedactedActorPrefix) {
		t.Errorf("expected fail-closed redaction, got actor=%q", got[0].ActorID)
	}
	if got[0].IP != "" {
		t.Errorf("expected fail-closed IP scrub, got %q", got[0].IP)
	}
}

type errRedactionStore struct{}

func (errRedactionStore) Add(context.Context, string, string) error { return nil }
func (errRedactionStore) Has(context.Context, string) (bool, error) { return false, errSimulated }

var errSimulated = errSimulatedT{}

type errSimulatedT struct{}

func (errSimulatedT) Error() string { return "simulated lookup error" }
