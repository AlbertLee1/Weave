package index_test

import (
	"fmt"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
)

// --- ApplyBatch tests ---

func TestManager_ApplyBatch_AtomicPerIndex(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	ops := []index.BatchOp{
		{
			Type:       index.BatchOpIndex,
			PrimaryKey: "emp-1",
			Document:   map[string]interface{}{"name": "Alice", "age": 30, "active": true},
		},
		{
			Type:       index.BatchOpIndex,
			PrimaryKey: "emp-2",
			Document:   map[string]interface{}{"name": "Bob", "age": 28, "active": true},
		},
	}

	if err := mgr.ApplyBatch("employee", ops); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	count, err := mgr.DocCount("employee")
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected doc count 2 after atomic batch, got %d", count)
	}
}

func TestManager_ApplyBatch_MixedIndexAndDelete(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Pre-populate emp-1.
	if err := mgr.IndexDocument("employee", "emp-1", map[string]interface{}{"name": "Alice"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// Batch: delete emp-1, create emp-2.
	ops := []index.BatchOp{
		{Type: index.BatchOpDelete, PrimaryKey: "emp-1"},
		{
			Type:       index.BatchOpIndex,
			PrimaryKey: "emp-2",
			Document:   map[string]interface{}{"name": "Bob"},
		},
	}
	if err := mgr.ApplyBatch("employee", ops); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	count, err := mgr.DocCount("employee")
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected doc count 1 (emp-1 deleted, emp-2 created), got %d", count)
	}

	idx := mgr.GetIndex("employee")
	doc, err := idx.Document("emp-2")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if doc == nil {
		t.Fatal("expected emp-2 in index after ApplyBatch")
	}
}

func TestManager_ApplyBatch_UnknownIndex(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	ops := []index.BatchOp{
		{Type: index.BatchOpIndex, PrimaryKey: "x", Document: map[string]interface{}{"foo": "bar"}},
	}
	if err := mgr.ApplyBatch("nonexistent", ops); err == nil {
		t.Fatal("expected error for unknown object type")
	}
}

func TestManager_ApplyBatch_Empty(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Empty batch is a no-op.
	if err := mgr.ApplyBatch("employee", nil); err != nil {
		t.Fatalf("ApplyBatch(nil): %v", err)
	}
	if err := mgr.ApplyBatch("employee", []index.BatchOp{}); err != nil {
		t.Fatalf("ApplyBatch([]): %v", err)
	}
}

func TestManager_ApplyBatch_UnknownOpType(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	ops := []index.BatchOp{
		{Type: "INVALID", PrimaryKey: "emp-1"},
	}
	if err := mgr.ApplyBatch("employee", ops); err == nil {
		t.Fatal("expected error for unknown op type")
	}
}

// --- FieldMappingForBaseType tests ---

func TestFieldMapping_String(t *testing.T) {
	fm := index.FieldMappingForBaseType("string", true)
	if fm.Type != "text" {
		t.Fatalf("expected type %q, got %q", "text", fm.Type)
	}
}

func TestFieldMapping_Integer(t *testing.T) {
	fm := index.FieldMappingForBaseType("integer", true)
	if fm.Type != "number" {
		t.Fatalf("expected type %q, got %q", "number", fm.Type)
	}
}

func TestFieldMapping_Boolean(t *testing.T) {
	fm := index.FieldMappingForBaseType("boolean", true)
	if fm.Type != "boolean" {
		t.Fatalf("expected type %q, got %q", "boolean", fm.Type)
	}
}

func TestFieldMapping_Date(t *testing.T) {
	fm := index.FieldMappingForBaseType("date", true)
	if fm.Type != "datetime" {
		t.Fatalf("expected type %q, got %q", "datetime", fm.Type)
	}
}

func TestFieldMapping_Geopoint(t *testing.T) {
	fm := index.FieldMappingForBaseType("geopoint", true)
	if fm.Type != "geopoint" {
		t.Fatalf("expected type %q, got %q", "geopoint", fm.Type)
	}
}

func TestFieldMapping_NotSearchable(t *testing.T) {
	fm := index.FieldMappingForBaseType("string", false)
	if fm.Index != false {
		t.Fatal("expected Index to be false for non-searchable field")
	}
	if fm.Store != true {
		t.Fatal("expected Store to be true for non-searchable field")
	}
}

// TestManager_EnsureIndex_HonoursAnalyzerNotIndexed is the Consumer-path half
// of US-010. The canonical bootstrap flow is:
//
//  1. funnel.Consumer / rehydrate.EnsureAllIndexes walks OMS properties.
//  2. Each property is converted to an index.Property.
//  3. mgr.EnsureIndex allocates the Bleve index via the internal buildMapping.
//
// Before US-010, buildMapping used FieldMappingForBaseType which did not know
// about the analyzer hint, so even with a not_indexed TypeConfig on the OMS
// row, the rehydrated Bleve index would happily tokenise and search the
// property. This test drives an index.Property{Analyzer: "not_indexed"} all
// the way through EnsureIndex + IndexDocument + Search and asserts that the
// Bleve index excludes it from full-text matches while still returning the
// stored value via Hit.Fields.
func TestManager_EnsureIndex_HonoursAnalyzerNotIndexed(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "blob", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotIndexed},
	}
	if _, err := mgr.EnsureIndex("patent", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	doc := map[string]interface{}{
		"id":   "p1",
		"blob": "confidential prior art notes",
	}
	if err := mgr.IndexDocument("patent", "p1", doc); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// Field match on the not_indexed property must miss.
	blobQ := bleve.NewMatchQuery("confidential")
	blobQ.SetField("blob")
	res, err := mgr.Search("patent", bleve.NewSearchRequest(blobQ))
	if err != nil {
		t.Fatalf("search blob: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("blob field search after EnsureIndex got total=%d, want 0 (not_indexed)", res.Total)
	}

	// Stored payload must still come back when searching by id.
	idQ := bleve.NewMatchQuery("p1")
	idQ.SetField("id")
	idReq := bleve.NewSearchRequest(idQ)
	idReq.Fields = []string{"blob"}
	idRes, err := mgr.Search("patent", idReq)
	if err != nil {
		t.Fatalf("search id: %v", err)
	}
	if idRes.Total != 1 {
		t.Fatalf("id search got total=%d, want 1", idRes.Total)
	}
	if got := idRes.Hits[0].Fields["blob"]; got != "confidential prior art notes" {
		t.Errorf("stored blob = %v, want original payload", got)
	}
}

// --- Manager tests ---

func sampleProperties() []index.Property {
	return []index.Property{
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "age", BaseType: "integer", IsSearchable: true},
		{APIName: "active", BaseType: "boolean", IsSearchable: true},
	}
}

func TestManager_EnsureIndex_New(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	idx, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
}

func TestManager_EnsureIndex_Idempotent(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	idx1, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx2, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both calls should return the same index instance
	if idx1 != idx2 {
		t.Fatal("expected EnsureIndex to return the same index on second call")
	}
}

func TestManager_GetIndex_Exists(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	_, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := mgr.GetIndex("employee")
	if idx == nil {
		t.Fatal("expected non-nil index for existing type")
	}
}

func TestManager_GetIndex_NotExists(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	idx := mgr.GetIndex("nonexistent")
	if idx != nil {
		t.Fatal("expected nil for non-existent index")
	}
}

func TestManager_DropIndex(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	_, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mgr.DropIndex("employee"); err != nil {
		t.Fatalf("unexpected error dropping index: %v", err)
	}

	idx := mgr.GetIndex("employee")
	if idx != nil {
		t.Fatal("expected nil after dropping index")
	}
}

func TestManager_DropIndex_NotExists(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	err := mgr.DropIndex("nonexistent")
	if err != nil {
		t.Fatalf("expected no error dropping non-existent index, got: %v", err)
	}
}

func TestManager_IndexDocument(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	_, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := map[string]interface{}{
		"name":   "Alice",
		"age":    30,
		"active": true,
	}
	if err := mgr.IndexDocument("employee", "emp-1", doc); err != nil {
		t.Fatalf("unexpected error indexing document: %v", err)
	}

	count, err := mgr.DocCount("employee")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected doc count 1, got %d", count)
	}
}

func TestManager_DeleteDocument(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	_, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := map[string]interface{}{
		"name":   "Bob",
		"age":    25,
		"active": true,
	}
	if err := mgr.IndexDocument("employee", "emp-2", doc); err != nil {
		t.Fatalf("unexpected error indexing document: %v", err)
	}

	if err := mgr.DeleteDocument("employee", "emp-2"); err != nil {
		t.Fatalf("unexpected error deleting document: %v", err)
	}

	count, err := mgr.DocCount("employee")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected doc count 0 after delete, got %d", count)
	}
}

func TestManager_Search_MatchAll(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	_, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"emp-1", map[string]interface{}{"name": "Alice", "age": 30, "active": true}},
		{"emp-2", map[string]interface{}{"name": "Bob", "age": 25, "active": false}},
		{"emp-3", map[string]interface{}{"name": "Charlie", "age": 35, "active": true}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("unexpected error indexing %s: %v", d.id, err)
		}
	}

	query := bleve.NewMatchAllQuery()
	req := bleve.NewSearchRequest(query)
	req.Size = 10

	result, err := mgr.Search("employee", req)
	if err != nil {
		t.Fatalf("unexpected error searching: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("expected 3 results, got %d", result.Total)
	}
}

func TestManager_Search_ByField(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	_, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"emp-1", map[string]interface{}{"name": "Alice Smith", "age": 30, "active": true}},
		{"emp-2", map[string]interface{}{"name": "Bob Jones", "age": 25, "active": false}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("unexpected error indexing %s: %v", d.id, err)
		}
	}

	// MatchQuery runs the field's analyzer over the query term so it stays
	// robust against analyzer upgrades (e.g. the US-012 switch to the
	// English stemmer: "Alice" → "alic"). TermQuery bypasses the analyzer
	// and would silently drop to zero hits whenever the stemmer or any
	// token filter mutates the raw surface form.
	query := bleve.NewMatchQuery("alice")
	query.SetField("name")
	req := bleve.NewSearchRequest(query)

	result, err := mgr.Search("employee", req)
	if err != nil {
		t.Fatalf("unexpected error searching: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected 1 result, got %d", result.Total)
	}
	if result.Hits[0].ID != "emp-1" {
		t.Fatalf("expected hit ID %q, got %q", "emp-1", result.Hits[0].ID)
	}
}

func TestManager_DocCount(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	_, err := mgr.EnsureIndex("employee", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count, err := mgr.DocCount("employee")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected initial doc count 0, got %d", count)
	}

	for i := 0; i < 5; i++ {
		doc := map[string]interface{}{"name": "user", "age": i, "active": true}
		if err := mgr.IndexDocument("employee", fmt.Sprintf("emp-%d", i), doc); err != nil {
			t.Fatalf("unexpected error indexing: %v", err)
		}
	}

	count, err = mgr.DocCount("employee")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected doc count 5, got %d", count)
	}
}

func TestManager_Close(t *testing.T) {
	mgr := index.NewManager(t.TempDir())

	_, err := mgr.EnsureIndex("type1", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = mgr.EnsureIndex("type2", sampleProperties())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("unexpected error closing manager: %v", err)
	}

	// After close, GetIndex should return nil for all types
	if mgr.GetIndex("type1") != nil {
		t.Fatal("expected nil after close for type1")
	}
	if mgr.GetIndex("type2") != nil {
		t.Fatal("expected nil after close for type2")
	}
}
