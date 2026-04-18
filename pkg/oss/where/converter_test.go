package where

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
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
	return searchWithWhereOpts(t, idx, clause, nil)
}

// searchWithWhereOpts converts a WhereClause with options to a Bleve query, executes it, and returns sorted doc IDs.
func searchWithWhereOpts(t *testing.T, idx bleve.Index, clause *WhereClause, opts *ConvertOptions) []string {
	t.Helper()

	var q query.Query
	var err error
	if opts != nil {
		q, err = ConvertToBleveQueryWithOpts(clause, opts)
	} else {
		q, err = ConvertToBleveQuery(clause)
	}
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

// --- PointInPolygon unit tests ---

func TestPointInPolygon_Inside(t *testing.T) {
	// Simple square polygon around (0,0): corners at (-1,-1), (1,-1), (1,1), (-1,1)
	polygon := [][]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}, {-1, -1}}
	if !PointInPolygon(0, 0, polygon) {
		t.Fatal("expected point (0,0) inside square polygon")
	}
}

func TestPointInPolygon_Outside(t *testing.T) {
	polygon := [][]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}, {-1, -1}}
	if PointInPolygon(5, 5, polygon) {
		t.Fatal("expected point (5,5) outside square polygon")
	}
}

func TestPointInPolygon_OnEdge(t *testing.T) {
	// Points on edges are implementation-defined; just make sure no panic.
	polygon := [][]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}, {-1, -1}}
	_ = PointInPolygon(1, 0, polygon)
}

func TestPointInPolygon_Triangle(t *testing.T) {
	// Triangle: (0,0), (10,0), (5,10)
	polygon := [][]float64{{0, 0}, {10, 0}, {5, 10}, {0, 0}}
	if !PointInPolygon(5, 5, polygon) {
		t.Fatal("expected point (5,5) inside triangle")
	}
	if PointInPolygon(0, 10, polygon) {
		t.Fatal("expected point (0,10) outside triangle")
	}
}

func TestPointInPolygon_EmptyPolygon(t *testing.T) {
	if PointInPolygon(0, 0, nil) {
		t.Fatal("expected false for nil polygon")
	}
	if PointInPolygon(0, 0, [][]float64{}) {
		t.Fatal("expected false for empty polygon")
	}
}

// --- setupGeoIndex creates a Bleve index with GeoShape field mapping for polygon tests ---

func setupGeoIndex(t *testing.T) bleve.Index {
	t.Helper()

	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()

	// GeoShape field so that GeoShapeQuery works
	geoField := bleve.NewGeoShapeFieldMapping()
	docMapping.AddFieldMappingsAt("location", geoField)

	docMapping.AddFieldMappingsAt("name", bleve.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "geo_test"), indexMapping)
	if err != nil {
		t.Fatalf("create geo index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	// Index point documents as GeoJSON Points
	// NYC: ~40.7128, -74.0060
	err = idx.Index("nyc", map[string]interface{}{
		"name": "nyc",
		"location": map[string]interface{}{
			"type":        "Point",
			"coordinates": []float64{-74.0060, 40.7128},
		},
	})
	if err != nil {
		t.Fatalf("index nyc: %v", err)
	}

	// LA: ~34.0522, -118.2437
	err = idx.Index("la", map[string]interface{}{
		"name": "la",
		"location": map[string]interface{}{
			"type":        "Point",
			"coordinates": []float64{-118.2437, 34.0522},
		},
	})
	if err != nil {
		t.Fatalf("index la: %v", err)
	}

	// London: ~51.5074, -0.1278
	err = idx.Index("london", map[string]interface{}{
		"name": "london",
		"location": map[string]interface{}{
			"type":        "Point",
			"coordinates": []float64{-0.1278, 51.5074},
		},
	})
	if err != nil {
		t.Fatalf("index london: %v", err)
	}

	return idx
}

// --- withinPolygon tests ---

func TestWithinPolygon_PointInside(t *testing.T) {
	idx := setupGeoIndex(t)
	// Polygon covering the US East Coast (contains NYC, not LA or London)
	clause := &WhereClause{
		Type:  "withinPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-80.0, 35.0],
				[-70.0, 35.0],
				[-70.0, 45.0],
				[-80.0, 45.0],
				[-80.0, 35.0]
			]]
		}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"nyc"})
}

func TestWithinPolygon_PointOutside(t *testing.T) {
	idx := setupGeoIndex(t)
	// Small polygon around London — NYC and LA should not match
	clause := &WhereClause{
		Type:  "withinPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-1.0, 51.0],
				[0.5, 51.0],
				[0.5, 52.0],
				[-1.0, 52.0],
				[-1.0, 51.0]
			]]
		}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"london"})
}

func TestWithinPolygon_InvalidValue(t *testing.T) {
	clause := &WhereClause{
		Type:  "withinPolygon",
		Field: "location",
		Value: json.RawMessage(`"invalid"`),
	}
	_, err := ConvertToBleveQuery(clause)
	if err == nil {
		t.Fatal("expected error for invalid withinPolygon value")
	}
}

// --- intersectsPolygon tests ---

func TestIntersectsPolygon_PointInside(t *testing.T) {
	idx := setupGeoIndex(t)
	// Polygon covering US East Coast
	clause := &WhereClause{
		Type:  "intersectsPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-80.0, 35.0],
				[-70.0, 35.0],
				[-70.0, 45.0],
				[-80.0, 45.0],
				[-80.0, 35.0]
			]]
		}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"nyc"})
}

func TestIntersectsPolygon_NoMatch(t *testing.T) {
	idx := setupGeoIndex(t)
	// Polygon in the middle of the Pacific Ocean — no points match
	clause := &WhereClause{
		Type:  "intersectsPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-170.0, 10.0],
				[-160.0, 10.0],
				[-160.0, 20.0],
				[-170.0, 20.0],
				[-170.0, 10.0]
			]]
		}`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{})
}

// --- doesNotIntersectPolygon tests ---

func TestDoesNotIntersectPolygon_ExcludesInside(t *testing.T) {
	idx := setupGeoIndex(t)
	// Polygon covering US East Coast — NYC is inside, so it should be excluded
	clause := &WhereClause{
		Type:  "doesNotIntersectPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-80.0, 35.0],
				[-70.0, 35.0],
				[-70.0, 45.0],
				[-80.0, 45.0],
				[-80.0, 35.0]
			]]
		}`),
	}
	ids := searchWithWhere(t, idx, clause)
	// LA and London are outside the polygon
	assertIDs(t, ids, []string{"la", "london"})
}

// --- doesNotIntersectBoundingBox tests ---

func TestDoesNotIntersectBoundingBox_ExcludesInside(t *testing.T) {
	idx := setupGeoIndex(t)
	// Bounding box covering US East Coast — NYC is inside
	clause := &WhereClause{
		Type:  "doesNotIntersectBoundingBox",
		Field: "location",
		Value: json.RawMessage(`{
			"topLeft": {"latitude": 45.0, "longitude": -80.0},
			"bottomRight": {"latitude": 35.0, "longitude": -70.0}
		}`),
	}
	ids := searchWithWhere(t, idx, clause)
	// LA and London are outside the bounding box
	assertIDs(t, ids, []string{"la", "london"})
}

// --- MatchClause polygon tests (in-memory) ---

func TestMatchClause_WithinPolygon_Inside(t *testing.T) {
	row := map[string]interface{}{
		"location": map[string]interface{}{
			"latitude":  40.7128,
			"longitude": -74.0060,
		},
	}
	clause := &WhereClause{
		Type:  "withinPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-80.0, 35.0],
				[-70.0, 35.0],
				[-70.0, 45.0],
				[-80.0, 45.0],
				[-80.0, 35.0]
			]]
		}`),
	}
	if !MatchClause(clause, row) {
		t.Fatal("expected NYC inside East Coast polygon")
	}
}

func TestMatchClause_WithinPolygon_Outside(t *testing.T) {
	row := map[string]interface{}{
		"location": map[string]interface{}{
			"latitude":  34.0522,
			"longitude": -118.2437,
		},
	}
	clause := &WhereClause{
		Type:  "withinPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-80.0, 35.0],
				[-70.0, 35.0],
				[-70.0, 45.0],
				[-80.0, 45.0],
				[-80.0, 35.0]
			]]
		}`),
	}
	if MatchClause(clause, row) {
		t.Fatal("expected LA outside East Coast polygon")
	}
}

func TestMatchClause_DoesNotIntersectPolygon(t *testing.T) {
	row := map[string]interface{}{
		"location": map[string]interface{}{
			"latitude":  34.0522,
			"longitude": -118.2437,
		},
	}
	clause := &WhereClause{
		Type:  "doesNotIntersectPolygon",
		Field: "location",
		Value: json.RawMessage(`{
			"polygon": [[
				[-80.0, 35.0],
				[-70.0, 35.0],
				[-70.0, 45.0],
				[-80.0, 45.0],
				[-80.0, 35.0]
			]]
		}`),
	}
	// LA is outside the East Coast polygon, so doesNotIntersect → true
	if !MatchClause(clause, row) {
		t.Fatal("expected doesNotIntersectPolygon to match for LA (outside polygon)")
	}
}

// --- containsAllTermsInOrderPrefixLastTerm tests ---

func setupAutocompleteIndex(t *testing.T) bleve.Index {
	t.Helper()

	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("name", bleve.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "autocomplete_test"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"js", map[string]interface{}{"name": "John Smith"}},
		{"sj", map[string]interface{}{"name": "Smith John"}},
		{"john", map[string]interface{}{"name": "John"}},
		{"jane", map[string]interface{}{"name": "Jane"}},
		{"james", map[string]interface{}{"name": "James"}},
		{"bob", map[string]interface{}{"name": "Bob"}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("index doc %s: %v", d.id, err)
		}
	}

	return idx
}

func TestContainsAllTermsInOrderPrefixLastTerm_MultiTerm(t *testing.T) {
	idx := setupAutocompleteIndex(t)
	// 'John S' should match 'John Smith' but not 'Smith John'
	clause := &WhereClause{
		Type:  "containsAllTermsInOrderPrefixLastTerm",
		Field: "name",
		Value: json.RawMessage(`"John S"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"js"})
}

func TestContainsAllTermsInOrderPrefixLastTerm_SingleTerm(t *testing.T) {
	idx := setupAutocompleteIndex(t)
	// 'J' should match all docs with a term starting with 'j': John, Jane, James, John Smith, Smith John
	clause := &WhereClause{
		Type:  "containsAllTermsInOrderPrefixLastTerm",
		Field: "name",
		Value: json.RawMessage(`"J"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"james", "jane", "john", "js", "sj"})
}

func TestContainsAllTermsInOrderPrefixLastTerm_Empty(t *testing.T) {
	idx := setupAutocompleteIndex(t)
	clause := &WhereClause{
		Type:  "containsAllTermsInOrderPrefixLastTerm",
		Field: "name",
		Value: json.RawMessage(`""`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{})
}

func TestContainsAllTermsInOrderPrefixLastTerm_ExactMatch(t *testing.T) {
	idx := setupAutocompleteIndex(t)
	// 'John Smith' (complete terms) should still match via phrase prefix
	clause := &WhereClause{
		Type:  "containsAllTermsInOrderPrefixLastTerm",
		Field: "name",
		Value: json.RawMessage(`"John Smith"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"js"})
}

// --- MatchClause containsAllTermsInOrderPrefixLastTerm tests (in-memory) ---

func TestMatchClause_ContainsAllTermsInOrderPrefixLastTerm_Order(t *testing.T) {
	clause := &WhereClause{
		Type:  "containsAllTermsInOrderPrefixLastTerm",
		Field: "name",
		Value: json.RawMessage(`"John S"`),
	}
	// "John Smith" → match (john followed by word starting with s)
	if !MatchClause(clause, map[string]interface{}{"name": "John Smith"}) {
		t.Fatal("expected 'John S' to match 'John Smith'")
	}
	// "Smith John" → no match (wrong order)
	if MatchClause(clause, map[string]interface{}{"name": "Smith John"}) {
		t.Fatal("expected 'John S' to NOT match 'Smith John'")
	}
}

func TestMatchClause_ContainsAllTermsInOrderPrefixLastTerm_SingleTerm(t *testing.T) {
	clause := &WhereClause{
		Type:  "containsAllTermsInOrderPrefixLastTerm",
		Field: "name",
		Value: json.RawMessage(`"J"`),
	}
	if !MatchClause(clause, map[string]interface{}{"name": "John"}) {
		t.Fatal("expected 'J' to match 'John'")
	}
	if !MatchClause(clause, map[string]interface{}{"name": "Jane"}) {
		t.Fatal("expected 'J' to match 'Jane'")
	}
	if !MatchClause(clause, map[string]interface{}{"name": "James"}) {
		t.Fatal("expected 'J' to match 'James'")
	}
	if MatchClause(clause, map[string]interface{}{"name": "Bob"}) {
		t.Fatal("expected 'J' to NOT match 'Bob'")
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

// --- Fuzzy Search tests ---

// setupFuzzyTestIndex creates a Bleve index with names suitable for fuzzy matching tests.
func setupFuzzyTestIndex(t *testing.T) bleve.Index {
	t.Helper()

	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("name", bleve.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("city", bleve.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "fuzzy"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"name": "John Smith", "city": "New York"}},
		{"2", map[string]interface{}{"name": "Jane Doe", "city": "Los Angeles"}},
		{"3", map[string]interface{}{"name": "James Brown", "city": "Chicago"}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("index doc %s: %v", d.id, err)
		}
	}
	return idx
}

func TestFuzzy_ContainsAllTerms_MatchWithMaxEdits1(t *testing.T) {
	// "Jonh" (transposition typo) should match "John" with fuzzy maxEdits=1
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "containsAllTerms",
		Field: "name",
		Value: json.RawMessage(`"Jonh"`),
	}
	opts := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 1}}
	ids := searchWithWhereOpts(t, idx, clause, opts)
	assertIDs(t, ids, []string{"1"})
}

func TestFuzzy_ContainsAllTerms_NoMatchWithoutFuzzy(t *testing.T) {
	// "Jonh" (typo) should NOT match "John" without fuzzy
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "containsAllTerms",
		Field: "name",
		Value: json.RawMessage(`"Jonh"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{})
}

func TestFuzzy_ContainsAnyTerm_MatchWithMaxEdits1(t *testing.T) {
	// "Jonh" should match "John" with fuzzy via containsAnyTerm
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "containsAnyTerm",
		Field: "name",
		Value: json.RawMessage(`"Jonh"`),
	}
	opts := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 1}}
	ids := searchWithWhereOpts(t, idx, clause, opts)
	assertIDs(t, ids, []string{"1"})
}

func TestFuzzy_Eq_MatchWithMaxEdits1(t *testing.T) {
	// "jonh" should match "john" (analyzed) with fuzzy via eq
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "eq",
		Field: "name",
		Value: json.RawMessage(`"Jonh"`),
	}
	opts := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 1}}
	ids := searchWithWhereOpts(t, idx, clause, opts)
	assertIDs(t, ids, []string{"1"})
}

func TestFuzzy_DefaultMaxEdits(t *testing.T) {
	// When fuzzy is present but MaxEdits is 0 (omitted), default to 1
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "containsAllTerms",
		Field: "name",
		Value: json.RawMessage(`"Jonh"`),
	}
	opts := &ConvertOptions{Fuzzy: &FuzzyConfig{}} // MaxEdits omitted (zero value)
	ids := searchWithWhereOpts(t, idx, clause, opts)
	assertIDs(t, ids, []string{"1"})
}

func TestFuzzy_ThroughAndClause(t *testing.T) {
	// Fuzzy should propagate through logical operators (and)
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type: "and",
		Value: json.RawMessage(`[
			{"type": "containsAllTerms", "field": "name", "value": "Jonh"},
			{"type": "containsAllTerms", "field": "name", "value": "Smtih"}
		]`),
	}
	opts := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 1}}
	ids := searchWithWhereOpts(t, idx, clause, opts)
	assertIDs(t, ids, []string{"1"})
}

// --- "fuzzy" WhereClause operator (bleve.NewFuzzyQuery) ---
//
// These tests exercise the explicit fuzzy operator — a single-term FuzzyQuery
// that doesn't re-tokenise the input. It complements MatchQuery.SetFuzziness
// by giving callers a way to target one indexed token directly.

func TestFuzzyOperator_MatchWithDefaultMaxEdits(t *testing.T) {
	// No FuzzyConfig supplied → operator falls back to maxEdits=1.
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "fuzzy",
		Field: "name",
		Value: json.RawMessage(`"jonh"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1"})
}

func TestFuzzyOperator_ExactMatch(t *testing.T) {
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "fuzzy",
		Field: "name",
		Value: json.RawMessage(`"john"`),
	}
	ids := searchWithWhere(t, idx, clause)
	assertIDs(t, ids, []string{"1"})
}

func TestFuzzyOperator_HonoursMaxEdits(t *testing.T) {
	// "kofka" → "kafka" needs one edit. With MaxEdits=2 it matches; the
	// "disable-fuzzy" semantic for this operator is expressed by passing no
	// FuzzyConfig at all and using a literal operator ("contains") instead —
	// a non-nil FuzzyConfig with MaxEdits=0 means "use the default (1)".
	idx := setupFuzzyTestIndex(t)
	if err := idx.Index("4", map[string]interface{}{"name": "kafka", "city": "brno"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	clause := &WhereClause{
		Type:  "fuzzy",
		Field: "name",
		Value: json.RawMessage(`"kofka"`),
	}

	opts1 := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 1}}
	ids := searchWithWhereOpts(t, idx, clause, opts1)
	assertIDs(t, ids, []string{"4"})

	// MaxEdits=2 still matches since 1 edit ≤ 2.
	opts2 := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 2}}
	ids = searchWithWhereOpts(t, idx, clause, opts2)
	assertIDs(t, ids, []string{"4"})
}

func TestFuzzyOperator_MaxEdits2Tolerance(t *testing.T) {
	// "kaffca" → "kafka" is two edits away; maxEdits=1 misses, maxEdits=2 hits.
	idx := setupFuzzyTestIndex(t)
	if err := idx.Index("4", map[string]interface{}{"name": "kafka", "city": "brno"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	clause := &WhereClause{
		Type:  "fuzzy",
		Field: "name",
		Value: json.RawMessage(`"kaffca"`),
	}

	opts1 := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 1}}
	ids := searchWithWhereOpts(t, idx, clause, opts1)
	assertIDs(t, ids, []string{})

	opts2 := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 2}}
	ids = searchWithWhereOpts(t, idx, clause, opts2)
	assertIDs(t, ids, []string{"4"})
}

func TestFuzzyOperator_EmptyValueRejected(t *testing.T) {
	clause := &WhereClause{
		Type:  "fuzzy",
		Field: "name",
		Value: json.RawMessage(`"   "`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatalf("expected error for empty fuzzy value")
	}
}

func TestFuzzyOperator_NonStringRejected(t *testing.T) {
	clause := &WhereClause{
		Type:  "fuzzy",
		Field: "age",
		Value: json.RawMessage(`42`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatalf("expected error for numeric fuzzy value")
	}
}

func TestFuzzyOperator_UsesBleveFuzzyQuery(t *testing.T) {
	// Sanity: the operator must produce a *query.FuzzyQuery, not a MatchQuery —
	// PRD US-232 explicitly requires Bleve FuzzyQuery integration.
	clause := &WhereClause{
		Type:  "fuzzy",
		Field: "name",
		Value: json.RawMessage(`"kafka"`),
	}
	q, err := ConvertToBleveQueryWithOpts(clause, &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 2}})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if _, ok := q.(*query.FuzzyQuery); !ok {
		t.Fatalf("expected *query.FuzzyQuery, got %T", q)
	}
}
