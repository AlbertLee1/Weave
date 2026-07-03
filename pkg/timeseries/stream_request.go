package timeseries

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// StreamPointsRequest is the wire body of Foundry's
// POST .../timeseries/{property}/streamPoints (StreamTimeSeriesPointsRequest).
//
// Both fields are optional; an empty body is a valid request that streams
// the full series (backward compatible with pre-body clients). See
// https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/time-series-properties/stream-points
type StreamPointsRequest struct {
	Range     *TimeRange           `json:"range,omitempty"`
	Aggregate *AggregateTimeSeries `json:"aggregate,omitempty"`
}

// TimeRange is Foundry's absolute-or-relative range discriminator. The
// `type` field selects the variant ("absolute" | "relative"). startTime /
// endTime carry either ISO 8601 timestamps (absolute) or RelativeTime
// objects (relative), so they are decoded lazily against the discriminator.
type TimeRange struct {
	Type      string          `json:"type"`
	StartTime json.RawMessage `json:"startTime,omitempty"`
	EndTime   json.RawMessage `json:"endTime,omitempty"`
}

// RelativeTime is a relative offset from the current moment, e.g. "5 MONTHS
// BEFORE" ({when:BEFORE, value:5, unit:MONTHS}).
type RelativeTime struct {
	When  string `json:"when"`  // RelativeTimeRelation: BEFORE | AFTER
	Value int    `json:"value"` //
	Unit  string `json:"unit"`  // RelativeTimeSeriesTimeUnit
}

// AggregateTimeSeries is Foundry's AggregateTimeSeries: an aggregation
// method plus a windowing strategy.
type AggregateTimeSeries struct {
	Method   string                        `json:"method"`
	Strategy TimeSeriesAggregationStrategy `json:"strategy"`
}

// TimeSeriesAggregationStrategy is the strategy discriminator. Only the
// PERIODIC strategy — which downsamples into fixed-width windows — is
// supported on this single-machine server; it maps onto the shared
// Downsampler. windowSize carries a PreciseDuration for the periodic case.
type TimeSeriesAggregationStrategy struct {
	Type       string           `json:"type"` // rolling | periodic | cumulative
	WindowSize *PreciseDuration `json:"windowSize,omitempty"`
}

// PreciseDuration is Foundry's fixed-width duration {value, unit}. Each day
// is 24 hours and each week is 7 days (per PreciseTimeUnit).
type PreciseDuration struct {
	Value int    `json:"value"`
	Unit  string `json:"unit"`
	Type  string `json:"type,omitempty"`
}

// TimeWindow is a resolved [Start, End) filter. HasStart / HasEnd flag which
// bounds are present; an absent bound is unbounded on that side. Start is
// inclusive and End is exclusive, matching Foundry's AbsoluteTimeRange.
type TimeWindow struct {
	Start    time.Time
	End      time.Time
	HasStart bool
	HasEnd   bool
}

// Contains reports whether t falls inside the window (start inclusive, end
// exclusive). An empty window (no bounds) contains everything.
func (w TimeWindow) Contains(t time.Time) bool {
	if w.HasStart && t.Before(w.Start) {
		return false
	}
	if w.HasEnd && !t.Before(w.End) {
		return false
	}
	return true
}

// Filter returns the subset of points inside the window, preserving order.
// An empty window returns a copy of all points.
func (w TimeWindow) Filter(points []Point) []Point {
	out := make([]Point, 0, len(points))
	for _, p := range points {
		if w.Contains(p.Time) {
			out = append(out, p)
		}
	}
	return out
}

// Resolve turns a RelativeTime into an absolute instant relative to now.
// BEFORE subtracts and AFTER adds; MONTHS / YEARS use calendar arithmetic
// (AddDate) while the sub-month units use fixed durations.
func (rt RelativeTime) Resolve(now time.Time) (time.Time, error) {
	var sign int
	switch strings.ToUpper(strings.TrimSpace(rt.When)) {
	case "BEFORE":
		sign = -1
	case "AFTER":
		sign = 1
	default:
		return time.Time{}, fmt.Errorf("relative time: unknown relation %q (want BEFORE or AFTER)", rt.When)
	}
	n := sign * rt.Value
	switch strings.ToUpper(strings.TrimSpace(rt.Unit)) {
	case "MILLISECONDS":
		return now.Add(time.Duration(n) * time.Millisecond), nil
	case "SECONDS":
		return now.Add(time.Duration(n) * time.Second), nil
	case "MINUTES":
		return now.Add(time.Duration(n) * time.Minute), nil
	case "HOURS":
		return now.Add(time.Duration(n) * time.Hour), nil
	case "DAYS":
		return now.AddDate(0, 0, n), nil
	case "WEEKS":
		return now.AddDate(0, 0, n*7), nil
	case "MONTHS":
		return now.AddDate(0, n, 0), nil
	case "YEARS":
		return now.AddDate(n, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("relative time: unknown unit %q", rt.Unit)
	}
}

// Resolve turns the TimeRange into a concrete TimeWindow. Relative bounds
// are computed against now; absolute bounds parse ISO 8601 timestamps.
func (tr TimeRange) Resolve(now time.Time) (TimeWindow, error) {
	switch strings.ToLower(strings.TrimSpace(tr.Type)) {
	case "absolute":
		return tr.resolveAbsolute()
	case "relative":
		return tr.resolveRelative(now)
	default:
		return TimeWindow{}, fmt.Errorf("range.type must be \"absolute\" or \"relative\", got %q", tr.Type)
	}
}

func (tr TimeRange) resolveAbsolute() (TimeWindow, error) {
	var w TimeWindow
	start, hasStart, err := parseAbsoluteBound(tr.StartTime)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("range.startTime: %w", err)
	}
	w.Start, w.HasStart = start, hasStart
	end, hasEnd, err := parseAbsoluteBound(tr.EndTime)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("range.endTime: %w", err)
	}
	w.End, w.HasEnd = end, hasEnd
	if w.HasStart && w.HasEnd && w.End.Before(w.Start) {
		return TimeWindow{}, fmt.Errorf("range.endTime %s is before startTime %s", w.End, w.Start)
	}
	return w, nil
}

func (tr TimeRange) resolveRelative(now time.Time) (TimeWindow, error) {
	var w TimeWindow
	start, hasStart, err := parseRelativeBound(tr.StartTime, now)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("range.startTime: %w", err)
	}
	w.Start, w.HasStart = start, hasStart
	end, hasEnd, err := parseRelativeBound(tr.EndTime, now)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("range.endTime: %w", err)
	}
	w.End, w.HasEnd = end, hasEnd
	if w.HasStart && w.HasEnd && w.End.Before(w.Start) {
		return TimeWindow{}, fmt.Errorf("range.endTime %s is before startTime %s", w.End, w.Start)
	}
	return w, nil
}

func parseAbsoluteBound(raw json.RawMessage) (time.Time, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, false, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return time.Time{}, false, fmt.Errorf("must be an ISO 8601 string: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid timestamp %q: %w", s, err)
	}
	return t, true, nil
}

func parseRelativeBound(raw json.RawMessage, now time.Time) (time.Time, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, false, nil
	}
	var rt RelativeTime
	if err := json.Unmarshal(raw, &rt); err != nil {
		return time.Time{}, false, fmt.Errorf("must be a RelativeTime object: %w", err)
	}
	t, err := rt.Resolve(now)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

// Resolve maps the AggregateTimeSeries onto a DownsampleSpec. Only the
// PERIODIC strategy is supported (it downsamples into fixed-width buckets);
// ROLLING / CUMULATIVE are rejected. The method maps onto the shared
// DownsampleAggregation taxonomy; statistical methods with no bucket-reduce
// equivalent (STANDARD_DEVIATION, PERCENT_CHANGE, DIFFERENCE, PRODUCT) are
// rejected. Start / End are left zero for the caller to fill from any range.
func (a AggregateTimeSeries) Resolve() (DownsampleSpec, error) {
	agg, ok := methodToAggregation(a.Method)
	if !ok {
		return DownsampleSpec{}, fmt.Errorf("aggregate.method %q is not supported (want SUM, MEAN, MIN, MAX, COUNT, FIRST, or LAST)", a.Method)
	}
	switch strings.ToLower(strings.TrimSpace(a.Strategy.Type)) {
	case "periodic":
		// handled below
	case "rolling", "cumulative":
		return DownsampleSpec{}, fmt.Errorf("aggregate.strategy %q is not supported on this server (only periodic)", a.Strategy.Type)
	default:
		return DownsampleSpec{}, fmt.Errorf("aggregate.strategy.type must be \"periodic\", got %q", a.Strategy.Type)
	}
	if a.Strategy.WindowSize == nil {
		return DownsampleSpec{}, fmt.Errorf("aggregate.strategy.windowSize is required for periodic aggregation")
	}
	step, err := a.Strategy.WindowSize.Duration()
	if err != nil {
		return DownsampleSpec{}, fmt.Errorf("aggregate.strategy.windowSize: %w", err)
	}
	spec := DownsampleSpec{Step: step, Aggregation: agg}
	if err := spec.Validate(); err != nil {
		return DownsampleSpec{}, err
	}
	return spec, nil
}

// Duration converts a PreciseDuration to a time.Duration. All PreciseTimeUnit
// values are fixed-width (a day is 24h, a week is 7 days).
func (d PreciseDuration) Duration() (time.Duration, error) {
	if d.Value <= 0 {
		return 0, fmt.Errorf("value must be positive, got %d", d.Value)
	}
	var unit time.Duration
	switch strings.ToUpper(strings.TrimSpace(d.Unit)) {
	case "NANOSECONDS":
		unit = time.Nanosecond
	case "SECONDS":
		unit = time.Second
	case "MINUTES":
		unit = time.Minute
	case "HOURS":
		unit = time.Hour
	case "DAYS":
		unit = 24 * time.Hour
	case "WEEKS":
		unit = 7 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("unknown unit %q", d.Unit)
	}
	return time.Duration(d.Value) * unit, nil
}

func methodToAggregation(method string) (DownsampleAggregation, bool) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "MEAN":
		return DownsampleAvg, true
	case "SUM":
		return DownsampleSum, true
	case "MIN":
		return DownsampleMin, true
	case "MAX":
		return DownsampleMax, true
	case "COUNT":
		return DownsampleCount, true
	case "FIRST":
		return DownsampleFirst, true
	case "LAST":
		return DownsampleLast, true
	default:
		return "", false
	}
}

// downsampleBucket accumulates the running aggregates for one epoch-aligned
// window during an in-memory reduce.
type downsampleBucket struct {
	ts     time.Time
	count  int
	sum    float64
	min    float64
	max    float64
	firstT time.Time
	firstV float64
	lastT  time.Time
	lastV  float64
}

func (b *downsampleBucket) add(t time.Time, v float64) {
	b.count++
	b.sum += v
	if v < b.min {
		b.min = v
	}
	if v > b.max {
		b.max = v
	}
	if t.Before(b.firstT) {
		b.firstT, b.firstV = t, v
	}
	if !t.Before(b.lastT) {
		b.lastT, b.lastV = t, v
	}
}

func (b *downsampleBucket) reduce(agg DownsampleAggregation) (float64, error) {
	switch agg {
	case DownsampleAvg:
		return b.sum / float64(b.count), nil
	case DownsampleSum:
		return b.sum, nil
	case DownsampleMin:
		return b.min, nil
	case DownsampleMax:
		return b.max, nil
	case DownsampleCount:
		return float64(b.count), nil
	case DownsampleFirst:
		return b.firstV, nil
	case DownsampleLast:
		return b.lastV, nil
	default:
		return 0, fmt.Errorf("downsample: unsupported aggregation %q", agg)
	}
}

// withinSpec reports whether t is inside spec's [Start, End) window; a zero
// bound means unbounded on that side.
func withinSpec(spec DownsampleSpec, t time.Time) bool {
	if !spec.Start.IsZero() && t.Before(spec.Start) {
		return false
	}
	if !spec.End.IsZero() && !t.Before(spec.End) {
		return false
	}
	return true
}

// DownsampleInMemory reduces points into epoch-aligned buckets of width
// spec.Step, applying spec.Aggregation per bucket. It is the fallback used
// when the configured Store does not implement Downsampler. Points outside
// spec.Start / spec.End (start inclusive, end exclusive; zero means
// unbounded) are dropped before bucketing. Output is sorted by time
// ascending. Non-numeric values yield ErrNonNumericValue.
func DownsampleInMemory(points []Point, spec DownsampleSpec) ([]Point, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	buckets := map[int64]*downsampleBucket{}
	for _, p := range points {
		if !withinSpec(spec, p.Time) {
			continue
		}
		v, err := numericValue(p.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNonNumericValue, err)
		}
		bucketStart := p.Time.UTC().Truncate(spec.Step)
		key := bucketStart.UnixNano()
		b, ok := buckets[key]
		if !ok {
			b = &downsampleBucket{ts: bucketStart, min: v, max: v, firstT: p.Time, firstV: v, lastT: p.Time, lastV: v}
			buckets[key] = b
		}
		b.add(p.Time, v)
	}
	out := make([]Point, 0, len(buckets))
	for _, b := range buckets {
		v, err := b.reduce(spec.Aggregation)
		if err != nil {
			return nil, err
		}
		out = append(out, Point{Time: b.ts, Value: v})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}
