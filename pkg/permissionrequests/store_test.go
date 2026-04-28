package permissionrequests

import (
	"context"
	"errors"
	"testing"
)

const targetRID = "ri.ontology.main.object.t1"

func newRequest(id, requester, target string) *Request {
	return &Request{
		ID:          id,
		TargetRID:   target,
		RequestedBy: requester,
		Reason:      "needs access",
	}
}

func TestMemoryStore_CreateGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	req := newRequest("r1", "user:alice", targetRID)
	if err := store.Create(ctx, req); err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != StatusPending {
		t.Fatalf("default status: want PENDING, got %q", req.Status)
	}
	if req.CreatedAt.IsZero() {
		t.Fatalf("default created_at: zero")
	}

	got, err := store.Get(ctx, "r1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RequestedBy != "user:alice" {
		t.Fatalf("requestedBy mismatch: %q", got.RequestedBy)
	}
}

func TestMemoryStore_CreateDuplicateID(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.Create(ctx, newRequest("r1", "user:alice", targetRID)); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := store.Create(ctx, newRequest("r1", "user:bob", targetRID)); err == nil {
		t.Fatalf("duplicate id: expected error")
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	_ = store.Create(ctx, newRequest("r1", "user:alice", targetRID))
	_ = store.Create(ctx, newRequest("r2", "user:bob", targetRID))
	_ = store.Create(ctx, &Request{
		ID: "r3", TargetRID: "ri.ontology.main.object.t2",
		RequestedBy: "user:alice", Status: StatusApproved,
	})

	// list all
	rows, total, err := store.List(ctx, ListQuery{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("list all: want 3, got total=%d rows=%d", total, len(rows))
	}

	// filter by status
	rows, total, err = store.List(ctx, ListQuery{Status: StatusPending})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("list pending: want 2, got total=%d rows=%d", total, len(rows))
	}

	// filter by requester
	rows, total, err = store.List(ctx, ListQuery{RequestedBy: "user:alice"})
	if err != nil {
		t.Fatalf("list mine: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("list mine: want 2, got total=%d rows=%d", total, len(rows))
	}

	// filter by target rid
	rows, total, err = store.List(ctx, ListQuery{TargetRID: targetRID})
	if err != nil {
		t.Fatalf("list target: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("list target: want 2, got total=%d rows=%d", total, len(rows))
	}
}

func TestMemoryStore_ListPagination(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		_ = store.Create(ctx, newRequest(id, "user:alice", targetRID))
	}
	rows, total, err := store.List(ctx, ListQuery{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("page 0: %v", err)
	}
	if total != 5 || len(rows) != 2 {
		t.Fatalf("page 0: want total=5 len=2, got total=%d len=%d", total, len(rows))
	}
	rows, _, err = store.List(ctx, ListQuery{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("page tail: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("page tail: want 1, got %d", len(rows))
	}
	rows, _, err = store.List(ctx, ListQuery{Limit: 2, Offset: 100})
	if err != nil {
		t.Fatalf("page off-end: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("page off-end: want 0, got %d", len(rows))
	}
}

func TestMemoryStore_DecideApprove(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, newRequest("r1", "user:alice", targetRID))

	if err := store.Decide(ctx, "r1", Decision{
		Status: StatusApproved, By: "user:admin", Note: "ok",
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, _ := store.Get(ctx, "r1")
	if got.Status != StatusApproved {
		t.Fatalf("status: %q", got.Status)
	}
	if got.DecidedBy != "user:admin" {
		t.Fatalf("decidedBy: %q", got.DecidedBy)
	}
	if got.DecisionNote != "ok" {
		t.Fatalf("decisionNote: %q", got.DecisionNote)
	}
	if got.DecidedAt == nil {
		t.Fatalf("decidedAt: nil")
	}
}

func TestMemoryStore_DecideReject(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, newRequest("r1", "user:alice", targetRID))

	if err := store.Decide(ctx, "r1", Decision{
		Status: StatusRejected, By: "user:admin", Note: "denied",
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	got, _ := store.Get(ctx, "r1")
	if got.Status != StatusRejected {
		t.Fatalf("status: %q", got.Status)
	}
}

func TestMemoryStore_DecideAlreadyDecided(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, newRequest("r1", "user:alice", targetRID))
	_ = store.Decide(ctx, "r1", Decision{Status: StatusApproved, By: "user:admin"})

	if err := store.Decide(ctx, "r1", Decision{Status: StatusRejected, By: "user:admin"}); !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("re-decide: want ErrAlreadyDecided, got %v", err)
	}
}

func TestMemoryStore_DecideNotFound(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Decide(context.Background(), "missing", Decision{Status: StatusApproved}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("decide missing: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_DecideInvalidStatus(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, newRequest("r1", "user:alice", targetRID))

	if err := store.Decide(ctx, "r1", Decision{Status: "WHATEVER"}); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status: want ErrInvalidStatus, got %v", err)
	}
	if err := store.Decide(ctx, "r1", Decision{Status: StatusPending}); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("pending decision: want ErrInvalidStatus, got %v", err)
	}
}

func TestModel_ValidateTargetRID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"empty", "", false},
		{"whitespace", "   ", false},
		{"plain string", "object/123", false},
		{"valid rid", "ri.ontology.main.object.abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTargetRID(tt.in)
			if tt.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatalf("want err, got ok")
			}
		})
	}
}

func TestModel_ValidateReason(t *testing.T) {
	if err := ValidateReason(""); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := ValidateReason("short note"); err != nil {
		t.Fatalf("short: %v", err)
	}
	long := make([]byte, MaxReasonLength+1)
	for i := range long {
		long[i] = 'x'
	}
	if err := ValidateReason(string(long)); err == nil {
		t.Fatalf("oversize: want err")
	}
}

func TestModel_IsTerminalStatus(t *testing.T) {
	if !IsTerminalStatus(StatusApproved) || !IsTerminalStatus(StatusRejected) {
		t.Fatalf("approved/rejected should be terminal")
	}
	if IsTerminalStatus(StatusPending) || IsTerminalStatus("") {
		t.Fatalf("pending/empty should be non-terminal")
	}
}
