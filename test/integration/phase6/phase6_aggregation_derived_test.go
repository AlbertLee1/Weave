//go:build integration

package phase6_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
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

// TestAggregationDerived_CustomerOrderCountByCountry is the Phase 6 cross-US
// scenario tying withProperties (US-001/US-002) to Aggregation (Phase 5). A
// Customer ObjectSet is wrapped in a `withProperties` node that attaches a
// forward-count derived value `orderCount` via a fake `customerOrders` link.
// The test then POSTs to /objectSets/aggregate asking for groupBy=country with
// metrics count + avg(orderCount), and asserts every bucket matches a
// ground-truth aggregation computed directly from the seeded fixture.
//
// This exercises the full handler path (Execute → derived value attach →
// aggregate), so if aggregation silently falls back to reading orderCount from
// the base Bleve index — where it does NOT exist and would read back as nil /
// zero — the assertions fail loudly.
func TestAggregationDerived_CustomerOrderCountByCountry(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "northwind_aggderived",
		DisplayName: "Northwind (Agg × Derived)",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}
	ot := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "customer",
		DisplayName: "Customer",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, ot); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// Bleve index for customers. country + contactTitle are not_analyzed so
	// exact groupBy facets split them by raw string value, matching the live
	// typeclass contract.
	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "country", BaseType: "string", IsSearchable: true, Analyzer: "not_analyzed"},
		{APIName: "contactTitle", BaseType: "string", IsSearchable: true, Analyzer: "not_analyzed"},
	}
	if _, err := mgr.EnsureIndex("customer", props); err != nil {
		t.Fatalf("ensure customer index: %v", err)
	}

	// Seed customers from the Northwind CSV fixture. Each row is indexed with
	// its country + contactTitle so the aggregation path has real group
	// buckets to facet on.
	f, err := os.Open(northwindFixturePath(t, "customers.csv"))
	if err != nil {
		t.Fatalf("open customers.csv: %v", err)
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read customers.csv: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("customers.csv empty")
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
	pkIdx := idxOf("customerID")
	nameIdx := idxOf("companyName")
	countryIdx := idxOf("country")
	titleIdx := idxOf("contactTitle")
	if pkIdx < 0 || nameIdx < 0 || countryIdx < 0 || titleIdx < 0 {
		t.Fatalf("customers.csv missing expected columns")
	}

	type customerRow struct {
		pk      string
		country string
		title   string
	}
	var seeded []customerRow
	for _, row := range records[1:] {
		if pkIdx >= len(row) {
			continue
		}
		pk := row[pkIdx]
		doc := map[string]interface{}{
			"id":           pk,
			"name":         row[nameIdx],
			"country":      row[countryIdx],
			"contactTitle": row[titleIdx],
		}
		if err := mgr.IndexDocument("customer", pk, doc); err != nil {
			t.Fatalf("index customer %s: %v", pk, err)
		}
		seeded = append(seeded, customerRow{pk: pk, country: row[countryIdx], title: row[titleIdx]})
	}
	if len(seeded) != 91 {
		t.Fatalf("northwind customer fixture drift: expected 91 rows, got %d", len(seeded))
	}

	// Deterministic orderCount plan: every Nth customer gets N-1 orders via a
	// synthetic ownerOrders link, cycling through counts 0..4. The resulting
	// distribution is dense enough that every country bucket sees multiple
	// values, so the avg(orderCount) assertion checks real variance — not a
	// constant.
	edges := map[string]map[string][]string{"customerOrders": {}}
	perPKCount := make(map[string]int, len(seeded))
	for i, row := range seeded {
		count := i % 5 // 0, 1, 2, 3, 4
		perPKCount[row.pk] = count
		if count == 0 {
			continue
		}
		targets := make([]string, 0, count)
		for k := 0; k < count; k++ {
			targets = append(targets, fmt.Sprintf("ord-%s-%d", row.pk, k))
		}
		edges["customerOrders"][row.pk] = targets
	}

	linkResolver := &perPKLinkResolver{edges: edges}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, linkResolver, store)
	handler := objectset.NewHandler(executor, mgr, store)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/aggregate", handler.Aggregate)

	// ---- Ground truth ------------------------------------------------------
	type groupKey struct{ country, title string }
	perCountryCount := map[string]int{}
	perCountrySum := map[string]int{}
	perGroupCount := map[groupKey]int{}
	perGroupSum := map[groupKey]int{}
	for _, row := range seeded {
		c := perPKCount[row.pk]
		perCountryCount[row.country]++
		perCountrySum[row.country] += c
		k := groupKey{row.country, row.title}
		perGroupCount[k]++
		perGroupSum[k] += c
	}

	// ---- Call 1: single-layer groupBy=country, count + avg(orderCount) -----
	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "withProperties",
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "customer",
			},
			"derivedProperties": []map[string]interface{}{
				{
					"name":      "orderCount",
					"link":      "customerOrders",
					"direction": "forward",
					"metric":    "count",
				},
			},
		},
		"aggregation": []map[string]interface{}{
			{"type": "count", "name": "count"},
			{"type": "avg", "field": "orderCount", "name": "avgOrders"},
		},
		"groupBy": []map[string]interface{}{
			{"type": "exact", "field": "country"},
		},
	}
	rawBody, _ := json.Marshal(body)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v2/ontologies/northwind_aggderived/objectSets/aggregate",
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
	rows1, _ := resp1["data"].([]interface{})
	if len(rows1) == 0 {
		t.Fatalf("call 1: no rows returned: %s", rr.Body.String())
	}
	seenCountries := map[string]bool{}
	for _, raw := range rows1 {
		row, _ := raw.(map[string]interface{})
		grp, _ := row["group"].(map[string]interface{})
		metrics, _ := row["metrics"].([]interface{})
		country, _ := grp["country"].(string)
		seenCountries[country] = true

		wantCount, ok := perCountryCount[country]
		if !ok {
			t.Errorf("call 1: unexpected country bucket %q", country)
			continue
		}
		wantAvg := float64(perCountrySum[country]) / float64(wantCount)

		gotCount, gotAvg, err := extractCountAvg(metrics)
		if err != nil {
			t.Errorf("call 1 country=%s: %v", country, err)
			continue
		}
		if gotCount != wantCount {
			t.Errorf("call 1 country=%s: count=%d, want %d", country, gotCount, wantCount)
		}
		if !approxEqual(gotAvg, wantAvg, 1e-9) {
			t.Errorf("call 1 country=%s: avg=%v, want %v", country, gotAvg, wantAvg)
		}
	}
	for country := range perCountryCount {
		if !seenCountries[country] {
			t.Errorf("call 1: missing country bucket %q", country)
		}
	}

	// ---- Call 2: two-layer groupBy=country × contactTitle ------------------
	body2 := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "withProperties",
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "customer",
			},
			"derivedProperties": []map[string]interface{}{
				{
					"name":      "orderCount",
					"link":      "customerOrders",
					"direction": "forward",
					"metric":    "count",
				},
			},
		},
		"aggregation": []map[string]interface{}{
			{"type": "count", "name": "count"},
			{"type": "avg", "field": "orderCount", "name": "avgOrders"},
		},
		"groupBy": []map[string]interface{}{
			{"type": "exact", "field": "country"},
			{"type": "exact", "field": "contactTitle"},
		},
	}
	rawBody2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest(
		http.MethodPost,
		"/api/v2/ontologies/northwind_aggderived/objectSets/aggregate",
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
	rows2, _ := resp2["data"].([]interface{})
	seenKeys := map[groupKey]bool{}
	for _, raw := range rows2 {
		row, _ := raw.(map[string]interface{})
		grp, _ := row["group"].(map[string]interface{})
		metrics, _ := row["metrics"].([]interface{})
		country, _ := grp["country"].(string)
		title, _ := grp["contactTitle"].(string)
		k := groupKey{country: country, title: title}
		seenKeys[k] = true

		wantCount, ok := perGroupCount[k]
		if !ok {
			t.Errorf("call 2: unexpected group bucket %+v", k)
			continue
		}
		wantAvg := float64(perGroupSum[k]) / float64(wantCount)

		gotCount, gotAvg, err := extractCountAvg(metrics)
		if err != nil {
			t.Errorf("call 2 %+v: %v", k, err)
			continue
		}
		if gotCount != wantCount {
			t.Errorf("call 2 %+v: count=%d, want %d", k, gotCount, wantCount)
		}
		if !approxEqual(gotAvg, wantAvg, 1e-9) {
			t.Errorf("call 2 %+v: avg=%v, want %v", k, gotAvg, wantAvg)
		}
	}
	// Ensure every non-empty (country, title) combo shows up.
	missing := 0
	var missingKeys []string
	for k := range perGroupCount {
		if !seenKeys[k] {
			missing++
			missingKeys = append(missingKeys, fmt.Sprintf("%s/%s", k.country, k.title))
		}
	}
	if missing > 0 {
		sort.Strings(missingKeys)
		t.Errorf("call 2: missing %d group buckets: %v", missing, missingKeys)
	}
}

// extractCountAvg pulls the named "count" and "avgOrders" metrics out of a
// decoded aggregation row. count comes back as a JSON number (uint64 on the
// wire) and avgOrders as float64 — both surface through json.Unmarshal as
// float64. Accepts a wrong-shape payload with a clear error instead of a
// silent zero.
func extractCountAvg(metrics []interface{}) (int, float64, error) {
	var count int
	var avg float64
	var countSet, avgSet bool
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
		case "avgOrders":
			if value == nil {
				avg = 0
				avgSet = true
				continue
			}
			f, ok := value.(float64)
			if !ok {
				return 0, 0, fmt.Errorf("avgOrders value not float64: %T", value)
			}
			avg = f
			avgSet = true
		}
	}
	if !countSet {
		return 0, 0, fmt.Errorf("count metric missing")
	}
	if !avgSet {
		return 0, 0, fmt.Errorf("avgOrders metric missing")
	}
	return count, avg, nil
}

func approxEqual(a, b, tol float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tol
}
