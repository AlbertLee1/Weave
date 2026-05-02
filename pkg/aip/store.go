package aip

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrThreadNotFound is returned by Store methods when the requested
// thread does not exist.
var ErrThreadNotFound = errors.New("aip: thread not found")

// ErrThreadAlreadyExists is returned by Store.CreateThread when the id
// is already taken.
var ErrThreadAlreadyExists = errors.New("aip: thread already exists")

// Store is the narrow persistence surface the AIP handlers depend on.
// Kept off oms.Repository intentionally so adding AIP doesn't cascade
// into the ~15 in-memory repo stubs scattered across the codebase
// (see featureflags.Store / tenants.Store for prior art).
type Store interface {
	CreateThread(ctx context.Context, t *Thread) error
	GetThread(ctx context.Context, id string) (*Thread, error)
	ListThreads(ctx context.Context, createdBy string) ([]*Thread, error)
	UpdateThread(ctx context.Context, id string, upd ThreadUpdate) error
	DeleteThread(ctx context.Context, id string) error

	AppendMessage(ctx context.Context, m *Message) error
	ListMessages(ctx context.Context, threadID string) ([]*Message, error)

	// GetMessage looks up a single message by id. Returns
	// ErrMessageNotFound when missing. Used by the fork handler to
	// resolve the pivot message before copying its ancestry.
	GetMessage(ctx context.Context, id int64) (*Message, error)

	// ForkThread creates a new thread that copies every message on the
	// pivot's ancestor chain (root → pivot inclusive) into the new
	// thread, then returns the new thread + its copied messages. The
	// new messages are stamped with branch_id='main' (the new thread
	// is itself the new branch); their parent_message_id chain is
	// rewritten so the copied root has parent_message_id=NULL and each
	// subsequent message points at the previous copy. The original
	// thread is left untouched.
	ForkThread(ctx context.Context, srcThreadID string, pivotMessageID int64, newThread *Thread) (*Thread, []*Message, error)
}

// ErrMessageNotFound is returned by Store.GetMessage / ForkThread when
// the pivot message id does not exist or does not belong to the source
// thread.
var ErrMessageNotFound = errors.New("aip: message not found")

// ErrPivotThreadMismatch is returned by ForkThread when the pivot
// message belongs to a different thread than the source.
var ErrPivotThreadMismatch = errors.New("aip: pivot message does not belong to source thread")

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu       sync.RWMutex
	threads  map[string]*Thread
	messages map[string][]*Message
	nextID   int64
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		threads:  map[string]*Thread{},
		messages: map[string][]*Message{},
	}
}

// CreateThread inserts t. Stamps CreatedAt / UpdatedAt when zero.
func (s *MemoryStore) CreateThread(_ context.Context, t *Thread) error {
	if t == nil {
		return errors.New("aip: thread is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.threads[t.ID]; ok {
		return ErrThreadAlreadyExists
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	cp := *t
	s.threads[t.ID] = &cp
	return nil
}

// GetThread returns the named thread or ErrThreadNotFound.
func (s *MemoryStore) GetThread(_ context.Context, id string) (*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}
	cp := *t
	return &cp, nil
}

// ListThreads returns all threads owned by createdBy. When createdBy is
// "" every thread is returned (admin / dev callers). Output is sorted
// newest-first by CreatedAt.
func (s *MemoryStore) ListThreads(_ context.Context, createdBy string) ([]*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Thread, 0, len(s.threads))
	for _, t := range s.threads {
		if createdBy != "" && t.CreatedBy != createdBy {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// UpdateThread applies a partial update; ErrThreadNotFound when missing.
func (s *MemoryStore) UpdateThread(_ context.Context, id string, upd ThreadUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[id]
	if !ok {
		return ErrThreadNotFound
	}
	if upd.Title != nil {
		t.Title = *upd.Title
	}
	if upd.Model != nil {
		t.Model = *upd.Model
	}
	if upd.SystemPrompt != nil {
		t.SystemPrompt = *upd.SystemPrompt
	}
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// DeleteThread removes the thread and its messages. ErrThreadNotFound
// when missing.
func (s *MemoryStore) DeleteThread(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.threads[id]; !ok {
		return ErrThreadNotFound
	}
	delete(s.threads, id)
	delete(s.messages, id)
	return nil
}

// AppendMessage inserts m at the tail of its thread. Allocates a
// monotonic ID and stamps CreatedAt / TokenCount when unset. Returns
// ErrThreadNotFound when the thread does not exist.
//
// When BranchID is empty the store stamps DefaultBranchID. When
// ParentMessageID is nil the store auto-links the new message to the
// last message previously appended on the same branch — preserving the
// linear-history contract for callers that have not opted into the
// US-374 branch-tree API.
func (s *MemoryStore) AppendMessage(_ context.Context, m *Message) error {
	if m == nil {
		return errors.New("aip: message is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[m.ThreadID]
	if !ok {
		return ErrThreadNotFound
	}
	s.nextID++
	m.ID = s.nextID
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.BranchID == "" {
		m.BranchID = DefaultBranchID
	}
	if m.ParentMessageID == nil {
		if last := lastOnBranch(s.messages[m.ThreadID], m.BranchID); last != nil {
			id := last.ID
			m.ParentMessageID = &id
		}
	}
	cp := *m
	if len(m.ToolCalls) > 0 {
		cp.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
	}
	if m.ParentMessageID != nil {
		pid := *m.ParentMessageID
		cp.ParentMessageID = &pid
	}
	s.messages[m.ThreadID] = append(s.messages[m.ThreadID], &cp)
	t.UpdatedAt = m.CreatedAt
	return nil
}

// lastOnBranch returns the highest-id message on the given branch in src,
// or nil when the branch is empty.
func lastOnBranch(src []*Message, branchID string) *Message {
	var last *Message
	for _, m := range src {
		if m.BranchID != branchID {
			continue
		}
		if last == nil || m.ID > last.ID {
			last = m
		}
	}
	return last
}

// GetMessage returns the message with the given id. Returns
// ErrMessageNotFound when not found.
func (s *MemoryStore) GetMessage(_ context.Context, id int64) (*Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, msgs := range s.messages {
		for _, m := range msgs {
			if m.ID == id {
				cp := *m
				if len(m.ToolCalls) > 0 {
					cp.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
				}
				if m.ParentMessageID != nil {
					pid := *m.ParentMessageID
					cp.ParentMessageID = &pid
				}
				return &cp, nil
			}
		}
	}
	return nil, ErrMessageNotFound
}

// ForkThread creates newThread, then copies every ancestor of pivotMessageID
// (root → pivot inclusive) into newThread with branch_id=DefaultBranchID
// and a freshly chained parent_message_id. The source thread is left
// untouched. Returns the persisted thread (with stamped timestamps) and
// the ordered slice of copied messages.
func (s *MemoryStore) ForkThread(_ context.Context, srcThreadID string, pivotMessageID int64, newThread *Thread) (*Thread, []*Message, error) {
	if newThread == nil {
		return nil, nil, errors.New("aip: fork thread is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.threads[srcThreadID]; !ok {
		return nil, nil, ErrThreadNotFound
	}
	// First locate the pivot anywhere in the store so we can tell
	// apart "doesn't exist" from "belongs to a different thread".
	var pivot *Message
	for _, msgs := range s.messages {
		for _, m := range msgs {
			if m.ID == pivotMessageID {
				pivot = m
				break
			}
		}
		if pivot != nil {
			break
		}
	}
	if pivot == nil {
		return nil, nil, ErrMessageNotFound
	}
	if pivot.ThreadID != srcThreadID {
		return nil, nil, ErrPivotThreadMismatch
	}
	srcMsgs := s.messages[srcThreadID]
	byID := make(map[int64]*Message, len(srcMsgs))
	for _, m := range srcMsgs {
		byID[m.ID] = m
	}

	// Walk pivot → root via ParentMessageID then reverse. A nil parent
	// terminates the walk; cycles are impossible because parents are
	// strictly older ids.
	var chain []*Message
	cur := pivot
	for cur != nil {
		chain = append(chain, cur)
		if cur.ParentMessageID == nil {
			break
		}
		next, found := byID[*cur.ParentMessageID]
		if !found {
			break
		}
		cur = next
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	if _, exists := s.threads[newThread.ID]; exists {
		return nil, nil, ErrThreadAlreadyExists
	}
	now := time.Now().UTC()
	if newThread.CreatedAt.IsZero() {
		newThread.CreatedAt = now
	}
	if newThread.UpdatedAt.IsZero() {
		newThread.UpdatedAt = now
	}
	threadCopy := *newThread
	s.threads[newThread.ID] = &threadCopy

	copied := make([]*Message, 0, len(chain))
	var prevID *int64
	for _, src := range chain {
		s.nextID++
		id := s.nextID
		cp := *src
		cp.ID = id
		cp.ThreadID = newThread.ID
		cp.BranchID = DefaultBranchID
		cp.CreatedAt = now
		if prevID != nil {
			pid := *prevID
			cp.ParentMessageID = &pid
		} else {
			cp.ParentMessageID = nil
		}
		if len(src.ToolCalls) > 0 {
			cp.ToolCalls = append([]ToolCall(nil), src.ToolCalls...)
		}
		s.messages[newThread.ID] = append(s.messages[newThread.ID], &cp)
		copied = append(copied, &cp)
		prevID = &id
	}
	threadCopy.UpdatedAt = now
	*newThread = threadCopy

	out := make([]*Message, 0, len(copied))
	for _, m := range copied {
		c := *m
		if len(m.ToolCalls) > 0 {
			c.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
		}
		if m.ParentMessageID != nil {
			pid := *m.ParentMessageID
			c.ParentMessageID = &pid
		}
		out = append(out, &c)
	}
	return &threadCopy, out, nil
}

// ListMessages returns every message in threadID ordered by ID asc.
// Empty thread returns an empty (non-nil) slice. Returns
// ErrThreadNotFound when the thread does not exist.
func (s *MemoryStore) ListMessages(_ context.Context, threadID string) ([]*Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.threads[threadID]; !ok {
		return nil, ErrThreadNotFound
	}
	src := s.messages[threadID]
	out := make([]*Message, 0, len(src))
	for _, m := range src {
		cp := *m
		if len(m.ToolCalls) > 0 {
			cp.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
		}
		if m.ParentMessageID != nil {
			pid := *m.ParentMessageID
			cp.ParentMessageID = &pid
		}
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
