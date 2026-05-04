package timeseries

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DownsampleAggregation enumerates the aggregations a Downsampler must
// support. The names mirror applyResample's `agg` taxonomy so a single
// resample step in a transform chain can be pushed down identically.
type DownsampleAggregation string

const (
	DownsampleAvg   DownsampleAggregation = "avg"
	DownsampleSum   DownsampleAggregation = "sum"
	DownsampleMin   DownsampleAggregation = "min"
	DownsampleMax   DownsampleAggregation = "max"
	DownsampleCount DownsampleAggregation = "count"
)

// NormalizeAggregation maps the resample-style names (including "mean")
// to a canonical DownsampleAggregation. Returns the empty string for
// unsupported names so callers can branch off ok-style return values.
func NormalizeAggregation(name string) (DownsampleAggregation, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "avg", "mean":
		return DownsampleAvg, true
	case "sum":
		return DownsampleSum, true
	case "min":
		return DownsampleMin, true
	case "max":
		return DownsampleMax, true
	case "count":
		return DownsampleCount, true
	default:
		return "", false
	}
}

// DownsampleSpec describes a server-side downsample query. Step is the
// bucket width (e.g. 5*time.Minute, time.Hour); Start/End bound the
// inclusive window. Empty Start/End mean "all time" — the implementation
// expands the bounds to the natural window of the backend.
type DownsampleSpec struct {
	Start       time.Time
	End         time.Time
	Step        time.Duration
	Aggregation DownsampleAggregation
}

// Validate enforces the minimum invariants every Downsampler relies on:
// non-zero positive step, non-empty aggregation, End >= Start when both
// are supplied.
func (s DownsampleSpec) Validate() error {
	if s.Step <= 0 {
		return fmt.Errorf("downsample: step must be positive, got %s", s.Step)
	}
	if s.Aggregation == "" {
		return fmt.Errorf("downsample: aggregation is required")
	}
	if !s.Start.IsZero() && !s.End.IsZero() && s.End.Before(s.Start) {
		return fmt.Errorf("downsample: end %s is before start %s", s.End, s.Start)
	}
	return nil
}

// Downsampler is implemented by Store backends that can push a downsample
// query down to the underlying engine. Wiring is via Go's structural
// typing so an off-tree backend can satisfy it without importing this
// package's marker.
//
// US-435: VictoriaMetrics hosts a per-(series, step) reduce in
// constant-time-on-the-client-side regardless of the underlying point
// count. For a 100M-point series, the response is ~288 buckets at
// step=5m or ~24 buckets at step=1h, so the wire payload is bounded by
// step-vs-window rather than series cardinality.
type Downsampler interface {
	DownsamplePoints(ctx context.Context, key SeriesKey, spec DownsampleSpec) ([]Point, error)
}
