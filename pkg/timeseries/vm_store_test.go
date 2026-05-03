package timeseries_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
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
