package modelmesh

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ModelExecutor is the per-node side-effecting hook the runner invokes
// for each ModelNode. The wiring story in cmd/server/main.go is
// expected to plug this in over the existing Function dispatcher (the
// VTX-049 funcruntime client for Python-runtime functions, the
// VTX-050 HTTP-runtime wrapper for live model deployments). Returning
// a non-nil error short-circuits the whole mesh: any sibling already
// in flight is allowed to finish, but no further layers run.
type ModelExecutor func(ctx context.Context, m ModelNode) error

// RunResult captures one model's execution outcome. Started/Completed
// are populated even when Err is non-nil so the caller can attribute
// latency to the offending node. Results are returned in topological
// order (layer 0 first); within a layer, the sibling that finished
// first appears first.
type RunResult struct {
	ModelID   string    `json:"modelId"`
	Started   time.Time `json:"started"`
	Completed time.Time `json:"completed"`
	Err       error     `json:"-"`
	ErrMsg    string    `json:"error,omitempty"`
}

// Runner coordinates layer-by-layer execution of a model mesh. The
// zero value is usable: Concurrency=0 falls back to GOMAXPROCS-style
// "unbounded within a layer" by sizing the worker pool to the layer
// width. Setting Concurrency>0 caps the number of in-flight model
// invocations per layer, useful for protecting downstream Function
// runtimes from a thundering herd.
type Runner struct {
	Concurrency int
}

// Run plans `models` into topological layers and executes each layer
// concurrently. On the first error within any layer, Run drains the
// remaining sibling executions in that layer (so caller-supplied
// metrics see consistent timing), then returns without descending
// into downstream layers. The returned slice contains one RunResult
// per executed model; nodes in skipped downstream layers are absent.
func (r *Runner) Run(ctx context.Context, models []ModelNode, exec ModelExecutor) ([]RunResult, error) {
	if exec == nil {
		return nil, errors.New("modelmesh: executor is required")
	}
	layers, err := TopologicalLayers(models)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ModelNode, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	var results []RunResult
	for _, layer := range layers {
		layerResults, layerErr := r.runLayer(ctx, layer, byID, exec)
		results = append(results, layerResults...)
		if layerErr != nil {
			return results, layerErr
		}
	}
	return results, nil
}

// runLayer fans the layer out across the worker pool, waits for every
// worker, then returns the results in completion order alongside the
// first error encountered (if any). Subsequent errors within the same
// layer are dropped — they're effectively redundant once the run is
// already aborting downstream.
func (r *Runner) runLayer(ctx context.Context, layer Layer, byID map[string]ModelNode, exec ModelExecutor) ([]RunResult, error) {
	concurrency := r.Concurrency
	if concurrency <= 0 || concurrency > len(layer) {
		concurrency = len(layer)
	}

	type slot struct {
		idx int
		res RunResult
	}
	out := make(chan slot, len(layer))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for idx, id := range layer {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, id string) {
			defer wg.Done()
			defer func() { <-sem }()
			node := byID[id]
			started := time.Now()
			err := exec(ctx, node)
			completed := time.Now()
			res := RunResult{
				ModelID:   id,
				Started:   started,
				Completed: completed,
				Err:       err,
			}
			if err != nil {
				res.ErrMsg = err.Error()
			}
			out <- slot{idx: idx, res: res}
		}(idx, id)
	}
	wg.Wait()
	close(out)

	collected := make([]RunResult, 0, len(layer))
	var firstErr error
	for s := range out {
		collected = append(collected, s.res)
		if firstErr == nil && s.res.Err != nil {
			firstErr = fmt.Errorf("modelmesh: model %q failed: %w", s.res.ModelID, s.res.Err)
		}
	}
	return collected, firstErr
}
