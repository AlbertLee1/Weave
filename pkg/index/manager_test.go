package index_test

import (
	"fmt"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
)

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

	query := bleve.NewTermQuery("alice")
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
