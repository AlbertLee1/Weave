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
	// US-051: BuildMapping now reserves a KeywordField for MarkingsField on
	// every ObjectType. The schema-authored properties still number 3.
	if len(dm.Properties) != 4 {
		t.Errorf("got %d property mappings, want 4 (3 schema + %s)", len(dm.Properties), MarkingsField)
	}
	if _, ok := dm.Properties[MarkingsField]; !ok {
		t.Errorf("missing %s keyword mapping", MarkingsField)
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

// TestBuildMappingNotIndexed is the US-010 acceptance test. A property tagged
// with analyzer=not_indexed must round-trip through the API (stored) but be
// completely invisible to full-text / term queries on its field. This is the
// Foundry "hide from search, keep in payload" semantic for attachment blobs,
// long prose, or PII that still needs to travel over the wire.
func TestBuildMappingNotIndexed(t *testing.T) {
	ot := &oms.ObjectType{
		APIName: "Patent",
		Properties: []oms.Property{
			{
				APIName:      "id",
				BaseType:     "string",
				IsSearchable: true,
			},
			{
				APIName:      "abstract",
				BaseType:     "string",
				IsSearchable: true,
				TypeConfig:   json.RawMessage(`{"analyzer":"not_indexed"}`),
			},
		},
	}

	im := BuildMapping(ot)
	if im == nil {
		t.Fatal("BuildMapping returned nil")
	}

	// Sanity: the FieldMapping for abstract must be stored but not indexed.
	dm := im.DefaultMapping
	absDM, ok := dm.Properties["abstract"]
	if !ok {
		t.Fatalf("missing abstract mapping")
	}
	if len(absDM.Fields) != 1 {
		t.Fatalf("abstract got %d fields, want 1", len(absDM.Fields))
	}
	fm := absDM.Fields[0]
	if fm.Index {
		t.Errorf("abstract Index = true, want false (not_indexed)")
	}
	if !fm.Store {
		t.Errorf("abstract Store = false, want true (not_indexed keeps payload)")
	}

	idx, err := bleve.NewMemOnly(im)
	if err != nil {
		t.Fatalf("NewMemOnly: %v", err)
	}
	defer idx.Close()

	doc := map[string]interface{}{
		"id":       "p1",
		"abstract": "quantum compute entanglement",
	}
	if err := idx.Index("p1", doc); err != nil {
		t.Fatalf("index p1: %v", err)
	}

	// Field-scoped query on abstract must return zero hits: the property is
	// excluded from the inverted index entirely.
	absQ := bleve.NewMatchQuery("quantum")
	absQ.SetField("abstract")
	absRes, err := idx.Search(bleve.NewSearchRequest(absQ))
	if err != nil {
		t.Fatalf("search abstract=quantum: %v", err)
	}
	if absRes.Total != 0 {
		t.Errorf("abstract field search expected 0 hits, got total=%d", absRes.Total)
	}

	// A TermQuery on the stored value is equally dead — stored != indexed.
	termQ := bleve.NewTermQuery("quantum")
	termQ.SetField("abstract")
	termRes, err := idx.Search(bleve.NewSearchRequest(termQ))
	if err != nil {
		t.Fatalf("term abstract=quantum: %v", err)
	}
	if termRes.Total != 0 {
		t.Errorf("abstract term search expected 0 hits, got total=%d", termRes.Total)
	}

	// But the stored payload is still retrievable — search by the indexed
	// id field and ask Bleve to return the abstract field. The returned
	// Hit.Fields map must carry the full stored value so that callers of
	// oss.FormatObject can include it in the wire response.
	idQ := bleve.NewMatchQuery("p1")
	idQ.SetField("id")
	idReq := bleve.NewSearchRequest(idQ)
	idReq.Fields = []string{"abstract"}
	idRes, err := idx.Search(idReq)
	if err != nil {
		t.Fatalf("search id=p1: %v", err)
	}
	if idRes.Total != 1 {
		t.Fatalf("id field search got total=%d, want 1", idRes.Total)
	}
	got, ok := idRes.Hits[0].Fields["abstract"]
	if !ok {
		t.Fatalf("stored abstract missing from Hit.Fields: %+v", idRes.Hits[0].Fields)
	}
	if got != "quantum compute entanglement" {
		t.Errorf("stored abstract = %v, want %q", got, "quantum compute entanglement")
	}
}

// TestBuildMappingCJKAnalyzer is the US-237 acceptance test. A property
// declaring `analyzer: cjk` in its TypeConfig must produce a TextField wired
// to Bleve's CJK analyzer (unicode tokenizer + width filter + lowercase +
// CJK bigram filter). Critically, a MatchQuery for "中国银行" must match a
// document whose value is "中国银行总部" — this is the exact PRD example.
func TestBuildMappingCJKAnalyzer(t *testing.T) {
	ot := &oms.ObjectType{
		APIName: "Bank",
		Properties: []oms.Property{
			{APIName: "id", BaseType: "string", IsSearchable: true},
			{
				APIName:      "name",
				BaseType:     "string",
				IsSearchable: true,
				TypeConfig:   json.RawMessage(`{"analyzer":"cjk"}`),
			},
		},
	}

	im := BuildMapping(ot)
	if im == nil {
		t.Fatal("BuildMapping returned nil")
	}

	dm := im.DefaultMapping
	nameDM, ok := dm.Properties["name"]
	if !ok {
		t.Fatalf("missing name mapping")
	}
	if len(nameDM.Fields) != 1 {
		t.Fatalf("name got %d fields, want 1", len(nameDM.Fields))
	}
	if got := nameDM.Fields[0].Analyzer; got != AnalyzerCJK {
		t.Errorf("name analyzer = %q, want %q", got, AnalyzerCJK)
	}

	idx, err := bleve.NewMemOnly(im)
	if err != nil {
		t.Fatalf("NewMemOnly: %v", err)
	}
	defer idx.Close()

	docs := map[string]map[string]interface{}{
		"1": {"id": "1", "name": "中国银行总部"},
		"2": {"id": "2", "name": "美国华尔街分行"},
		"3": {"id": "3", "name": "中国工商银行"},
	}
	for id, doc := range docs {
		if err := idx.Index(id, doc); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}

	// The PRD acceptance: searching for "中国银行" lights up the doc whose
	// name is "中国银行总部". With bigram analysis the query expands to
	// {中国, 国银, 银行}, all three of which are present in doc 1.
	mq := bleve.NewMatchQuery("中国银行")
	mq.SetField("name")
	res, err := idx.Search(bleve.NewSearchRequest(mq))
	if err != nil {
		t.Fatalf("search 中国银行: %v", err)
	}
	hits := make(map[string]bool, len(res.Hits))
	for _, h := range res.Hits {
		hits[h.ID] = true
	}
	if !hits["1"] {
		t.Errorf("expected doc 1 (中国银行总部) to match 中国银行; hits=%v", hits)
	}
	// Doc 3 (中国工商银行) shares only {中国} as a complete bigram with the
	// query — partial overlap is fine, but doc 2 must NOT match.
	if hits["2"] {
		t.Errorf("doc 2 (美国华尔街分行) should not match 中国银行; hits=%v", hits)
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
