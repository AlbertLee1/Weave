//go:build integration

package phase6_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/rid"
)

// TestPercentileGroupBy_NorthwindFreightByCountry is the Phase 6 cross-US test
// pairing US-016/US-017 (HdrHistogram-backed approxPercentile + multi-percentile)
// with the groupBy pipeline (US-013) and accuracy marker (US-014).
//
// It seeds the real Northwind orders fixture into a Bleve index, then POSTs to
// /objectSets/aggregate asking for the p95 of `freight` grouped by `shipCountry`
// (a not_analyzed string typeclass field, so term buckets split cleanly). The
// per-bucket approximation is compared against a sort-based exact percentile
// computed directly from the seeded values; the assertion is that relative
// error stays ≤ 5%, matching the Foundry parity target from US-018.
//
// The test runs twice to exercise both contract shapes:
//
//  1. Scalar percentile (Percentile: &p95) — MetricValue.Value is float64.
//  2. Multi-percentile ([50, 95, 99])       — MetricValue.Value is
//     map[string]float64 keyed by the percentile string. This verifies that
//     a single HdrHistogram pass drives multiple percentiles per group, which
//     is the interesting shared-work path at aggregation scale.
//
// On top of the per-bucket numeric check, the top-level `accuracy` field must
// be present (either "ACCURATE" when the scan fits the data or "APPROXIMATE"
// when MaxDocScanSize truncates it). Empty accuracy means the US-014 plumbing
// regressed and is rejected here.
func TestPercentileGroupBy_NorthwindFreightByCountry(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "northwind_percentile",
		DisplayName: "Northwind (Percentile × GroupBy)",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}
	ot := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "order",
		DisplayName: "Order",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, ot); err != nil {
		t.Fatalf("create order: %v", err)
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	// Typed props: freight is a double (NewNumericFieldMapping) so
	// percentile scans read back as float64. shipCountry is not_analyzed
	// so each distinct raw country is its own facet bucket.
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "freight", BaseType: "double", IsSearchable: true},
		{APIName: "shipCountry", BaseType: "string", IsSearchable: true, Analyzer: "not_analyzed"},
	}
	if _, err := mgr.EnsureIndex("order", props); err != nil {
		t.Fatalf("ensure order index: %v", err)
	}

	// ---- Seed from real Northwind orders.csv -------------------------------
	f, err := os.Open(northwindFixturePath(t, "orders.csv"))
	if err != nil {
		t.Fatalf("open orders.csv: %v", err)
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read orders.csv: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("orders.csv empty")
	}
	header := records[0]
	idxOf := func(col string) int {
		for i, h := range header {
			if h == col {
				return i
			}
		}
		return -1
	}
	pkIdx := idxOf("orderID")
	freightIdx := idxOf("freight")
	countryIdx := idxOf("shipCountry")
	if pkIdx < 0 || freightIdx < 0 || countryIdx < 0 {
		t.Fatalf("orders.csv missing expected columns")
	}

	// Ground-truth table: per-country slice of freight values. Every value
	// that makes it into the Bleve index also lands here, so the sort-based
	// reference tracks the approximation pass exactly.
	perCountryFreight := map[string][]float64{}
	seeded := 0
	for _, row := range records[1:] {
		if pkIdx >= len(row) || freightIdx >= len(row) || countryIdx >= len(row) {
			continue
		}
		pk := row[pkIdx]
		country := row[countryIdx]
		if country == "" || country == "NULL" {
			continue
		}
		freight, err := strconv.ParseFloat(row[freightIdx], 64)
		if err != nil {
			continue
		}
		doc := map[string]interface{}{
			"id":          pk,
			"freight":     freight,
			"shipCountry": country,
		}
		if err := mgr.IndexDocument("order", pk, doc); err != nil {
			t.Fatalf("index order %s: %v", pk, err)
		}
		perCountryFreight[country] = append(perCountryFreight[country], freight)
		seeded++
	}
	if seeded < 800 {
		t.Fatalf("orders.csv seed drift: expected ~830 rows, got %d", seeded)
	}
	if len(perCountryFreight) < 15 {
		t.Fatalf("expected ≥15 distinct shipCountry buckets, got %d", len(perCountryFreight))
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	handler := objectset.NewHandler(executor, mgr, store)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/aggregate", handler.Aggregate)

	const (
		relTol     = 0.05 // US-018 5% bound
		absTol     = 0.01 // guard against tiny denominators
		minBucketN = 10   // skip microscopic buckets where nearest-rank/Hdr rounding dominates
	)

	// ---- Call 1: scalar p95 per shipCountry --------------------------------
	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "order",
		},
		"aggregation": []map[string]interface{}{
			{"type": "count", "name": "count"},
			{"type": "approximatePercentile", "field": "freight", "percentile": 95.0, "name": "p95Freight"},
		},
		"groupBy": []map[string]interface{}{
			{"type": "exact", "field": "shipCountry", "maxGroupCount": 100},
		},
	}
	rawBody, _ := json.Marshal(body)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v2/ontologies/northwind_percentile/objectSets/aggregate",
		bytes.NewReader(rawBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("call 1: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp1 map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("call 1 unmarshal: %v", err)
	}
	accuracy1, _ := resp1["accuracy"].(string)
	if accuracy1 != "ACCURATE" && accuracy1 != "APPROXIMATE" {
		t.Errorf("call 1: accuracy = %q, want ACCURATE or APPROXIMATE", accuracy1)
	}
	rows1, _ := resp1["data"].([]interface{})
	if len(rows1) == 0 {
		t.Fatalf("call 1: empty data: %s", rr.Body.String())
	}

	checked := 0
	for _, raw := range rows1 {
		row, _ := raw.(map[string]interface{})
		grp, _ := row["group"].(map[string]interface{})
		metrics, _ := row["metrics"].([]interface{})
		country, _ := grp["shipCountry"].(string)
		if country == "" {
			continue
		}
		values, ok := perCountryFreight[country]
		if !ok {
			t.Errorf("call 1: unexpected bucket %q", country)
			continue
		}
		gotCount, gotP95, err := extractCountScalarPercentile(metrics, "p95Freight")
		if err != nil {
			t.Errorf("call 1 %s: %v", country, err)
			continue
		}
		if gotCount != len(values) {
			t.Errorf("call 1 %s: count=%d, want %d", country, gotCount, len(values))
		}
		if len(values) < minBucketN {
			continue
		}
		want := referencePercentileForBucket(values, 95)
		if !percentileWithinTolerance(gotP95, want, relTol, absTol) {
			rel := relError(gotP95, want)
			t.Errorf("call 1 %s: p95=%.4f want≈%.4f (rel err %.4f, N=%d)",
				country, gotP95, want, rel, len(values))
		}
		checked++
	}
	if checked == 0 {
		t.Fatalf("call 1: no bucket had ≥%d values — fixture drift?", minBucketN)
	}

	// ---- Call 2: multi-percentile [50, 95, 99] per shipCountry -------------
	body2 := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "order",
		},
		"aggregation": []map[string]interface{}{
			{
				"type":        "approximatePercentile",
				"field":       "freight",
				"percentiles": []float64{50, 95, 99},
				"name":        "freightBands",
			},
		},
		"groupBy": []map[string]interface{}{
			{"type": "exact", "field": "shipCountry", "maxGroupCount": 100},
		},
	}
	rawBody2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest(
		http.MethodPost,
		"/api/v2/ontologies/northwind_percentile/objectSets/aggregate",
		bytes.NewReader(rawBody2),
	)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("call 2: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	var resp2 map[string]interface{}
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("call 2 unmarshal: %v", err)
	}
	accuracy2, _ := resp2["accuracy"].(string)
	if accuracy2 != "ACCURATE" && accuracy2 != "APPROXIMATE" {
		t.Errorf("call 2: accuracy = %q, want ACCURATE or APPROXIMATE", accuracy2)
	}
	rows2, _ := resp2["data"].([]interface{})
	if len(rows2) == 0 {
		t.Fatalf("call 2: empty data: %s", rr2.Body.String())
	}

	multiChecked := 0
	for _, raw := range rows2 {
		row, _ := raw.(map[string]interface{})
		grp, _ := row["group"].(map[string]interface{})
		metrics, _ := row["metrics"].([]interface{})
		country, _ := grp["shipCountry"].(string)
		if country == "" {
			continue
		}
		values, ok := perCountryFreight[country]
		if !ok || len(values) < minBucketN {
			continue
		}
		gotBands, err := extractMultiPercentile(metrics, "freightBands")
		if err != nil {
			t.Errorf("call 2 %s: %v", country, err)
			continue
		}
		for _, p := range []float64{50, 95, 99} {
			key := fmt.Sprintf("%g", p)
			got, ok := gotBands[key]
			if !ok {
				t.Errorf("call 2 %s: missing percentile %q in %v", country, key, gotBands)
				continue
			}
			want := referencePercentileForBucket(values, p)
			if !percentileWithinTolerance(got, want, relTol, absTol) {
				rel := relError(got, want)
				t.Errorf("call 2 %s p%v: got %.4f want≈%.4f (rel err %.4f, N=%d)",
					country, p, got, want, rel, len(values))
			}
		}
		multiChecked++
	}
	if multiChecked == 0 {
		t.Fatalf("call 2: no bucket had ≥%d values — fixture drift?", minBucketN)
	}
}

// referencePercentileForBucket is the ground-truth percentile used to bound
// the HdrHistogram approximation per country bucket. It mirrors the same
// rank-rounding as hdrhistogram-go's Histogram.ValueAtPercentile:
//
//	count = int64((P/100)*N + 0.5); max(1); min(N)
//	return sorted[count-1]
//
// Nearest-rank (ceil) would be an equally valid percentile definition, but it
// disagrees with HdrHistogram for small N (Argentina N=16 at p95: nearest-rank
// → sorted[15]=217.86 vs HdrHistogram → sorted[14]=90.85, one full rank
// apart). Since US-036 is checking approximation quality, not contending
// definitions, the reference uses the same tie-breaking as the algorithm; the
// remaining delta is purely HdrHistogram's 3-sig-fig bucket precision, which
// stays well inside the 5% relative tolerance imposed below.
func referencePercentileForBucket(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 100 {
		return sorted[len(sorted)-1]
	}
	count := int((percentile/100.0)*float64(len(sorted)) + 0.5)
	if count < 1 {
		count = 1
	}
	if count > len(sorted) {
		count = len(sorted)
	}
	return sorted[count-1]
}

// percentileWithinTolerance compares a percentile approximation against an
// exact reference using the looser of `relTol` relative error and `absTol`
// absolute error. The absolute floor prevents false positives when the exact
// value is unusually small — freight values as low as 0.3 exist in Northwind
// and would otherwise make any rounding explode the relative error.
func percentileWithinTolerance(got, want, relTol, absTol float64) bool {
	if math.IsNaN(got) || math.IsNaN(want) {
		return false
	}
	diff := math.Abs(got - want)
	if diff <= absTol {
		return true
	}
	if want == 0 {
		return diff <= absTol
	}
	return diff/math.Abs(want) <= relTol
}

func relError(got, want float64) float64 {
	if want == 0 {
		return math.Abs(got - want)
	}
	return math.Abs(got-want) / math.Abs(want)
}

// extractCountScalarPercentile pulls a `count` metric and a named scalar
// percentile metric out of a decoded metrics slice. Returns descriptive errors
// so a shape mismatch (e.g. multi-percentile map slipping into a scalar row)
// surfaces immediately.
func extractCountScalarPercentile(metrics []interface{}, percentileName string) (int, float64, error) {
	var count int
	var pct float64
	var countSet, pctSet bool
	for _, raw := range metrics {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return 0, 0, fmt.Errorf("metric entry not object: %T", raw)
		}
		name, _ := m["name"].(string)
		value := m["value"]
		switch name {
		case "count":
			f, ok := value.(float64)
			if !ok {
				return 0, 0, fmt.Errorf("count value not float64: %T", value)
			}
			count = int(f)
			countSet = true
		case percentileName:
			f, ok := value.(float64)
			if !ok {
				return 0, 0, fmt.Errorf("%s value not scalar float64: %T", percentileName, value)
			}
			pct = f
			pctSet = true
		}
	}
	if !countSet {
		return 0, 0, fmt.Errorf("count metric missing")
	}
	if !pctSet {
		return 0, 0, fmt.Errorf("%s metric missing", percentileName)
	}
	return count, pct, nil
}

// extractMultiPercentile returns the map[string]float64 value of a named
// multi-percentile metric. The wire format comes through json.Unmarshal as
// map[string]interface{} with float64 leaves, which is recoerced here.
func extractMultiPercentile(metrics []interface{}, name string) (map[string]float64, error) {
	for _, raw := range metrics {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if m["name"] != name {
			continue
		}
		value := m["value"]
		asMap, ok := value.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s value not map: %T", name, value)
		}
		out := make(map[string]float64, len(asMap))
		for k, v := range asMap {
			f, ok := v.(float64)
			if !ok {
				return nil, fmt.Errorf("%s[%s] not float64: %T", name, k, v)
			}
			out[k] = f
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s metric missing", name)
}
