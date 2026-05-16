package comments

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Get / Update / Delete targets a comment
// id that no longer exists or whose row was already soft-deleted (and
// therefore cannot be edited / re-deleted). Maps to 404 CommentNotFound
// at the handler.
var ErrNotFound = errors.New("comments: not found")

// ErrForbidden is returned when the caller is not the comment's author
// and so cannot mutate it. Maps to 403 CommentForbidden at the handler.
var ErrForbidden = errors.New("comments: forbidden")

// ErrInvalidParent is returned when Create references a parent_id that
// does not exist, points at a different target_rid, or is itself a
// reply (one-deep threading only). Maps to 400 InvalidCommentParent.
var ErrInvalidParent = errors.New("comments: invalid parent")

// Store is the narrow persistence surface for comments. Kept off
// oms.Repository so adding comments doesn't cascade into the codebase's
// many in-memory repo stubs (same dep-direction trick as
// savedsearches.Store / actiontemplates.Store / dashboards.Store).
type Store interface {
	Create(ctx context.Context, c *Comment) error
	Get(ctx context.Context, id string) (*Comment, error)
	List(ctx context.Context, q ListQuery) (rows []*Comment, total int, err error)
	Update(ctx context.Context, id, author string, upd Update) error
	Delete(ctx context.Context, id, author string) error
	// DeleteAllForUser hard-removes every row authored by userID,
	// including soft-deleted tombstones. Backs the US-494 GDPR
	// cascade-erase contract — once it returns, the user_id column
	// holds zero references to userID. Idempotent: a missing user
	// returns (0, nil).
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// ListQuery scopes a List call. TargetRID is required; ParentID may be
// empty (returns top-level + replies for the target) or set to filter
// to a single thread. Limit / Offset are used as-is by the store; the
// handler is responsible for clamping defaults.
type ListQuery struct {
	TargetRID string
	ParentID  string
	Limit     int
	Offset    int
	// IncludeDeleted controls whether soft-deleted rows surface. The
	// public handler always passes false; the field exists so admin
	// audits can opt in if needed in a later story.
	IncludeDeleted bool
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu   sync.RWMutex
	rows map[string]*Comment
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]*Comment{}}
}

func cloneComment(c *Comment) *Comment {
	cp := *c
	if c.DeletedAt != nil {
		t := *c.DeletedAt
		cp.DeletedAt = &t
	}
	return &cp
}

// publicView returns a copy with the Body redacted when the row is
// soft-deleted, matching the contract the wire returns.
func publicView(c *Comment) *Comment {
	cp := cloneComment(c)
	if cp.DeletedAt != nil {
		cp.Body = ""
	}
	return cp
}

// Create inserts c. When ParentID is set it must reference a live
// (non-deleted) row that targets the SAME TargetRID and is itself
// top-level — replies-of-replies are rejected with ErrInvalidParent so
// the Comments tab can render a flat two-level tree without recursion.
func (m *MemoryStore) Create(_ context.Context, c *Comment) error {
	if c == nil {
		return errors.New("comments: nil comment")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.rows[c.ID]; exists {
		return errors.New("comments: id already exists")
	}
	if c.ParentID != "" {
		parent, ok := m.rows[c.ParentID]
		if !ok || parent.DeletedAt != nil {
			return ErrInvalidParent
		}
		if parent.TargetRID != c.TargetRID {
			return ErrInvalidParent
		}
		if parent.ParentID != "" {
			return ErrInvalidParent
		}
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}
	m.rows[c.ID] = cloneComment(c)
	return nil
}

// Get returns the row regardless of soft-delete state — the handler
// decides what to return on the wire. Soft-deleted rows return the
// underlying row so the handler can emit the {body:"", deletedAt:...}
// tombstone shape. Missing ids return ErrNotFound.
func (m *MemoryStore) Get(_ context.Context, id string) (*Comment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	return publicView(row), nil
}

// List returns rows for the given TargetRID, ordered by CreatedAt
// ASC (top-down thread reading). The total count counts ALL matching
// rows for the target/parent scope (used by the SPA pager) regardless
// of Limit/Offset.
func (m *MemoryStore) List(_ context.Context, q ListQuery) ([]*Comment, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	matches := make([]*Comment, 0, len(m.rows))
	for _, row := range m.rows {
		if row.TargetRID != q.TargetRID {
			continue
		}
		if q.ParentID != "" && row.ParentID != q.ParentID {
			continue
		}
		if !q.IncludeDeleted && row.DeletedAt != nil {
			// Tombstones still surface (so the SPA can render
			// "[deleted]") but Body is redacted by publicView.
		}
		matches = append(matches, publicView(row))
	}
	sort.Slice(matches, func(i, j int) bool {
		if !matches[i].CreatedAt.Equal(matches[j].CreatedAt) {
			return matches[i].CreatedAt.Before(matches[j].CreatedAt)
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

// Update applies a partial update. Only the original author may edit;
// other callers see ErrForbidden. Soft-deleted rows are immutable and
// return ErrNotFound.
func (m *MemoryStore) Update(_ context.Context, id, author string, upd Update) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotFound
	}
	if row.DeletedAt != nil {
		return ErrNotFound
	}
	if row.Author != author {
		return ErrForbidden
	}
	if upd.Body != nil {
		row.Body = *upd.Body
	}
	row.UpdatedAt = time.Now().UTC()
	return nil
}

// DeleteAllForUser hard-removes every row authored by userID, including
// soft-deleted tombstones. Reply rows authored by other users keep
// their parent reference even when the parent vanishes — the handler
// already tolerates a dangling parent_id (renders the thread head as
// "[removed]") and rebuilding the chain would re-introduce userID
// references via the historical body.
func (m *MemoryStore) DeleteAllForUser(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, row := range m.rows {
		if row.Author == userID {
			delete(m.rows, id)
			n++
		}
	}
	return n, nil
}

// Delete soft-deletes a comment. Only the original author may delete;
// other callers see ErrForbidden. Already-deleted rows return
// ErrNotFound so re-delete attempts surface a clean 404 rather than
// silently no-op.
func (m *MemoryStore) Delete(_ context.Context, id, author string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotFound
	}
	if row.DeletedAt != nil {
		return ErrNotFound
	}
	if row.Author != author {
		return ErrForbidden
	}
	now := time.Now().UTC()
	row.DeletedAt = &now
	row.UpdatedAt = now
	return nil
}
