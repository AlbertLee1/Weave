package watches

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when an unwatch targets a (user, targetRid)
// pair that has no row. Maps to 404 WatchNotFound at the handler.
var ErrNotFound = errors.New("watches: not found")

// Store is the narrow persistence surface for follow relationships. Kept
// off oms.Repository so adding the watches table doesn't cascade into
// the codebase's many in-memory repo stubs (same dep-direction trick as
// savedsearches.Store / comments.Store / dashboards.Store).
type Store interface {
	// Create idempotently records that user wants to follow target.
	// If a row already exists, the existing row is returned via *w
	// rather than producing a duplicate-key error so the SPA's "click
	// the bell twice" path is benign.
	Create(ctx context.Context, w *Watch) error
	// Delete removes the (userID, targetRID) row. ErrNotFound when no
	// row matches so the SPA can disambiguate "already unwatched".
	Delete(ctx context.Context, userID, targetRID string) error
	// List returns every row owned by userID, sorted by CreatedAt
	// DESC (most recent watch first).
	List(ctx context.Context, userID string) ([]*Watch, error)
	// IsWatching is a single-row probe that backs the WatchButton's
	// initial-state query without forcing a List walk on the SPA.
	IsWatching(ctx context.Context, userID, targetRID string) (bool, error)
	// WatchersFor returns userIDs grouped by targetRID for the requested
	// targets. Targets with no watchers are absent from the map (callers
	// iterate). Backs the US-338 activity-notification fan-out — given a
	// batch of changed object RIDs, ask "who has subscribed?" in one
	// query rather than N IsWatching probes.
	WatchersFor(ctx context.Context, targetRIDs []string) (map[string][]string, error)
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu   sync.RWMutex
	rows map[string]*Watch // keyed by id
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]*Watch{}}
}

func cloneWatch(w *Watch) *Watch {
	cp := *w
	return &cp
}

// Create records that w.UserID follows w.TargetRID. If a row already
// exists for the same (UserID, TargetRID), the existing row is copied
// back into *w so callers always receive the canonical id and timestamp.
func (m *MemoryStore) Create(_ context.Context, w *Watch) error {
	if w == nil {
		return errors.New("watches: nil watch")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.UserID == w.UserID && row.TargetRID == w.TargetRID {
			*w = *cloneWatch(row)
			return nil
		}
	}
	if w.ID == "" {
		return errors.New("watches: id required")
	}
	if _, exists := m.rows[w.ID]; exists {
		return errors.New("watches: id already exists")
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	m.rows[w.ID] = cloneWatch(w)
	return nil
}

// Delete removes the (userID, targetRID) row. ErrNotFound when no row
// matches the pair.
func (m *MemoryStore) Delete(_ context.Context, userID, targetRID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, row := range m.rows {
		if row.UserID == userID && row.TargetRID == targetRID {
			delete(m.rows, id)
			return nil
		}
	}
	return ErrNotFound
}

// List returns every row owned by userID, ordered most-recent first.
func (m *MemoryStore) List(_ context.Context, userID string) ([]*Watch, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Watch, 0, len(m.rows))
	for _, row := range m.rows {
		if row.UserID != userID {
			continue
		}
		out = append(out, cloneWatch(row))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// IsWatching reports whether a row for (userID, targetRID) exists.
func (m *MemoryStore) IsWatching(_ context.Context, userID, targetRID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, row := range m.rows {
		if row.UserID == userID && row.TargetRID == targetRID {
			return true, nil
		}
	}
	return false, nil
}

// WatchersFor returns userIDs grouped by targetRID for every requested
// target that has at least one row. Targets with zero watchers are
// absent from the returned map so the activity-fanout caller can range
// over the result without nil-checking each bucket. UserIDs inside a
// bucket are sorted lexicographically so the wire shape is deterministic
// for tests.
func (m *MemoryStore) WatchersFor(_ context.Context, targetRIDs []string) (map[string][]string, error) {
	if len(targetRIDs) == 0 {
		return map[string][]string{}, nil
	}
	wanted := make(map[string]struct{}, len(targetRIDs))
	for _, t := range targetRIDs {
		wanted[t] = struct{}{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string][]string{}
	for _, row := range m.rows {
		if _, ok := wanted[row.TargetRID]; !ok {
			continue
		}
		out[row.TargetRID] = append(out[row.TargetRID], row.UserID)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, nil
}
