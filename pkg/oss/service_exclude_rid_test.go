package oss_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oss"
)

// hasRID marshals a WireObject to its Foundry-flattened wire shape and reports
// whether the reserved `__rid` key is present. This mirrors exactly what the
// HTTP layer serializes, so the assertion tracks observable behavior.
func hasRID(t *testing.T, obj *oss.WireObject) bool {
	t.Helper()
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal WireObject: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal WireObject: %v", err)
	}
	_, ok := m["__rid"]
	return ok
}

// TestSearchObjects_ExcludeRID is the unit-level contract for the Foundry
// SearchObjectsRequestV2.excludeRid field on the service.
//
//   - ExcludeRID=true  -> returned objects carry no __rid
//   - ExcludeRID unset -> returned objects carry __rid (backward compatible)
func TestSearchObjects_ExcludeRID(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)

	t.Run("excludeRid true omits __rid", func(t *testing.T) {
		page, err := svc.SearchObjects(context.Background(), oss.SearchObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
			ExcludeRID:  true,
		})
		if err != nil {
			t.Fatalf("SearchObjects: %v", err)
		}
		if len(page.Data) == 0 {
			t.Fatalf("expected rows, got none")
		}
		for _, obj := range page.Data {
			if hasRID(t, obj) {
				t.Errorf("excludeRid=true must omit __rid; object=%+v", obj)
			}
		}
	})

	t.Run("default keeps __rid", func(t *testing.T) {
		page, err := svc.SearchObjects(context.Background(), oss.SearchObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
		})
		if err != nil {
			t.Fatalf("SearchObjects: %v", err)
		}
		if len(page.Data) == 0 {
			t.Fatalf("expected rows, got none")
		}
		for _, obj := range page.Data {
			if !hasRID(t, obj) {
				t.Errorf("default search must keep __rid; object=%+v", obj)
			}
		}
	})
}

// TestListObjects_ExcludeRID is the unit-level contract for the Foundry
// `?excludeRid=true` list query parameter on the service.
func TestListObjects_ExcludeRID(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)

	t.Run("excludeRid true omits __rid", func(t *testing.T) {
		page, err := svc.ListObjects(context.Background(), oss.ListObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
			ExcludeRID:  true,
		})
		if err != nil {
			t.Fatalf("ListObjects: %v", err)
		}
		if len(page.Data) == 0 {
			t.Fatalf("expected rows, got none")
		}
		for _, obj := range page.Data {
			if hasRID(t, obj) {
				t.Errorf("excludeRid=true must omit __rid; object=%+v", obj)
			}
		}
	})

	t.Run("default keeps __rid", func(t *testing.T) {
		page, err := svc.ListObjects(context.Background(), oss.ListObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
		})
		if err != nil {
			t.Fatalf("ListObjects: %v", err)
		}
		if len(page.Data) == 0 {
			t.Fatalf("expected rows, got none")
		}
		for _, obj := range page.Data {
			if !hasRID(t, obj) {
				t.Errorf("default list must keep __rid; object=%+v", obj)
			}
		}
	})
}
