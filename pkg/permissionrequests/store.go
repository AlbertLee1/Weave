package permissionrequests

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Get / Decide targets an id that does
// not exist. Maps to 404 PermissionRequestNotFound at the handler.
var ErrNotFound = errors.New("permissionrequests: not found")

// ErrAlreadyDecided is returned when Decide hits a row whose Status is
// already APPROVED or REJECTED. Maps to 409 PermissionRequestAlreadyDecided
// — terminal states are final.
var ErrAlreadyDecided = errors.New("permissionrequests: already decided")

// ErrInvalidStatus is returned when Decide is called with anything other
// than APPROVED / REJECTED. Pending → pending is not a transition; the
// handler enforces this before the store sees the call but the store
// guards belt-and-braces.
var ErrInvalidStatus = errors.New("permissionrequests: invalid decision status")

// Store is the narrow persistence surface for permission requests. Kept
// off oms.Repository so adding the permission_requests table doesn't
// cascade into the codebase's many in-memory repo stubs (same
// dep-direction trick as savedsearches.Store / comments.Store /
// dashboards.Store / watches.Store).
type Store interface {
	Create(ctx context.Context, r *Request) error
	Get(ctx context.Context, id string) (*Request, error)
	List(ctx context.Context, q ListQuery) (rows []*Request, total int, err error)
	Decide(ctx context.Context, id string, dec Decision) error
}

// ListQuery scopes a List call. Status="" returns every row; setting
// Status filters to PENDING / APPROVED / REJECTED. RequestedBy="" returns
// every requester; setting it scopes to "my requests". TargetRID
// optionally narrows to a single resource (e.g. the owner browsing the
// inbox for a specific object). Limit / Offset are used as-is by the
// store; the handler clamps defaults.
type ListQuery struct {
	Status      string
	RequestedBy string
	TargetRID   string
	Limit       int
	Offset      int
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu   sync.RWMutex
	rows map[string]*Request
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]*Request{}}
}

func cloneRequest(r *Request) *Request {
	cp := *r
	if r.DecidedAt != nil {
		t := *r.DecidedAt
		cp.DecidedAt = &t
	}
	return &cp
}

// Create inserts r. Caller is expected to have populated ID, TargetRID,
// RequestedBy, and Reason; Status defaults to PENDING when empty,
// CreatedAt / UpdatedAt default to time.Now() when zero.
func (m *MemoryStore) Create(_ context.Context, r *Request) error {
	if r == nil {
		return errors.New("permissionrequests: nil request")
	}
	if r.ID == "" {
		return errors.New("permissionrequests: id required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.rows[r.ID]; exists {
		return errors.New("permissionrequests: id already exists")
	}
	now := time.Now().UTC()
	if r.Status == "" {
		r.Status = StatusPending
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	m.rows[r.ID] = cloneRequest(r)
	return nil
}

// Get returns the row by id. Missing ids return ErrNotFound.
func (m *MemoryStore) Get(_ context.Context, id string) (*Request, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRequest(row), nil
}

// List returns rows ordered by CreatedAt DESC (newest first). Total
// counts every matching row regardless of Limit / Offset so the SPA
// pager can render a "X of Y" footer.
func (m *MemoryStore) List(_ context.Context, q ListQuery) ([]*Request, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	matches := make([]*Request, 0, len(m.rows))
	for _, row := range m.rows {
		if q.Status != "" && row.Status != q.Status {
			continue
		}
		if q.RequestedBy != "" && row.RequestedBy != q.RequestedBy {
			continue
		}
		if q.TargetRID != "" && row.TargetRID != q.TargetRID {
			continue
		}
		matches = append(matches, cloneRequest(row))
	}
	sort.Slice(matches, func(i, j int) bool {
		if !matches[i].CreatedAt.Equal(matches[j].CreatedAt) {
			return matches[i].CreatedAt.After(matches[j].CreatedAt)
		}
		return matches[i].ID < matches[j].ID
	})
	total := len(matches)
	start := q.Offset
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := total
	if q.Limit > 0 && start+q.Limit < end {
		end = start + q.Limit
	}
	return matches[start:end], total, nil
}

// Decide transitions a PENDING row to APPROVED or REJECTED, stamping
// DecidedBy / DecisionNote / DecidedAt. Already-decided rows return
// ErrAlreadyDecided so callers can surface a clean 409.
func (m *MemoryStore) Decide(_ context.Context, id string, dec Decision) error {
	if !IsTerminalStatus(dec.Status) {
		return ErrInvalidStatus
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotFound
	}
	if IsTerminalStatus(row.Status) {
		return ErrAlreadyDecided
	}
	now := time.Now().UTC()
	row.Status = dec.Status
	row.DecidedBy = dec.By
	row.DecisionNote = dec.Note
	row.DecidedAt = &now
	row.UpdatedAt = now
	return nil
}
