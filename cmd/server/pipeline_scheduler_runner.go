package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liyang/weave/pkg/pipeline"
)

var errScheduledPipelineRuntimeNotConfigured = errors.New("pipeline runtime not configured")

type scheduledPipelineRunRecorder struct {
	store      pipeline.Store
	nodeRunner pipeline.NodeRunner
	now        func() time.Time
}

func newScheduledPipelineRunRecorder(store pipeline.Store) *scheduledPipelineRunRecorder {
	return &scheduledPipelineRunRecorder{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func newScheduledPipelineDAGRunRecorder(store pipeline.Store, nodeRunner pipeline.NodeRunner) *scheduledPipelineRunRecorder {
	recorder := newScheduledPipelineRunRecorder(store)
	recorder.nodeRunner = nodeRunner
	return recorder
}

func (r *scheduledPipelineRunRecorder) RunPipeline(ctx context.Context, p *pipeline.Pipeline) error {
	if r.store == nil {
		return errors.New("pipeline run recorder: store is nil")
	}
	if p == nil {
		return errors.New("pipeline run recorder: pipeline is nil")
	}
	if r.nodeRunner != nil {
		return r.runDAGAndRecord(ctx, p)
	}
	return r.recordRuntimeNotConfigured(ctx, p)
}

func (r *scheduledPipelineRunRecorder) recordRuntimeNotConfigured(ctx context.Context, p *pipeline.Pipeline) error {
	started := r.now()
	finished := r.now()
	run := &pipeline.PipelineRun{
		PipelineID:   p.ID,
		Status:       "failed",
		StartedAt:    started,
		FinishedAt:   &finished,
		ErrorMessage: errScheduledPipelineRuntimeNotConfigured.Error(),
		TriggeredBy:  "schedule",
	}
	if err := r.store.AppendPipelineRun(ctx, run); err != nil {
		return fmt.Errorf("record scheduled pipeline run: %w", err)
	}
	return errScheduledPipelineRuntimeNotConfigured
}

func (r *scheduledPipelineRunRecorder) runDAGAndRecord(ctx context.Context, p *pipeline.Pipeline) error {
	result, runErr := pipeline.RunDAG(ctx, p, pipeline.RunOptions{
		Runner: r.nodeRunner,
		Now:    r.now,
	})
	started := r.now()
	finished := r.now()
	status := "failed"
	errorMessage := ""
	if result != nil {
		started = result.StartedAt
		finished = result.FinishedAt
		status = result.Status
		errorMessage = result.Error
	}
	if runErr != nil && errorMessage == "" {
		errorMessage = runErr.Error()
	}
	run := &pipeline.PipelineRun{
		PipelineID:   p.ID,
		Status:       status,
		StartedAt:    started,
		FinishedAt:   &finished,
		ErrorMessage: errorMessage,
		Result:       result,
		TriggeredBy:  "schedule",
	}
	if err := r.store.AppendPipelineRun(ctx, run); err != nil {
		return fmt.Errorf("record scheduled pipeline run: %w", err)
	}
	return runErr
}
