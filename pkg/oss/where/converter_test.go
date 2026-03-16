package where

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

// setupTestIndex creates a Bleve index in a temp dir with test documents.
func setupTestIndex(t *testing.T) bleve.Index {
	t.Helper()

	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()

	docMapping.AddFieldMappingsAt("name", bleve.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("age", bleve.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("active", bleve.NewBooleanFieldMapping())
	docMapping.AddFieldMappingsAt("createdAt", bleve.NewDateTimeFieldMapping())
	docMapping.AddFieldMappingsAt("description", bleve.NewTextFieldMapping())

	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "test"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"name": "alice", "age": float64(30), "active": true, "description": "software engineer at acme"}},
		{"2", map[string]interface{}{"name": "bob", "age": float64(25), "active": false, "description": "product manager at globex"}},
		{"3", map[string]interface{}{"name": "charlie", "age": float64(35), "active": true, "description": "senior software engineer"}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("index doc %s: %v", d.id, err)
		}
	}

	return idx
}

// searchWithWhere converts a WhereClause to a Bleve query, executes it, and returns sorted doc IDs.
func searchWithWhere(t *testing.T, idx bleve.Index, clause *WhereClause) []string {
	t.Helper()

	q, err := ConvertToBleveQuery(clause)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	req := bleve.NewSearchRequest(q)
	req.Size = 100
	res, err := idx.Search(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var ids []string
	for _, hit := range res.Hits {
		ids = append(ids, hit.ID)
	}
	sort.Strings(ids)
	return ids
}

// assertIDs checks that the returned IDs match the expected ones.
func assertIDs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) == 0 {
		got = []string{}
	}
	if len(want) == 0 {
		want = []string{}
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// --- SplitTerms tests (3) ---

func TestSplitTerms_Simple(t *testing.T) {
	result := SplitTerms("hello world")
	if len(result) != 2 || result[0] != "hello" || result[1] != "world" {
		t.Fatalf("got %v, want [hello world]", result)
	}
}

func TestSplitTerms_ExtraSpaces(t *testing.T) {
	result := SplitTerms("  hello   world  ")
	if len(result) != 2 || result[0] != "hello" || result[1] != "world" {
		t.Fatalf("got %v, want [hello world]", result)
	}
}

func TestSplitTerms_Empty(t *testing.T) {
	result := SplitTerms("")
	if len(result) != 0 {
		t.Fatalf("got %v, want []", result)
	}
}

// --- Eq operator tests (3) ---

func TestEq_String(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "eq",
		Field: "name",
		Value: json.RawMessage(`"alice"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1"})
}

func TestEq_Number(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "eq",
		Field: "age",
		Value: json.RawMessage(`30`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1"})
}

func TestEq_Boolean(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "eq",
		Field: "active",
		Value: json.RawMessage(`true`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "3"})
}

// --- Range operator tests (4) ---

func TestGt_Number(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "gt",
		Field: "age",
		Value: json.RawMessage(`30`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"3"})
}

func TestGte_Number(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "gte",
		Field: "age",
		Value: json.RawMessage(`30`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "3"})
}

func TestLt_Number(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "lt",
		Field: "age",
		Value: json.RawMessage(`30`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"2"})
}

func TestLte_Number(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "lte",
		Field: "age",
		Value: json.RawMessage(`25`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"2"})
}

// --- IsNull tests (2) ---

func TestIsNull_True(t *testing.T) {
	idx := setupTestIndex(t)

	// Index a doc without a "name" field to test isNull=true.
	err := idx.Index("4", map[string]interface{}{"age": float64(40), "active": true, "description": "no name person"})
	if err != nil {
		t.Fatalf("index doc 4: %v", err)
	}

	clause := &WhereClause{
		Type:  "isNull",
		Field: "name",
		Value: json.RawMessage(`true`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"4"})
}

func TestIsNull_False(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "isNull",
		Field: "name",
		Value: json.RawMessage(`false`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "2", "3"})
}

// --- Text search tests (5) ---

func TestContains_Term(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "contains",
		Field: "name",
		Value: json.RawMessage(`"alice"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1"})
}

func TestContainsAllTerms(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "containsAllTerms",
		Field: "description",
		Value: json.RawMessage(`"software engineer"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "3"})
}

func TestContainsAnyTerm(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "containsAnyTerm",
		Field: "description",
		Value: json.RawMessage(`"manager engineer"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "2", "3"})
}

func TestContainsAllTermsInOrder(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "containsAllTermsInOrder",
		Field: "description",
		Value: json.RawMessage(`"software engineer"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "3"})
}

func TestStartsWith(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "startsWith",
		Field: "name",
		Value: json.RawMessage(`"ali"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1"})
}

// --- Logical operator tests (5) ---

func TestAnd_TwoClauses(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type: "and",
		Value: json.RawMessage(`[
			{"type": "eq", "field": "active", "value": true},
			{"type": "gt", "field": "age", "value": 30}
		]`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"3"})
}

func TestOr_TwoClauses(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type: "or",
		Value: json.RawMessage(`[
			{"type": "eq", "field": "name", "value": "alice"},
			{"type": "eq", "field": "name", "value": "bob"}
		]`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "2"})
}

func TestNot_Simple(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type: "not",
		Value: json.RawMessage(`{"type": "eq", "field": "active", "value": false}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "3"})
}

func TestAnd_Nested(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type: "and",
		Value: json.RawMessage(`[
			{
				"type": "or",
				"value": [
					{"type": "eq", "field": "name", "value": "alice"},
					{"type": "eq", "field": "name", "value": "charlie"}
				]
			},
			{"type": "eq", "field": "active", "value": true}
		]`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1", "3"})
}

func TestOr_Empty(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "or",
		Value: json.RawMessage(`[]`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{})
}

// --- Edge case tests (4) ---

func TestUnsupportedType(t *testing.T) {
	clause := &WhereClause{
		Type:  "unknown",
		Field: "name",
		Value: json.RawMessage(`"test"`),
	}
	_, err := ConvertToBleveQuery(clause)
	if err == nil {
		t.Fatal("expected error for unsupported type, got nil")
	}
}

func TestEq_EmptyString(t *testing.T) {
	idx := setupTestIndex(t)
	clause := &WhereClause{
		Type:  "eq",
		Field: "name",
		Value: json.RawMessage(`""`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{})
}

func TestConvert_NilClause(t *testing.T) {
	_, err := ConvertToBleveQuery(nil)
	if err == nil {
		t.Fatal("expected error for nil clause, got nil")
	}
}

func TestRange_WithDateString(t *testing.T) {
	idx := setupTestIndex(t)

	// Index a document with a date field.
	err := idx.Index("4", map[string]interface{}{
		"name":      "dave",
		"age":       float64(28),
		"active":    true,
		"createdAt": "2024-01-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("index doc 4: %v", err)
	}

	clause := &WhereClause{
		Type:  "gte",
		Field: "createdAt",
		Value: json.RawMessage(`"2024-01-01T00:00:00Z"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"4"})
}

// --- JSON parsing tests (2) ---

func TestWhereClause_UnmarshalJSON(t *testing.T) {
	raw := `{"type": "eq", "field": "name", "value": "alice"}`
	var clause WhereClause
	if err := json.Unmarshal([]byte(raw), &clause); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if clause.Type != "eq" {
		t.Fatalf("got type %q, want eq", clause.Type)
	}
	if clause.Field != "name" {
		t.Fatalf("got field %q, want name", clause.Field)
	}
	var val string
	if err := json.Unmarshal(clause.Value, &val); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}
	if val != "alice" {
		t.Fatalf("got value %q, want alice", val)
	}
}

// --- Geospatial operator tests (3) ---

func TestWithinBoundingBox_Basic(t *testing.T) {
	clause := &WhereClause{
		Type:  "withinBoundingBox",
		Field: "location",
		Value: json.RawMessage(`{
			"topLeft": {"latitude": 41.0, "longitude": -74.0},
			"bottomRight": {"latitude": 40.0, "longitude": -73.0}
		}`),
	}
	q, err := ConvertToBleveQuery(clause)
	if err != nil {
		t.Fatalf("ConvertToBleveQuery: %v", err)
	}
	if q == nil {
		t.Fatal("expected non-nil query")
	}
}

func TestWithinDistanceOf_Basic(t *testing.T) {
	clause := &WhereClause{
		Type:  "withinDistanceOf",
		Field: "location",
		Value: json.RawMessage(`{
			"center": {"latitude": 40.7128, "longitude": -74.0060},
			"distance": "10km"
		}`),
	}
	q, err := ConvertToBleveQuery(clause)
	if err != nil {
		t.Fatalf("ConvertToBleveQuery: %v", err)
	}
	if q == nil {
		t.Fatal("expected non-nil query")
	}
}

func TestWithinPolygon_NotSupported(t *testing.T) {
	clause := &WhereClause{
		Type:  "withinPolygon",
		Field: "location",
		Value: json.RawMessage(`{}`),
	}
	_, err := ConvertToBleveQuery(clause)
	if err == nil {
		t.Fatal("expected error for withinPolygon, got nil")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("expected 'not yet supported' error, got: %v", err)
	}
}

func TestWhereClause_ComplexNested(t *testing.T) {
	raw := `{
		"type": "and",
		"value": [
			{
				"type": "or",
				"value": [
					{"type": "eq", "field": "name", "value": "alice"},
					{"type": "eq", "field": "name", "value": "bob"}
				]
			},
			{
				"type": "not",
				"value": {"type": "eq", "field": "active", "value": false}
			}
		]
	}`
	var clause WhereClause
	if err := json.Unmarshal([]byte(raw), &clause); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	idx := setupTestIndex(t)
	ids := searchWithWhere(t, idx, &clause)
	// alice is active (true), bob is active (false).
	// or: alice OR bob => 1, 2
	// not active=false => 1, 3
	// and: intersection => 1 (alice)
	assertIDs(t, ids, []string{"1"})
}
