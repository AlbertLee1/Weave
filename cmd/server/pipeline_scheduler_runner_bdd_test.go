package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/pipeline"
)

func TestBDD_ScheduledPipelineTick_GivenRuntimeUnavailable_WhenRunnerFires_ThenRunHistoryRecordsFailure(t *testing.T) {
	ctx := context.Background()
	store := pipeline.NewMemoryStore()
	pl := &pipeline.Pipeline{
		ID:        "scheduled_customers",
		Name:      "Scheduled Customers",
		Schedule:  "@every 1h",
		Enabled:   true,
		CreatedBy: "user:operator",
		Inputs:    []pipeline.Input{{Name: "src", Type: "objectset"}},
		Outputs:   []pipeline.Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	}
	if err := store.CreatePipeline(ctx, pl); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	runner := newScheduledPipelineRunRecorder(store)
	err := runner.RunPipeline(ctx, pl)
	if err == nil {
		t.Fatal("expected runtime-not-configured error so the scheduler logger receives the failure")
	}
	if !strings.Contains(err.Error(), "runtime not configured") {
		t.Fatalf("RunPipeline error = %v, want runtime-not-configured detail", err)
	}

	page, err := store.ListPipelineRuns(ctx, pl.ID, pipeline.ListRunsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if len(page.Runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(page.Runs))
	}
	run := page.Runs[0]
	if run.PipelineID != pl.ID {
		t.Errorf("PipelineID = %q, want %q", run.PipelineID, pl.ID)
	}
	if run.Status != "failed" {
		t.Errorf("Status = %q, want failed", run.Status)
	}
	if run.TriggeredBy != "schedule" {
		t.Errorf("TriggeredBy = %q, want schedule", run.TriggeredBy)
	}
	if run.StartedAt.IsZero() {
		t.Error("StartedAt was not set")
	}
	if run.FinishedAt == nil || run.FinishedAt.IsZero() {
		t.Fatal("FinishedAt was not set")
	}
	if run.FinishedAt.Before(run.StartedAt) {
		t.Errorf("FinishedAt %s is before StartedAt %s", run.FinishedAt, run.StartedAt)
	}
	if !strings.Contains(run.ErrorMessage, "runtime not configured") {
		t.Errorf("ErrorMessage = %q, want runtime-not-configured detail", run.ErrorMessage)
	}
}

func TestBDD_ScheduledPipelineTick_GivenRunHistoryAppendFails_WhenRunnerFires_ThenErrorPropagates(t *testing.T) {
	ctx := context.Background()
	store := pipeline.NewMemoryStore()
	runner := newScheduledPipelineRunRecorder(store)

	err := runner.RunPipeline(ctx, &pipeline.Pipeline{ID: "missing"})
	if !errors.Is(err, pipeline.ErrPipelineNotFound) {
		t.Fatalf("RunPipeline error = %v, want ErrPipelineNotFound", err)
	}
}

func TestBDD_ScheduledPipelineTick_GivenDAGRunnerConfigured_WhenRunnerFires_ThenRunHistoryRecordsSuccessResult(t *testing.T) {
	ctx := context.Background()
	store := pipeline.NewMemoryStore()
	pl := scheduledPipelineFixture()
	if err := store.CreatePipeline(ctx, pl); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	seen := map[string]bool{}
	nodeRunner := pipeline.NodeRunnerFunc(func(ctx context.Context, node pipeline.DAGNode, attempt int) error {
		seen[node.Name] = true
		return nil
	})
	runner := newScheduledPipelineDAGRunRecorder(store, nodeRunner)

	if err := runner.RunPipeline(ctx, pl); err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	page, err := store.ListPipelineRuns(ctx, pl.ID, pipeline.ListRunsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if len(page.Runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(page.Runs))
	}
	run := page.Runs[0]
	if run.Status != "success" {
		t.Fatalf("Status = %q, want success", run.Status)
	}
	if run.TriggeredBy != "schedule" {
		t.Fatalf("TriggeredBy = %q, want schedule", run.TriggeredBy)
	}
	if run.Result == nil {
		t.Fatal("Result was not persisted")
	}
	if run.Result.Status != "success" {
		t.Fatalf("Result.Status = %q, want success", run.Result.Status)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(run.Result.FinishedAt) {
		t.Fatalf("FinishedAt = %v, want RunDAG finishedAt %v", run.FinishedAt, run.Result.FinishedAt)
	}
	for _, name := range []string{"src", "filter", "sink"} {
		if !seen[name] {
			t.Fatalf("node runner did not execute %q", name)
		}
		if got := run.Result.Nodes[name].Status; got != pipeline.NodeStatusSuccess {
			t.Fatalf("%s status = %q, want success", name, got)
		}
	}
}

func TestBDD_ScheduledPipelineTick_GivenDAGRunnerFails_WhenRunnerFires_ThenRunHistoryRecordsFailureResult(t *testing.T) {
	ctx := context.Background()
	store := pipeline.NewMemoryStore()
	pl := scheduledPipelineFixture()
	if err := store.CreatePipeline(ctx, pl); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	nodeErr := errors.New("sink refused batch")
	nodeRunner := pipeline.NodeRunnerFunc(func(ctx context.Context, node pipeline.DAGNode, attempt int) error {
		if node.Name == "sink" {
			return nodeErr
		}
		return nil
	})
	runner := newScheduledPipelineDAGRunRecorder(store, nodeRunner)

	err := runner.RunPipeline(ctx, pl)
	if !errors.Is(err, nodeErr) {
		t.Fatalf("RunPipeline error = %v, want node error", err)
	}

	page, err := store.ListPipelineRuns(ctx, pl.ID, pipeline.ListRunsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if len(page.Runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(page.Runs))
	}
	run := page.Runs[0]
	if run.Status != "failed" {
		t.Fatalf("Status = %q, want failed", run.Status)
	}
	if run.Result == nil {
		t.Fatal("Result was not persisted")
	}
	if got := run.Result.Nodes["sink"].Status; got != pipeline.NodeStatusFailed {
		t.Fatalf("sink status = %q, want failed", got)
	}
	if !strings.Contains(run.ErrorMessage, "sink refused batch") {
		t.Fatalf("ErrorMessage = %q, want node failure detail", run.ErrorMessage)
	}
	if !strings.Contains(run.Result.Error, "sink refused batch") {
		t.Fatalf("Result.Error = %q, want node failure detail", run.Result.Error)
	}
}

func scheduledPipelineFixture() *pipeline.Pipeline {
	return &pipeline.Pipeline{
		ID:        "scheduled_dag",
		Name:      "Scheduled DAG",
		Schedule:  "0 * * * *",
		Enabled:   true,
		CreatedBy: "user:operator",
		Inputs: []pipeline.Input{
			{Name: "src", Type: "objectset"},
		},
		Transforms: []pipeline.Transform{
			{Name: "filter", Type: "filter", Inputs: []string{"src"}},
		},
		Outputs: []pipeline.Output{
			{Name: "sink", Type: "jdbc", Input: "filter"},
		},
	}
}
