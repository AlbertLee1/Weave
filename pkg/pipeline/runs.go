package pipeline

import (
	"context"
	"errors"
	"sort"
	"time"
)

// ErrPipelineRunNotFound is returned by Store run-lookup methods when
// the requested run does not exist (or belongs to a different pipeline).
var ErrPipelineRunNotFound = errors.New("pipeline: run not found")

// PipelineRun is one persisted execution record for a Pipeline. The shape
// mirrors RunResult but adds row-level identity (ID, CreatedAt) so the
// list/detail handlers can paginate and reference individual runs after
// they finish.
//
// FinishedAt is nullable so an in-flight run row can be appended at
// dispatch time and updated after the runner returns; today every memory
// path persists at terminal state, but the schema accommodates the future
// async-progress UX (US-300+).
//
// LastCommittedOffset (US-378) is the high-water-mark offset the run
// successfully advanced the pipeline to. APPEND-mode runs read it from
// the prior successful run and write the post-run value here. FULL-mode
// runs leave it at 0.
type PipelineRun struct {
	ID                  int64      `json:"id"`
	PipelineID          string     `json:"pipelineId"`
	Status              string     `json:"status"`
	StartedAt           time.Time  `json:"startedAt"`
	FinishedAt          *time.Time `json:"finishedAt,omitempty"`
	ErrorMessage        string     `json:"errorMessage,omitempty"`
	Result              *RunResult `json:"result,omitempty"`
	TriggeredBy         string     `json:"triggeredBy,omitempty"`
	LastCommittedOffset int64      `json:"lastCommittedOffset,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

// ListRunsOptions tunes ListPipelineRuns. Cursor is exclusive — when
// non-zero, only runs with id strictly less than Cursor are returned.
// Combined with the descending-id ORDER BY clause this yields stable
// keyset pagination without OFFSET drift even if new runs land mid-walk.
type ListRunsOptions struct {
	Limit  int
	Cursor int64
}

// ListRunsPage is the wire shape for one page of runs. NextCursor is 0
// when no further pages remain.
type ListRunsPage struct {
	Runs       []*PipelineRun `json:"runs"`
	NextCursor int64          `json:"nextCursor,omitempty"`
}

// Default and ceiling for the per-page run count. Mirrors the shape used
// by aip/logic.ListRuns.
const (
	defaultRunPageSize = 50
	maxRunPageSize     = 200
)

// AppendPipelineRun stamps run.ID + run.CreatedAt and persists the row.
// Returns ErrPipelineNotFound when run.PipelineID does not match an
// existing pipeline.
func (s *MemoryStore) AppendPipelineRun(_ context.Context, run *PipelineRun) error {
	if run == nil {
		return errors.New("pipeline: run is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pipelines[run.PipelineID]; !ok {
		return ErrPipelineNotFound
	}
	s.lastRunID++
	run.ID = s.lastRunID
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	s.runs = append(s.runs, clonePipelineRun(run))
	return nil
}

// GetPipelineRun returns the run row scoped to pipelineID.
// ErrPipelineRunNotFound when missing OR when the row exists under a
// different pipeline (don't leak existence across pipelines).
func (s *MemoryStore) GetPipelineRun(_ context.Context, pipelineID string, runID int64) (*PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.runs {
		if r.ID == runID && r.PipelineID == pipelineID {
			return clonePipelineRun(r), nil
		}
	}
	return nil, ErrPipelineRunNotFound
}

// LatestCommittedOffset returns the highest last_committed_offset among
// successful runs of pipelineID. Returns 0 when no successful run exists
// (the conventional "scan from the start" sentinel for APPEND mode).
// ErrPipelineNotFound when the pipeline itself is missing.
func (s *MemoryStore) LatestCommittedOffset(_ context.Context, pipelineID string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.pipelines[pipelineID]; !ok {
		return 0, ErrPipelineNotFound
	}
	var max int64
	for _, r := range s.runs {
		if r.PipelineID != pipelineID {
			continue
		}
		if r.Status != "success" {
			continue
		}
		if r.LastCommittedOffset > max {
			max = r.LastCommittedOffset
		}
	}
	return max, nil
}

// ListPipelineRuns returns runs for pipelineID newest-first (descending id).
// ErrPipelineNotFound when the pipeline does not exist.
func (s *MemoryStore) ListPipelineRuns(_ context.Context, pipelineID string, opts ListRunsOptions) (*ListRunsPage, error) {
	limit := normaliseRunLimit(opts.Limit)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.pipelines[pipelineID]; !ok {
		return nil, ErrPipelineNotFound
	}
	matched := make([]*PipelineRun, 0, len(s.runs))
	for _, r := range s.runs {
		if r.PipelineID != pipelineID {
			continue
		}
		if opts.Cursor > 0 && r.ID >= opts.Cursor {
			continue
		}
		matched = append(matched, clonePipelineRun(r))
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].ID > matched[j].ID
	})
	page := &ListRunsPage{}
	if len(matched) > limit {
		page.Runs = matched[:limit]
		page.NextCursor = page.Runs[len(page.Runs)-1].ID
	} else {
		page.Runs = matched
	}
	return page, nil
}

func normaliseRunLimit(limit int) int {
	if limit <= 0 {
		return defaultRunPageSize
	}
	if limit > maxRunPageSize {
		return maxRunPageSize
	}
	return limit
}

func clonePipelineRun(r *PipelineRun) *PipelineRun {
	if r == nil {
		return nil
	}
	cp := *r
	if r.FinishedAt != nil {
		t := *r.FinishedAt
		cp.FinishedAt = &t
	}
	if r.Result != nil {
		rr := *r.Result
		cp.Result = &rr
	}
	return &cp
}
