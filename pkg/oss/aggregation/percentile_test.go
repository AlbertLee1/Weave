package aggregation

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// setupGaussianIndex builds a Bleve index with n points drawn from a
// Gaussian distribution with the given mean and stddev. The RNG is seeded
// for determinism so percentile assertions are reproducible across runs.
func setupGaussianIndex(t *testing.T, n int, mean, stddev float64, seed int64) bleve.Index {
	t.Helper()
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("latency", mapping.NewNumericFieldMapping())
	idxMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "percentile"), idxMapping)
	if err != nil {
		t.Fatalf("bleve.New: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	r := rand.New(rand.NewSource(seed))
	batch := idx.NewBatch()
	for i := 0; i < n; i++ {
		v := r.NormFloat64()*stddev + mean
		if v < 0 {
			v = 0
		}
		if err := batch.Index(fmt.Sprintf("d%d", i), map[string]interface{}{"latency": v}); err != nil {
			t.Fatalf("batch.Index: %v", err)
		}
	}
	if err := idx.Batch(batch); err != nil {
		t.Fatalf("idx.Batch: %v", err)
	}
	return idx
}

// normalInverseCDF returns the inverse of the standard normal CDF via
// Beasley-Springer-Moro. Sufficient precision for percentile expectations
// in the p50/p95/p99 range used by these tests.
func normalInverseCDF(p float64) float64 {
	a := []float64{
		-3.969683028665376e+01,
		2.209460984245205e+02,
		-2.759285104469687e+02,
		1.383577518672690e+02,
		-3.066479806614716e+01,
		2.506628277459239e+00,
	}
	b := []float64{
		-5.447609879822406e+01,
		1.615858368580409e+02,
		-1.556989798598866e+02,
		6.680131188771972e+01,
		-1.328068155288572e+01,
	}
	c := []float64{
		-7.784894002430293e-03,
		-3.223964580411365e-01,
		-2.400758277161838e+00,
		-2.549732539343734e+00,
		4.374664141464968e+00,
		2.938163982698783e+00,
	}
	d := []float64{
		7.784695709041462e-03,
		3.224671290700398e-01,
		2.445134137142996e+00,
		3.754408661907416e+00,
	}
	pLow := 0.02425
	pHigh := 1 - pLow

	var x float64
	switch {
	case p < pLow:
		q := math.Sqrt(-2 * math.Log(p))
		x = (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= pHigh:
		q := p - 0.5
		r := q * q
		x = (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		x = -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	return x
}

// TestApproximatePercentile_Gaussian10k verifies that approximatePercentile
// returns p50/p95/p99 values within a loose tolerance of the analytic
// normal-distribution quantiles on a 10k Gaussian dataset. US-016 only
// establishes the HdrHistogram-backed path; the tight ≤5% assertion lives
// in US-018's bench test.
func TestApproximatePercentile_Gaussian10k(t *testing.T) {
	const (
		n      = 10000
		mean   = 5000.0
		stddev = 1000.0
	)
	idx := setupGaussianIndex(t, n, mean, stddev, 42)
	eng := NewEngine()
	eng.MaxDocScanSize = 20000

	percentiles := []float64{50, 95, 99}
	for _, p := range percentiles {
		t.Run(fmt.Sprintf("p%v", p), func(t *testing.T) {
			pv := p
			resp, err := eng.Aggregate(idx, &AggregationRequest{
				Aggregations: []AggregationSpec{
					{Type: "approximatePercentile", Field: "latency", Percentile: &pv, Name: "lat"},
				},
			})
			if err != nil {
				t.Fatalf("Aggregate: %v", err)
			}
			if len(resp.Data) != 1 || len(resp.Data[0].Metrics) != 1 {
				t.Fatalf("data/metrics shape = %v", resp.Data)
			}
			got, ok := resp.Data[0].Metrics[0].Value.(float64)
			if !ok {
				t.Fatalf("metric value type = %T, want float64", resp.Data[0].Metrics[0].Value)
			}
			want := mean + stddev*normalInverseCDF(p/100.0)
			tolerance := 0.1 * want
			if math.Abs(got-want) > tolerance {
				t.Errorf("p%v = %.2f, want ~%.2f (±%.2f)", p, got, want, tolerance)
			}
		})
	}
}

// TestApproximatePercentile_Scalar confirms a single-percentile request
// surfaces a scalar float64 on the response (not a map or slice). US-017
// relaxes this to allow multi-percentile map responses.
func TestApproximatePercentile_Scalar(t *testing.T) {
	idx := setupAccuracyIndex(t, 40)
	eng := NewEngine()

	p := 75.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximatePercentile", Field: "price", Percentile: &p, Name: "p75"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Metrics) != 1 {
		t.Fatalf("data/metrics shape = %v", resp.Data)
	}
	if _, ok := resp.Data[0].Metrics[0].Value.(float64); !ok {
		t.Errorf("p75 value type = %T, want float64 scalar", resp.Data[0].Metrics[0].Value)
	}
}

// TestComputeApproxPercentileHdr_DirectCall exercises the HdrHistogram-backed
// percentile function directly on a Gaussian dataset, independent of the
// Bleve search path. This is the canonical US-016 unit test: if the
// HdrHistogram implementation is missing the package fails to compile,
// making the red→green cycle explicit.
func TestComputeApproxPercentileHdr_DirectCall(t *testing.T) {
	const (
		n      = 10000
		mean   = 5000.0
		stddev = 1000.0
	)
	r := rand.New(rand.NewSource(7))
	values := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		v := r.NormFloat64()*stddev + mean
		if v < 0 {
			v = 0
		}
		values = append(values, v)
	}

	for _, p := range []float64{50, 95, 99} {
		got, err := computeApproxPercentileHdr(values, p)
		if err != nil {
			t.Fatalf("computeApproxPercentileHdr p%v: %v", p, err)
		}
		want := mean + stddev*normalInverseCDF(p/100.0)
		tolerance := 0.1 * want
		if math.Abs(got-want) > tolerance {
			t.Errorf("p%v = %.2f, want ~%.2f (±%.2f)", p, got, want, tolerance)
		}
	}
}
