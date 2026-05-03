package timeseries

import (
	"errors"
	"math"
	"testing"
	"time"
)

// mustTime is a tiny helper so the test tables stay readable.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return ts
}

func pointsAt(times []time.Time, values []float64) []Point {
	out := make([]Point, len(times))
	for i := range times {
		out[i] = Point{Time: times[i], Value: values[i]}
	}
	return out
}

func TestApplyChain_DiffBasic(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	pts := []Point{
		{Time: t0, Value: 10.0},
		{Time: t0.Add(time.Minute), Value: 14.0},
		{Time: t0.Add(2 * time.Minute), Value: 11.0},
	}
	out, err := ApplyChain(pts, []TransformSpec{{Op: OpDiff}})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if got := out[0].Value.(float64); got != 4.0 {
		t.Errorf("[0] got %v want 4", got)
	}
	if got := out[1].Value.(float64); got != -3.0 {
		t.Errorf("[1] got %v want -3", got)
	}
	if !out[0].Time.Equal(t0.Add(time.Minute)) {
		t.Errorf("diff drops point 0 — output[0].Time should be input[1].Time")
	}
}

func TestApplyChain_DiffEmpty(t *testing.T) {
	out, err := ApplyChain(nil, []TransformSpec{{Op: OpDiff}})
	if err != nil {
		t.Fatalf("diff(nil): %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len=%d want 0", len(out))
	}
	out, err = ApplyChain([]Point{{Time: time.Now(), Value: 1.0}}, []TransformSpec{{Op: OpDiff}})
	if err != nil {
		t.Fatalf("diff(single): %v", err)
	}
	if len(out) != 0 {
		t.Errorf("diff on single-point: len=%d want 0", len(out))
	}
}

func TestApplyChain_SMA(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	times := make([]time.Time, 5)
	for i := range times {
		times[i] = t0.Add(time.Duration(i) * time.Minute)
	}
	pts := pointsAt(times, []float64{1, 2, 3, 4, 5})
	out, err := ApplyChain(pts, []TransformSpec{{Op: OpSMA, Params: map[string]interface{}{"window": 3.0}}})
	if err != nil {
		t.Fatalf("sma: %v", err)
	}
	want := []float64{2, 3, 4}
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	for i, p := range out {
		if got := p.Value.(float64); math.Abs(got-want[i]) > 1e-9 {
			t.Errorf("[%d] got %v want %v", i, got, want[i])
		}
	}
	if !out[0].Time.Equal(times[2]) {
		t.Errorf("first SMA output anchors at input[window-1].Time")
	}
}

func TestApplyChain_SMAValidatesWindow(t *testing.T) {
	pts := []Point{{Time: time.Now(), Value: 1.0}}
	for _, params := range []map[string]interface{}{
		{},                    // missing window
		{"window": 0.0},       // zero
		{"window": -1.0},      // negative
		{"window": 1.5},       // non-integer
		{"window": "abc"},     // wrong type
	} {
		if _, err := ApplyChain(pts, []TransformSpec{{Op: OpSMA, Params: params}}); err == nil {
			t.Errorf("sma params=%v should fail", params)
		} else if !errors.Is(err, ErrInvalidTransform) {
			t.Errorf("err=%v should wrap ErrInvalidTransform", err)
		}
	}
}

func TestApplyChain_EMA(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	pts := []Point{
		{Time: t0, Value: 10.0},
		{Time: t0.Add(time.Minute), Value: 20.0},
		{Time: t0.Add(2 * time.Minute), Value: 30.0},
	}
	out, err := ApplyChain(pts, []TransformSpec{{Op: OpEMA, Params: map[string]interface{}{"alpha": 0.5}}})
	if err != nil {
		t.Fatalf("ema: %v", err)
	}
	// alpha=0.5: y[0]=10; y[1]=15; y[2]=22.5
	want := []float64{10, 15, 22.5}
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	for i, p := range out {
		if got := p.Value.(float64); math.Abs(got-want[i]) > 1e-9 {
			t.Errorf("[%d] got %v want %v", i, got, want[i])
		}
	}
}

func TestApplyChain_EMAValidatesAlpha(t *testing.T) {
	pts := []Point{{Time: time.Now(), Value: 1.0}}
	for _, alpha := range []interface{}{0.0, -0.1, 1.1, "abc"} {
		params := map[string]interface{}{"alpha": alpha}
		if _, err := ApplyChain(pts, []TransformSpec{{Op: OpEMA, Params: params}}); err == nil {
			t.Errorf("ema alpha=%v should fail", alpha)
		}
	}
	if _, err := ApplyChain(pts, []TransformSpec{{Op: OpEMA}}); err == nil {
		t.Errorf("ema with no alpha should fail")
	}
}

func TestApplyChain_ResampleAvg(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	pts := []Point{
		{Time: t0, Value: 10.0},
		{Time: t0.Add(20 * time.Second), Value: 20.0}, // same minute as t0
		{Time: t0.Add(70 * time.Second), Value: 30.0}, // next minute
		{Time: t0.Add(80 * time.Second), Value: 40.0}, // next minute
	}
	out, err := ApplyChain(pts, []TransformSpec{{Op: OpResample, Params: map[string]interface{}{
		"interval": "1m",
		"agg":      "avg",
	}}})
	if err != nil {
		t.Fatalf("resample: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if got := out[0].Value.(float64); got != 15.0 {
		t.Errorf("bucket0 avg got %v want 15", got)
	}
	if got := out[1].Value.(float64); got != 35.0 {
		t.Errorf("bucket1 avg got %v want 35", got)
	}
	// First bucket aligned to t0 (already at minute boundary) since
	// truncate(t0, 1m) == t0.
	if !out[0].Time.Equal(t0) {
		t.Errorf("bucket0 time got %v want %v", out[0].Time, t0)
	}
	if !out[1].Time.Equal(t0.Add(time.Minute)) {
		t.Errorf("bucket1 time got %v want %v", out[1].Time, t0.Add(time.Minute))
	}
}

func TestApplyChain_ResampleAggregations(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	// Two buckets, three points each, values 1/2/3 then 4/5/6.
	pts := []Point{
		{Time: t0, Value: 1.0},
		{Time: t0.Add(10 * time.Second), Value: 2.0},
		{Time: t0.Add(20 * time.Second), Value: 3.0},
		{Time: t0.Add(60 * time.Second), Value: 4.0},
		{Time: t0.Add(70 * time.Second), Value: 5.0},
		{Time: t0.Add(80 * time.Second), Value: 6.0},
	}
	cases := map[string][2]float64{
		"sum":   {6, 15},
		"min":   {1, 4},
		"max":   {3, 6},
		"count": {3, 3},
	}
	for agg, want := range cases {
		out, err := ApplyChain(pts, []TransformSpec{{Op: OpResample, Params: map[string]interface{}{
			"interval": "1m",
			"agg":      agg,
		}}})
		if err != nil {
			t.Errorf("resample %s: %v", agg, err)
			continue
		}
		if len(out) != 2 {
			t.Errorf("resample %s len=%d want 2", agg, len(out))
			continue
		}
		for i, w := range want {
			if got := out[i].Value.(float64); got != w {
				t.Errorf("resample %s bucket%d got %v want %v", agg, i, got, w)
			}
		}
	}
}

func TestApplyChain_ResampleValidates(t *testing.T) {
	pts := []Point{{Time: time.Now(), Value: 1.0}}
	for _, params := range []map[string]interface{}{
		{},                                        // missing interval
		{"interval": 5},                           // wrong type
		{"interval": "abc"},                       // unparseable
		{"interval": "0s"},                        // non-positive
		{"interval": "1m", "agg": "median"},       // unknown agg
		{"interval": "1m", "agg": 5},              // wrong agg type
	} {
		if _, err := ApplyChain(pts, []TransformSpec{{Op: OpResample, Params: params}}); err == nil {
			t.Errorf("resample params=%v should fail", params)
		}
	}
}

func TestApplyChain_Scale(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	pts := []Point{
		{Time: t0, Value: 0.0},
		{Time: t0.Add(time.Minute), Value: 100.0},
	}
	// Celsius -> Fahrenheit: 0->32, 100->212.
	out, err := ApplyChain(pts, []TransformSpec{{Op: OpScale, Params: map[string]interface{}{
		"factor": 1.8,
		"offset": 32.0,
	}}})
	if err != nil {
		t.Fatalf("scale: %v", err)
	}
	if got := out[0].Value.(float64); math.Abs(got-32) > 1e-9 {
		t.Errorf("[0] got %v want 32", got)
	}
	if got := out[1].Value.(float64); math.Abs(got-212) > 1e-9 {
		t.Errorf("[1] got %v want 212", got)
	}
}

func TestApplyChain_ScaleDefaultOffset(t *testing.T) {
	pts := []Point{{Time: time.Now(), Value: 5.0}}
	out, err := ApplyChain(pts, []TransformSpec{{Op: OpScale, Params: map[string]interface{}{"factor": 2.0}}})
	if err != nil {
		t.Fatalf("scale: %v", err)
	}
	if got := out[0].Value.(float64); got != 10.0 {
		t.Errorf("got %v want 10", got)
	}
}

func TestApplyChain_ScaleValidates(t *testing.T) {
	pts := []Point{{Time: time.Now(), Value: 1.0}}
	for _, params := range []map[string]interface{}{
		{},                                          // missing factor
		{"factor": "abc"},                           // wrong type
		{"factor": math.Inf(1)},                     // non-finite
		{"factor": math.NaN()},                      // NaN
		{"factor": 1.0, "offset": math.Inf(-1)},     // non-finite offset
	} {
		if _, err := ApplyChain(pts, []TransformSpec{{Op: OpScale, Params: params}}); err == nil {
			t.Errorf("scale params=%v should fail", params)
		}
	}
}

func TestApplyChain_Chained(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	pts := []Point{
		{Time: t0, Value: 10.0},
		{Time: t0.Add(time.Minute), Value: 20.0},
		{Time: t0.Add(2 * time.Minute), Value: 30.0},
		{Time: t0.Add(3 * time.Minute), Value: 40.0},
	}
	// Chain: scale x2 then diff. After scale: [20, 40, 60, 80]. After
	// diff: [20, 20, 20].
	out, err := ApplyChain(pts, []TransformSpec{
		{Op: OpScale, Params: map[string]interface{}{"factor": 2.0}},
		{Op: OpDiff},
	})
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	for i, p := range out {
		if got := p.Value.(float64); got != 20.0 {
			t.Errorf("[%d] got %v want 20", i, got)
		}
	}
}

func TestApplyChain_UnknownOp(t *testing.T) {
	pts := []Point{{Time: time.Now(), Value: 1.0}}
	_, err := ApplyChain(pts, []TransformSpec{{Op: TransformOp("ohnoes")}})
	if err == nil {
		t.Fatalf("expected error for unknown op")
	}
	if !errors.Is(err, ErrInvalidTransform) {
		t.Errorf("err=%v should wrap ErrInvalidTransform", err)
	}
}

func TestApplyChain_NonNumericValueRejected(t *testing.T) {
	pts := []Point{
		{Time: time.Now(), Value: "hello"},
	}
	_, err := ApplyChain(pts, []TransformSpec{{Op: OpScale, Params: map[string]interface{}{"factor": 2.0}}})
	if err == nil {
		t.Fatalf("expected error for non-numeric input")
	}
	if !errors.Is(err, ErrInvalidTransform) {
		t.Errorf("err=%v should wrap ErrInvalidTransform", err)
	}
}

func TestApplyChain_EmptyChainReturnsCopy(t *testing.T) {
	t0 := mustTime(t, "2026-04-01T00:00:00Z")
	pts := []Point{{Time: t0, Value: 1.0}}
	out, err := ApplyChain(pts, nil)
	if err != nil {
		t.Fatalf("empty chain: %v", err)
	}
	if len(out) != 1 || out[0].Value.(float64) != 1.0 {
		t.Errorf("empty chain should round-trip input")
	}
	// Mutating the output should not touch the input — ApplyChain copies.
	out[0].Value = 99.0
	if pts[0].Value.(float64) != 1.0 {
		t.Errorf("ApplyChain must defensively copy the input slice")
	}
}

func TestApplyChain_ResampleTruncatesUTCAligned(t *testing.T) {
	// Truncate(1h) aligns to UTC epoch hour boundaries. Confirm a point
	// arriving at 00:30 lands in the bucket starting at 00:00.
	t0 := mustTime(t, "2026-04-01T00:30:00Z")
	pts := []Point{
		{Time: t0, Value: 5.0},
		{Time: t0.Add(15 * time.Minute), Value: 10.0},
	}
	out, err := ApplyChain(pts, []TransformSpec{{Op: OpResample, Params: map[string]interface{}{
		"interval": "1h",
		"agg":      "avg",
	}}})
	if err != nil {
		t.Fatalf("resample: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len=%d want 1", len(out))
	}
	wantBucket := mustTime(t, "2026-04-01T00:00:00Z")
	if !out[0].Time.Equal(wantBucket) {
		t.Errorf("bucket got %v want %v", out[0].Time, wantBucket)
	}
	if got := out[0].Value.(float64); got != 7.5 {
		t.Errorf("avg got %v want 7.5", got)
	}
}
