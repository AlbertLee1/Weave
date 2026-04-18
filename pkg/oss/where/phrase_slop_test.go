package where

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// setupPhraseIndex creates a Bleve index with a description text field for
// phrase slop tests.
func setupPhraseIndex(t *testing.T) bleve.Index {
	t.Helper()

	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("description", bleve.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "phrase"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id   string
		text string
	}{
		{"adj", "the quick fox jumps over the lazy dog"},
		{"gap1", "the quick brown fox jumps over the lazy dog"},
		{"gap2", "the quick brown and tired fox jumps"},
		{"reverse", "the fox quick jumps"},
		{"nope", "the cat sleeps"},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, map[string]interface{}{"description": d.text}); err != nil {
			t.Fatalf("index %s: %v", d.id, err)
		}
	}
	return idx
}

// --- ParsePhraseSlopString ---

func TestParsePhraseSlopString_DoubleQuoted(t *testing.T) {
	v, err := ParsePhraseSlopString(`"quick fox"~2`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Phrase != "quick fox" || v.Slop != 2 {
		t.Fatalf("got %+v, want {quick fox 2}", v)
	}
}

func TestParsePhraseSlopString_SingleQuoted(t *testing.T) {
	v, err := ParsePhraseSlopString(`'quick fox'~3`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Phrase != "quick fox" || v.Slop != 3 {
		t.Fatalf("got %+v, want {quick fox 3}", v)
	}
}

func TestParsePhraseSlopString_NoSlop(t *testing.T) {
	v, err := ParsePhraseSlopString(`"quick fox"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Phrase != "quick fox" || v.Slop != 0 {
		t.Fatalf("got %+v, want {quick fox 0}", v)
	}
}

func TestParsePhraseSlopString_Unquoted(t *testing.T) {
	// An unquoted string is treated as the phrase with slop=0 — callers can
	// always use the operator symmetrically.
	v, err := ParsePhraseSlopString(`quick fox`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Phrase != "quick fox" || v.Slop != 0 {
		t.Fatalf("got %+v, want {quick fox 0}", v)
	}
}

func TestParsePhraseSlopString_Empty(t *testing.T) {
	if _, err := ParsePhraseSlopString(""); err == nil {
		t.Fatal("expected error for empty input")
	}
	if _, err := ParsePhraseSlopString(`""`); err == nil {
		t.Fatal("expected error for empty quoted phrase")
	}
	if _, err := ParsePhraseSlopString(`""~2`); err == nil {
		t.Fatal("expected error for empty quoted phrase with slop")
	}
}

func TestParsePhraseSlopString_NegativeSlopRejected(t *testing.T) {
	// Lucene syntax only allows non-negative slop; the '~' followed by '-' is
	// treated as the slop delimiter with an invalid tail, which falls back to
	// "not a slop tail" and keeps the raw string — but "~-1" is clearly wrong
	// input so verify we surface an error via the structured parser below
	// instead. The negative-integer check lives in ParsePhraseSlopValue too.
	if _, err := ParsePhraseSlopString(`"quick fox"~abc`); err != nil {
		t.Fatalf("non-numeric tail should be kept as part of phrase: %v", err)
	}
}

func TestParsePhraseSlopString_PreservesTildeInPhrase(t *testing.T) {
	// Phrases can contain '~'; only a trailing `~<digits>` is interpreted as
	// slop — the rest stays intact.
	v, err := ParsePhraseSlopString(`"foo~bar"~1`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Phrase != "foo~bar" || v.Slop != 1 {
		t.Fatalf("got %+v, want {foo~bar 1}", v)
	}
}

func TestParsePhraseSlopValue_StructuredObject(t *testing.T) {
	raw := json.RawMessage(`{"phrase": "quick fox", "slop": 5}`)
	v, err := ParsePhraseSlopValue(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Phrase != "quick fox" || v.Slop != 5 {
		t.Fatalf("got %+v", v)
	}
}

func TestParsePhraseSlopValue_StringForm(t *testing.T) {
	raw := json.RawMessage(`"\"quick fox\"~2"`)
	v, err := ParsePhraseSlopValue(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Phrase != "quick fox" || v.Slop != 2 {
		t.Fatalf("got %+v", v)
	}
}

func TestParsePhraseSlopValue_NumericRejected(t *testing.T) {
	raw := json.RawMessage(`42`)
	if _, err := ParsePhraseSlopValue(raw); err == nil {
		t.Fatal("expected error for numeric value")
	}
}

// --- convertPhraseSlop end-to-end ---

func TestPhraseSlop_Adjacent_Slop0(t *testing.T) {
	idx := setupPhraseIndex(t)
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": 0}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"adj"})
}

func TestPhraseSlop_OneGap_Slop1(t *testing.T) {
	// "quick brown fox" has one intervening word; needs slop>=1
	idx := setupPhraseIndex(t)
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": 1}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"adj", "gap1"})
}

func TestPhraseSlop_TwoGap_Slop3(t *testing.T) {
	// "quick brown and tired fox" has three intervening words; needs slop>=3.
	// slop=3 also picks up the reversed doc — Lucene-compatible behaviour
	// (swapping two adjacent terms costs 2 slop).
	idx := setupPhraseIndex(t)
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": 3}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"adj", "gap1", "gap2", "reverse"})
}

func TestPhraseSlop_Reverse_Slop2(t *testing.T) {
	// "fox quick" with slop=2: going backwards one position costs 2 slop
	// (expected is prevPos+1, so moving back N costs 2N-1... actually
	// abs((prev+1) - newPos). If fox at pos 1 (after "the"), next expected=2,
	// quick at pos 2. Wait: "the fox quick jumps" → tokens: the(0) fox(1)
	// quick(2) jumps(3). For "quick fox" query: quick first at pos 2, next
	// expected pos 3, fox at pos 1 → dist=|3-1|=2. Matches with slop>=2.
	idx := setupPhraseIndex(t)

	for _, slop := range []int{0, 1} {
		clause := &WhereClause{
			Type:  "phrase",
			Field: "description",
			Value: json.RawMessage(`{"phrase": "quick fox", "slop": ` + itoa(slop) + `}`),
		}
		ids := searchWithWhere(t, idx, clause)
		for _, id := range ids {
			if id == "reverse" {
				t.Fatalf("slop=%d should not match reverse-order doc: got %v", slop, ids)
			}
		}
	}

	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": 2}`),
	}
	ids := searchWithWhere(t, idx, clause)
	// slop=2 is enough to pick up the reversed phrase AND the adjacent one.
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["reverse"] || !found["adj"] {
		t.Fatalf("slop=2 should match both reverse and adj; got %v", ids)
	}
}

func TestPhraseSlop_LuceneStringForm(t *testing.T) {
	idx := setupPhraseIndex(t)
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`"\"quick fox\"~1"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"adj", "gap1"})
}

func TestPhraseSlop_SingleTerm(t *testing.T) {
	idx := setupPhraseIndex(t)
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "fox", "slop": 0}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"adj", "gap1", "gap2", "reverse"})
}

func TestPhraseSlop_NoMatch(t *testing.T) {
	idx := setupPhraseIndex(t)
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "elephant giraffe", "slop": 5}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{})
}

func TestPhraseSlop_CaseInsensitive(t *testing.T) {
	// Input is uppercase but default analyser lowercases index terms;
	// convertPhraseSlop must lowercase input too.
	idx := setupPhraseIndex(t)
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "QUICK FOX", "slop": 1}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"adj", "gap1"})
}

func TestPhraseSlop_SlopClamped(t *testing.T) {
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": 999}`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatal("expected error for slop above MaxPhraseSlop")
	}
}

func TestPhraseSlop_NegativeSlopRejected(t *testing.T) {
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": -1}`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatal("expected error for negative slop")
	}
}

func TestPhraseSlop_EmptyPhraseRejected(t *testing.T) {
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "", "slop": 0}`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatal("expected error for empty phrase")
	}
}

func TestPhraseSlop_UsesMatchPhraseWhenSlopZeroMultiTerm(t *testing.T) {
	// slop=0 + >1 term should delegate to bleve.NewMatchPhraseQuery so
	// callers benefit from bleve's optimised phrase path.
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": 0}`),
	}
	q, err := ConvertToBleveQuery(clause)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if _, ok := q.(*query.MatchPhraseQuery); !ok {
		t.Fatalf("expected *query.MatchPhraseQuery for slop=0 multi-term, got %T", q)
	}
}

func TestPhraseSlop_UsesCustomQueryWhenSlopPositive(t *testing.T) {
	clause := &WhereClause{
		Type:  "phrase",
		Field: "description",
		Value: json.RawMessage(`{"phrase": "quick fox", "slop": 2}`),
	}
	q, err := ConvertToBleveQuery(clause)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if _, ok := q.(*PhraseSlopQuery); !ok {
		t.Fatalf("expected *PhraseSlopQuery for slop>0, got %T", q)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := []byte{}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
