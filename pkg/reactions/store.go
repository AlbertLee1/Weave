package reactions

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Delete targets a (user, target, emoji)
// triple that has no row. Maps to 404 ReactionNotFound at the handler.
var ErrNotFound = errors.New("reactions: not found")

// Store is the narrow persistence surface for emoji reactions. Kept off
// oms.Repository so adding the reactions table doesn't cascade into the
// codebase's many in-memory repo stubs (same dep-direction trick as
// watches.Store / comments.Store).
type Store interface {
	// Create idempotently records a reaction. If a row already exists
	// for the same (UserID, TargetRID, Emoji) the existing row is
	// copied back into *r so callers always receive the canonical id
	// and timestamp.
	Create(ctx context.Context, r *Reaction) error
	// Delete removes the (userID, targetRID, emoji) row. ErrNotFound
	// when no row matches so the SPA can disambiguate "already
	// un-reacted".
	Delete(ctx context.Context, userID, targetRID, emoji string) error
	// AggregateForTarget returns one bucket per distinct emoji on the
	// target, with the caller's "mine" flag set when (userID, target,
	// emoji) has a row. The bucket order is descending count, then
	// ascending emoji string for deterministic wire shape.
	AggregateForTarget(ctx context.Context, userID, targetRID string) ([]EmojiCount, error)
	// DeleteAllForUser hard-removes every row owned by userID. Backs
	// the US-494 GDPR cascade-erase contract: post-call the user_id
	// column carries zero references to userID. Idempotent — a missing
	// user returns (0, nil).
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu   sync.RWMutex
	rows map[string]*Reaction // keyed by id
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]*Reaction{}}
}

func cloneReaction(r *Reaction) *Reaction {
	cp := *r
	return &cp
}

// Create records that r.UserID has reacted with r.Emoji to r.TargetRID.
// If a row already exists for the same triple, the existing row is
// copied back into *r so callers always receive the canonical id and
// timestamp.
func (m *MemoryStore) Create(_ context.Context, r *Reaction) error {
	if r == nil {
		return errors.New("reactions: nil reaction")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.UserID == r.UserID && row.TargetRID == r.TargetRID && row.Emoji == r.Emoji {
			*r = *cloneReaction(row)
			return nil
		}
	}
	if r.ID == "" {
		return errors.New("reactions: id required")
	}
	if _, exists := m.rows[r.ID]; exists {
		return errors.New("reactions: id already exists")
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	m.rows[r.ID] = cloneReaction(r)
	return nil
}

// Delete removes the (userID, targetRID, emoji) row. ErrNotFound when no
// row matches the triple.
func (m *MemoryStore) Delete(_ context.Context, userID, targetRID, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, row := range m.rows {
		if row.UserID == userID && row.TargetRID == targetRID && row.Emoji == emoji {
			delete(m.rows, id)
			return nil
		}
	}
	return ErrNotFound
}

// DeleteAllForUser removes every row whose UserID matches.
func (m *MemoryStore) DeleteAllForUser(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, row := range m.rows {
		if row.UserID == userID {
			delete(m.rows, id)
			n++
		}
	}
	return n, nil
}

// AggregateForTarget returns one bucket per distinct emoji on the
// target. Buckets are ordered by descending count, then ascending emoji
// string so the wire shape is deterministic for tests.
func (m *MemoryStore) AggregateForTarget(_ context.Context, userID, targetRID string) ([]EmojiCount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := map[string]int{}
	mine := map[string]bool{}
	for _, row := range m.rows {
		if row.TargetRID != targetRID {
			continue
		}
		counts[row.Emoji]++
		if row.UserID == userID {
			mine[row.Emoji] = true
		}
	}
	out := make([]EmojiCount, 0, len(counts))
	for emoji, n := range counts {
		out = append(out, EmojiCount{Emoji: emoji, Count: n, Mine: mine[emoji]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Emoji < out[j].Emoji
	})
	return out, nil
}
