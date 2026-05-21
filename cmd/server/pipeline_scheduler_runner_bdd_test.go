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
