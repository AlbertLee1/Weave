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
	store pipeline.Store
	now   func() time.Time
}

func newScheduledPipelineRunRecorder(store pipeline.Store) *scheduledPipelineRunRecorder {
	return &scheduledPipelineRunRecorder{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (r *scheduledPipelineRunRecorder) RunPipeline(ctx context.Context, p *pipeline.Pipeline) error {
	if r.store == nil {
		return errors.New("pipeline run recorder: store is nil")
	}
	if p == nil {
		return errors.New("pipeline run recorder: pipeline is nil")
	}
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
