package scenarioruns

import (
	"context"
	"sort"
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

// ListRunsForScenario implements Repo. Round 68. Returns every run
// matching scenarioRID sorted by StartedAt DESC (newest first).
// The PG implementation will use the scenario_runs_scenario_idx
// index from migration 000109; here we filter the in-memory map
// and sort the result so the wire ordering is deterministic.
func (m *MemoryRepo) ListRunsForScenario(_ context.Context, scenarioRID string) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Run{}
	for _, r := range m.runs {
		if r.ScenarioRID == scenarioRID {
			out = append(out, cloneRun(r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
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
