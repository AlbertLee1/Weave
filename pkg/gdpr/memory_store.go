package gdpr

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrJobNotFound is returned by JobStore.GetJob / UpdateJob when the
// job_id is unknown.
var ErrJobNotFound = errors.New("gdpr: job not found")

// MemoryJobStore is the in-memory test double for JobStore. Safe for
// concurrent use.
type MemoryJobStore struct {
	mu   sync.Mutex
	jobs map[string]*ErasureJob
}

// NewMemoryJobStore returns an empty in-memory JobStore.
func NewMemoryJobStore() *MemoryJobStore {
	return &MemoryJobStore{jobs: map[string]*ErasureJob{}}
}

func (s *MemoryJobStore) CreateJob(_ context.Context, job *ErasureJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = job.CreatedAt
	}
	cp := *job
	s.jobs[job.JobID] = &cp
	return nil
}

func (s *MemoryJobStore) GetJob(_ context.Context, id string) (*ErasureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	cp := *j
	return &cp, nil
}

func (s *MemoryJobStore) UpdateJob(_ context.Context, id string, upd JobUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if upd.Status != "" {
		j.Status = upd.Status
	}
	if upd.Progress != nil {
		j.Progress = *upd.Progress
	}
	if upd.Steps != nil {
		j.Steps = append([]StepResult(nil), upd.Steps...)
	}
	if upd.ErrorMessage != nil {
		j.ErrorMessage = *upd.ErrorMessage
	}
	if upd.ProofHash != nil {
		j.ProofHash = *upd.ProofHash
	}
	j.UpdatedAt = time.Now()
	return nil
}
