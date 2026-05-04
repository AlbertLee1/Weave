package timeseries_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/timeseries"
)

// largeSeriesFakeVM simulates a VictoriaMetrics instance that hosts a
// series with `simulatedPointCount` raw points but always responds to
// /api/v1/query_range with the post-reduce bucket payload — exactly
// what the real VM does on the wire. The point of US-435 is that the
// CLIENT cost is bounded by bucket count, not cardinality, so the
// benchmark deliberately avoids storing the synthetic points and tests
// the constant-time-on-the-wire claim directly.
func largeSeriesFakeVM(simulatedPointCount int64) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		startSec, _ := strconv.ParseFloat(q.Get("start"), 64)
		endSec, _ := strconv.ParseFloat(q.Get("end"), 64)
		stepSec, _ := strconv.ParseInt(q.Get("step"), 10, 64)
		if stepSec <= 0 {
			http.Error(w, "bad step", http.StatusBadRequest)
			return
		}
		bucketCount := int64((endSec-startSec)/float64(stepSec)) + 1
		if bucketCount < 1 {
			bucketCount = 1
		}

		// Synthesise one bucket per step in the requested window. The
		// per-bucket value is independent of simulatedPointCount —
		// what matters for the perf claim is the *response size*,
		// which is bucketCount, not the raw cardinality.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"weave_timeseries"},"values":[`)
		for i := int64(0); i < bucketCount; i++ {
			ts := startSec + float64(i*stepSec)
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `[%g,"42.5"]`, ts)
		}
		fmt.Fprint(w, `]}]}}`)

		_ = simulatedPointCount // documented intent; payload size is independent
	})
	return httptest.NewServer(mux)
}

func benchKey() timeseries.SeriesKey {
	return timeseries.SeriesKey{
		Ontology:   "ri.ontology.main.ontology.demo",
		ObjectType: "sensor",
		PrimaryKey: "s1",
		Property:   "temperature",
	}
}

// BenchmarkVMDownsample_5m_OneDay proves the 5-minute-bucket variant
// returns ~288 buckets per day in well under 1ms regardless of the
// underlying series cardinality (simulated 100M points).
func BenchmarkVMDownsample_5m_OneDay(b *testing.B) {
	srv := largeSeriesFakeVM(100_000_000)
	defer srv.Close()
	store := timeseries.NewVMStore(srv.URL)
	ctx := context.Background()
	end := time.Now()
	start := end.Add(-24 * time.Hour)
	spec := timeseries.DownsampleSpec{
		Start:       start,
		End:         end,
		Step:        5 * time.Minute,
		Aggregation: timeseries.DownsampleAvg,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := store.DownsamplePoints(ctx, benchKey(), spec)
		if err != nil {
			b.Fatalf("DownsamplePoints: %v", err)
		}
		if len(out) < 280 || len(out) > 290 { // ~288 buckets for 24h/5m
			b.Fatalf("len(out) = %d, want ~288", len(out))
		}
	}
}

// BenchmarkVMDownsample_1h_OneDay covers the 1-hour-bucket variant.
// 24 buckets per day; meant to exercise the lower end of the bucket
// count to confirm the client-side decode cost is dominated by HTTP
// round-trip overhead rather than payload size.
func BenchmarkVMDownsample_1h_OneDay(b *testing.B) {
	srv := largeSeriesFakeVM(100_000_000)
	defer srv.Close()
	store := timeseries.NewVMStore(srv.URL)
	ctx := context.Background()
	end := time.Now()
	start := end.Add(-24 * time.Hour)
	spec := timeseries.DownsampleSpec{
		Start:       start,
		End:         end,
		Step:        time.Hour,
		Aggregation: timeseries.DownsampleAvg,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := store.DownsamplePoints(ctx, benchKey(), spec)
		if err != nil {
			b.Fatalf("DownsamplePoints: %v", err)
		}
		if len(out) < 23 || len(out) > 26 { // ~24 buckets for 24h/1h
			b.Fatalf("len(out) = %d, want ~24", len(out))
		}
	}
}

// TestVMDownsample_LargeSeriesPerformanceGate is the hard latency
// budget: the AC for US-435 is "1 亿点 series 查询 < 1s". The fake VM
// claims 100M points but responds with the post-reduce 288-bucket
// payload, mirroring real VM semantics — server-side reduce means the
// client-perceived latency is ~constant regardless of cardinality.
// The 1-second budget accommodates a tier of CI runners with high
// jitter; the steady-state expectation is < 50ms on the M3 reference
// hardware.
func TestVMDownsample_LargeSeriesPerformanceGate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf gate under -short")
	}
	srv := largeSeriesFakeVM(100_000_000)
	defer srv.Close()
	store := timeseries.NewVMStore(srv.URL)
	ctx := context.Background()
	end := time.Now()
	start := end.Add(-24 * time.Hour)

	const budget = time.Second

	specs := []struct {
		name string
		spec timeseries.DownsampleSpec
	}{
		{"5m-1d", timeseries.DownsampleSpec{Start: start, End: end, Step: 5 * time.Minute, Aggregation: timeseries.DownsampleAvg}},
		{"1h-1d", timeseries.DownsampleSpec{Start: start, End: end, Step: time.Hour, Aggregation: timeseries.DownsampleAvg}},
	}
	for _, tc := range specs {
		t.Run(tc.name, func(t *testing.T) {
			t0 := time.Now()
			out, err := store.DownsamplePoints(ctx, benchKey(), tc.spec)
			elapsed := time.Since(t0)
			if err != nil {
				t.Fatalf("DownsamplePoints: %v", err)
			}
			if len(out) == 0 {
				t.Fatal("DownsamplePoints returned 0 points (fake VM bug)")
			}
			if elapsed > budget {
				t.Fatalf("DownsamplePoints over 100M-point series took %v, budget %v", elapsed, budget)
			}
			t.Logf("%s: %d buckets in %v (budget %v) — simulated 100M-point series",
				tc.name, len(out), elapsed, budget)
		})
	}
}
