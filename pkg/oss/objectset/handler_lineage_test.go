package objectset_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// runLineageRequest mounts the lineage handler on a chi router and replays a
// GET request against it.
func runLineageRequest(t *testing.T, h *objectset.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/lineage", h.GetObjectSetLineage)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeLineageResp(t *testing.T, body []byte) objectset.LineageResponse {
	t.Helper()
	var resp objectset.LineageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode lineage response: %v\nbody=%s", err, string(body))
	}
	return resp
}

// TestObjectSetLineage_BaseSet — single-node lineage for a leaf base ObjectSet.
func TestObjectSetLineage_BaseSet(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	rid := store.Put(def)

	w := runLineageRequest(t, handler, "/api/v2/ontologies/o/objectSets/"+rid+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeLineageResp(t, w.Body.Bytes())
	if resp.RID != rid {
		t.Errorf("RID = %q, want %q", resp.RID, rid)
	}
	if resp.Root == "" {
		t.Fatalf("Root is empty; want a synthetic node id")
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d (%v)", len(resp.Nodes), resp.Nodes)
	}
	if resp.Nodes[0].Type != "base" {
		t.Errorf("node[0].Type = %q, want base", resp.Nodes[0].Type)
	}
	if resp.Nodes[0].ObjectType != "employee" {
		t.Errorf("node[0].ObjectType = %q, want employee", resp.Nodes[0].ObjectType)
	}
	if resp.Nodes[0].ID != resp.Root {
		t.Errorf("Nodes[0].ID = %q, root = %q; want them equal for a single-node tree", resp.Nodes[0].ID, resp.Root)
	}
	if len(resp.Edges) != 0 {
		t.Errorf("want 0 edges for a base set, got %d (%v)", len(resp.Edges), resp.Edges)
	}
}

// TestObjectSetLineage_Filter — filter wrapped over a base set: 2 nodes, 1
// edge from base -> filter, root = filter.
func TestObjectSetLineage_Filter(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Where:     json.RawMessage(`{"type":"eq","field":"name","value":"alice"}`),
	}
	rid := store.Put(def)

	w := runLineageRequest(t, handler, "/api/v2/ontologies/o/objectSets/"+rid+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeLineageResp(t, w.Body.Bytes())

	if len(resp.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(resp.Nodes))
	}
	if len(resp.Edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(resp.Edges))
	}

	rootNode := findNode(t, resp, resp.Root)
	if rootNode.Type != "filter" {
		t.Errorf("root node type = %q, want filter", rootNode.Type)
	}
	if len(rootNode.Where) == 0 {
		t.Errorf("root filter node should carry Where, got %q", string(rootNode.Where))
	}

	if resp.Edges[0].To != resp.Root {
		t.Errorf("edge.To = %q, want root %q", resp.Edges[0].To, resp.Root)
	}
	if resp.Edges[0].Operation != "filter" {
		t.Errorf("edge.Operation = %q, want filter", resp.Edges[0].Operation)
	}
	src := findNode(t, resp, resp.Edges[0].From)
	if src.Type != "base" || src.ObjectType != "employee" {
		t.Errorf("edge source = %+v, want base employee", src)
	}
}

// TestObjectSetLineage_Union — union of two base sets: 3 nodes, 2 edges, root
// = union.
func TestObjectSetLineage_Union(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)
	def := &objectset.Definition{
		Type: "union",
		ObjectSets: []*objectset.Definition{
			{Type: "base", ObjectType: "employee"},
			{Type: "base", ObjectType: "department"},
		},
	}
	rid := store.Put(def)

	w := runLineageRequest(t, handler, "/api/v2/ontologies/o/objectSets/"+rid+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeLineageResp(t, w.Body.Bytes())

	if len(resp.Nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d (%+v)", len(resp.Nodes), resp.Nodes)
	}
	if len(resp.Edges) != 2 {
		t.Fatalf("want 2 edges, got %d (%+v)", len(resp.Edges), resp.Edges)
	}
	if findNode(t, resp, resp.Root).Type != "union" {
		t.Errorf("root type = %q, want union", findNode(t, resp, resp.Root).Type)
	}
	for _, e := range resp.Edges {
		if e.To != resp.Root {
			t.Errorf("edge.To = %q, want root %q", e.To, resp.Root)
		}
		if e.Operation != "union" {
			t.Errorf("edge.Operation = %q, want union", e.Operation)
		}
	}
}

// TestObjectSetLineage_WithPropertiesAggregation — withProperties carrying
// derivedProperties (link-hop count) surfaces aggregation metric details on
// the node so callers can render the operation chain.
func TestObjectSetLineage_WithPropertiesAggregation(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "deptCount", Link: "employeeDept", Metric: "count"},
		},
	}
	rid := store.Put(def)

	w := runLineageRequest(t, handler, "/api/v2/ontologies/o/objectSets/"+rid+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeLineageResp(t, w.Body.Bytes())

	root := findNode(t, resp, resp.Root)
	if root.Type != "withProperties" {
		t.Fatalf("root type = %q, want withProperties", root.Type)
	}
	if len(root.DerivedProperties) != 1 {
		t.Fatalf("want 1 derivedProperty, got %d", len(root.DerivedProperties))
	}
	dp := root.DerivedProperties[0]
	if dp.Name != "deptCount" || dp.Metric != "count" || dp.Link != "employeeDept" {
		t.Errorf("derivedProperty = %+v, want {deptCount,employeeDept,count}", dp)
	}
}

// TestObjectSetLineage_SearchAround — searchAround records the link + direction
// on the node and the edge operation.
func TestObjectSetLineage_SearchAround(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Link:      "employeeDept",
		Direction: "forward",
	}
	rid := store.Put(def)

	w := runLineageRequest(t, handler, "/api/v2/ontologies/o/objectSets/"+rid+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeLineageResp(t, w.Body.Bytes())

	root := findNode(t, resp, resp.Root)
	if root.Type != "searchAround" {
		t.Fatalf("root type = %q, want searchAround", root.Type)
	}
	if root.Link != "employeeDept" {
		t.Errorf("root.Link = %q, want employeeDept", root.Link)
	}
	if root.Direction != "forward" {
		t.Errorf("root.Direction = %q, want forward", root.Direction)
	}
	if len(resp.Edges) != 1 || resp.Edges[0].Operation != "searchAround" {
		t.Errorf("edges = %+v, want one searchAround edge", resp.Edges)
	}
}

// TestObjectSetLineage_Nested — filter(union(base,base)): 4 nodes, 3 edges,
// reachable in BFS from the root.
func TestObjectSetLineage_Nested(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)
	def := &objectset.Definition{
		Type: "filter",
		ObjectSet: &objectset.Definition{
			Type: "union",
			ObjectSets: []*objectset.Definition{
				{Type: "base", ObjectType: "employee"},
				{Type: "base", ObjectType: "department"},
			},
		},
		Where: json.RawMessage(`{"type":"eq","field":"x","value":1}`),
	}
	rid := store.Put(def)

	w := runLineageRequest(t, handler, "/api/v2/ontologies/o/objectSets/"+rid+"/lineage")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeLineageResp(t, w.Body.Bytes())

	if len(resp.Nodes) != 4 {
		t.Fatalf("want 4 nodes, got %d (%+v)", len(resp.Nodes), resp.Nodes)
	}
	if len(resp.Edges) != 3 {
		t.Fatalf("want 3 edges, got %d (%+v)", len(resp.Edges), resp.Edges)
	}
	if findNode(t, resp, resp.Root).Type != "filter" {
		t.Errorf("root type = %q, want filter", findNode(t, resp, resp.Root).Type)
	}

	// Operation counts: filter (1), union (2)
	opCounts := map[string]int{}
	for _, e := range resp.Edges {
		opCounts[e.Operation]++
	}
	if opCounts["filter"] != 1 {
		t.Errorf("filter edges = %d, want 1", opCounts["filter"])
	}
	if opCounts["union"] != 2 {
		t.Errorf("union edges = %d, want 2", opCounts["union"])
	}
}

// TestObjectSetLineage_NotFound — unknown rid returns 404
// ObjectSetNotFound (parity with GetObjectSet).
func TestObjectSetLineage_NotFound(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	w := runLineageRequest(t, handler, "/api/v2/ontologies/o/objectSets/does-not-exist/lineage")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ObjectSetNotFound") {
		t.Errorf("expected ObjectSetNotFound errorName, got %s", w.Body.String())
	}
}

// findNode is a tiny test helper that returns the LineageNode whose ID
// matches the supplied id, or fails the test.
func findNode(t *testing.T, resp objectset.LineageResponse, id string) objectset.LineageNode {
	t.Helper()
	for _, n := range resp.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node with id %q in %+v", id, resp.Nodes)
	return objectset.LineageNode{}
}
