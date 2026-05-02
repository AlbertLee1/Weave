package aggregation

import (
	"fmt"
	"math"

	"github.com/influxdata/tdigest"
)

// DefaultTDigestCompression caps the t-digest centroid budget for the
// production approximatePercentile path. At c=100 the digest holds at most
// 2c=200 processed centroids and 8c=800 unprocessed centroids. With each
// centroid taking 16 bytes (Mean + Weight float64s), the worst-case
// digest occupies (200+800)*16 = 16000 bytes — comfortably under the
// US-368 16 KiB memory ceiling for an arbitrarily large input stream.
//
// This compression also yields p99 relative error well under 1% on smooth
// distributions (Gaussian, exponential) at 1M points; see the US-368
// accuracy gates in tdigest_percentile_test.go.
const DefaultTDigestCompression = 100.0

// computeApproxPercentileTDigest returns a single percentile (0–100) of the
// supplied float64 slice using a t-digest sketch with the package-default
// compression. Returns NaN on empty input to match the Bleve dispatch
// contract upstream.
func computeApproxPercentileTDigest(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	td := newBoundedTDigest()
	for _, v := range values {
		td.Add(v, 1)
	}
	return td.Quantile(percentile / 100.0)
}

// computeApproxPercentilesTDigest returns a set of percentiles (each 0–100)
// of the supplied float64 slice in a SINGLE pass over one shared t-digest.
// Keys in the returned map use the canonical `%g` formatting of each
// requested percentile so callers can round-trip them through JSON.
//
// Returns an empty map when either input slice is empty.
func computeApproxPercentilesTDigest(values []float64, percentiles []float64) map[string]float64 {
	out := make(map[string]float64, len(percentiles))
	if len(values) == 0 || len(percentiles) == 0 {
		return out
	}
	td := newBoundedTDigest()
	for _, v := range values {
		td.Add(v, 1)
	}
	for _, p := range percentiles {
		out[fmt.Sprintf("%g", p)] = td.Quantile(p / 100.0)
	}
	return out
}

// newBoundedTDigest constructs a t-digest at the package-default compression.
// Every production approximatePercentile call routes through here so the
// memory and accuracy guarantees stay uniform across the engine.
func newBoundedTDigest() *tdigest.TDigest {
	return tdigest.NewWithCompression(DefaultTDigestCompression)
}
