package scenarioruns

import (
	"context"
	"sync"
)

// MemoryRepo is an in-memory scenario_runs repository. It is primarily used
// by degraded server boots and contract tests so the HTTP lifecycle remains
// mounted before the durable PG implementation is wired.
type MemoryRepo struct {
	mu   sync.Mutex
	runs map[string]Run
}

// NewMemoryRepo returns an empty concurrency-safe in-memory Repo.
func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{runs: make(map[string]Run)}
}

func (m *MemoryRepo) CreateRun(_ context.Context, r Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.RID] = cloneRun(r)
	return nil
}

func (m *MemoryRepo) GetRun(_ context.Context, rid string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[rid]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	return cloneRun(r), nil
}

func (m *MemoryRepo) SaveCheckpoint(_ context.Context, cp RunCheckpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[cp.RunRID]
	if !ok {
		return ErrRunNotFound
	}
	r.Status = cp.Status
	r.Error = cp.Error
	r.Checkpoint = cloneCheckpoint(cp)
	if IsTerminal(cp.Status) {
		completedAt := cp.UpdatedAt
		r.CompletedAt = &completedAt
	}
	m.runs[cp.RunRID] = cloneRun(r)
	return nil
}

func (m *MemoryRepo) ListResumable(_ context.Context) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Run{}
	for _, r := range m.runs {
		if IsResumable(r.Status) {
			out = append(out, cloneRun(r))
		}
	}
	return out, nil
}

func cloneRun(r Run) Run {
	out := r
	out.Checkpoint = cloneCheckpoint(r.Checkpoint)
	if r.CompletedAt != nil {
		completedAt := *r.CompletedAt
		out.CompletedAt = &completedAt
	}
	return out
}

func cloneCheckpoint(cp RunCheckpoint) RunCheckpoint {
	out := cp
	out.Completed = append([]string(nil), cp.Completed...)
	if cp.AttemptsByID != nil {
		out.AttemptsByID = make(map[string]int, len(cp.AttemptsByID))
		for k, v := range cp.AttemptsByID {
			out.AttemptsByID[k] = v
		}
	}
	return out
}
