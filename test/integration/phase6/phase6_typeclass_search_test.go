//go:build integration

package phase6_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestTypeClassSearch_CountryKeywordAndDescriptionStemmed is the Phase 6
// cross-US gate test that ties TypeClass-driven Bleve field mapping (US-050)
// to Where-clause search on loadObjects.
//
// Scenario:
//   - customer.country is indexed with analyzer.not_analyzed → Keyword field
//     (case-sensitive exact match).
//   - customer.description is indexed with analyzer.standard → English
//     snowball (lowercase + Porter stemmer).
//
// Assertions:
//  1. filter { country eq "usa" } returns 0 matches. The indexed token is
//     raw "USA" (keyword analyzer is identity) so the lowercase query must
//     not collide with it.
//  2. filter { country eq "USA" } returns exactly the rows whose country
//     column is "USA" — control that the keyword mapping is wired at all.
//  3. filter { description contains "run" } returns every row whose
//     description contains a Porter stem that collapses to "run" (e.g.
//     "runs", "running"), while documents that only contain unrelated
//     tokens ("boat", "orders") do not match. This proves the standard
//     analyzer is actually running at index time — without the stemmer
//     the base TermQuery("run") path would miss every "runs"/"running"
//     document and return 0 rows.
func TestTypeClassSearch_CountryKeywordAndDescriptionStemmed(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "northwind_typeclass",
		DisplayName: "Northwind (TypeClass × Search)",
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

	// Bleve index:
	//   * country → not_analyzed keyword (exact, case-sensitive)
	//   * description → standard (English snowball → stemmed)
	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "country", BaseType: "string", IsSearchable: true, Analyzer: "not_analyzed"},
		{APIName: "description", BaseType: "string", IsSearchable: true, Analyzer: "standard"},
	}
	if _, err := mgr.EnsureIndex("customer", props); err != nil {
		t.Fatalf("ensure customer index: %v", err)
	}

	// Deterministic fixture. Country cases are intentionally mixed so lowercase
	// queries do not accidentally hit on a casual match. Descriptions use the
	// Porter-safe stem triple run/runs/running (the codebase pattern note
	// explicitly calls this out as a stable fixture for English snowball).
	rows := []struct {
		pk, country, description string
	}{
		{"ALFKI", "Germany", "The supplier runs daily deliveries"},
		{"ANATR", "Mexico", "The wholesaler ran late last quarter"},
		{"ANTON", "Mexico", "Running promotions every week"},
		{"BOTTM", "Canada", "Coastal storage with boat access"},
		{"BSBEV", "UK", "Beverage specialist in London"},
		{"USAAA", "USA", "Bulk orders and imports"},
		{"USABB", "USA", "Primary distribution partner in Boston"},
	}
	// Expected outcomes ---------------------------------------------------
	// country == "USA" rows: USAAA, USABB.
	wantExactUSA := []string{"USAAA", "USABB"}
	sort.Strings(wantExactUSA)
	// description tokens stemmed to "run": "runs daily" (ALFKI) and
	// "Running promotions" (ANTON). "ran" does NOT stem to "run" under
	// Porter English, so ANATR is excluded. Everything else is unrelated.
	wantStemmedRun := []string{"ALFKI", "ANTON"}
	sort.Strings(wantStemmedRun)

	for _, r := range rows {
		doc := map[string]interface{}{
			"id":          r.pk,
			"country":     r.country,
			"description": r.description,
		}
		if err := mgr.IndexDocument("customer", r.pk, doc); err != nil {
			t.Fatalf("index %s: %v", r.pk, err)
		}
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	handler := objectset.NewHandler(executor, mgr, store)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

	call := func(t *testing.T, label string, where map[string]interface{}) (int, []string) {
		t.Helper()
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type": "filter",
				"objectSet": map[string]interface{}{
					"type":       "base",
					"objectType": "customer",
				},
				"where": where,
			},
			"select":   []string{"id", "country", "description"},
			"pageSize": 100,
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v2/ontologies/northwind_typeclass/objectSets/loadObjects",
			bytes.NewReader(raw),
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", label, rr.Code, rr.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: unmarshal: %v", label, err)
		}
		tc, _ := resp["totalCount"].(string)
		total := 0
		for _, ch := range tc {
			total = total*10 + int(ch-'0')
		}
		data, _ := resp["data"].([]interface{})
		pks := make([]string, 0, len(data))
		for _, raw := range data {
			row, _ := raw.(map[string]interface{})
			if pk, ok := row["__primaryKey"].(string); ok && pk != "" {
				pks = append(pks, pk)
			}
		}
		sort.Strings(pks)
		return total, pks
	}

	// ---- 1. country == "usa" (lowercase) must return ZERO rows -----------
	total, pks := call(t, "country eq usa", map[string]interface{}{
		"type":  "eq",
		"field": "country",
		"value": "usa",
	})
	if total != 0 || len(pks) != 0 {
		t.Errorf("country eq usa: expected 0 matches, got totalCount=%d pks=%v", total, pks)
	}

	// ---- 2. country == "USA" control: the not_analyzed field is wired ----
	total, pks = call(t, "country eq USA", map[string]interface{}{
		"type":  "eq",
		"field": "country",
		"value": "USA",
	})
	if total != len(wantExactUSA) {
		t.Errorf("country eq USA: expected totalCount=%d, got %d", len(wantExactUSA), total)
	}
	if !sameStringSlice(pks, wantExactUSA) {
		t.Errorf("country eq USA: got pks=%v want=%v", pks, wantExactUSA)
	}

	// ---- 3. description contains "run" exercises the English stemmer ----
	total, pks = call(t, "description contains run", map[string]interface{}{
		"type":  "contains",
		"field": "description",
		"value": "run",
	})
	if total != len(wantStemmedRun) {
		t.Errorf("description contains run: expected totalCount=%d, got %d (pks=%v)", len(wantStemmedRun), total, pks)
	}
	if !sameStringSlice(pks, wantStemmedRun) {
		t.Errorf("description contains run: got pks=%v want=%v", pks, wantStemmedRun)
	}

	// ---- 4. control: description contains a non-stemmed token ------------
	// "Running" should go through the Porter stemmer at query time as well,
	// BUT the existing convertContains in pkg/oss/where uses TermQuery which
	// bypasses the query-side analyzer. "Running" therefore does NOT stem
	// down to "run" at query time and must return zero matches. This pins
	// the current (documented) TermQuery behaviour so future regressions
	// toward MatchQuery surface loudly here.
	total, _ = call(t, "description contains Running", map[string]interface{}{
		"type":  "contains",
		"field": "description",
		"value": "Running",
	})
	if total != 0 {
		t.Errorf("description contains Running: expected 0 (TermQuery bypasses query-side stemmer), got %d", total)
	}
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
