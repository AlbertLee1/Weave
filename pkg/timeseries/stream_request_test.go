package timeseries

import (
	"encoding/json"
	"testing"
	"time"
)

// mustTime is defined in transform_test.go (same package).

// TestRelativeTimeResolve pins the Foundry RelativeTime semantics: BEFORE
// subtracts, AFTER adds, and each RelativeTimeSeriesTimeUnit maps to a
// calendar-correct offset (months/years use AddDate, not fixed durations).
func TestRelativeTimeResolve(t *testing.T) {
	now := mustTime(t, "2026-07-03T12:00:00Z")
	cases := []struct {
		name    string
		rt      RelativeTime
		want    time.Time
		wantErr bool
	}{
		{"before-5-months", RelativeTime{When: "BEFORE", Value: 5, Unit: "MONTHS"}, mustTime(t, "2026-02-03T12:00:00Z"), false},
		{"after-2-hours", RelativeTime{When: "AFTER", Value: 2, Unit: "HOURS"}, mustTime(t, "2026-07-03T14:00:00Z"), false},
		{"before-30-seconds", RelativeTime{When: "BEFORE", Value: 30, Unit: "SECONDS"}, mustTime(t, "2026-07-03T11:59:30Z"), false},
		{"before-1-week", RelativeTime{When: "BEFORE", Value: 1, Unit: "WEEKS"}, mustTime(t, "2026-06-26T12:00:00Z"), false},
		{"before-90-days", RelativeTime{When: "BEFORE", Value: 90, Unit: "DAYS"}, now.AddDate(0, 0, -90), false},
		{"after-1-year", RelativeTime{When: "AFTER", Value: 1, Unit: "YEARS"}, mustTime(t, "2027-07-03T12:00:00Z"), false},
		{"before-500-millis", RelativeTime{When: "BEFORE", Value: 500, Unit: "MILLISECONDS"}, now.Add(-500 * time.Millisecond), false},
		{"after-15-minutes", RelativeTime{When: "AFTER", Value: 15, Unit: "MINUTES"}, mustTime(t, "2026-07-03T12:15:00Z"), false},
		{"bad-when", RelativeTime{When: "SIDEWAYS", Value: 1, Unit: "HOURS"}, time.Time{}, true},
		{"bad-unit", RelativeTime{When: "BEFORE", Value: 1, Unit: "FORTNIGHTS"}, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.rt.Resolve(now)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTimeRangeResolveAbsolute checks the absolute discriminator resolves
// to an inclusive-start / exclusive-end window, tolerates partial bounds,
// and rejects malformed timestamps and missing discriminators.
func TestTimeRangeResolveAbsolute(t *testing.T) {
	now := mustTime(t, "2026-07-03T12:00:00Z")

	full := TimeRange{}
	if err := json.Unmarshal([]byte(`{"type":"absolute","startTime":"2026-04-02T00:00:00Z","endTime":"2026-04-03T00:00:00Z"}`), &full); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	w, err := full.Resolve(now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !w.HasStart || !w.HasEnd {
		t.Fatalf("want both bounds set, got %+v", w)
	}
	if !w.Start.Equal(mustTime(t, "2026-04-02T00:00:00Z")) || !w.End.Equal(mustTime(t, "2026-04-03T00:00:00Z")) {
		t.Errorf("window = %+v", w)
	}
	// Start inclusive, end exclusive.
	if !w.Contains(mustTime(t, "2026-04-02T00:00:00Z")) {
		t.Error("start must be inclusive")
	}
	if w.Contains(mustTime(t, "2026-04-03T00:00:00Z")) {
		t.Error("end must be exclusive")
	}

	startOnly := TimeRange{}
	_ = json.Unmarshal([]byte(`{"type":"absolute","startTime":"2026-04-02T00:00:00Z"}`), &startOnly)
	w, err = startOnly.Resolve(now)
	if err != nil {
		t.Fatalf("resolve start-only: %v", err)
	}
	if !w.HasStart || w.HasEnd {
		t.Errorf("want start-only window, got %+v", w)
	}

	bad := TimeRange{}
	_ = json.Unmarshal([]byte(`{"type":"absolute","startTime":"not-a-time"}`), &bad)
	if _, err := bad.Resolve(now); err == nil {
		t.Error("expected error for malformed timestamp")
	}

	noType := TimeRange{}
	_ = json.Unmarshal([]byte(`{"startTime":"2026-04-02T00:00:00Z"}`), &noType)
	if _, err := noType.Resolve(now); err == nil {
		t.Error("expected error for missing discriminator")
	}
}

// TestTimeRangeResolveRelative resolves a relative window against a fixed
// now and confirms partial bounds and bad units error out.
func TestTimeRangeResolveRelative(t *testing.T) {
	now := mustTime(t, "2026-07-03T12:00:00Z")

	rel := TimeRange{}
	if err := json.Unmarshal([]byte(`{"type":"relative","startTime":{"when":"BEFORE","value":6,"unit":"MONTHS"}}`), &rel); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	w, err := rel.Resolve(now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !w.HasStart || w.HasEnd {
		t.Fatalf("want start-only relative window, got %+v", w)
	}
	if !w.Start.Equal(now.AddDate(0, -6, 0)) {
		t.Errorf("start = %v, want %v", w.Start, now.AddDate(0, -6, 0))
	}

	both := TimeRange{}
	_ = json.Unmarshal([]byte(`{"type":"relative","startTime":{"when":"BEFORE","value":2,"unit":"HOURS"},"endTime":{"when":"BEFORE","value":1,"unit":"HOURS"}}`), &both)
	w, err = both.Resolve(now)
	if err != nil {
		t.Fatalf("resolve both: %v", err)
	}
	if !w.Start.Equal(now.Add(-2*time.Hour)) || !w.End.Equal(now.Add(-time.Hour)) {
		t.Errorf("window = %+v", w)
	}

	badUnit := TimeRange{}
	_ = json.Unmarshal([]byte(`{"type":"relative","startTime":{"when":"BEFORE","value":1,"unit":"FORTNIGHTS"}}`), &badUnit)
	if _, err := badUnit.Resolve(now); err == nil {
		t.Error("expected error for bad unit")
	}
}

// TestAggregateResolve maps the Foundry periodic strategy + method onto a
// DownsampleSpec, and rejects unsupported strategies/methods.
func TestAggregateResolve(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantStep time.Duration
		wantAgg  DownsampleAggregation
		wantErr  bool
	}{
		{"mean-1-day", `{"method":"MEAN","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"DAYS","type":"duration"}}}`, 24 * time.Hour, DownsampleAvg, false},
		{"sum-5-min", `{"method":"SUM","strategy":{"type":"periodic","windowSize":{"value":5,"unit":"MINUTES","type":"duration"}}}`, 5 * time.Minute, DownsampleSum, false},
		{"min-1-hour", `{"method":"MIN","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"HOURS","type":"duration"}}}`, time.Hour, DownsampleMin, false},
		{"max-2-weeks", `{"method":"MAX","strategy":{"type":"periodic","windowSize":{"value":2,"unit":"WEEKS","type":"duration"}}}`, 2 * 7 * 24 * time.Hour, DownsampleMax, false},
		{"count-30-sec", `{"method":"COUNT","strategy":{"type":"periodic","windowSize":{"value":30,"unit":"SECONDS","type":"duration"}}}`, 30 * time.Second, DownsampleCount, false},
		{"first-1-day", `{"method":"FIRST","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"DAYS","type":"duration"}}}`, 24 * time.Hour, DownsampleFirst, false},
		{"last-1-day", `{"method":"LAST","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"DAYS","type":"duration"}}}`, 24 * time.Hour, DownsampleLast, false},
		{"unsupported-method", `{"method":"STANDARD_DEVIATION","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"DAYS","type":"duration"}}}`, 0, "", true},
		{"unsupported-strategy-rolling", `{"method":"MEAN","strategy":{"type":"rolling","windowSize":{"type":"pointsCount","count":5}}}`, 0, "", true},
		{"unsupported-strategy-cumulative", `{"method":"MEAN","strategy":{"type":"cumulative"}}`, 0, "", true},
		{"missing-window", `{"method":"MEAN","strategy":{"type":"periodic"}}`, 0, "", true},
		{"zero-window", `{"method":"MEAN","strategy":{"type":"periodic","windowSize":{"value":0,"unit":"DAYS","type":"duration"}}}`, 0, "", true},
		{"bad-window-unit", `{"method":"MEAN","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"EONS","type":"duration"}}}`, 0, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a AggregateTimeSeries
			if err := json.Unmarshal([]byte(tc.body), &a); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			spec, err := a.Resolve()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if spec.Step != tc.wantStep {
				t.Errorf("step = %v, want %v", spec.Step, tc.wantStep)
			}
			if spec.Aggregation != tc.wantAgg {
				t.Errorf("agg = %v, want %v", spec.Aggregation, tc.wantAgg)
			}
		})
	}
}

// TestDownsampleInMemory verifies the in-process bucket reduce used as the
// fallback when the store does not implement Downsampler. Buckets align to
// the UTC epoch, matching applyResample and the PG/VM backends.
func TestDownsampleInMemory(t *testing.T) {
	points := []Point{
		{Time: mustTime(t, "2026-04-01T00:00:00Z"), Value: 10.0},
		{Time: mustTime(t, "2026-04-01T06:00:00Z"), Value: 20.0},
		{Time: mustTime(t, "2026-04-01T12:00:00Z"), Value: 30.0},
		{Time: mustTime(t, "2026-04-02T03:00:00Z"), Value: 100.0},
	}
	// Two 1-day buckets: [04-01] {10,20,30}, [04-02] {100}.
	cases := []struct {
		agg  DownsampleAggregation
		want []float64
	}{
		{DownsampleAvg, []float64{20, 100}},
		{DownsampleSum, []float64{60, 100}},
		{DownsampleMin, []float64{10, 100}},
		{DownsampleMax, []float64{30, 100}},
		{DownsampleCount, []float64{3, 1}},
		{DownsampleFirst, []float64{10, 100}},
		{DownsampleLast, []float64{30, 100}},
	}
	for _, tc := range cases {
		t.Run(string(tc.agg), func(t *testing.T) {
			out, err := DownsampleInMemory(points, DownsampleSpec{Step: 24 * time.Hour, Aggregation: tc.agg})
			if err != nil {
				t.Fatalf("downsample: %v", err)
			}
			if len(out) != len(tc.want) {
				t.Fatalf("len = %d, want %d (%v)", len(out), len(tc.want), out)
			}
			for i, w := range tc.want {
				got, ok := out[i].Value.(float64)
				if !ok {
					t.Fatalf("bucket %d value not float64: %T", i, out[i].Value)
				}
				if got != w {
					t.Errorf("bucket %d = %v, want %v", i, got, w)
				}
			}
			// Buckets ordered ascending.
			if out[0].Time.After(out[1].Time) {
				t.Error("buckets not sorted ascending")
			}
		})
	}
}

// TestDownsampleInMemory_WindowFilter confirms spec.Start/End bound the
// input before bucketing (start inclusive, end exclusive).
func TestDownsampleInMemory_WindowFilter(t *testing.T) {
	points := []Point{
		{Time: mustTime(t, "2026-04-01T00:00:00Z"), Value: 1.0},
		{Time: mustTime(t, "2026-04-02T00:00:00Z"), Value: 2.0},
		{Time: mustTime(t, "2026-04-03T00:00:00Z"), Value: 3.0},
	}
	out, err := DownsampleInMemory(points, DownsampleSpec{
		Start:       mustTime(t, "2026-04-02T00:00:00Z"),
		End:         mustTime(t, "2026-04-03T00:00:00Z"),
		Step:        24 * time.Hour,
		Aggregation: DownsampleSum,
	})
	if err != nil {
		t.Fatalf("downsample: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1 (%v)", len(out), out)
	}
	if out[0].Value.(float64) != 2.0 {
		t.Errorf("value = %v, want 2 (only 04-02 in window)", out[0].Value)
	}
}

// TestTimeWindowFilter checks the range-only filtering path used when no
// aggregate is supplied.
func TestTimeWindowFilter(t *testing.T) {
	points := []Point{
		{Time: mustTime(t, "2026-04-01T00:00:00Z"), Value: "a"},
		{Time: mustTime(t, "2026-04-02T00:00:00Z"), Value: "b"},
		{Time: mustTime(t, "2026-04-03T00:00:00Z"), Value: "c"},
	}
	w := TimeWindow{Start: mustTime(t, "2026-04-02T00:00:00Z"), End: mustTime(t, "2026-04-03T00:00:00Z"), HasStart: true, HasEnd: true}
	out := w.Filter(points)
	if len(out) != 1 || out[0].Value != "b" {
		t.Fatalf("filtered = %v, want single point b", out)
	}
	// Empty window returns all points untouched.
	all := TimeWindow{}.Filter(points)
	if len(all) != 3 {
		t.Errorf("empty window dropped points: %v", all)
	}
}
