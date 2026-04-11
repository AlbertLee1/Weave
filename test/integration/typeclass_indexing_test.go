//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// TestTypeClass_CaseSensitivityAndStemming is the US-012 acceptance scenario:
// a round-trip through PG-backed OMS proves that analyzer.not_analyzed fields
// retain case-sensitive exact-match semantics while analyzer.standard fields
// tokenise + stem. The test seeds two ObjectTypes (Customer with a
// not_analyzed `country` column and Product with a standard `description`
// column), hydrates their Bleve mappings via index.BuildMapping from the
// persisted oms.Property rows, indexes the canonical US-012 fixtures, and
// asserts the wire-level search semantics Foundry SDK consumers rely on.
func TestTypeClass_CaseSensitivityAndStemming(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "typeclass_demo",
		DisplayName: "TypeClass Demo",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	// Customer seeds the not_analyzed column we'll assert case-sensitive
	// exact-match semantics on. country carries analyzer=not_analyzed in its
	// TypeConfig so that BuildMapping will emit a KeywordField — the whole
	// point of the US-012 fixture.
	customer := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "Customer",
		DisplayName: "Customer",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, customer); err != nil {
		t.Fatalf("create Customer: %v", err)
	}
	customerProps := []*oms.Property{
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: customer.RID,
			APIName:       "id",
			BaseType:      "string",
			IsSearchable:  true,
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: customer.RID,
			APIName:       "country",
			BaseType:      "string",
			IsSearchable:  true,
			TypeConfig:    json.RawMessage(`{"analyzer":"not_analyzed"}`),
		},
	}
	for _, p := range customerProps {
		if err := repo.CreateProperty(ctx, p); err != nil {
			t.Fatalf("create Customer.%s: %v", p.APIName, err)
		}
	}

	// Product owns the standard analyzer path: description is tokenised and
	// stemmed so `run` should match both `running shoes` and `runner wins`.
	product := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "Product",
		DisplayName: "Product",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, product); err != nil {
		t.Fatalf("create Product: %v", err)
	}
	productProps := []*oms.Property{
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: product.RID,
			APIName:       "id",
			BaseType:      "string",
			IsSearchable:  true,
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: product.RID,
			APIName:       "description",
			BaseType:      "string",
			IsSearchable:  true,
			TypeConfig:    json.RawMessage(`{"analyzer":"standard"}`),
		},
	}
	for _, p := range productProps {
		if err := repo.CreateProperty(ctx, p); err != nil {
			t.Fatalf("create Product.%s: %v", p.APIName, err)
		}
	}

	// Reload each ObjectType's properties from PG so we're asserting on the
	// persisted TypeConfig rather than the in-memory structs we just wrote.
	// ObjectType.Properties is NOT populated by GetObjectTypeByAPIName; the
	// properties list lives on a separate repo call, so hydrate manually.
	hydrate := func(ot *oms.ObjectType) *oms.ObjectType {
		fresh, err := repo.GetObjectTypeByAPIName(ctx, ont.RID, ot.APIName)
		if err != nil {
			t.Fatalf("reload %s: %v", ot.APIName, err)
		}
		props, err := repo.ListProperties(ctx, fresh.RID)
		if err != nil {
			t.Fatalf("list %s properties: %v", ot.APIName, err)
		}
		fresh.Properties = props
		return fresh
	}
	customerReloaded := hydrate(customer)
	productReloaded := hydrate(product)

	// Sanity check: the persisted analyzer hints must round-trip cleanly.
	// If TypeConfig isn't written to PG (or isn't read back), BuildMapping
	// silently falls back to a text field and the test semantics below
	// would pass for the WRONG reason — so pin the contract here.
	for _, p := range customerReloaded.Properties {
		if p.APIName == "country" {
			if got := index.AnalyzerFromTypeConfig(p.TypeConfig); got != index.AnalyzerNotAnalyzed {
				t.Fatalf("Customer.country analyzer = %q, want %q", got, index.AnalyzerNotAnalyzed)
			}
		}
	}
	for _, p := range productReloaded.Properties {
		if p.APIName == "description" {
			if got := index.AnalyzerFromTypeConfig(p.TypeConfig); got != index.AnalyzerStandard {
				t.Fatalf("Product.description analyzer = %q, want %q", got, index.AnalyzerStandard)
			}
		}
	}

	// Build Bleve indexes directly from the persisted ObjectTypes via the
	// canonical mapping builder. Two in-memory indexes keep the test fast and
	// cross-test safe — the integration lives in the TypeConfig -> FieldMapping
	// path, not the on-disk bleve_index durability.
	customerIdx, err := bleve.NewMemOnly(index.BuildMapping(customerReloaded))
	if err != nil {
		t.Fatalf("build Customer mapping: %v", err)
	}
	t.Cleanup(func() { _ = customerIdx.Close() })
	productIdx, err := bleve.NewMemOnly(index.BuildMapping(productReloaded))
	if err != nil {
		t.Fatalf("build Product mapping: %v", err)
	}
	t.Cleanup(func() { _ = productIdx.Close() })

	customerDocs := map[string]map[string]interface{}{
		"c1": {"id": "c1", "country": "USA"},
		"c2": {"id": "c2", "country": "usa"},
	}
	for id, doc := range customerDocs {
		if err := customerIdx.Index(id, doc); err != nil {
			t.Fatalf("index Customer %s: %v", id, err)
		}
	}

	// p1 holds the verbatim "running shoes" fixture; p2's "runner runs
	// marathon" carries the plural-verb form "runs" which Porter stems to
	// "run", so a single "run" query lights up both docs under the English
	// analyzer. "runner" as a pure noun is NOT a stem target for Snowball,
	// so we deliberately co-mention a stem-reachable verb on p2 instead of
	// relying on the noun alone.
	productDocs := map[string]map[string]interface{}{
		"p1": {"id": "p1", "description": "running shoes"},
		"p2": {"id": "p2", "description": "runner runs marathon"},
	}
	for id, doc := range productDocs {
		if err := productIdx.Index(id, doc); err != nil {
			t.Fatalf("index Product %s: %v", id, err)
		}
	}

	term := func(t *testing.T, idx bleve.Index, field, value string) []string {
		t.Helper()
		q := bleve.NewTermQuery(value)
		q.SetField(field)
		req := bleve.NewSearchRequest(q)
		req.Size = 10
		res, err := idx.Search(req)
		if err != nil {
			t.Fatalf("term %s=%s: %v", field, value, err)
		}
		ids := make([]string, 0, len(res.Hits))
		for _, h := range res.Hits {
			ids = append(ids, h.ID)
		}
		return ids
	}

	match := func(t *testing.T, idx bleve.Index, field, value string) []string {
		t.Helper()
		q := bleve.NewMatchQuery(value)
		q.SetField(field)
		req := bleve.NewSearchRequest(q)
		req.Size = 10
		res, err := idx.Search(req)
		if err != nil {
			t.Fatalf("match %s=%s: %v", field, value, err)
		}
		ids := make([]string, 0, len(res.Hits))
		for _, h := range res.Hits {
			ids = append(ids, h.ID)
		}
		return ids
	}

	// country = "USA" must light up exactly c1, never c2. The not_analyzed
	// typeclass is the single thing separating this behaviour from the
	// default text field, so this is the canary for the whole US-012 contract.
	if got := term(t, customerIdx, "country", "USA"); len(got) != 1 || got[0] != "c1" {
		t.Errorf("country=USA got %v, want [c1]", got)
	}
	if got := term(t, customerIdx, "country", "usa"); len(got) != 1 || got[0] != "c2" {
		t.Errorf("country=usa got %v, want [c2]", got)
	}

	// description search for "run" must stem-match both the running shoes
	// (p1) and the runner (p2) docs. bleve's standard analyzer applies a
	// snowball stemmer, so "run" -> "run" matches both "running" and
	// "runner".
	gotRun := match(t, productIdx, "description", "run")
	if len(gotRun) != 2 {
		t.Fatalf("description match 'run' got %d hits %v, want 2", len(gotRun), gotRun)
	}
	seen := map[string]bool{}
	for _, id := range gotRun {
		seen[id] = true
	}
	if !seen["p1"] || !seen["p2"] {
		t.Errorf("description match 'run' = %v, want both p1 and p2", gotRun)
	}

	// Distinctive token check: "SHOES" (uppercase) must still hit p1 only,
	// proving both that (a) the standard text analyzer lowercases queries
	// and (b) Porter stemming does not erroneously collapse unrelated
	// vocabulary — only p1 carries the shoes/shoe stem so p2 must stay
	// quiet. This is the counter-check to the case-sensitivity assertion
	// on the not_analyzed country field above: text fields are
	// case-INSENSITIVE, keyword fields are case-SENSITIVE.
	if got := match(t, productIdx, "description", "SHOES"); len(got) != 1 || got[0] != "p1" {
		t.Errorf("description match 'SHOES' got %v, want [p1]", got)
	}
}
