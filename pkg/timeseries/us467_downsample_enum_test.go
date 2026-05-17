package timeseries_test

// US-467 unit coverage for the downsample aggregation enum: the PRD adds
// `first` / `last` alongside the existing avg/sum/min/max/count names, so
// the normalizer must accept them and *PGStore must satisfy Downsampler.

import (
	"testing"

	"github.com/liyang/weave/pkg/timeseries"
)

func TestNormalizeAggregation_Given_FirstLastNames_When_Normalized_Then_AcceptedAsCanonical(t *testing.T) {
	cases := []struct {
		in   string
		want timeseries.DownsampleAggregation
	}{
		{"first", timeseries.DownsampleFirst},
		{"FIRST", timeseries.DownsampleFirst},
		{" first ", timeseries.DownsampleFirst},
		{"last", timeseries.DownsampleLast},
		{"LAST", timeseries.DownsampleLast},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := timeseries.NormalizeAggregation(tc.in)
			if !ok {
				t.Fatalf("NormalizeAggregation(%q) ok=false, want true", tc.in)
			}
			if got != tc.want {
				t.Errorf("NormalizeAggregation(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPGStore_SatisfiesDownsampler(t *testing.T) {
	// Compile-time check: *PGStore must satisfy the Downsampler interface
	// after US-467 wires DownsamplePoints onto the PG-backed store.
	var _ timeseries.Downsampler = (*timeseries.PGStore)(nil)
}
