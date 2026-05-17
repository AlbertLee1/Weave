package audit

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AuditEvent represents a single audit log entry. It mirrors the audit_events
// table from migration 000020, extended with chain fields from migration
// 000062 (US-266 tamper-proof chain).
type AuditEvent struct {
	ID           string          `json:"id"`
	ActorID      string          `json:"actor_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceRID  string          `json:"resource_rid"`
	DiffJSON     json.RawMessage `json:"diff_json,omitempty"`
	IP           string          `json:"ip"`
	UserAgent    string          `json:"user_agent"`
	Timestamp    time.Time       `json:"ts"`

	// Chain fields — populated by the Store on Insert.
	ChainSeq  int64  `json:"chain_seq,omitempty"`
	PrevHash  string `json:"prev_hash,omitempty"`
	EntryHash string `json:"entry_hash,omitempty"`
}

// ListFilter constrains which events Store.List returns.
// PageSize and Offset provide offset-based pagination. When PageSize is 0 all
// matching events are returned (backwards-compatible default).
type ListFilter struct {
	ActorID      string
	Action       string
	ResourceType string
	// ResourceRID matches audit_events.resource_rid exactly. Added for
	// US-493 so the admin `GET /api/admin/audit` endpoint can scope to a
	// single resource (e.g. "show me everything that ever happened to
	// this ObjectType / Session / Policy").
	ResourceRID string
	From        *time.Time
	To          *time.Time
	PageSize    int
	Offset      int
}

// Store is the persistence interface for audit events.
type Store interface {
	Insert(ctx context.Context, evt AuditEvent) error
	List(ctx context.Context, f ListFilter) ([]AuditEvent, error)
}

// Record fills in the event's ID and timestamp, then persists it via the store.
func Record(ctx context.Context, store Store, evt AuditEvent) error {
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	return store.Insert(ctx, evt)
}

// MemoryStore is an in-memory Store used by unit tests. Safe for concurrent use.
type MemoryStore struct {
	mu     sync.Mutex
	events []AuditEvent
}

// NewMemoryStore returns an empty in-memory audit store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Insert(_ context.Context, evt AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Chain fields: prev_hash points at the tail's entry_hash; chain_seq
	// is the 1-based position in the store. Any caller-supplied values
	// are overwritten — the Store is the authority on the chain.
	var prevHash string
	if len(s.events) > 0 {
		prevHash = s.events[len(s.events)-1].EntryHash
	}
	evt.PrevHash = prevHash
	evt.ChainSeq = int64(len(s.events) + 1)
	h, err := HashEvent(prevHash, evt)
	if err != nil {
		return err
	}
	evt.EntryHash = h

	s.events = append(s.events, evt)
	return nil
}

// ListChain returns every event in the store ORDERED BY chain_seq ASC, so
// callers can walk the linkage with VerifyChain. In-memory stores always
// preserve insertion order, so this is equivalent to returning a snapshot.
func (s *MemoryStore) ListChain(_ context.Context) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEvent, len(s.events))
	copy(out, s.events)
	return out, nil
}

func (s *MemoryStore) List(_ context.Context, f ListFilter) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var filtered []AuditEvent
	for _, e := range s.events {
		if f.ActorID != "" && e.ActorID != f.ActorID {
			continue
		}
		if f.Action != "" && e.Action != f.Action {
			continue
		}
		if f.ResourceType != "" && e.ResourceType != f.ResourceType {
			continue
		}
		if f.ResourceRID != "" && e.ResourceRID != f.ResourceRID {
			continue
		}
		if f.From != nil && e.Timestamp.Before(*f.From) {
			continue
		}
		if f.To != nil && e.Timestamp.After(*f.To) {
			continue
		}
		filtered = append(filtered, e)
	}

	// Sort by timestamp DESC (newest first) to match PGStore ORDER BY ts DESC.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	// Apply offset-based pagination.
	if f.Offset > 0 {
		if f.Offset >= len(filtered) {
			return nil, nil
		}
		filtered = filtered[f.Offset:]
	}
	if f.PageSize > 0 && len(filtered) > f.PageSize {
		filtered = filtered[:f.PageSize]
	}

	return filtered, nil
}

// Events returns a snapshot of all stored events. Test helper only.
func (s *MemoryStore) Events() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]AuditEvent, len(s.events))
	copy(cp, s.events)
	return cp
}
