package userprefs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ErrNotFound is returned when Get targets a user_id that has never
// stored a preferences row. Handlers map this to "return defaults"
// rather than 404 — the wire contract is that every authenticated user
// has a virtual default row even before their first PUT.
var ErrNotFound = errors.New("userprefs: not found")

// Store is the narrow persistence surface. Off oms.Repository so adding
// a new resource doesn't cascade into the codebase's many in-memory
// repo stubs (same dep-direction trick as savedsearches.Store).
type Store interface {
	// Get returns the persisted row for userID. ErrNotFound when no row
	// has been written yet — callers convert that to a virtual zero-
	// value Preferences so first-load PUTs can be bare upserts.
	Get(ctx context.Context, userID string) (*Preferences, error)

	// Upsert inserts the row if absent or merges the partial update
	// into the existing row. The returned *Preferences is the post-
	// merge state including timestamps.
	Upsert(ctx context.Context, userID string, upd Update) (*Preferences, error)

	// DeleteAllForUser hard-removes the preferences row keyed by
	// userID. Backs the US-494 GDPR cascade-erase contract: post-call
	// Get returns ErrNotFound and the user_id PK column carries zero
	// references. Idempotent — a missing user returns (0, nil).
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu   sync.RWMutex
	rows map[string]*Preferences
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]*Preferences{}}
}

func clonePrefs(p *Preferences) *Preferences {
	cp := *p
	if p.Notifications != nil {
		cp.Notifications = append(json.RawMessage(nil), p.Notifications...)
	}
	if p.Hotkeys != nil {
		cp.Hotkeys = append(json.RawMessage(nil), p.Hotkeys...)
	}
	return &cp
}

// Get returns a clone so callers mutating the slice can't corrupt the
// store. Returns ErrNotFound when the row is absent.
func (m *MemoryStore) Get(_ context.Context, userID string) (*Preferences, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return clonePrefs(row), nil
}

// Upsert merges upd into the existing row (or creates one with default
// payloads when absent) and returns the post-merge state. Validation
// happens here so handler tests don't need to round-trip through PG to
// catch invalid theme / language input.
func (m *MemoryStore) Upsert(_ context.Context, userID string, upd Update) (*Preferences, error) {
	if userID == "" {
		return nil, errors.New("userprefs: empty user id")
	}
	if upd.Theme != nil {
		t := NormaliseTheme(*upd.Theme)
		if err := ValidateTheme(t); err != nil {
			return nil, err
		}
		upd.Theme = &t
	}
	if upd.Language != nil {
		l := NormaliseLanguage(*upd.Language)
		if err := ValidateLanguage(l); err != nil {
			return nil, err
		}
		upd.Language = &l
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[userID]
	now := time.Now().UTC()
	if !ok {
		row = &Preferences{
			UserID:        userID,
			Notifications: append(json.RawMessage(nil), DefaultPayload...),
			Hotkeys:       append(json.RawMessage(nil), DefaultPayload...),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		m.rows[userID] = row
	}
	if upd.Theme != nil {
		row.Theme = *upd.Theme
	}
	if upd.Language != nil {
		row.Language = *upd.Language
	}
	if upd.Notifications != nil {
		raw := *upd.Notifications
		if len(raw) == 0 {
			raw = DefaultPayload
		}
		row.Notifications = append(json.RawMessage(nil), raw...)
	}
	if upd.Hotkeys != nil {
		raw := *upd.Hotkeys
		if len(raw) == 0 {
			raw = DefaultPayload
		}
		row.Hotkeys = append(json.RawMessage(nil), raw...)
	}
	row.UpdatedAt = now
	return clonePrefs(row), nil
}

// DeleteAllForUser removes the preferences row for userID. Reports the
// number of rows actually removed (0 or 1) so the GDPR job log records
// a real row count.
func (m *MemoryStore) DeleteAllForUser(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[userID]; !ok {
		return 0, nil
	}
	delete(m.rows, userID)
	return 1, nil
}
