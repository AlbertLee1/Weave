package objectset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/oss/pagination"
)

// TestInterfacePaging_Page5_ThreeSubtypes_Given_AsymmetricBuckets_When_PageAll_Then_StableCompositeCursor
// hardens the US-463 acceptance: a composite {objectTypeApiName, innerCursor}
// cursor merges three sub-types in deterministic order, and walking every
// page at pageSize=5 yields every row exactly once with no drops, no dupes,
// and a strictly increasing global PK sequence.
//
// Bucket sizes are asymmetric (7 / 5 / 11) so we exercise mid-pagination
// sub-stream exhaustion: type "ti3_a" drains by page 2, "ti3_b" drains by
// page 3, and "ti3_c" keeps draining through the final tail. After type "a"
// drains, its sub-cursor MUST be absent from the emitted MultiTypeCursor so
// the wire token shrinks monotonically as sub-streams complete.
func TestInterfacePaging_Page5_ThreeSubtypes_Given_AsymmetricBuckets_When_PageAll_Then_StableCompositeCursor(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { _ = mgr.Close() })

	type subtypeSeed struct {
		apiName string
		prefix  string
		count   int
	}
	seeds := []subtypeSeed{
		{apiName: "ti3_a", prefix: "a", count: 7},
		{apiName: "ti3_b", prefix: "b", count: 5},
		{apiName: "ti3_c", prefix: "c", count: 11},
	}

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	expectedRows := map[string]string{} // pk -> apiName
	expectedPerType := map[string]int{}
	for _, s := range seeds {
		if _, err := mgr.EnsureIndex(s.apiName, props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", s.apiName, err)
		}
		for i := 1; i <= s.count; i++ {
			pk := fmt.Sprintf("%s%02d", s.prefix, i)
			if err := mgr.IndexDocument(s.apiName, pk, map[string]interface{}{
				"id":   pk,
				"name": fmt.Sprintf("%s-%d", s.apiName, i),
			}); err != nil {
				t.Fatalf("IndexDocument %s/%s: %v", s.apiName, pk, err)
			}
			expectedRows[pk] = s.apiName
		}
		expectedPerType[s.apiName] = s.count
	}
	totalExpected := len(expectedRows)
	if totalExpected != 23 {
		t.Fatalf("test fixture drift: expected 23 rows, got %d", totalExpected)
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetInterfaceResolver(&fakeInterfaceResolver{
		types: map[string][]string{"HasOwnerR1": {"ti3_a", "ti3_b", "ti3_c"}},
	})
	handler := objectset.NewHandler(executor, mgr, store)

	// Preconditions on the executor: per-type buckets must be present, sized
	// correctly, and pre-sorted ASC so the handler-side heap merge stays
	// stable across pages.
	result, err := executor.Execute(context.Background(), &objectset.Definition{
		Type:          "interfaceBase",
		InterfaceType: "HasOwnerR1",
	})
	if err != nil {
		t.Fatalf("executor.Execute: %v", err)
	}
	if got := len(result.PerTypePKs); got != 3 {
		t.Fatalf("expected 3 per-type buckets, got %d", got)
	}
	for _, s := range seeds {
		bucket, ok := result.PerTypePKs[s.apiName]
		if !ok {
			t.Fatalf("missing per-type bucket %s", s.apiName)
		}
		if len(bucket) != s.count {
			t.Fatalf("bucket %s: got %d rows, want %d", s.apiName, len(bucket), s.count)
		}
		if !sort.StringsAreSorted(bucket) {
			t.Fatalf("bucket %s not sorted ASC: %v", s.apiName, bucket)
		}
	}

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	const pageSize = 5
	// (apiName,pk) tuple key so PK collisions across types stay distinct.
	seen := make(map[string]string, totalExpected)
	perType := make(map[string]int, len(seeds))
	pageToken := ""
	pageRowCounts := make([]int, 0, 8)
	pageTokens := make([]string, 0, 8) // tokens AFTER each page; last is "".
	// Capture the post-page-2 cursor so we can resume from it later.
	resumeToken := ""
	const resumeAfterPage = 2
	// Global flat-PK sequence to assert strict monotone increase across all
	// pages (heap-merge correctness, not just per-page correctness).
	flatPKsSeen := make([]string, 0, totalExpected)
	pageCount := 0
	const maxPages = 10
	for {
		if pageCount >= maxPages {
			t.Fatalf("paging did not terminate after %d iterations", maxPages)
		}
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":          "interfaceBase",
				"interfaceType": "HasOwnerR1",
			},
			"select":   []string{"id", "name"},
			"pageSize": pageSize,
		}
		if pageToken != "" {
			body["pageToken"] = pageToken
		}
		raw, _ := json.Marshal(body)

		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v2/ontologies/test/objectSets/loadObjectsOrInterfaces?preview=true",
			bytes.NewReader(raw),
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("page %d: expected 200, got %d: %s", pageCount, rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("page %d: unmarshal: %v", pageCount, err)
		}
		data, ok := resp["data"].([]interface{})
		if !ok {
			t.Fatalf("page %d: expected data array, got %T", pageCount, resp["data"])
		}

		for _, item := range data {
			row, ok := item.(map[string]interface{})
			if !ok {
				t.Fatalf("page %d: expected row object, got %T", pageCount, item)
			}
			pk, _ := row["$primaryKey"].(string)
			ap, _ := row["$apiName"].(string)
			if pk == "" || ap == "" {
				t.Fatalf("page %d: missing $primaryKey/$apiName: %+v", pageCount, row)
			}
			if wantAp, ok := expectedRows[pk]; !ok || wantAp != ap {
				t.Fatalf("page %d: unexpected row pk=%s apiName=%s (want %s)", pageCount, pk, ap, wantAp)
			}
			key := ap + "|" + pk
			if prior, dup := seen[key]; dup {
				t.Fatalf("page %d: duplicate row %s (prior page %s)", pageCount, key, prior)
			}
			seen[key] = fmt.Sprintf("page %d", pageCount)
			perType[ap]++
			flatPKsSeen = append(flatPKsSeen, pk)
		}

		nextToken, _ := resp["nextPageToken"].(string)
		pageRowCounts = append(pageRowCounts, len(data))
		pageTokens = append(pageTokens, nextToken)
		pageCount++

		// Capture the cursor after page 2 (0-indexed page 1) for the resume
		// assertion below. We snapshot the OUTBOUND token, so the next request
		// using this token returns page 3.
		if pageCount == resumeAfterPage && nextToken != "" {
			resumeToken = nextToken
		}

		if nextToken == "" {
			break
		}
		// Non-final pages MUST be exactly pageSize.
		if len(data) != pageSize {
			t.Fatalf("non-final page %d returned %d rows, expected %d", pageCount-1, len(data), pageSize)
		}
		if nextToken == pageToken {
			t.Fatalf("page %d: nextPageToken did not advance", pageCount-1)
		}

		// Cursor stability: decoding the emitted token, re-encoding, then
		// decoding again MUST yield the same SubCursors. This guards against
		// a future regression where MultiTypeCursor.Encode becomes
		// non-deterministic (e.g., switching SubCursors from slice to map).
		mc1, err := pagination.DecodeMultiTypeCursor(nextToken)
		if err != nil {
			t.Fatalf("page %d: decode emitted token: %v", pageCount-1, err)
		}
		reencoded := mc1.Encode()
		mc2, err := pagination.DecodeMultiTypeCursor(reencoded)
		if err != nil {
			t.Fatalf("page %d: decode reencoded token: %v", pageCount-1, err)
		}
		if len(mc1.SubCursors) != len(mc2.SubCursors) {
			t.Fatalf("page %d: decode→encode→decode SubCursor count mismatch: %d vs %d", pageCount-1, len(mc1.SubCursors), len(mc2.SubCursors))
		}
		for i := range mc1.SubCursors {
			a, b := mc1.SubCursors[i], mc2.SubCursors[i]
			if a.ObjectType != b.ObjectType || a.InnerCursor != b.InnerCursor || len(a.SortKeys) != len(b.SortKeys) {
				t.Fatalf("page %d: SubCursor[%d] not stable: %+v vs %+v", pageCount-1, i, a, b)
			}
			for k := range a.SortKeys {
				if a.SortKeys[k] != b.SortKeys[k] {
					t.Fatalf("page %d: SubCursor[%d].SortKeys[%d] not stable: %+v vs %+v", pageCount-1, i, k, a.SortKeys[k], b.SortKeys[k])
				}
			}
		}

		// US-463 wire contract: every live SubCursor names a sub-type that
		// still has rows left. Sub-types whose offsets reached the bucket
		// length MUST be dropped from the emitted token.
		for _, sc := range mc1.SubCursors {
			if sc.ObjectType == "" {
				t.Fatalf("page %d: SubCursor missing ObjectType: %+v", pageCount-1, sc)
			}
			if sc.IsExhausted() {
				t.Fatalf("page %d: exhausted SubCursor %+v leaked into emitted token", pageCount-1, sc)
			}
		}

		pageToken = nextToken
	}

	// 23 rows / page size 5 = ceil(23/5) = 5 pages with a final tail of 3.
	if pageCount != 5 {
		t.Fatalf("expected 5 pages at pageSize=5 over 23 rows, got %d (page row counts=%v)", pageCount, pageRowCounts)
	}
	if got := pageRowCounts[len(pageRowCounts)-1]; got != 3 {
		t.Fatalf("final page row count: got %d, want 3", got)
	}
	if pageTokens[len(pageTokens)-1] != "" {
		t.Errorf("final page must emit empty nextPageToken, got %q", pageTokens[len(pageTokens)-1])
	}

	if len(seen) != totalExpected {
		t.Fatalf("expected %d unique rows after full walk, got %d", totalExpected, len(seen))
	}
	for _, s := range seeds {
		if perType[s.apiName] != expectedPerType[s.apiName] {
			t.Errorf("type %s: got %d, want %d", s.apiName, perType[s.apiName], expectedPerType[s.apiName])
		}
	}

	// Heap-merge correctness: the global flat PK sequence across all pages
	// must be strictly monotone ASC since every per-type bucket is sorted
	// ASC and ties break by sub-type api name.
	for i := 1; i < len(flatPKsSeen); i++ {
		if flatPKsSeen[i-1] >= flatPKsSeen[i] {
			t.Errorf("heap-merge order broken at row %d: %q >= %q (flat=%v)", i, flatPKsSeen[i-1], flatPKsSeen[i], flatPKsSeen)
		}
	}

	// Resume: re-issue a fresh request using the captured page-2 cursor and
	// confirm the next 5 rows match the ones we got in the original walk.
	// This proves the composite cursor is a true serializable resumption
	// point, not just a same-process bookkeeping artifact.
	if resumeToken == "" {
		t.Fatalf("missing resume token after page %d", resumeAfterPage)
	}
	// Indices of the rows the original walk produced AFTER page 2 (0-indexed
	// pages 0..1 are pages 1..2 = 10 rows already drained).
	expectedAfterResume := flatPKsSeen[resumeAfterPage*pageSize : resumeAfterPage*pageSize+pageSize]

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":          "interfaceBase",
			"interfaceType": "HasOwnerR1",
		},
		"select":    []string{"id", "name"},
		"pageSize":  pageSize,
		"pageToken": resumeToken,
	}
	rawBody, _ := json.Marshal(body)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v2/ontologies/test/objectSets/loadObjectsOrInterfaces?preview=true",
		bytes.NewReader(rawBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resume: unmarshal: %v", err)
	}
	data, ok := resp["data"].([]interface{})
	if !ok || len(data) != pageSize {
		t.Fatalf("resume: expected %d rows, got %v", pageSize, data)
	}
	resumedPKs := make([]string, 0, pageSize)
	for _, item := range data {
		row := item.(map[string]interface{})
		resumedPKs = append(resumedPKs, row["$primaryKey"].(string))
	}
	for i, want := range expectedAfterResume {
		if resumedPKs[i] != want {
			t.Errorf("resume row %d: got %q, want %q (full resumed=%v / expected=%v)", i, resumedPKs[i], want, resumedPKs, expectedAfterResume)
		}
	}
}

// TestInterfacePaging_Page5_ThreeSubtypes_Given_PreviousResumeMidPage_When_DropsExhaustedType_Then_NoOrphanRows
// asserts the specific US-463 invariant that a sub-cursor dropped from a
// previous page's emitted token (because that sub-type was exhausted)
// stays dropped on resumption — i.e., the handler does NOT re-include that
// sub-type's rows. This guards against a regression where a missing
// SubCursor for a known result type is reinterpreted as "offset=0" instead
// of "exhausted".
func TestInterfacePaging_Page5_ThreeSubtypes_Given_PreviousResumeMidPage_When_DropsExhaustedType_Then_NoOrphanRows(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { _ = mgr.Close() })

	props := []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}
	types := []string{"r463_a", "r463_b", "r463_c"}
	for _, ot := range types {
		if _, err := mgr.EnsureIndex(ot, props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}
	// type a: 1 row (drains in page 1), type b: 4 rows, type c: 6 rows.
	if err := mgr.IndexDocument("r463_a", "a01", map[string]interface{}{"id": "a01"}); err != nil {
		t.Fatalf("seed r463_a: %v", err)
	}
	for i := 1; i <= 4; i++ {
		pk := fmt.Sprintf("b%02d", i)
		if err := mgr.IndexDocument("r463_b", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("seed r463_b %s: %v", pk, err)
		}
	}
	for i := 1; i <= 6; i++ {
		pk := fmt.Sprintf("c%02d", i)
		if err := mgr.IndexDocument("r463_c", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("seed r463_c %s: %v", pk, err)
		}
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetInterfaceResolver(&fakeInterfaceResolver{
		types: map[string][]string{"R463Drop": types},
	})
	handler := objectset.NewHandler(executor, mgr, store)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	const pageSize = 5
	getPage := func(token string) (rows []map[string]interface{}, next string) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":          "interfaceBase",
				"interfaceType": "R463Drop",
			},
			"select":   []string{"id"},
			"pageSize": pageSize,
		}
		if token != "" {
			body["pageToken"] = token
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/t/objectSets/loadObjectsOrInterfaces?preview=true", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("getPage: expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("getPage: unmarshal: %v", err)
		}
		nextRaw, _ := resp["nextPageToken"].(string)
		dataRaw, _ := resp["data"].([]interface{})
		rows = make([]map[string]interface{}, 0, len(dataRaw))
		for _, item := range dataRaw {
			rows = append(rows, item.(map[string]interface{}))
		}
		return rows, nextRaw
	}

	page1, tok1 := getPage("")
	if len(page1) != pageSize {
		t.Fatalf("page 1 row count: got %d, want %d", len(page1), pageSize)
	}
	// Expected page 1 (heap-merge ASC): a01, b01, b02, b03, b04. Type a is
	// exhausted; type b is also exhausted; only type c has rows left.
	expected1 := []string{"a01", "b01", "b02", "b03", "b04"}
	for i, want := range expected1 {
		if got := page1[i]["$primaryKey"]; got != want {
			t.Errorf("page 1 row %d: got %v, want %s", i, got, want)
		}
	}

	mc, err := pagination.DecodeMultiTypeCursor(tok1)
	if err != nil {
		t.Fatalf("decode tok1: %v", err)
	}
	// Only the live sub-type (r463_c) may appear in the emitted token.
	if len(mc.SubCursors) != 1 || mc.SubCursors[0].ObjectType != "r463_c" {
		t.Fatalf("expected exactly one live SubCursor for r463_c, got %+v", mc.SubCursors)
	}

	// Resume: page 2 should yield ONLY the remaining type-c rows, never
	// reanimate type a or b even though the resume cursor omits them.
	page2, tok2 := getPage(tok1)
	if len(page2) != 5 {
		t.Fatalf("page 2 row count: got %d, want 5", len(page2))
	}
	expected2 := []string{"c01", "c02", "c03", "c04", "c05"}
	for i, want := range expected2 {
		if got := page2[i]["$primaryKey"]; got != want {
			t.Errorf("page 2 row %d: got %v, want %s", i, got, want)
		}
		if ap := page2[i]["$apiName"]; ap != "r463_c" {
			t.Errorf("page 2 row %d: $apiName got %v, want r463_c", i, ap)
		}
	}
	// Final page is c06 alone.
	page3, tok3 := getPage(tok2)
	if len(page3) != 1 || page3[0]["$primaryKey"] != "c06" {
		t.Fatalf("page 3: expected [c06], got %v", page3)
	}
	if tok3 != "" {
		t.Errorf("page 3 must terminate with empty nextPageToken, got %q", tok3)
	}
}
