package index

import (
	"encoding/json"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/oms"
)

// TestBuildMappingKeywordVsText exercises the analyzer.not_analyzed vs
// analyzer.standard selection on BuildMapping. A property declaring
// `analyzer: not_analyzed` in its TypeConfig must produce a case-sensitive
// KeywordField; the default / "standard" analyzer must tokenize and stem.
func TestBuildMappingKeywordVsText(t *testing.T) {
	ot := &oms.ObjectType{
		APIName: "Customer",
		Properties: []oms.Property{
			{
				APIName:      "country",
				BaseType:     "string",
				IsSearchable: true,
				TypeConfig:   json.RawMessage(`{"analyzer":"not_analyzed"}`),
			},
			{
				APIName:      "description",
				BaseType:     "string",
				IsSearchable: true,
				TypeConfig:   json.RawMessage(`{"analyzer":"standard"}`),
			},
			{
				APIName:      "id",
				BaseType:     "string",
				IsSearchable: true,
			},
		},
	}

	im := BuildMapping(ot)
	if im == nil {
		t.Fatal("BuildMapping returned nil")
	}

	idx, err := bleve.NewMemOnly(im)
	if err != nil {
		t.Fatalf("NewMemOnly: %v", err)
	}
	defer idx.Close()

	docs := map[string]map[string]interface{}{
		"1": {"id": "1", "country": "USA", "description": "running shoes"},
		"2": {"id": "2", "country": "usa", "description": "the runner wins"},
	}
	for id, doc := range docs {
		if err := idx.Index(id, doc); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}

	// Exact-match queries on the not_analyzed keyword field must be
	// case-sensitive: "USA" matches only doc 1, "usa" matches only doc 2.
	search := func(field, term string) []string {
		q := bleve.NewTermQuery(term)
		q.SetField(field)
		req := bleve.NewSearchRequest(q)
		req.Size = 10
		res, err := idx.Search(req)
		if err != nil {
			t.Fatalf("search %s=%s: %v", field, term, err)
		}
		ids := make([]string, 0, len(res.Hits))
		for _, h := range res.Hits {
			ids = append(ids, h.ID)
		}
		return ids
	}

	if got := search("country", "USA"); len(got) != 1 || got[0] != "1" {
		t.Errorf("country=USA got %v, want [1]", got)
	}
	if got := search("country", "usa"); len(got) != 1 || got[0] != "2" {
		t.Errorf("country=usa got %v, want [2]", got)
	}

	// Text fields use the standard analyzer: tokenized + lowercased, so a
	// MatchQuery with an upper-case term must still hit the mixed-case doc.
	// This contrasts with the not_analyzed field above, which would return
	// zero hits for the same case mismatch.
	matchQ := bleve.NewMatchQuery("RUNNING")
	matchQ.SetField("description")
	matchRes, err := idx.Search(bleve.NewSearchRequest(matchQ))
	if err != nil {
		t.Fatalf("match RUNNING: %v", err)
	}
	if matchRes.Total != 1 || matchRes.Hits[0].ID != "1" {
		t.Errorf("description match 'RUNNING' got total=%d hits=%v, want 1 [1]", matchRes.Total, matchRes.Hits)
	}

	// And the same tokeniser splits the phrase: searching for just "shoes"
	// still lights up the "running shoes" doc.
	shoesQ := bleve.NewMatchQuery("shoes")
	shoesQ.SetField("description")
	shoesRes, err := idx.Search(bleve.NewSearchRequest(shoesQ))
	if err != nil {
		t.Fatalf("match shoes: %v", err)
	}
	if shoesRes.Total != 1 || shoesRes.Hits[0].ID != "1" {
		t.Errorf("description match 'shoes' got total=%d, want 1", shoesRes.Total)
	}
}

// TestBuildMappingDefaultsToText ensures properties without a TypeConfig
// analyzer still index as searchable text (backwards compatible with older
// Property rows that lack the analyzer hint).
func TestBuildMappingDefaultsToText(t *testing.T) {
	ot := &oms.ObjectType{
		APIName: "Product",
		Properties: []oms.Property{
			{APIName: "name", BaseType: "string", IsSearchable: true},
			{APIName: "price", BaseType: "double", IsSearchable: true},
			{APIName: "active", BaseType: "boolean", IsSearchable: true},
		},
	}

	im := BuildMapping(ot)
	if im == nil {
		t.Fatal("BuildMapping returned nil")
	}

	if im.DefaultMapping == nil {
		t.Fatal("DefaultMapping is nil")
	}
	dm := im.DefaultMapping
	if len(dm.Properties) != 3 {
		t.Errorf("got %d property mappings, want 3", len(dm.Properties))
	}
	nameDM, ok := dm.Properties["name"]
	if !ok {
		t.Fatalf("missing name mapping")
	}
	if len(nameDM.Fields) != 1 || nameDM.Fields[0].Type != "text" {
		t.Errorf("name field = %+v, want text", nameDM.Fields)
	}
	// Default/standard text analyzer => Analyzer name is empty (inherits
	// DefaultAnalyzer == "standard"); explicitly must NOT be "keyword".
	if nameDM.Fields[0].Analyzer == "keyword" {
		t.Errorf("name field analyzer = keyword, want default/standard")
	}
}

// TestBuildMappingNotAnalyzed verifies the single-field shape: the returned
// mapping for a not_analyzed field must use the keyword analyzer explicitly.
func TestBuildMappingNotAnalyzed(t *testing.T) {
	ot := &oms.ObjectType{
		APIName: "Ticker",
		Properties: []oms.Property{
			{
				APIName:      "symbol",
				BaseType:     "string",
				IsSearchable: true,
				TypeConfig:   json.RawMessage(`{"analyzer":"not_analyzed"}`),
			},
		},
	}
	im := BuildMapping(ot)
	dm := im.DefaultMapping
	symDM, ok := dm.Properties["symbol"]
	if !ok {
		t.Fatalf("missing symbol mapping")
	}
	if len(symDM.Fields) != 1 {
		t.Fatalf("got %d fields, want 1", len(symDM.Fields))
	}
	fm := symDM.Fields[0]
	if fm.Type != "text" {
		t.Errorf("type = %q, want text", fm.Type)
	}
	if fm.Analyzer != "keyword" {
		t.Errorf("analyzer = %q, want keyword", fm.Analyzer)
	}
}
