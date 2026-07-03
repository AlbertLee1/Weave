package aggregation

import (
	"errors"
	"testing"
)

// Foundry parity: accuracy=REQUIRE_ACCURATE means "return an error when exact
// results cannot be guaranteed", NOT "silently downgrade the response accuracy
// badge to APPROXIMATE". These tests pin the two ways an aggregation can fail
// to be exact — the MaxDocScanSize scan truncation, and the scanned-row
// APPROXIMATE threshold — and assert that REQUIRE_ACCURATE surfaces
// ErrAccuracyNotGuaranteed while ALLOW_APPROXIMATE keeps the pre-existing
// 200 + APPROXIMATE badge behavior.

func TestRequireAccurate_TruncatedScan_ReturnsError(t *testing.T) {
	idx := setupAccuracyIndex(t, 20)
	eng := NewEngine()
	eng.MaxDocScanSize = 5 // 20 docs, scan 5 -> avg scan is truncated

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyRequireAccurate,
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "price", Name: "avgPrice"},
		},
	})
	if err == nil {
		t.Fatalf("expected an error, got resp=%+v", resp)
	}
	if !errors.Is(err, ErrAccuracyNotGuaranteed) {
		t.Fatalf("error = %v, want errors.Is(ErrAccuracyNotGuaranteed)", err)
	}
	if resp != nil {
		t.Errorf("resp = %+v, want nil on accuracy error", resp)
	}
}

func TestAllowApproximate_TruncatedScan_ReturnsApproximateBadge(t *testing.T) {
	idx := setupAccuracyIndex(t, 20)
	eng := NewEngine()
	eng.MaxDocScanSize = 5

	// Default (blank) Accuracy == ALLOW_APPROXIMATE. Behavior must be
	// unchanged: 200-equivalent success with an APPROXIMATE badge.
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "price", Name: "avgPrice"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
	}
	if _, ok := findMetric(resp.Data[0].Metrics, "avgPrice"); !ok {
		t.Errorf("avgPrice metric missing from approximate response")
	}
}

func TestAllowApproximateExplicit_TruncatedScan_ReturnsApproximateBadge(t *testing.T) {
	idx := setupAccuracyIndex(t, 20)
	eng := NewEngine()
	eng.MaxDocScanSize = 5

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyAllowApproximate,
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "price", Name: "avgPrice"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
	}
}

func TestRequireAccurate_ScanThresholdExceeded_ReturnsError(t *testing.T) {
	idx := setupAccuracyIndex(t, 10)
	eng := NewEngine()
	eng.MaxDocScanSize = 100 // no scan truncation (10 docs < 100)

	threshold := int64(5) // 10 scanned rows > 5 -> forced APPROXIMATE
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy:                 AccuracyRequireAccurate,
		ApproximateScanThreshold: &threshold,
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "price", Name: "avgPrice"},
		},
	})
	if err == nil {
		t.Fatalf("expected an error, got resp=%+v", resp)
	}
	if !errors.Is(err, ErrAccuracyNotGuaranteed) {
		t.Fatalf("error = %v, want errors.Is(ErrAccuracyNotGuaranteed)", err)
	}
}

func TestAllowApproximate_ScanThresholdExceeded_ReturnsApproximateBadge(t *testing.T) {
	idx := setupAccuracyIndex(t, 10)
	eng := NewEngine()
	eng.MaxDocScanSize = 100

	threshold := int64(5)
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		ApproximateScanThreshold: &threshold,
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "price", Name: "avgPrice"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
	}
}

// Regression guard: REQUIRE_ACCURATE on a fully-scanned, below-threshold
// aggregation must keep returning a normal ACCURATE response (no error). This
// protects the "transparently promote approximate algorithms to exact" path
// that is intentionally kept as a superset of the Foundry contract.
func TestRequireAccurate_FullScan_NoError(t *testing.T) {
	idx := setupAccuracyIndex(t, 10)
	eng := NewEngine()
	eng.MaxDocScanSize = 100

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyRequireAccurate,
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "price", Name: "avgPrice"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("accuracy = %q, want ACCURATE", resp.Accuracy)
	}
}

// A truncated groupBy leaf under REQUIRE_ACCURATE must also error — the
// scan-truncation verdict propagates out of the per-bucket metric scan.
func TestRequireAccurate_GroupByTruncatedLeaf_ReturnsError(t *testing.T) {
	idx := setupAccuracyIndex(t, 30)
	eng := NewEngine()
	eng.MaxDocScanSize = 4

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyRequireAccurate,
		Aggregations: []AggregationSpec{
			{Type: "sum", Field: "price", Name: "sumPrice"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "region"},
		},
	})
	if err == nil {
		t.Fatalf("expected an error, got resp=%+v", resp)
	}
	if !errors.Is(err, ErrAccuracyNotGuaranteed) {
		t.Fatalf("error = %v, want errors.Is(ErrAccuracyNotGuaranteed)", err)
	}
}

// A truncated sub-aggregation under REQUIRE_ACCURATE must error even when the
// top-level metric (count) is itself exact — the sub-aggregation recursion
// carries the same accuracy contract and its wrapped error must still satisfy
// errors.Is(ErrAccuracyNotGuaranteed).
func TestRequireAccurate_SubAggregationTruncated_ReturnsError(t *testing.T) {
	idx := setupAccuracyIndex(t, 30)
	eng := NewEngine()
	eng.MaxDocScanSize = 4

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyRequireAccurate,
		Aggregations: []AggregationSpec{
			{Type: "count", Name: "n"},
		},
		SubAggregations: []SubAggregationSpec{
			{
				Name: "priceAvg",
				Aggregations: []AggregationSpec{
					{Type: "avg", Field: "price", Name: "avgPrice"},
				},
			},
		},
	})
	if err == nil {
		t.Fatalf("expected an error, got resp=%+v", resp)
	}
	if !errors.Is(err, ErrAccuracyNotGuaranteed) {
		t.Fatalf("error = %v, want errors.Is(ErrAccuracyNotGuaranteed)", err)
	}
}
