// Package scenarioruns implements the Vertex Scenario Execution Service
// (VTX-057) — a lightweight, in-process workflow runner that turns a
// Scenario's models + actions into a DAG of activities, executes them
// with a per-activity retry policy, supports cancellation via ctx, and
// checkpoints progress to scenario_runs so a worker that crashes
// mid-run can resume without re-executing the activities that already
// finished.
//
// The package is HTTP-thin: types + workflow + service are pure Go
// with no transport coupling. The Handler is a chi-based façade wired
// in by cmd/server/main.go alongside the other Vertex registrations.
//
// We intentionally do not pull in a workflow engine (Temporal etc.)
// because Weave is a single-machine ontology engine — the cross-process
// guarantees those engines provide are not on the table for the
// foreseeable future. Resume semantics here are deliberately weak: a
// worker that crashes in the middle of an in-flight activity will
// re-run that activity on resume (the checkpoint records progress
// after each activity transition, not during). Activities are expected
// to be idempotent.
package scenarioruns

import (
	"errors"
	"time"
)

// RunStatus is the lifecycle of a single Scenario Run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCanceled  RunStatus = "canceled"
)

// ActivityKind discriminates between the two activity sources Vertex
// scenarios produce: model invocations (from the Model Mesh planner)
// and Function-backed Actions. The runner does not behave differently
// per kind — discrimination is for observability and for the executor
// wired in by main.go to dispatch to the right downstream service.
type ActivityKind string

const (
	ActivityKindModel  ActivityKind = "model"
	ActivityKindAction ActivityKind = "action"
)

// Activity is one node in the workflow DAG. Layer is the topological
// generation (layer 0 first); siblings within a layer can in principle
// run concurrently, though the v1 Workflow runs them sequentially in
// deterministic ID order to keep the failure surface tractable.
type Activity struct {
	ID    string
	Kind  ActivityKind
	Layer int
}

// RetryPolicy controls per-activity retry behavior. MaxAttempts counts
// total attempts including the first call (so MaxAttempts=3 means 1 +
// 2 retries). BackoffMs is the fixed sleep between attempts.
type RetryPolicy struct {
	MaxAttempts int
	BackoffMs   int
}

// Normalize fills in defaults for a zero-value RetryPolicy. The default
// MaxAttempts is 3 to match the BDD acceptance ("retry policy=3, 第 1
// 次失败 Then 自动重试 2 次"). BackoffMs is clamped to non-negative.
func (p RetryPolicy) Normalize() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 3
	}
	if p.BackoffMs < 0 {
		p.BackoffMs = 0
	}
	return p
}

// ActivityResult is the per-activity outcome the workflow returns. It
// captures the attempt count even on success so callers can observe
// transient failures the runner masked.
type ActivityResult struct {
	ActivityID string    `json:"activityId"`
	Attempts   int       `json:"attempts"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
	Err        error     `json:"-"`
	ErrMsg     string    `json:"error,omitempty"`
}

// RunCheckpoint is the restartable state for a single Run. The
// Workflow rewrites the checkpoint after every activity transition;
// the Repo persists it to scenario_runs.state. On resume, the service
// uses Completed to skip activities that already terminal-succeeded.
type RunCheckpoint struct {
	RunRID       string         `json:"runRid"`
	ScenarioRID  string         `json:"scenarioRid"`
	Status       RunStatus      `json:"status"`
	Completed    []string       `json:"completed,omitempty"`
	LastActivity string         `json:"lastActivity,omitempty"`
	AttemptsByID map[string]int `json:"attemptsById,omitempty"`
	Error        string         `json:"error,omitempty"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

// Run is the persisted form of a Scenario Run.
type Run struct {
	RID         string         `json:"rid"`
	ScenarioRID string         `json:"scenarioRid"`
	Status      RunStatus      `json:"status"`
	Error       string         `json:"error,omitempty"`
	Checkpoint  RunCheckpoint  `json:"checkpoint"`
	StartedAt   time.Time      `json:"startedAt"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// Sentinel errors.
var (
	ErrRunNotFound     = errors.New("scenarioruns: run not found")
	ErrAlreadyTerminal = errors.New("scenarioruns: run already terminal")
)

// IsTerminal reports whether s represents a finished run (no more state
// transitions possible).
func IsTerminal(s RunStatus) bool {
	return s == RunStatusSucceeded || s == RunStatusFailed || s == RunStatusCanceled
}

// IsResumable reports whether s represents a run a worker could
// continue executing (pending or running).
func IsResumable(s RunStatus) bool {
	return s == RunStatusPending || s == RunStatusRunning
}

// SkipCompleted returns the subset of activities whose IDs do not
// appear in completed, preserving the input ordering. Used by the
// resume path to fast-forward past activities the prior worker
// finished before crashing.
func SkipCompleted(activities []Activity, completed []string) []Activity {
	if len(completed) == 0 {
		return activities
	}
	set := make(map[string]struct{}, len(completed))
	for _, id := range completed {
		set[id] = struct{}{}
	}
	out := make([]Activity, 0, len(activities))
	for _, a := range activities {
		if _, ok := set[a.ID]; ok {
			continue
		}
		out = append(out, a)
	}
	return out
}
