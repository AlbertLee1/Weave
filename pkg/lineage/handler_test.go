package lineage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// fakeStore is an in-memory oms.LineageStore for handler tests. It indexes
// edges by both upstream_rid and downstream_rid so the BFS traversal can be
// driven without touching PG.
type fakeStore struct {
	mu       sync.Mutex
	upstream map[string][]oms.LineageEdge // key: downstream_rid
	down     map[string][]oms.LineageEdge // key: upstream_rid
	err      error
	calls    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		upstream: map[string][]oms.LineageEdge{},
		down:     map[string][]oms.LineageEdge{},
	}
}

func (f *fakeStore) addEdge(e oms.LineageEdge) {
	f.upstream[e.DownstreamRID] = append(f.upstream[e.DownstreamRID], e)
	f.down[e.UpstreamRID] = append(f.down[e.UpstreamRID], e)
}

func (f *fakeStore) InsertLineageEdge(_ context.Context, edge *oms.LineageEdge) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.addEdge(*edge)
	return nil
}

func (f *fakeStore) ListUpstreamLineage(_ context.Context, downstream string, limit int) ([]oms.LineageEdge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	edges := f.upstream[downstream]
	if limit > 0 && len(edges) > limit {
		edges = edges[:limit]
	}
	out := make([]oms.LineageEdge, len(edges))
	copy(out, edges)
	return out, nil
}

func (f *fakeStore) ListDownstreamLineage(_ context.Context, upstream string, limit int) ([]oms.LineageEdge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	edges := f.down[upstream]
	if limit > 0 && len(edges) > limit {
		edges = edges[:limit]
	}
	out := make([]oms.LineageEdge, len(edges))
	copy(out, edges)
	return out, nil
}

func runRequest(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, w.Body.String())
	}
	return resp
}

const (
	objEmp1   = "ri.phonograph2-objects.main.object.Employee.EMP-001"
	objEmp2   = "ri.phonograph2-objects.main.object.Employee.EMP-002"
	actionLog = "ri.actions.main.action-log.42"
	pipeline1 = "ri.pipelines.main.run.7"
)

// TestHandler_UpstreamDefaults verifies the default direction (upstream) and
// depth (1) when only {rid} is supplied.
func TestHandler_UpstreamDefaults(t *testing.T) {
	store := newFakeStore()
	store.addEdge(oms.LineageEdge{
		UpstreamRID: actionLog, DownstreamRID: objEmp1,
		Operation: "MODIFY", Timestamp: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC),
	})

	w := runRequest(t, NewHandler(store), "/api/v2/objects/"+objEmp1+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Root != objEmp1 {
		t.Errorf("root: got %q, want %q", resp.Root, objEmp1)
	}
	if resp.Direction != DirectionUpstream {
		t.Errorf("direction: got %q, want %q", resp.Direction, DirectionUpstream)
	}
	if resp.Depth != DefaultDepth {
		t.Errorf("depth: got %d, want %d", resp.Depth, DefaultDepth)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("nodes: got %d, want 2 (%+v)", len(resp.Nodes), resp.Nodes)
	}
	if len(resp.Edges) != 1 {
		t.Fatalf("edges: got %d, want 1", len(resp.Edges))
	}
	e := resp.Edges[0]
	if e.From != actionLog || e.To != objEmp1 {
		t.Errorf("edge endpoints: got %s -> %s", e.From, e.To)
	}
	if e.Operation != "MODIFY" {
		t.Errorf("operation: got %q", e.Operation)
	}
	// Type derives from the RID's resource-type segment.
	wantTypes := map[string]string{
		objEmp1:   "object",
		actionLog: "action-log",
	}
	for _, n := range resp.Nodes {
		if got, ok := wantTypes[n.RID]; ok && n.Type != got {
			t.Errorf("node %s: type got %q, want %q", n.RID, n.Type, got)
		}
	}
}

// TestHandler_DownstreamDirection verifies direction=downstream walks the
// inverse of upstream.
func TestHandler_DownstreamDirection(t *testing.T) {
	store := newFakeStore()
	store.addEdge(oms.LineageEdge{UpstreamRID: actionLog, DownstreamRID: objEmp1, Operation: "CREATE"})
	store.addEdge(oms.LineageEdge{UpstreamRID: actionLog, DownstreamRID: objEmp2, Operation: "CREATE"})

	w := runRequest(t, NewHandler(store), "/api/v2/objects/"+actionLog+"/lineage?direction=downstream")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Direction != DirectionDownstream {
		t.Errorf("direction: %q", resp.Direction)
	}
	if len(resp.Nodes) != 3 {
		t.Errorf("nodes: got %d, want 3", len(resp.Nodes))
	}
	if len(resp.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(resp.Edges))
	}
}

// TestHandler_DepthBoundedBFS verifies that depth=2 fans out two hops but
// stops before depth 3.
func TestHandler_DepthBoundedBFS(t *testing.T) {
	store := newFakeStore()
	hop2 := "ri.datasets.main.dataset.X"
	hop3 := "ri.pipelines.main.run.99"
	store.addEdge(oms.LineageEdge{UpstreamRID: actionLog, DownstreamRID: objEmp1, Operation: "MODIFY"})
	store.addEdge(oms.LineageEdge{UpstreamRID: hop2, DownstreamRID: actionLog, Operation: "INGEST"})
	store.addEdge(oms.LineageEdge{UpstreamRID: hop3, DownstreamRID: hop2, Operation: "PIPELINE"})

	w := runRequest(t, NewHandler(store), "/api/v2/objects/"+objEmp1+"/lineage?depth=2")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Depth != 2 {
		t.Errorf("depth: got %d", resp.Depth)
	}
	rids := map[string]bool{}
	for _, n := range resp.Nodes {
		rids[n.RID] = true
	}
	if !rids[objEmp1] || !rids[actionLog] || !rids[hop2] {
		t.Errorf("expected first 2 hops in graph, got %v", rids)
	}
	if rids[hop3] {
		t.Errorf("expected hop3 (depth 3) NOT in graph at depth=2")
	}
	if len(resp.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(resp.Edges))
	}
}

// TestHandler_BothDirection verifies the both direction explores upstream
// AND downstream from the root in one call.
func TestHandler_BothDirection(t *testing.T) {
	store := newFakeStore()
	store.addEdge(oms.LineageEdge{UpstreamRID: actionLog, DownstreamRID: objEmp1, Operation: "CREATE"})
	store.addEdge(oms.LineageEdge{UpstreamRID: objEmp1, DownstreamRID: objEmp2, Operation: "DERIVED"})

	w := runRequest(t, NewHandler(store), "/api/v2/objects/"+objEmp1+"/lineage?direction=both")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Direction != DirectionBoth {
		t.Errorf("direction: %q", resp.Direction)
	}
	rids := map[string]bool{}
	for _, n := range resp.Nodes {
		rids[n.RID] = true
	}
	if !(rids[objEmp1] && rids[actionLog] && rids[objEmp2]) {
		t.Errorf("expected all 3 nodes, got %v", rids)
	}
	if len(resp.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(resp.Edges))
	}
}

// TestHandler_InvalidDirection rejects any direction outside the allowed set.
func TestHandler_InvalidDirection(t *testing.T) {
	w := runRequest(t, NewHandler(newFakeStore()),
		"/api/v2/objects/"+objEmp1+"/lineage?direction=sideways")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !contains(got, "InvalidDirection") {
		t.Errorf("expected InvalidDirection error, got %s", got)
	}
}

// TestHandler_InvalidDepth rejects non-integer / out-of-range depth values.
func TestHandler_InvalidDepth(t *testing.T) {
	tests := []string{"0", "-1", "abc", "11"}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			w := runRequest(t, NewHandler(newFakeStore()),
				"/api/v2/objects/"+objEmp1+"/lineage?depth="+raw)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("depth=%q: want 400, got %d: %s", raw, w.Code, w.Body.String())
			}
			if got := w.Body.String(); !contains(got, "InvalidDepth") {
				t.Errorf("depth=%q: expected InvalidDepth error, got %s", raw, got)
			}
		})
	}
}

// TestHandler_StoreError surfaces a store failure as 500 LineageQueryFailed.
func TestHandler_StoreError(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("pg unreachable")
	w := runRequest(t, NewHandler(store), "/api/v2/objects/"+objEmp1+"/lineage")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !contains(got, "LineageQueryFailed") {
		t.Errorf("expected LineageQueryFailed, got %s", got)
	}
}

// TestHandler_NoStore — when LineageStore is nil the route returns 404
// rather than panicking, mirroring degraded-mode contracts elsewhere in
// the platform.
func TestHandler_NoStore(t *testing.T) {
	w := runRequest(t, NewHandler(nil), "/api/v2/objects/"+objEmp1+"/lineage")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !contains(got, "LineageNotConfigured") {
		t.Errorf("expected LineageNotConfigured, got %s", got)
	}
}

// TestHandler_DedupesNodesAndEdges — diamond / repeat-visit topology must
// not produce duplicate nodes or edges in the response.
func TestHandler_DedupesNodesAndEdges(t *testing.T) {
	store := newFakeStore()
	hub := "ri.actions.main.action-log.99"
	a := "ri.phonograph2-objects.main.object.X.A"
	b := "ri.phonograph2-objects.main.object.X.B"
	merged := "ri.phonograph2-objects.main.object.Y.merged"
	store.addEdge(oms.LineageEdge{UpstreamRID: hub, DownstreamRID: a, Operation: "CREATE"})
	store.addEdge(oms.LineageEdge{UpstreamRID: hub, DownstreamRID: b, Operation: "CREATE"})
	store.addEdge(oms.LineageEdge{UpstreamRID: a, DownstreamRID: merged, Operation: "MERGE"})
	store.addEdge(oms.LineageEdge{UpstreamRID: b, DownstreamRID: merged, Operation: "MERGE"})

	w := runRequest(t, NewHandler(store), "/api/v2/objects/"+hub+"/lineage?direction=downstream&depth=2")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	rids := map[string]int{}
	for _, n := range resp.Nodes {
		rids[n.RID]++
	}
	for rid, count := range rids {
		if count != 1 {
			t.Errorf("node %s appeared %d times, want exactly 1", rid, count)
		}
	}
	if len(resp.Nodes) != 4 {
		t.Errorf("nodes: got %d, want 4", len(resp.Nodes))
	}
	// 2 hub→leaf + 2 leaf→merged = 4 edges, no duplicates.
	if len(resp.Edges) != 4 {
		t.Errorf("edges: got %d, want 4", len(resp.Edges))
	}
}

// TestHandler_TruncatedFlag — when ListUpstreamLineage returns its full
// limit the response surfaces truncated=true so callers know more rows
// exist beyond the BFS frontier.
func TestHandler_TruncatedFlag(t *testing.T) {
	store := newFakeStore()
	for i := 0; i < pageLimit; i++ {
		store.addEdge(oms.LineageEdge{
			UpstreamRID:   "ri.actions.main.action-log." + itoa(i),
			DownstreamRID: objEmp1,
			Operation:     "MODIFY",
		})
	}
	w := runRequest(t, NewHandler(store), "/api/v2/objects/"+objEmp1+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if !resp.Truncated {
		t.Errorf("expected Truncated=true when page limit hit")
	}
}

func TestNodeTypeExtraction(t *testing.T) {
	tests := []struct {
		rid  string
		want string
	}{
		{"ri.phonograph2-objects.main.object.Employee.EMP-001", "object"},
		{"ri.actions.main.action-log.42", "action-log"},
		{"ri.pipelines.main.run.7", "run"},
		{"not-a-rid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NodeType(tt.rid); got != tt.want {
			t.Errorf("NodeType(%q) = %q, want %q", tt.rid, got, tt.want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		(len(needle) > 0 && (indexOf(haystack, needle) >= 0)))
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
