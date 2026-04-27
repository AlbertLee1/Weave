package logic

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrFlowNotFound is returned by Store methods when the requested flow
// does not exist.
var ErrFlowNotFound = errors.New("aip-logic: flow not found")

// ErrFlowAlreadyExists is returned by Store.CreateFlow when the id is
// already taken.
var ErrFlowAlreadyExists = errors.New("aip-logic: flow already exists")

// Store is the narrow persistence surface the Logic-Flow handlers
// depend on. Kept off oms.Repository so adding flows doesn't cascade
// into the existing in-memory repo stubs.
type Store interface {
	CreateFlow(ctx context.Context, f *Flow) error
	GetFlow(ctx context.Context, id string) (*Flow, error)
	ListFlows(ctx context.Context, createdBy string) ([]*Flow, error)
	UpdateFlow(ctx context.Context, id string, upd FlowUpdate) error
	DeleteFlow(ctx context.Context, id string) error

	AppendRun(ctx context.Context, run *Run) error
	ListRuns(ctx context.Context, flowID string, limit int) ([]*Run, error)
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu     sync.RWMutex
	flows  map[string]*Flow
	runs   map[string][]*Run
	nextID int64
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		flows: map[string]*Flow{},
		runs:  map[string][]*Run{},
	}
}

// CreateFlow inserts f. Stamps timestamps when zero.
func (s *MemoryStore) CreateFlow(_ context.Context, f *Flow) error {
	if f == nil {
		return errors.New("aip-logic: flow is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flows[f.ID]; ok {
		return ErrFlowAlreadyExists
	}
	now := time.Now().UTC()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	if f.UpdatedAt.IsZero() {
		f.UpdatedAt = now
	}
	cp := cloneFlow(f)
	s.flows[f.ID] = cp
	return nil
}

// GetFlow returns the named flow or ErrFlowNotFound.
func (s *MemoryStore) GetFlow(_ context.Context, id string) (*Flow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flows[id]
	if !ok {
		return nil, ErrFlowNotFound
	}
	return cloneFlow(f), nil
}

// ListFlows returns flows owned by createdBy. When createdBy is "" all
// flows are returned. Output is sorted newest-first by CreatedAt.
func (s *MemoryStore) ListFlows(_ context.Context, createdBy string) ([]*Flow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Flow, 0, len(s.flows))
	for _, f := range s.flows {
		if createdBy != "" && f.CreatedBy != createdBy {
			continue
		}
		out = append(out, cloneFlow(f))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// UpdateFlow applies a partial update; ErrFlowNotFound when missing.
func (s *MemoryStore) UpdateFlow(_ context.Context, id string, upd FlowUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[id]
	if !ok {
		return ErrFlowNotFound
	}
	if upd.Name != nil {
		f.Name = *upd.Name
	}
	if upd.Description != nil {
		f.Description = *upd.Description
	}
	if upd.Nodes != nil {
		f.Nodes = cloneNodes(*upd.Nodes)
	}
	if upd.Edges != nil {
		f.Edges = append([]Edge(nil), *upd.Edges...)
	}
	f.UpdatedAt = time.Now().UTC()
	return nil
}

// DeleteFlow removes the flow and its runs.
func (s *MemoryStore) DeleteFlow(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flows[id]; !ok {
		return ErrFlowNotFound
	}
	delete(s.flows, id)
	delete(s.runs, id)
	return nil
}

// AppendRun stores run under run.FlowID. ErrFlowNotFound when the flow
// does not exist.
func (s *MemoryStore) AppendRun(_ context.Context, run *Run) error {
	if run == nil {
		return errors.New("aip-logic: run is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flows[run.FlowID]; !ok {
		return ErrFlowNotFound
	}
	s.nextID++
	run.ID = s.nextID
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	cp := *run
	s.runs[run.FlowID] = append(s.runs[run.FlowID], &cp)
	return nil
}

// ListRuns returns at most `limit` runs for flowID, newest first.
// limit <= 0 returns every run.
func (s *MemoryStore) ListRuns(_ context.Context, flowID string, limit int) ([]*Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.flows[flowID]; !ok {
		return nil, ErrFlowNotFound
	}
	src := s.runs[flowID]
	out := make([]*Run, 0, len(src))
	for _, r := range src {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func cloneFlow(f *Flow) *Flow {
	cp := *f
	cp.Nodes = cloneNodes(f.Nodes)
	cp.Edges = append([]Edge(nil), f.Edges...)
	return &cp
}

func cloneNodes(in []Node) []Node {
	out := make([]Node, len(in))
	for i, n := range in {
		nc := n
		if n.Config != nil {
			nc.Config = make(map[string]any, len(n.Config))
			for k, v := range n.Config {
				nc.Config[k] = v
			}
		}
		out[i] = nc
	}
	return out
}
