//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/rid"
)

// TestMultiGroupBy_NorthwindOrders is the US-015 acceptance scenario: a 3-layer
// nested aggregation (country ExactValue × price FixedWidth(100) × orderDate
// Duration(P3M)) runs end-to-end against a PG-backed Orders ObjectType whose
// Bleve mapping is hydrated through pkg/index.BuildMapping. The test seeds a
// 105-order Northwind-flavoured fixture where every leaf bucket's count, sum
// and avg can be hand-computed, then asserts every leaf in the response.
//
// The fixture is deliberate:
//   - 5 countries × 3 quarters × 7 orders = 105 docs (> the 100-row floor
//     called out in US-015 acceptance criteria).
//   - Each (country, quarter) combo carries the SAME 7-price grid so the
//     inner fixedWidth layer produces identical bucket boundaries regardless
//     of which country scope it was narrowed to. This keeps the expected
//     leaf map compact and independent of bleve's facet ordering.
//   - Prices 10, 50, 120, 180, 220, 280, 350 cleanly straddle 4 width-100
//     buckets: [0,100)=2 rows, [100,200)=2 rows, [200,300)=2 rows,
//     [300,400)=1 row. Sum of row counts per (country, quarter) is 7.
//   - Dates are chosen so each quarter lands cleanly in its OWN 90-day
//     Duration bucket starting from the Unix epoch: with durSec = 90 * 86400,
//     the engine floors hit.Fields[orderDate] (an epoch float64 emitted by
//     bleve's datetime FieldMapping) to the nearest multiple of durSec.
//     Three distinct epoch multiples → three distinct bucket rows per
//     (country, priceBucket) combo, matching the expected quarterly split.
//
// Total leaf rows: 5 × 4 × 3 = 60. Total count across leaves: 105.
func TestMultiGroupBy_NorthwindOrders(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "northwind_multigroupby",
		DisplayName: "Northwind MultiGroupBy Demo",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	// Orders ObjectType — shipCountry is not_analyzed (case-sensitive exact
	// match, same typeclass contract as US-012), freight is numeric double
	// (the metric + fixedWidth target), orderDate is a timestamp (driving
	// the duration layer via bleve's datetime FieldMapping).
	order := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "Order",
		DisplayName: "Order",
		PrimaryKey:  "orderID",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, order); err != nil {
		t.Fatalf("create Order: %v", err)
	}
	orderProps := []*oms.Property{
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: order.RID,
			APIName:       "orderID",
			BaseType:      "string",
			IsSearchable:  true,
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: order.RID,
			APIName:       "shipCountry",
			BaseType:      "string",
			IsSearchable:  true,
			TypeConfig:    json.RawMessage(`{"analyzer":"not_analyzed"}`),
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: order.RID,
			APIName:       "freight",
			BaseType:      "double",
			IsSearchable:  true,
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: order.RID,
			APIName:       "orderDate",
			BaseType:      "timestamp",
			IsSearchable:  true,
		},
	}
	for _, p := range orderProps {
		if err := repo.CreateProperty(ctx, p); err != nil {
			t.Fatalf("create Order.%s: %v", p.APIName, err)
		}
	}

	// Re-read with populated Properties slice so BuildMapping sees the
	// persisted TypeConfig path (matches the live handler bootstrap).
	fresh, err := repo.GetObjectTypeByAPIName(ctx, ont.RID, order.APIName)
	if err != nil {
		t.Fatalf("reload Order: %v", err)
	}
	props, err := repo.ListProperties(ctx, fresh.RID)
	if err != nil {
		t.Fatalf("list Order properties: %v", err)
	}
	fresh.Properties = props

	// Pin the analyzer contract BEFORE we start aggregating, so a silent
	// TypeConfig write-side drop cannot mask a wrong-for-right-reason pass.
	for _, p := range fresh.Properties {
		if p.APIName == "shipCountry" {
			if got := index.AnalyzerFromTypeConfig(p.TypeConfig); got != index.AnalyzerNotAnalyzed {
				t.Fatalf("shipCountry analyzer = %q, want %q", got, index.AnalyzerNotAnalyzed)
			}
		}
	}

	idx, err := bleve.NewMemOnly(index.BuildMapping(fresh))
	if err != nil {
		t.Fatalf("build Order mapping: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	// -- Seed 105 orders -----------------------------------------------------
	countries := []string{"Germany", "France", "Spain", "UK", "USA"}
	// Three "quarters" of 2024 mapped onto three anchor dates that each fall
	// squarely inside a distinct 90-day duration bucket counted from the
	// Unix epoch. We do NOT depend on calendar-quarter boundaries — we only
	// care that the engine's floor(epoch/(90*86400))*(90*86400) assignment
	// places these three dates in three different slots.
	quarterDates := []time.Time{
		time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2024, 4, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2024, 7, 15, 12, 0, 0, 0, time.UTC),
	}
	// Same 7-row price grid for every (country, quarter) combo. Designed to
	// straddle exactly four width-100 buckets with known per-bucket row
	// counts. See the comment on TestMultiGroupBy_NorthwindOrders for the
	// per-bucket breakdown.
	prices := []float64{10, 50, 120, 180, 220, 280, 350}

	// Sanity: the three quarter dates must land in three distinct 90-day
	// duration buckets. If bucketStart math changes upstream, surface that
	// here rather than in a downstream assertion that looks like a data bug.
	const durSec int64 = 90 * 86400
	quarterBuckets := make(map[string]string, len(quarterDates))
	seenBucket := map[int64]bool{}
	for _, d := range quarterDates {
		bs := (d.Unix() / durSec) * durSec
		if seenBucket[bs] {
			t.Fatalf("quarter dates collide into the same 90-day bucket: %d", bs)
		}
		seenBucket[bs] = true
		quarterBuckets[d.Format(time.RFC3339)] = time.Unix(bs, 0).UTC().Format(time.RFC3339)
	}

	type leafKey struct {
		country     string
		priceBucket string
		quarter     string // the RFC3339 bucket start as emitted by engine
	}
	expected := map[leafKey]struct {
		count int
		sum   float64
	}{}
	priceBucketName := func(p float64) string {
		lo := math.Floor(p/100) * 100
		return fmt.Sprintf("[%.0f,%.0f)", lo, lo+100)
	}

	docID := 10000
	for _, country := range countries {
		for _, q := range quarterDates {
			for _, price := range prices {
				docID++
				doc := map[string]interface{}{
					"orderID":     fmt.Sprintf("O%d", docID),
					"shipCountry": country,
					"freight":     price,
					"orderDate":   q.Format(time.RFC3339),
				}
				if err := idx.Index(doc["orderID"].(string), doc); err != nil {
					t.Fatalf("index %s: %v", doc["orderID"], err)
				}
				key := leafKey{
					country:     country,
					priceBucket: priceBucketName(price),
					quarter:     quarterBuckets[q.Format(time.RFC3339)],
				}
				agg := expected[key]
				agg.count++
				agg.sum += price
				expected[key] = agg
			}
		}
	}
	if got := len(expected); got != 60 {
		t.Fatalf("expected 60 distinct leaf buckets, got %d", got)
	}

	// -- Run the aggregation -------------------------------------------------
	eng := aggregation.NewEngine()
	width := 100.0
	resp, err := eng.Aggregate(idx, &aggregation.AggregationRequest{
		Aggregations: []aggregation.AggregationSpec{
			{Type: "count"},
			{Type: "sum", Field: "freight"},
			{Type: "avg", Field: "freight"},
		},
		GroupBy: []aggregation.GroupBySpec{
			{Type: "exact", Field: "shipCountry"},
			{Type: "fixedWidth", Field: "freight", Width: &width},
			{
				Type:          "duration",
				Field:         "orderDate",
				DurationValue: &aggregation.DurationValue{Unit: "DAYS", Value: 90},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// -- Assertions ----------------------------------------------------------
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("accuracy = %q, want ACCURATE (105 < MaxDocScanSize=10000)", resp.Accuracy)
	}
	if got := len(resp.Data); got != 60 {
		t.Fatalf("leaf row count = %d, want 60", got)
	}

	findMetric := func(row aggregation.AggregationRow, name string) (interface{}, bool) {
		for _, m := range row.Metrics {
			if m.Name == name {
				return m.Value, true
			}
		}
		return nil, false
	}

	seenLeaves := map[leafKey]bool{}
	var totalCount uint64
	var totalSum float64
	for i, row := range resp.Data {
		country, ok := row.Group["shipCountry"].(string)
		if !ok {
			t.Fatalf("row %d missing shipCountry: %+v", i, row.Group)
		}
		priceBucket, ok := row.Group["freight"].(string)
		if !ok {
			t.Fatalf("row %d missing freight bucket: %+v", i, row.Group)
		}
		quarter, ok := row.Group["orderDate"].(string)
		if !ok {
			t.Fatalf("row %d missing orderDate bucket: %+v", i, row.Group)
		}
		key := leafKey{country: country, priceBucket: priceBucket, quarter: quarter}
		if seenLeaves[key] {
			t.Errorf("duplicate leaf row for %+v", key)
		}
		seenLeaves[key] = true

		want, ok := expected[key]
		if !ok {
			t.Errorf("unexpected leaf row %+v", key)
			continue
		}

		countVal, ok := findMetric(row, "count")
		if !ok {
			t.Errorf("%+v: missing count metric", key)
			continue
		}
		countUint, ok := countVal.(uint64)
		if !ok {
			t.Errorf("%+v: count metric type = %T, want uint64", key, countVal)
			continue
		}
		if int(countUint) != want.count {
			t.Errorf("%+v: count = %d, want %d", key, countUint, want.count)
		}
		totalCount += countUint

		sumVal, ok := findMetric(row, "freight.sum")
		if !ok {
			t.Errorf("%+v: missing freight.sum metric", key)
			continue
		}
		sumFloat, ok := sumVal.(float64)
		if !ok {
			t.Errorf("%+v: sum metric type = %T, want float64", key, sumVal)
			continue
		}
		if math.Abs(sumFloat-want.sum) > 1e-6 {
			t.Errorf("%+v: sum = %v, want %v", key, sumFloat, want.sum)
		}
		totalSum += sumFloat

		avgVal, ok := findMetric(row, "freight.avg")
		if !ok {
			t.Errorf("%+v: missing freight.avg metric", key)
			continue
		}
		avgFloat, ok := avgVal.(float64)
		if !ok {
			t.Errorf("%+v: avg metric type = %T, want float64", key, avgVal)
			continue
		}
		wantAvg := want.sum / float64(want.count)
		if math.Abs(avgFloat-wantAvg) > 1e-6 {
			t.Errorf("%+v: avg = %v, want %v", key, avgFloat, wantAvg)
		}
	}

	if totalCount != 105 {
		t.Errorf("sum of leaf counts = %d, want 105", totalCount)
	}
	// Hand computation: per (country, quarter) sum is 10+50+120+180+220+280+350 = 1210.
	// 5 countries × 3 quarters × 1210 = 18150.
	if math.Abs(totalSum-18150) > 1e-6 {
		t.Errorf("sum of leaf sums = %v, want 18150", totalSum)
	}

	// Outer-layer country ordering must be ASC per sortGroupEntries contract.
	var countryOrder []string
	seen := map[string]bool{}
	for _, row := range resp.Data {
		c := row.Group["shipCountry"].(string)
		if !seen[c] {
			seen[c] = true
			countryOrder = append(countryOrder, c)
		}
	}
	wantCountryOrder := []string{"France", "Germany", "Spain", "UK", "USA"}
	if len(countryOrder) != len(wantCountryOrder) {
		t.Fatalf("country order len = %d, want %d (%v)", len(countryOrder), len(wantCountryOrder), countryOrder)
	}
	for i, c := range wantCountryOrder {
		if countryOrder[i] != c {
			t.Errorf("country order[%d] = %q, want %q (full=%v)", i, countryOrder[i], c, countryOrder)
		}
	}
}
