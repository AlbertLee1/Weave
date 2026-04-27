package pipeline

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrPipelineNotFound is returned by Store methods when the requested
// pipeline does not exist.
var ErrPipelineNotFound = errors.New("pipeline: not found")

// ErrPipelineAlreadyExists is returned by Store.CreatePipeline when the
// id is already taken.
var ErrPipelineAlreadyExists = errors.New("pipeline: already exists")

// Store is the narrow persistence surface the Pipeline handlers depend
// on. Kept off oms.Repository so adding pipelines doesn't cascade into
// the existing in-memory repo stubs (same pattern as aip.Store and
// aiplogic.Store).
type Store interface {
	CreatePipeline(ctx context.Context, p *Pipeline) error
	GetPipeline(ctx context.Context, id string) (*Pipeline, error)
	ListPipelines(ctx context.Context, createdBy string) ([]*Pipeline, error)
	UpdatePipeline(ctx context.Context, id string, upd PipelineUpdate) error
	DeletePipeline(ctx context.Context, id string) error
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu        sync.RWMutex
	pipelines map[string]*Pipeline
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{pipelines: map[string]*Pipeline{}}
}

// CreatePipeline inserts p. Stamps timestamps when zero. Returns
// ErrPipelineAlreadyExists when the id is taken.
func (s *MemoryStore) CreatePipeline(_ context.Context, p *Pipeline) error {
	if p == nil {
		return errors.New("pipeline: pipeline is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pipelines[p.ID]; ok {
		return ErrPipelineAlreadyExists
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	s.pipelines[p.ID] = ClonePipeline(p)
	return nil
}

// GetPipeline returns the named pipeline or ErrPipelineNotFound.
func (s *MemoryStore) GetPipeline(_ context.Context, id string) (*Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.pipelines[id]
	if !ok {
		return nil, ErrPipelineNotFound
	}
	return ClonePipeline(p), nil
}

// ListPipelines returns pipelines owned by createdBy. When createdBy is
// "" all pipelines are returned. Output is sorted newest-first by
// CreatedAt; ties break by id ascending.
func (s *MemoryStore) ListPipelines(_ context.Context, createdBy string) ([]*Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Pipeline, 0, len(s.pipelines))
	for _, p := range s.pipelines {
		if createdBy != "" && p.CreatedBy != createdBy {
			continue
		}
		out = append(out, ClonePipeline(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// UpdatePipeline applies a partial update; ErrPipelineNotFound when
// missing.
func (s *MemoryStore) UpdatePipeline(_ context.Context, id string, upd PipelineUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return ErrPipelineNotFound
	}
	if upd.Name != nil {
		p.Name = *upd.Name
	}
	if upd.Description != nil {
		p.Description = *upd.Description
	}
	if upd.Inputs != nil {
		p.Inputs = cloneInputs(*upd.Inputs)
	}
	if upd.Transforms != nil {
		p.Transforms = cloneTransforms(*upd.Transforms)
	}
	if upd.Outputs != nil {
		p.Outputs = cloneOutputs(*upd.Outputs)
	}
	if upd.Schedule != nil {
		p.Schedule = *upd.Schedule
	}
	if upd.Enabled != nil {
		p.Enabled = *upd.Enabled
	}
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// DeletePipeline removes the row. ErrPipelineNotFound when missing.
func (s *MemoryStore) DeletePipeline(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pipelines[id]; !ok {
		return ErrPipelineNotFound
	}
	delete(s.pipelines, id)
	return nil
}
