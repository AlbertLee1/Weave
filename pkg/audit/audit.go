package audit

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AuditEvent represents a single audit log entry. It mirrors the audit_events
// table from migration 000020.
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
}

// ListFilter constrains which events Store.List returns.
type ListFilter struct {
	ActorID      string
	Action       string
	ResourceType string
	From         *time.Time
	To           *time.Time
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
	s.events = append(s.events, evt)
	return nil
}

func (s *MemoryStore) List(_ context.Context, f ListFilter) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AuditEvent
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
		if f.From != nil && e.Timestamp.Before(*f.From) {
			continue
		}
		if f.To != nil && e.Timestamp.After(*f.To) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Events returns a snapshot of all stored events. Test helper only.
func (s *MemoryStore) Events() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]AuditEvent, len(s.events))
	copy(cp, s.events)
	return cp
}
