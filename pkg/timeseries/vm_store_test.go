package timeseries_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/timeseries"
)

// fakeVM is a minimal in-memory VictoriaMetrics test double covering the
// /api/v1/import + /api/v1/export endpoints VMStore relies on.
type fakeVM struct {
	mu   sync.Mutex
	data map[string][]vmPoint // matcher → ordered points
	srv  *httptest.Server

	// importErr / exportErr force the next call on each endpoint to return
	// an HTTP 500. Cleared after one use.
	importErr bool
	exportErr bool
}

type vmPoint struct {
	ts    int64
	value float64
}

type vmLine struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

func newFakeVM(t *testing.T) *fakeVM {
	t.Helper()
	vm := &fakeVM{data: map[string][]vmPoint{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/import", vm.handleImport)
	mux.HandleFunc("/api/v1/export", vm.handleExport)
	mux.HandleFunc("/api/v1/query_range", vm.handleQueryRange)
	vm.srv = httptest.NewServer(mux)
	t.Cleanup(vm.srv.Close)
	return vm
}

func (vm *fakeVM) URL() string { return vm.srv.URL }

func (vm *fakeVM) handleImport(w http.ResponseWriter, r *http.Request) {
	vm.mu.Lock()
	if vm.importErr {
		vm.importErr = false
		vm.mu.Unlock()
		http.Error(w, "forced import error", http.StatusInternalServerError)
		return
	}
	vm.mu.Unlock()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	for dec.More() {
		var line vmLine
		if err := dec.Decode(&line); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		matcher := metricToMatcher(line.Metric)
		vm.mu.Lock()
		for i := range line.Timestamps {
			vm.data[matcher] = append(vm.data[matcher], vmPoint{ts: line.Timestamps[i], value: line.Values[i]})
		}
		sort.SliceStable(vm.data[matcher], func(i, j int) bool {
			return vm.data[matcher][i].ts < vm.data[matcher][j].ts
		})
		vm.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleQueryRange parses VictoriaMetrics-style PromQL aggregations of
// the shape `<agg>_over_time(matcher[<step>s])` and reduces the stored
// points per bucket. Only the operators VMStore.DownsamplePoints emits
// are supported; anything else returns 400 so a regression in the
// client-side query builder is caught immediately.
func (vm *fakeVM) handleQueryRange(w http.ResponseWriter, r *http.Request) {
	vm.mu.Lock()
	if vm.exportErr {
		vm.exportErr = false
		vm.mu.Unlock()
		http.Error(w, "forced export error", http.StatusInternalServerError)
		return
	}
	vm.mu.Unlock()

	q := r.URL.Query()
	rawQuery := q.Get("query")
	startStr := q.Get("start")
	endStr := q.Get("end")
	stepStr := q.Get("step")
	if rawQuery == "" || startStr == "" || endStr == "" || stepStr == "" {
		http.Error(w, "missing query params", http.StatusBadRequest)
		return
	}
	op, matcher, _, err := parseOverTimeQuery(rawQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	startSec, err := strconv.ParseFloat(startStr, 64)
	if err != nil {
		http.Error(w, "bad start", http.StatusBadRequest)
		return
	}
	endSec, err := strconv.ParseFloat(endStr, 64)
	if err != nil {
		http.Error(w, "bad end", http.StatusBadRequest)
		return
	}
	stepSec, err := strconv.ParseInt(stepStr, 10, 64)
	if err != nil || stepSec <= 0 {
		http.Error(w, "bad step", http.StatusBadRequest)
		return
	}

	vm.mu.Lock()
	points := append([]vmPoint(nil), vm.data[matcher]...)
	vm.mu.Unlock()

	stepMs := stepSec * 1000
	startMs := int64(startSec * 1000)
	endMs := int64(endSec * 1000)

	type bucket struct {
		count int
		sum   float64
		min   float64
		max   float64
	}
	buckets := map[int64]*bucket{}
	for _, p := range points {
		if p.ts < startMs || p.ts > endMs {
			continue
		}
		bucketStart := (p.ts / stepMs) * stepMs
		b, ok := buckets[bucketStart]
		if !ok {
			b = &bucket{min: p.value, max: p.value}
			buckets[bucketStart] = b
		}
		b.count++
		b.sum += p.value
		if p.value < b.min {
			b.min = p.value
		}
		if p.value > b.max {
			b.max = p.value
		}
	}

	// Emit a single matrix series even when buckets is empty so the
	// client-side decoder exercises the success path.
	type pair = []interface{}
	values := make([]pair, 0, len(buckets))
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		b := buckets[k]
		var v float64
		switch op {
		case "avg_over_time":
			v = b.sum / float64(b.count)
		case "sum_over_time":
			v = b.sum
		case "min_over_time":
			v = b.min
		case "max_over_time":
			v = b.max
		case "count_over_time":
			v = float64(b.count)
		default:
			http.Error(w, "unsupported op "+op, http.StatusBadRequest)
			return
		}
		values = append(values, pair{float64(k) / 1000.0, formatPromValue(v)})
	}

	resp := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []map[string]interface{}{
				{
					"metric": matcherToMetric(matcher),
					"values": values,
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// parseOverTimeQuery extracts (op, matcher, stepDuration) from the
// canonical `<agg>_over_time(<matcher>[<step>s])` shape that
// VMStore.DownsamplePoints emits.
func parseOverTimeQuery(query string) (op, matcher string, stepSec int64, err error) {
	openParen := strings.Index(query, "(")
	closeParen := strings.LastIndex(query, ")")
	if openParen < 0 || closeParen < 0 || closeParen <= openParen {
		return "", "", 0, errors.New("malformed query")
	}
	op = query[:openParen]
	inner := query[openParen+1 : closeParen]
	openBracket := strings.LastIndex(inner, "[")
	closeBracket := strings.LastIndex(inner, "]")
	if openBracket < 0 || closeBracket < 0 || closeBracket <= openBracket {
		return "", "", 0, errors.New("missing range selector")
	}
	matcher = inner[:openBracket]
	stepRaw := strings.TrimSuffix(inner[openBracket+1:closeBracket], "s")
	step, err := strconv.ParseInt(stepRaw, 10, 64)
	if err != nil {
		return "", "", 0, fmt.Errorf("bad step: %w", err)
	}
	return op, matcher, step, nil
}

// formatPromValue mirrors the PromQL wire shape (string-encoded floats).
// strconv.FormatFloat with 'g' precision -1 produces the shortest
// round-tripping representation, which is what a real VictoriaMetrics
// response carries for finite values.
func formatPromValue(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func (vm *fakeVM) handleExport(w http.ResponseWriter, r *http.Request) {
	vm.mu.Lock()
	if vm.exportErr {
		vm.exportErr = false
		vm.mu.Unlock()
		http.Error(w, "forced export error", http.StatusInternalServerError)
		return
	}
	vm.mu.Unlock()

	matchers := r.URL.Query()["match[]"]
	if len(matchers) == 0 {
		http.Error(w, "match[] required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/stream+json")
	enc := json.NewEncoder(w)
	for _, m := range matchers {
		vm.mu.Lock()
		points := vm.data[m]
		vm.mu.Unlock()
		if len(points) == 0 {
			continue
		}
		line := vmLine{
			Metric:     matcherToMetric(m),
			Values:     make([]float64, len(points)),
			Timestamps: make([]int64, len(points)),
		}
		for i, p := range points {
			line.Values[i] = p.value
			line.Timestamps[i] = p.ts
		}
		_ = enc.Encode(line)
	}
}

// metricToMatcher converts an inbound metric map to the canonical matcher
// string emitted by VMStore.buildMatcher (so the fake's storage key
// matches across import/export).
func metricToMatcher(m map[string]string) string {
	return timeseries.VMMetricName + "{ontology=\"" + m["ontology"] +
		"\",object_type=\"" + m["object_type"] +
		"\",primary_key=\"" + m["primary_key"] +
		"\",property=\"" + m["property"] + "\"}"
}

// matcherToMetric is a minimal inverse of metricToMatcher used so the
// export response carries label metadata. Tests don't depend on label
// values reflecting back through, so a coarse decode is fine.
func matcherToMetric(matcher string) map[string]string {
	out := map[string]string{"__name__": timeseries.VMMetricName}
	open := strings.Index(matcher, "{")
	close := strings.LastIndex(matcher, "}")
	if open < 0 || close < 0 || close <= open {
		return out
	}
	inner := matcher[open+1 : close]
	for _, kv := range strings.Split(inner, ",") {
		eq := strings.Index(kv, "=")
		if eq < 0 {
			continue
		}
		k := kv[:eq]
		v := strings.TrimSuffix(strings.TrimPrefix(kv[eq+1:], `"`), `"`)
		out[k] = v
	}
	return out
}

func TestVMStore_AppendAndStream(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()
	key := testKey()

	t1 := mustTime(t, "2026-04-01T00:00:00Z")
	t2 := mustTime(t, "2026-04-02T00:00:00Z")
	t3 := mustTime(t, "2026-04-03T00:00:00Z")

	// Out-of-order to exercise the post-export sort.
	for _, p := range []timeseries.Point{
		{Time: t2, Value: 22.5},
		{Time: t1, Value: 21.0},
		{Time: t3, Value: 23.5},
	} {
		if err := store.AppendPoint(ctx, key, p); err != nil {
			t.Fatalf("AppendPoint: %v", err)
		}
	}

	first, err := store.FirstPoint(ctx, key)
	if err != nil {
		t.Fatalf("FirstPoint: %v", err)
	}
	if !first.Time.Equal(t1) || first.Value.(float64) != 21.0 {
		t.Errorf("FirstPoint = %+v, want time=%v value=21.0", first, t1)
	}

	last, err := store.LastPoint(ctx, key)
	if err != nil {
		t.Fatalf("LastPoint: %v", err)
	}
	if !last.Time.Equal(t3) || last.Value.(float64) != 23.5 {
		t.Errorf("LastPoint = %+v, want time=%v value=23.5", last, t3)
	}

	points, err := store.StreamPoints(ctx, key)
	if err != nil {
		t.Fatalf("StreamPoints: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("len = %d, want 3 (got %+v)", len(points), points)
	}
	for i, want := range []time.Time{t1, t2, t3} {
		if !points[i].Time.Equal(want) {
			t.Errorf("points[%d].Time = %v, want %v", i, points[i].Time, want)
		}
	}
}

func TestVMStore_FirstPoint_EmptyReturnsErrNoPoints(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()

	if _, err := store.FirstPoint(ctx, testKey()); !errors.Is(err, timeseries.ErrNoPoints) {
		t.Errorf("FirstPoint on empty: err = %v, want ErrNoPoints", err)
	}
	if _, err := store.LastPoint(ctx, testKey()); !errors.Is(err, timeseries.ErrNoPoints) {
		t.Errorf("LastPoint on empty: err = %v, want ErrNoPoints", err)
	}
	pts, err := store.StreamPoints(ctx, testKey())
	if err != nil {
		t.Errorf("StreamPoints on empty: unexpected err = %v", err)
	}
	if len(pts) != 0 {
		t.Errorf("StreamPoints on empty: len = %d, want 0", len(pts))
	}
}

func TestVMStore_AppendNonNumericReturnsError(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()

	err := store.AppendPoint(ctx, testKey(), timeseries.Point{
		Time:  mustTime(t, "2026-04-01T00:00:00Z"),
		Value: "hello",
	})
	if !errors.Is(err, timeseries.ErrNonNumericValue) {
		t.Errorf("AppendPoint(string): err = %v, want ErrNonNumericValue", err)
	}
}

func TestVMStore_AppendCoercesIntegerValues(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()
	key := testKey()
	ts := mustTime(t, "2026-04-01T00:00:00Z")

	if err := store.AppendPoint(ctx, key, timeseries.Point{Time: ts, Value: 42}); err != nil {
		t.Fatalf("AppendPoint(int): %v", err)
	}
	first, err := store.FirstPoint(ctx, key)
	if err != nil {
		t.Fatalf("FirstPoint: %v", err)
	}
	if first.Value.(float64) != 42 {
		t.Errorf("first.Value = %v, want 42", first.Value)
	}
}

func TestVMStore_AppendPointPropagatesHTTPError(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()

	vm.importErr = true
	err := store.AppendPoint(ctx, testKey(), timeseries.Point{
		Time:  mustTime(t, "2026-04-01T00:00:00Z"),
		Value: 1.0,
	})
	if err == nil {
		t.Fatal("AppendPoint: want HTTP error, got nil")
	}
	if !strings.Contains(err.Error(), "vm import: status 500") {
		t.Errorf("AppendPoint: got %q, want vm import status 500", err.Error())
	}
}

func TestVMStore_StreamPointsPropagatesHTTPError(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()

	vm.exportErr = true
	_, err := store.StreamPoints(ctx, testKey())
	if err == nil {
		t.Fatal("StreamPoints: want HTTP error, got nil")
	}
	if !strings.Contains(err.Error(), "vm export: status 500") {
		t.Errorf("StreamPoints: got %q, want vm export status 500", err.Error())
	}
}

func TestVMStore_MultipleSeriesIsolated(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()

	keyA := testKey()
	keyB := testKey()
	keyB.PrimaryKey = "s2"

	if err := store.AppendPoint(ctx, keyA, timeseries.Point{
		Time:  mustTime(t, "2026-04-01T00:00:00Z"),
		Value: 10.0,
	}); err != nil {
		t.Fatalf("AppendPoint A: %v", err)
	}

	if _, err := store.FirstPoint(ctx, keyB); !errors.Is(err, timeseries.ErrNoPoints) {
		t.Errorf("keyB FirstPoint: err = %v, want ErrNoPoints", err)
	}
	pointsA, err := store.StreamPoints(ctx, keyA)
	if err != nil {
		t.Fatalf("StreamPoints A: %v", err)
	}
	if len(pointsA) != 1 {
		t.Errorf("len(pointsA) = %d, want 1", len(pointsA))
	}
}

func TestVMStore_LabelEscaping(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()

	// PK contains a double-quote and a backslash so the matcher round-trip
	// has to escape and de-escape correctly. The fake's matcher decoder is
	// a coarse shim — we only check that AppendPoint succeeds without
	// VictoriaMetrics rejecting the label.
	key := timeseries.SeriesKey{
		Ontology:   "ri.ontology.main.ontology.demo",
		ObjectType: "sensor",
		PrimaryKey: `s"1\back`,
		Property:   "temperature",
	}
	if err := store.AppendPoint(ctx, key, timeseries.Point{
		Time:  mustTime(t, "2026-04-01T00:00:00Z"),
		Value: 9.9,
	}); err != nil {
		t.Fatalf("AppendPoint with reserved-char label: %v", err)
	}
}

// seedMinutePoints inserts `count` points one minute apart with a
// linearly-rising value so the per-bucket reduce verdicts (avg/sum/min/
// max/count) are deterministic and easy to compute by hand.
func seedMinutePoints(t *testing.T, store *timeseries.VMStore, key timeseries.SeriesKey, anchor time.Time, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < count; i++ {
		ts := anchor.Add(time.Duration(i) * time.Minute)
		if err := store.AppendPoint(ctx, key, timeseries.Point{Time: ts, Value: float64(i)}); err != nil {
			t.Fatalf("AppendPoint %d: %v", i, err)
		}
	}
}

func TestVMStore_DownsamplePoints_FiveMinuteAvg(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()
	key := testKey()
	anchor := mustTime(t, "2026-04-01T00:00:00Z")
	// 60 minute-spaced points → 12 five-minute buckets, value 0..59.
	seedMinutePoints(t, store, key, anchor, 60)

	out, err := store.DownsamplePoints(ctx, key, timeseries.DownsampleSpec{
		Start:       anchor,
		End:         anchor.Add(time.Hour),
		Step:        5 * time.Minute,
		Aggregation: timeseries.DownsampleAvg,
	})
	if err != nil {
		t.Fatalf("DownsamplePoints: %v", err)
	}
	if len(out) != 12 {
		t.Fatalf("len = %d, want 12 (got %+v)", len(out), out)
	}
	// Bucket k covers minutes [5k .. 5k+4]; values 5k..5k+4; mean = 5k+2.
	for i, p := range out {
		want := float64(5*i + 2)
		if got := p.Value.(float64); got != want {
			t.Errorf("bucket[%d] avg = %v, want %v", i, got, want)
		}
		wantTime := anchor.Add(time.Duration(5*i) * time.Minute)
		if !p.Time.Equal(wantTime) {
			t.Errorf("bucket[%d].Time = %v, want %v", i, p.Time, wantTime)
		}
	}
}

func TestVMStore_DownsamplePoints_OneHourSum(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()
	key := testKey()
	anchor := mustTime(t, "2026-04-01T00:00:00Z")
	// 120 minute-spaced points → 2 one-hour buckets.
	seedMinutePoints(t, store, key, anchor, 120)

	out, err := store.DownsamplePoints(ctx, key, timeseries.DownsampleSpec{
		Start:       anchor,
		End:         anchor.Add(2 * time.Hour),
		Step:        time.Hour,
		Aggregation: timeseries.DownsampleSum,
	})
	if err != nil {
		t.Fatalf("DownsamplePoints: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2 (got %+v)", len(out), out)
	}
	// Sum of 0..59 = 1770; sum of 60..119 = 5370.
	want := []float64{1770, 5370}
	for i, p := range out {
		if got := p.Value.(float64); got != want[i] {
			t.Errorf("bucket[%d] sum = %v, want %v", i, got, want[i])
		}
	}
}

func TestVMStore_DownsamplePoints_AllAggregations(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()
	key := testKey()
	anchor := mustTime(t, "2026-04-01T00:00:00Z")
	seedMinutePoints(t, store, key, anchor, 5) // values 0..4 in one 5m bucket

	cases := []struct {
		agg  timeseries.DownsampleAggregation
		want float64
	}{
		{timeseries.DownsampleAvg, 2},   // mean(0..4)
		{timeseries.DownsampleSum, 10},  // 0+1+2+3+4
		{timeseries.DownsampleMin, 0},   //
		{timeseries.DownsampleMax, 4},   //
		{timeseries.DownsampleCount, 5}, //
	}
	for _, tc := range cases {
		out, err := store.DownsamplePoints(ctx, key, timeseries.DownsampleSpec{
			Start:       anchor,
			End:         anchor.Add(10 * time.Minute),
			Step:        5 * time.Minute,
			Aggregation: tc.agg,
		})
		if err != nil {
			t.Fatalf("DownsamplePoints[%s]: %v", tc.agg, err)
		}
		if len(out) != 1 {
			t.Fatalf("DownsamplePoints[%s]: len = %d, want 1", tc.agg, len(out))
		}
		if got := out[0].Value.(float64); got != tc.want {
			t.Errorf("DownsamplePoints[%s]: got %v, want %v", tc.agg, got, tc.want)
		}
	}
}

func TestVMStore_DownsamplePoints_ValidationRejectsBadSpec(t *testing.T) {
	store := timeseries.NewVMStore("http://localhost") // no fake — validation should fire pre-network
	cases := []struct {
		name string
		spec timeseries.DownsampleSpec
	}{
		{"zero step", timeseries.DownsampleSpec{Aggregation: timeseries.DownsampleAvg}},
		{"negative step", timeseries.DownsampleSpec{Step: -time.Minute, Aggregation: timeseries.DownsampleAvg}},
		{"missing aggregation", timeseries.DownsampleSpec{Step: time.Minute}},
		{"end before start", timeseries.DownsampleSpec{
			Start:       mustTime(t, "2026-04-02T00:00:00Z"),
			End:         mustTime(t, "2026-04-01T00:00:00Z"),
			Step:        time.Minute,
			Aggregation: timeseries.DownsampleAvg,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.DownsamplePoints(context.Background(), testKey(), tc.spec)
			if err == nil {
				t.Errorf("DownsamplePoints: want validation err, got nil")
			}
		})
	}
}

func TestVMStore_DownsamplePoints_PropagatesHTTPError(t *testing.T) {
	vm := newFakeVM(t)
	store := timeseries.NewVMStore(vm.URL())
	ctx := context.Background()

	vm.exportErr = true // shared toggle: exportErr also fires for query_range
	_, err := store.DownsamplePoints(ctx, testKey(), timeseries.DownsampleSpec{
		Start:       mustTime(t, "2026-04-01T00:00:00Z"),
		End:         mustTime(t, "2026-04-01T01:00:00Z"),
		Step:        5 * time.Minute,
		Aggregation: timeseries.DownsampleAvg,
	})
	if err == nil {
		t.Fatal("DownsamplePoints: want HTTP error, got nil")
	}
	if !strings.Contains(err.Error(), "vm query_range: status 500") {
		t.Errorf("DownsamplePoints: got %q, want vm query_range status 500", err.Error())
	}
}

func TestVMStore_ImplementsDownsampler(t *testing.T) {
	// Compile-time gate: VMStore must satisfy timeseries.Downsampler so
	// the OSS handler's pushdown branch fires for VM-backed deployments.
	var _ timeseries.Downsampler = (*timeseries.VMStore)(nil)
}

func TestNormalizeAggregation(t *testing.T) {
	cases := []struct {
		in   string
		want timeseries.DownsampleAggregation
		ok   bool
	}{
		{"", timeseries.DownsampleAvg, true},
		{"avg", timeseries.DownsampleAvg, true},
		{"AVG", timeseries.DownsampleAvg, true},
		{"mean", timeseries.DownsampleAvg, true},
		{"sum", timeseries.DownsampleSum, true},
		{"min", timeseries.DownsampleMin, true},
		{"max", timeseries.DownsampleMax, true},
		{"count", timeseries.DownsampleCount, true},
		{"median", "", false},
		{"p99", "", false},
	}
	for _, tc := range cases {
		got, ok := timeseries.NormalizeAggregation(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("NormalizeAggregation(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
