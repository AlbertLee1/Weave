package objectset_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

func TestDiffPrimaryKeys_BasicSetSemantics(t *testing.T) {
	tests := []struct {
		name             string
		left, right      []string
		wantOnlyInLeft   []string
		wantOnlyInRight  []string
		wantInBoth       []string
	}{
		{
			name:            "completely disjoint",
			left:            []string{"a", "b", "c"},
			right:           []string{"x", "y"},
			wantOnlyInLeft:  []string{"a", "b", "c"},
			wantOnlyInRight: []string{"x", "y"},
			wantInBoth:      []string{},
		},
		{
			name:            "identical sets",
			left:            []string{"a", "b", "c"},
			right:           []string{"a", "b", "c"},
			wantOnlyInLeft:  []string{},
			wantOnlyInRight: []string{},
			wantInBoth:      []string{"a", "b", "c"},
		},
		{
			name:            "partial overlap",
			left:            []string{"a", "b", "c", "d"},
			right:           []string{"c", "d", "e", "f"},
			wantOnlyInLeft:  []string{"a", "b"},
			wantOnlyInRight: []string{"e", "f"},
			wantInBoth:      []string{"c", "d"},
		},
		{
			name:            "empty left",
			left:            nil,
			right:           []string{"a", "b"},
			wantOnlyInLeft:  []string{},
			wantOnlyInRight: []string{"a", "b"},
			wantInBoth:      []string{},
		},
		{
			name:            "empty right",
			left:            []string{"a", "b"},
			right:           nil,
			wantOnlyInLeft:  []string{"a", "b"},
			wantOnlyInRight: []string{},
			wantInBoth:      []string{},
		},
		{
			name:            "both empty",
			left:            nil,
			right:           nil,
			wantOnlyInLeft:  []string{},
			wantOnlyInRight: []string{},
			wantInBoth:      []string{},
		},
		{
			name:            "duplicate inputs deduped",
			left:            []string{"a", "a", "b", "b"},
			right:           []string{"b", "b", "c"},
			wantOnlyInLeft:  []string{"a"},
			wantOnlyInRight: []string{"c"},
			wantInBoth:      []string{"b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := objectset.DiffPrimaryKeys(tc.left, tc.right)
			assertSortedEqual(t, "onlyInLeft", got.OnlyInLeft, tc.wantOnlyInLeft)
			assertSortedEqual(t, "onlyInRight", got.OnlyInRight, tc.wantOnlyInRight)
			assertSortedEqual(t, "inBoth", got.InBoth, tc.wantInBoth)

			if got.LeftSize != countUnique(tc.left) {
				t.Errorf("LeftSize: got %d want %d", got.LeftSize, countUnique(tc.left))
			}
			if got.RightSize != countUnique(tc.right) {
				t.Errorf("RightSize: got %d want %d", got.RightSize, countUnique(tc.right))
			}
		})
	}
}

func TestDiffPrimaryKeys_LargeSetsBloomCorrectness(t *testing.T) {
	// 50K vs 50K with ~50% overlap; bloom filter false positives must
	// resolve to correct intersection / disjoint membership.
	const n = 50_000
	left := make([]string, n)
	right := make([]string, n)
	for i := 0; i < n; i++ {
		left[i] = fmt.Sprintf("pk-%08d", i)
		right[i] = fmt.Sprintf("pk-%08d", i+n/2) // overlap half
	}

	got := objectset.DiffPrimaryKeys(left, right)

	// Reference: build maps. This is the slow path we are optimising,
	// but it stays correct on 50K, which is what we cross-check against.
	leftSet := make(map[string]struct{}, n)
	for _, k := range left {
		leftSet[k] = struct{}{}
	}
	rightSet := make(map[string]struct{}, n)
	for _, k := range right {
		rightSet[k] = struct{}{}
	}

	wantOnlyInLeft := 0
	wantInBoth := 0
	for k := range leftSet {
		if _, ok := rightSet[k]; ok {
			wantInBoth++
		} else {
			wantOnlyInLeft++
		}
	}
	wantOnlyInRight := len(rightSet) - wantInBoth

	if len(got.OnlyInLeft) != wantOnlyInLeft {
		t.Errorf("OnlyInLeft: got %d want %d", len(got.OnlyInLeft), wantOnlyInLeft)
	}
	if len(got.OnlyInRight) != wantOnlyInRight {
		t.Errorf("OnlyInRight: got %d want %d", len(got.OnlyInRight), wantOnlyInRight)
	}
	if len(got.InBoth) != wantInBoth {
		t.Errorf("InBoth: got %d want %d", len(got.InBoth), wantInBoth)
	}

	// Spot-check sample membership: every InBoth element MUST exist in
	// both reference sets; every OnlyInLeft element must exist only in left.
	for _, k := range got.InBoth {
		if _, ok := leftSet[k]; !ok {
			t.Fatalf("InBoth element %q absent from left reference", k)
		}
		if _, ok := rightSet[k]; !ok {
			t.Fatalf("InBoth element %q absent from right reference", k)
		}
	}
	for _, k := range got.OnlyInLeft {
		if _, ok := leftSet[k]; !ok {
			t.Fatalf("OnlyInLeft element %q absent from left reference", k)
		}
		if _, ok := rightSet[k]; ok {
			t.Fatalf("OnlyInLeft element %q present in right reference", k)
		}
	}
	for _, k := range got.OnlyInRight {
		if _, ok := rightSet[k]; !ok {
			t.Fatalf("OnlyInRight element %q absent from right reference", k)
		}
		if _, ok := leftSet[k]; ok {
			t.Fatalf("OnlyInRight element %q present in left reference", k)
		}
	}

	// Result lists must be sorted ascending so SDK consumers can stream
	// without buffering the entire response.
	if !sort.StringsAreSorted(got.OnlyInLeft) {
		t.Error("OnlyInLeft must be sorted")
	}
	if !sort.StringsAreSorted(got.OnlyInRight) {
		t.Error("OnlyInRight must be sorted")
	}
	if !sort.StringsAreSorted(got.InBoth) {
		t.Error("InBoth must be sorted")
	}
}

func TestDiffPrimaryKeys_DurationMsAndStatsPopulated(t *testing.T) {
	got := objectset.DiffPrimaryKeys([]string{"a", "b"}, []string{"b", "c"})
	if got.DurationMs < 0 {
		t.Errorf("DurationMs negative: %d", got.DurationMs)
	}
	if got.LeftSize != 2 || got.RightSize != 2 {
		t.Errorf("sizes: got left=%d right=%d", got.LeftSize, got.RightSize)
	}
}

func TestDiffPrimaryKeys_MemoryStaysBelowThreshold_1MvsRotated1M(t *testing.T) {
	// US-430 acceptance bar: 1M vs 1M < 200 MB.
	// Only run under -short=false to avoid weighing down the unit suite.
	if testing.Short() {
		t.Skip("skipping memory benchmark under -short")
	}
	const n = 1_000_000

	left := make([]string, n)
	right := make([]string, n)
	for i := 0; i < n; i++ {
		left[i] = fmt.Sprintf("ri.obj.northwind.order.%010d", i)
		right[i] = fmt.Sprintf("ri.obj.northwind.order.%010d", i+n/4) // 75% overlap
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	got := objectset.DiffPrimaryKeys(left, right)

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	used := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	const memCap = 200 * 1024 * 1024
	if used > memCap {
		t.Errorf("heap alloc %d bytes exceeds 200 MB cap (used %d MiB)", used, used>>20)
	}
	if got.LeftSize != n || got.RightSize != n {
		t.Errorf("sizes: got %d / %d, want %d / %d", got.LeftSize, got.RightSize, n, n)
	}
}

func BenchmarkDiffPrimaryKeys_1MvsRotated1M(b *testing.B) {
	const n = 1_000_000
	left := make([]string, n)
	right := make([]string, n)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < n; i++ {
		left[i] = fmt.Sprintf("pk-%010d", i)
		right[i] = fmt.Sprintf("pk-%010d", i+n/2)
	}
	rng.Shuffle(n, func(i, j int) { left[i], left[j] = left[j], left[i] })
	rng.Shuffle(n, func(i, j int) { right[i], right[j] = right[j], right[i] })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = objectset.DiffPrimaryKeys(left, right)
	}
}

func TestHandler_DiffObjectSets_HappyPath(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))
	h := objectset.NewHandler(executor, mgr, objectset.NewStore(time.Hour))

	body := objectset.DiffObjectSetRequest{
		Left: &objectset.Definition{
			Type:        "static",
			ObjectType:  "northwind__order",
			PrimaryKeys: []string{"a", "b", "c"},
		},
		Right: &objectset.Definition{
			Type:        "static",
			ObjectType:  "northwind__order",
			PrimaryKeys: []string{"b", "c", "d"},
		},
	}
	raw, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/diff", h.Diff)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/northwind/objectSets/diff", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}
	var got objectset.DiffObjectSetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.LeftObjectType != "northwind__order" || got.RightObjectType != "northwind__order" {
		t.Errorf("object types: got left=%q right=%q", got.LeftObjectType, got.RightObjectType)
	}
	assertSortedEqual(t, "OnlyInLeft", got.OnlyInLeft, []string{"a"})
	assertSortedEqual(t, "OnlyInRight", got.OnlyInRight, []string{"d"})
	assertSortedEqual(t, "InBoth", got.InBoth, []string{"b", "c"})
	if got.LeftSize != 3 || got.RightSize != 3 || got.IntersectSize != 2 {
		t.Errorf("sizes: %+v", got)
	}
}

func TestHandler_DiffObjectSets_MissingSidesRejected(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))
	h := objectset.NewHandler(executor, mgr, objectset.NewStore(time.Hour))

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/diff", h.Diff)

	for _, body := range []objectset.DiffObjectSetRequest{
		{Right: &objectset.Definition{Type: "static", ObjectType: "x"}},
		{Left: &objectset.Definition{Type: "static", ObjectType: "x"}},
	} {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v2/ontologies/x/objectSets/diff", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("missing side: expected 400 got %d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestHandler_DiffObjectSets_RejectsCrossObjectType(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))
	h := objectset.NewHandler(executor, mgr, objectset.NewStore(time.Hour))

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/diff", h.Diff)

	body := objectset.DiffObjectSetRequest{
		Left: &objectset.Definition{
			Type:       "static",
			ObjectType: "order",
		},
		Right: &objectset.Definition{
			Type:       "static",
			ObjectType: "customer",
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v2/ontologies/x/objectSets/diff", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-type diff: expected 400 got %d body=%s", w.Code, w.Body.String())
	}
}

// --- helpers --------------------------------------------------------------

func assertSortedEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	if len(gotCopy) != len(wantCopy) {
		t.Errorf("%s: len mismatch — got %v, want %v", label, gotCopy, wantCopy)
		return
	}
	for i := range gotCopy {
		if gotCopy[i] != wantCopy[i] {
			t.Errorf("%s: got %v, want %v", label, gotCopy, wantCopy)
			return
		}
	}
}

func countUnique(in []string) int {
	if len(in) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(in))
	for _, k := range in {
		seen[k] = struct{}{}
	}
	return len(seen)
}

