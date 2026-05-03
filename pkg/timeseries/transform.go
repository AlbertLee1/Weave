package timeseries

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// TransformOp identifies one of the five built-in chain transforms.
//
//   - diff      : first-difference y[i] = v[i] - v[i-1] (drops index 0).
//   - sma       : simple moving average over a fixed window.
//   - ema       : exponential moving average with alpha smoothing factor.
//   - resample  : bucketize by interval, aggregate per bucket.
//   - scale     : multiplicative rescale (y = factor * v + offset).
type TransformOp string

const (
	OpDiff     TransformOp = "diff"
	OpSMA      TransformOp = "sma"
	OpEMA      TransformOp = "ema"
	OpResample TransformOp = "resample"
	OpScale    TransformOp = "scale"
)

// TransformSpec describes one step in a transform chain. The unmarshalled
// `params` is intentionally a free-form map; per-op validation lives in
// the Transform implementation so a future op can be added by extending
// applyTransform without changing the wire envelope.
type TransformSpec struct {
	Op     TransformOp            `json:"op"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// ErrInvalidTransform is returned by ApplyChain when a step is malformed
// (unknown op, missing/invalid params, non-numeric input value, etc.).
// Callers — including the HTTP handler — detect this with errors.Is and
// surface the error message verbatim to the client.
var ErrInvalidTransform = errors.New("timeseries: invalid transform")

// ApplyChain runs each TransformSpec sequentially against `points` and
// returns the resulting series. The input slice MUST be sorted by time
// ascending; ApplyChain re-sorts after `resample` because bucket order
// depends on map iteration order. ApplyChain treats nil/empty input
// uniformly: returns an empty slice with no error so a transform chain
// applied to an empty series produces an empty series.
func ApplyChain(points []Point, chain []TransformSpec) ([]Point, error) {
	out := make([]Point, len(points))
	copy(out, points)
	for i, spec := range chain {
		next, err := applyTransform(out, spec)
		if err != nil {
			return nil, fmt.Errorf("%w: step %d (%s): %s", ErrInvalidTransform, i, spec.Op, err.Error())
		}
		out = next
	}
	return out, nil
}

func applyTransform(points []Point, spec TransformSpec) ([]Point, error) {
	switch spec.Op {
	case OpDiff:
		return applyDiff(points)
	case OpSMA:
		return applySMA(points, spec.Params)
	case OpEMA:
		return applyEMA(points, spec.Params)
	case OpResample:
		return applyResample(points, spec.Params)
	case OpScale:
		return applyScale(points, spec.Params)
	default:
		return nil, fmt.Errorf("unknown op %q", spec.Op)
	}
}

// applyDiff produces the first-difference series. Output length is
// max(len-1, 0); the i-th output sample is at points[i+1].Time with
// value points[i+1].Value - points[i].Value. Non-numeric input fails
// with a typed error so callers can surface a clean 400.
func applyDiff(points []Point) ([]Point, error) {
	if len(points) < 2 {
		return []Point{}, nil
	}
	out := make([]Point, 0, len(points)-1)
	prev, err := numericValue(points[0].Value)
	if err != nil {
		return nil, fmt.Errorf("point 0: %w", err)
	}
	for i := 1; i < len(points); i++ {
		cur, err := numericValue(points[i].Value)
		if err != nil {
			return nil, fmt.Errorf("point %d: %w", i, err)
		}
		out = append(out, Point{Time: points[i].Time, Value: cur - prev})
		prev = cur
	}
	return out, nil
}

// applySMA emits a simple moving average with a trailing window. The
// first (window-1) samples are dropped so every output value is computed
// over a full window — matches the standard time-series convention
// (numpy.convolve(mode='valid')) rather than the "expanding window"
// alternative.
func applySMA(points []Point, params map[string]interface{}) ([]Point, error) {
	window, err := intParam(params, "window")
	if err != nil {
		return nil, err
	}
	if window <= 0 {
		return nil, fmt.Errorf("window must be positive, got %d", window)
	}
	if len(points) < window {
		return []Point{}, nil
	}
	values := make([]float64, len(points))
	for i, p := range points {
		v, err := numericValue(p.Value)
		if err != nil {
			return nil, fmt.Errorf("point %d: %w", i, err)
		}
		values[i] = v
	}
	out := make([]Point, 0, len(points)-window+1)
	var sum float64
	for i := 0; i < window; i++ {
		sum += values[i]
	}
	out = append(out, Point{Time: points[window-1].Time, Value: sum / float64(window)})
	for i := window; i < len(values); i++ {
		sum += values[i] - values[i-window]
		out = append(out, Point{Time: points[i].Time, Value: sum / float64(window)})
	}
	return out, nil
}

// applyEMA emits an exponential moving average. alpha ∈ (0, 1] is the
// smoothing factor: higher alpha tracks fresh samples more closely;
// lower alpha smooths more aggressively. The seed is points[0] verbatim
// so the EMA series has the same length as the input.
func applyEMA(points []Point, params map[string]interface{}) ([]Point, error) {
	alpha, err := floatParam(params, "alpha")
	if err != nil {
		return nil, err
	}
	if !(alpha > 0 && alpha <= 1) {
		return nil, fmt.Errorf("alpha must be in (0, 1], got %v", alpha)
	}
	if len(points) == 0 {
		return []Point{}, nil
	}
	out := make([]Point, 0, len(points))
	seed, err := numericValue(points[0].Value)
	if err != nil {
		return nil, fmt.Errorf("point 0: %w", err)
	}
	prev := seed
	out = append(out, Point{Time: points[0].Time, Value: seed})
	for i := 1; i < len(points); i++ {
		cur, err := numericValue(points[i].Value)
		if err != nil {
			return nil, fmt.Errorf("point %d: %w", i, err)
		}
		prev = alpha*cur + (1-alpha)*prev
		out = append(out, Point{Time: points[i].Time, Value: prev})
	}
	return out, nil
}

// applyResample groups input points into buckets of `interval` duration
// (e.g. "1h", "5m", "30s") and emits one output per non-empty bucket
// using the requested aggregation (avg | sum | min | max | count).
// Bucket boundaries align to the UTC epoch (ts = floor(t/interval) *
// interval), giving deterministic bucket alignment regardless of the
// arrival order. Output is sorted by time ascending.
func applyResample(points []Point, params map[string]interface{}) ([]Point, error) {
	intervalRaw, ok := params["interval"]
	if !ok {
		return nil, errors.New("interval is required")
	}
	intervalStr, ok := intervalRaw.(string)
	if !ok {
		return nil, fmt.Errorf("interval must be a duration string, got %T", intervalRaw)
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return nil, fmt.Errorf("interval: %w", err)
	}
	if interval <= 0 {
		return nil, fmt.Errorf("interval must be positive, got %s", interval)
	}
	agg := "avg"
	if v, ok := params["agg"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("agg must be a string, got %T", v)
		}
		agg = strings.ToLower(s)
	}
	switch agg {
	case "avg", "mean", "sum", "min", "max", "count":
	default:
		return nil, fmt.Errorf("unknown agg %q (want avg|sum|min|max|count)", agg)
	}
	if len(points) == 0 {
		return []Point{}, nil
	}

	type bucket struct {
		ts    time.Time
		count int
		sum   float64
		min   float64
		max   float64
	}
	buckets := map[int64]*bucket{}
	for i, p := range points {
		v, err := numericValue(p.Value)
		if err != nil {
			return nil, fmt.Errorf("point %d: %w", i, err)
		}
		bucketStart := p.Time.UTC().Truncate(interval)
		key := bucketStart.UnixNano()
		b, ok := buckets[key]
		if !ok {
			b = &bucket{ts: bucketStart, min: v, max: v}
			buckets[key] = b
		}
		b.count++
		b.sum += v
		if v < b.min {
			b.min = v
		}
		if v > b.max {
			b.max = v
		}
	}

	out := make([]Point, 0, len(buckets))
	for _, b := range buckets {
		var v float64
		switch agg {
		case "avg", "mean":
			v = b.sum / float64(b.count)
		case "sum":
			v = b.sum
		case "min":
			v = b.min
		case "max":
			v = b.max
		case "count":
			v = float64(b.count)
		}
		out = append(out, Point{Time: b.ts, Value: v})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

// applyScale applies y = factor * v + offset to every point. Both
// factor and offset accept any JSON number; missing offset defaults to
// 0. The transform is intended for unit conversion (Celsius→Fahrenheit
// is factor=1.8, offset=32) and visual normalisation.
func applyScale(points []Point, params map[string]interface{}) ([]Point, error) {
	factor, err := floatParam(params, "factor")
	if err != nil {
		return nil, err
	}
	if math.IsNaN(factor) || math.IsInf(factor, 0) {
		return nil, fmt.Errorf("factor must be finite, got %v", factor)
	}
	offset := 0.0
	if _, ok := params["offset"]; ok {
		o, err := floatParam(params, "offset")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(o) || math.IsInf(o, 0) {
			return nil, fmt.Errorf("offset must be finite, got %v", o)
		}
		offset = o
	}
	out := make([]Point, len(points))
	for i, p := range points {
		v, err := numericValue(p.Value)
		if err != nil {
			return nil, fmt.Errorf("point %d: %w", i, err)
		}
		out[i] = Point{Time: p.Time, Value: factor*v + offset}
	}
	return out, nil
}

// numericValue coerces a JSON-decoded value to float64. Mirrors the
// vm_store coerceFloat shape (int/uint/float/json.Number) so a chain
// applied to MemoryStore-sourced points (which preserve int input) and
// VMStore-sourced points (always float64) behaves identically.
func numericValue(v interface{}) (float64, error) {
	if v == nil {
		return 0, errors.New("value is nil")
	}
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int8:
		return float64(x), nil
	case int16:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case uint:
		return float64(x), nil
	case uint8:
		return float64(x), nil
	case uint16:
		return float64(x), nil
	case uint32:
		return float64(x), nil
	case uint64:
		return float64(x), nil
	}
	// json.Number is a string under the hood; try fmt parsing so callers
	// that use a json.Decoder with UseNumber() still feed a numeric path.
	if n, ok := v.(interface{ Float64() (float64, error) }); ok {
		f, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("non-numeric value: %v", err)
		}
		return f, nil
	}
	return 0, fmt.Errorf("non-numeric value: %T", v)
}

func intParam(params map[string]interface{}, name string) (int, error) {
	raw, ok := params[name]
	if !ok {
		return 0, fmt.Errorf("%s is required", name)
	}
	switch v := raw.(type) {
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("%s must be an integer, got %v", name, v)
		}
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	}
	return 0, fmt.Errorf("%s must be a number, got %T", name, raw)
}

func floatParam(params map[string]interface{}, name string) (float64, error) {
	raw, ok := params[name]
	if !ok {
		return 0, fmt.Errorf("%s is required", name)
	}
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	}
	return 0, fmt.Errorf("%s must be a number, got %T", name, raw)
}
