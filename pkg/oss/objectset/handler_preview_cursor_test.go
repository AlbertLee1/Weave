package objectset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestInterfacePagingHeapMerge seeds 3 ObjectTypes implementing a shared
// interface with 20 objects each, pages them 7 at a time through the
// loadObjectsOrInterfaces preview endpoint and verifies that heap-merge
// composite cursor pagination produces every object exactly once.
func TestInterfacePagingHeapMerge(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	types := []string{"ipaging_customer", "ipaging_employee", "ipaging_supplier"}
	prefix := map[string]string{
		"ipaging_customer": "c",
		"ipaging_employee": "e",
		"ipaging_supplier": "s",
	}
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	for _, ot := range types {
		if _, err := mgr.EnsureIndex(ot, props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
		for i := 1; i <= 20; i++ {
			pk := fmt.Sprintf("%s%02d", prefix[ot], i)
			if err := mgr.IndexDocument(ot, pk, map[string]interface{}{
				"id":   pk,
				"name": fmt.Sprintf("%s-%d", ot, i),
			}); err != nil {
				t.Fatalf("IndexDocument %s %s: %v", ot, pk, err)
			}
		}
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetInterfaceResolver(&fakeInterfaceResolver{
		types: map[string][]string{"HasOwner": types},
	})
	handler := objectset.NewHandler(executor, mgr, store)

	// Assert the executor also exposes per-type PK buckets so downstream
	// heap-merge pagination has per-type offsets to track.
	result, err := executor.Execute(context.Background(), &objectset.Definition{
		Type:          "interfaceBase",
		InterfaceType: "HasOwner",
	})
	if err != nil {
		t.Fatalf("executor.Execute: %v", err)
	}
	if result.PerTypePKs == nil {
		t.Fatal("expected Result.PerTypePKs to be populated for interfaceBase execution")
	}
	if len(result.PerTypePKs) != 3 {
		t.Fatalf("expected 3 per-type buckets, got %d", len(result.PerTypePKs))
	}
	for _, ot := range types {
		bucket, ok := result.PerTypePKs[ot]
		if !ok {
			t.Fatalf("expected per-type bucket for %s", ot)
		}
		if len(bucket) != 20 {
			t.Fatalf("expected 20 PKs for %s, got %d", ot, len(bucket))
		}
		// Each per-type bucket must be sorted ASC so heap merge is stable.
		for i := 1; i < len(bucket); i++ {
			if bucket[i-1] > bucket[i] {
				t.Fatalf("%s bucket not sorted: %v", ot, bucket)
			}
		}
	}

	// Page through loadObjectsOrInterfaces in 7-item pages until exhausted.
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	seen := make(map[string]string) // pk -> apiName
	pageToken := ""
	pageCount := 0
	maxPages := 20 // guard against infinite loops
	for {
		if pageCount >= maxPages {
			t.Fatalf("paging did not terminate after %d pages", maxPages)
		}
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":          "interfaceBase",
				"interfaceType": "HasOwner",
			},
			"select":   []string{"id", "name"},
			"pageSize": 7,
		}
		if pageToken != "" {
			body["pageToken"] = pageToken
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v2/ontologies/testOnt/objectSets/loadObjectsOrInterfaces?preview=true", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("page %d: expected 200, got %d: %s", pageCount, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("page %d: unmarshal: %v", pageCount, err)
		}

		data, ok := resp["data"].([]interface{})
		if !ok {
			t.Fatalf("page %d: expected data array, got %T", pageCount, resp["data"])
		}

		for _, raw := range data {
			item, ok := raw.(map[string]interface{})
			if !ok {
				t.Fatalf("page %d: expected item object, got %T", pageCount, raw)
			}
			pk, _ := item["$primaryKey"].(string)
			ap, _ := item["$apiName"].(string)
			if pk == "" || ap == "" {
				t.Fatalf("page %d: missing $primaryKey/$apiName on item: %+v", pageCount, item)
			}
			if prior, dup := seen[pk]; dup {
				t.Fatalf("page %d: duplicate pk %s (prior apiName=%s, new=%s)", pageCount, pk, prior, ap)
			}
			seen[pk] = ap
		}

		nextToken, _ := resp["nextPageToken"].(string)

		// Every non-final page must be exactly pageSize; final page is <= pageSize.
		if nextToken == "" {
			if len(data) == 0 && pageCount == 0 {
				t.Fatal("first page returned zero items")
			}
			pageCount++
			break
		}
		if len(data) != 7 {
			t.Fatalf("non-final page %d must have exactly 7 items, got %d", pageCount, len(data))
		}
		if nextToken == pageToken {
			t.Fatalf("page %d: nextPageToken did not advance", pageCount)
		}
		pageToken = nextToken
		pageCount++
	}

	if len(seen) != 60 {
		t.Fatalf("expected 60 unique objects across paging, got %d", len(seen))
	}
	expectedPages := (60 + 6) / 7 // ceil(60/7) = 9
	if pageCount != expectedPages {
		t.Fatalf("expected %d pages, got %d", expectedPages, pageCount)
	}

	// Sanity-check: each type contributed exactly 20 objects.
	typeCounts := make(map[string]int)
	for _, ap := range seen {
		typeCounts[ap]++
	}
	for _, ot := range types {
		if typeCounts[ot] != 20 {
			t.Errorf("expected 20 %s items, got %d", ot, typeCounts[ot])
		}
	}
}

// TestInterfacePagingHeapMerge_ExhaustedSubTypeDropped ensures that when one
// ObjectType has fewer items than the others, its sub-cursor is dropped
// from the composite cursor once exhausted and paging continues through
// the remaining types without interruption.
func TestInterfacePagingHeapMerge_ExhaustedSubTypeDropped(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}
	for _, ot := range []string{"ipaging_solo_a", "ipaging_solo_b"} {
		if _, err := mgr.EnsureIndex(ot, props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}
	// Type A has 1 object, type B has 5.
	if err := mgr.IndexDocument("ipaging_solo_a", "a01", map[string]interface{}{"id": "a01"}); err != nil {
		t.Fatalf("IndexDocument a01: %v", err)
	}
	for i := 1; i <= 5; i++ {
		pk := fmt.Sprintf("b%02d", i)
		if err := mgr.IndexDocument("ipaging_solo_b", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument %s: %v", pk, err)
		}
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetInterfaceResolver(&fakeInterfaceResolver{
		types: map[string][]string{"Solo": {"ipaging_solo_a", "ipaging_solo_b"}},
	})
	handler := objectset.NewHandler(executor, mgr, store)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	seen := make(map[string]string)
	pageToken := ""
	for pass := 0; pass < 10; pass++ {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":          "interfaceBase",
				"interfaceType": "Solo",
			},
			"select":   []string{"id"},
			"pageSize": 2,
		}
		if pageToken != "" {
			body["pageToken"] = pageToken
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v2/ontologies/testOnt/objectSets/loadObjectsOrInterfaces?preview=true", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("pass %d: expected 200, got %d: %s", pass, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("pass %d: unmarshal: %v", pass, err)
		}
		data, _ := resp["data"].([]interface{})
		for _, raw := range data {
			item := raw.(map[string]interface{})
			pk := item["$primaryKey"].(string)
			ap := item["$apiName"].(string)
			if _, dup := seen[pk]; dup {
				t.Fatalf("duplicate %s", pk)
			}
			seen[pk] = ap
		}
		nextToken, _ := resp["nextPageToken"].(string)
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	if len(seen) != 6 {
		t.Fatalf("expected 6 objects (1 A + 5 B), got %d: %v", len(seen), seen)
	}
	if seen["a01"] != "ipaging_solo_a" {
		t.Errorf("a01 must be ipaging_solo_a, got %q", seen["a01"])
	}
	for i := 1; i <= 5; i++ {
		pk := fmt.Sprintf("b%02d", i)
		if seen[pk] != "ipaging_solo_b" {
			t.Errorf("%s must be ipaging_solo_b, got %q", pk, seen[pk])
		}
	}
}
