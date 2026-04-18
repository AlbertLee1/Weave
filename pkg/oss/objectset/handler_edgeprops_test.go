package objectset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestLoadObjects_SearchAround_EdgeProperties (US-210): the LoadObjects
// response must surface per-edge properties under the "__edge" key when a
// searchAround step produces them. Exercises the full LoadObjects handler
// to guarantee the wire contract — EdgePropertiesProvider → Result.EdgeProperties
// → WireObject.Properties["__edge"] → MarshalJSON top-level key.
func TestLoadObjects_SearchAround_EdgeProperties(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	// Source: user index (drives the inner base ObjectSet).
	userProps := []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}
	if _, err := mgr.EnsureIndex("user", userProps); err != nil {
		t.Fatalf("EnsureIndex user: %v", err)
	}
	if err := mgr.IndexDocument("user", "u1", map[string]interface{}{"id": "u1"}); err != nil {
		t.Fatalf("IndexDocument user u1: %v", err)
	}

	// Target: group index. LoadObjects does a per-PK DocIDQuery on the
	// target ObjectType, so the target docs must exist for rows to appear.
	groupProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("group", groupProps); err != nil {
		t.Fatalf("EnsureIndex group: %v", err)
	}
	if err := mgr.IndexDocument("group", "g1", map[string]interface{}{"id": "g1", "name": "Admins"}); err != nil {
		t.Fatalf("IndexDocument group g1: %v", err)
	}
	if err := mgr.IndexDocument("group", "g2", map[string]interface{}{"id": "g2", "name": "Members"}); err != nil {
		t.Fatalf("IndexDocument group g2: %v", err)
	}

	resolver := &mockLinkResolverWithType{
		results:    map[string][]string{"membership": {"g1", "g2"}},
		targetType: map[string]string{"membership": "group"},
	}
	store := objectset.NewStore(time.Hour)
	executor := objectset.NewExecutor(mgr, resolver, store)
	executor.SetEdgePropertiesProvider(&stubEdgePropsProvider{
		byLink: map[string]map[string]map[string]interface{}{
			"membership": {
				"g1": {"role": "admin"},
				"g2": {"role": "member"},
			},
		},
	})

	h := objectset.NewHandler(executor, mgr, store)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", h.LoadObjects)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "searchAround",
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "user",
			},
			"link": "membership",
		},
		"select": []string{"id", "name"},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/test/objectSets/loadObjects",
		bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, rr.Body.String())
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 rows, got %d: %s", len(resp.Data), rr.Body.String())
	}
	edgeByPK := map[string]map[string]interface{}{}
	for _, row := range resp.Data {
		pk, _ := row["__primaryKey"].(string)
		edge, _ := row["__edge"].(map[string]interface{})
		edgeByPK[pk] = edge
	}
	if edgeByPK["g1"]["role"] != "admin" {
		t.Errorf("g1 __edge.role: got %v", edgeByPK["g1"])
	}
	if edgeByPK["g2"]["role"] != "member" {
		t.Errorf("g2 __edge.role: got %v", edgeByPK["g2"])
	}
}

// TestLoadObjects_SearchAround_NoEdgeProperties_NoSurface verifies that when
// the EdgePropertiesProvider returns nothing for an edge (say, a legacy link
// row without edge_properties), no "__edge" key is added to that row.
func TestLoadObjects_SearchAround_NoEdgeProperties_NoSurface(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	if _, err := mgr.EnsureIndex("user", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex user: %v", err)
	}
	_ = mgr.IndexDocument("user", "u1", map[string]interface{}{"id": "u1"})

	groupProps := []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}
	if _, err := mgr.EnsureIndex("group", groupProps); err != nil {
		t.Fatalf("EnsureIndex group: %v", err)
	}
	_ = mgr.IndexDocument("group", "g1", map[string]interface{}{"id": "g1"})

	resolver := &mockLinkResolverWithType{
		results:    map[string][]string{"membership": {"g1"}},
		targetType: map[string]string{"membership": "group"},
	}
	store := objectset.NewStore(time.Hour)
	executor := objectset.NewExecutor(mgr, resolver, store)
	// Provider returns nil for this link — no enrichment should surface.
	executor.SetEdgePropertiesProvider(&stubEdgePropsProvider{byLink: map[string]map[string]map[string]interface{}{}})

	h := objectset.NewHandler(executor, mgr, store)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", h.LoadObjects)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "searchAround",
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "user",
			},
			"link": "membership",
		},
		"select": []string{"id", "name"},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/test/objectSets/loadObjects",
		bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req.WithContext(context.Background()))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 row, got %d", len(resp.Data))
	}
	if _, present := resp.Data[0]["__edge"]; present {
		t.Errorf("__edge should be absent when no edge props are provided, got %v", resp.Data[0]["__edge"])
	}
}
