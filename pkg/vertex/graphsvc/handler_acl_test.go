package graphsvc_test

// VTX-013 — Share link / permission model. The BDD acceptance is:
//   - Graph A 由 user1 创建 When user2 无 read 权限访问 Then 403
//   - user1 生成 share link When user3 用 link 访问 Then 返回 graph
//     结构但 layers 内对象属性值替换为 "***"
//   - share link 已 revoke When user3 再访问 Then 410 Gone
//
// These tests assert the wire-level behaviour through the full chi router.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// newACLTestHandler wires a Handler with a share-link store installed so the
// share endpoints are reachable.
func newACLTestHandler(t *testing.T) (chi.Router, *graphsvc.MemRepo, *graphsvc.MemShareLinkStore) {
	t.Helper()
	repo := graphsvc.NewMemRepo()
	templates := graphsvc.NewMemTemplateStore()
	shareStore := graphsvc.NewMemShareLinkStore()
	h := graphsvc.NewHandler(repo, templates)
	h.SetShareLinkStore(shareStore)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, repo, shareStore
}

// doAsUser injects an authenticated user into the request context before
// dispatching to the router. user == "" sends the request anonymously.
func doAsUser(t *testing.T, r chi.Router, user, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req = req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: user}))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func createOwnedGraph(t *testing.T, r chi.Router, owner string) string {
	t.Helper()
	w := doAsUser(t, r, owner, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Owned Graph",
		"versioned":   true,
		"createdBy":   owner,
		"payload": map[string]any{
			"layers": []any{
				map[string]any{
					"objectTypeRid": "ri.ontology.main.object-type.airport",
					"objectType":    "Airport",
					"objects": []any{
						map[string]any{
							"objectRid": "ri.ontology.main.object.airport.JFK",
							"properties": map[string]any{
								"name": "John F. Kennedy",
								"code": "JFK",
							},
						},
					},
				},
			},
			"edges": []any{},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create graph status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	rid, _ := resp["rid"].(string)
	if rid == "" {
		t.Fatalf("missing rid in create response: %s", w.Body.String())
	}
	return rid
}

// TestGraphsACL_Given_OwnerCreatedGraph_When_StrangerGETs_Then_403
func TestGraphsACL_Given_OwnerCreatedGraph_When_StrangerGETs_Then_403(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	w := doAsUser(t, r, "user2", http.MethodGet, "/api/vertex/v1/graphs/"+rid, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger GET status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// TestGraphsACL_Given_OwnerCreatedGraph_When_OwnerGETs_Then_200
func TestGraphsACL_Given_OwnerCreatedGraph_When_OwnerGETs_Then_200(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	w := doAsUser(t, r, "user1", http.MethodGet, "/api/vertex/v1/graphs/"+rid, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("owner GET status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestGraphsACL_Given_LegacyGraphNoOwner_When_AnonGETs_Then_200
//
// Existing tests build graphs without createdBy. Ownerless graphs must remain
// publicly readable so VTX-009 / VTX-011 / VTX-012 tests keep passing.
func TestGraphsACL_Given_LegacyGraphNoOwner_When_AnonGETs_Then_200(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	w := doAsUser(t, r, "", http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Legacy",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	rid := resp["rid"].(string)

	gw := doAsUser(t, r, "", http.MethodGet, "/api/vertex/v1/graphs/"+rid, nil)
	if gw.Code != http.StatusOK {
		t.Fatalf("anon GET legacy status = %d, want 200", gw.Code)
	}
}

// TestGraphsACL_Given_OwnerCreatesShareLink_When_StrangerUsesLink_Then_200WithMaskedPayload
func TestGraphsACL_Given_OwnerCreatesShareLink_When_StrangerUsesLink_Then_200WithMaskedPayload(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	// Owner mints a share link.
	cw := doAsUser(t, r, "user1", http.MethodPost,
		"/api/vertex/v1/graphs/"+rid+"/share-links", nil)
	if cw.Code != http.StatusCreated {
		t.Fatalf("create share-link status = %d, want 201; body: %s", cw.Code, cw.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(cw.Body.Bytes(), &created)
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatalf("share-link response missing token: %s", cw.Body.String())
	}
	if g, _ := created["graphRid"].(string); g != rid {
		t.Errorf("graphRid = %q, want %q", g, rid)
	}

	// Stranger (user3) fetches via the share link.
	gw := doAsUser(t, r, "user3", http.MethodGet,
		"/api/vertex/v1/share-links/"+token+"/graph", nil)
	if gw.Code != http.StatusOK {
		t.Fatalf("share GET status = %d, want 200; body: %s", gw.Code, gw.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(gw.Body.Bytes(), &resp)
	if resp["rid"] != rid {
		t.Errorf("rid = %v, want %v", resp["rid"], rid)
	}
	payload, ok := resp["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing/not object: %v", resp["payload"])
	}
	layers, ok := payload["layers"].([]any)
	if !ok || len(layers) != 1 {
		t.Fatalf("layers = %v, want 1 element", payload["layers"])
	}
	layer0 := layers[0].(map[string]any)
	// Layer structure preserved.
	if layer0["objectType"] != "Airport" {
		t.Errorf("layer objectType = %v, want Airport (structure must be preserved)", layer0["objectType"])
	}
	objects, ok := layer0["objects"].([]any)
	if !ok || len(objects) != 1 {
		t.Fatalf("objects = %v, want 1 element", layer0["objects"])
	}
	obj0 := objects[0].(map[string]any)
	if obj0["objectRid"] != "ri.ontology.main.object.airport.JFK" {
		t.Errorf("objectRid lost in masking; got %v", obj0["objectRid"])
	}
	props, ok := obj0["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties map missing after masking: %v", obj0["properties"])
	}
	// Property KEYS must remain visible; VALUES must all be "***".
	for k, v := range props {
		if v != "***" {
			t.Errorf("property %q = %v, want \"***\" (values masked under share link)", k, v)
		}
	}
	if _, hasName := props["name"]; !hasName {
		t.Errorf("property key \"name\" missing — structure should be visible, only values masked")
	}
}

// TestGraphsACL_Given_RevokedShareLink_When_StrangerGETs_Then_410Gone
func TestGraphsACL_Given_RevokedShareLink_When_StrangerGETs_Then_410Gone(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	cw := doAsUser(t, r, "user1", http.MethodPost,
		"/api/vertex/v1/graphs/"+rid+"/share-links", nil)
	if cw.Code != http.StatusCreated {
		t.Fatalf("create share-link status = %d, want 201; body: %s", cw.Code, cw.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(cw.Body.Bytes(), &created)
	token := created["token"].(string)

	// Owner revokes.
	dw := doAsUser(t, r, "user1", http.MethodDelete,
		"/api/vertex/v1/share-links/"+token, nil)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204; body: %s", dw.Code, dw.Body.String())
	}

	// Subsequent access through the link → 410 Gone.
	gw := doAsUser(t, r, "user3", http.MethodGet,
		"/api/vertex/v1/share-links/"+token+"/graph", nil)
	if gw.Code != http.StatusGone {
		t.Fatalf("revoked share GET status = %d, want 410; body: %s", gw.Code, gw.Body.String())
	}
	body := gw.Body.String()
	if !strings.Contains(body, "revoked") && !strings.Contains(body, "Revoked") {
		t.Errorf("revoked body should mention revocation; got: %s", body)
	}
}

// TestGraphsACL_Given_UnknownShareLink_When_GET_Then_404
func TestGraphsACL_Given_UnknownShareLink_When_GET_Then_404(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	gw := doAsUser(t, r, "", http.MethodGet,
		"/api/vertex/v1/share-links/does-not-exist/graph", nil)
	if gw.Code != http.StatusNotFound {
		t.Errorf("unknown token status = %d, want 404", gw.Code)
	}
}

// TestGraphsACL_Given_OwnerGraph_When_StrangerPOSTsShareLink_Then_403
func TestGraphsACL_Given_OwnerGraph_When_StrangerPOSTsShareLink_Then_403(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	w := doAsUser(t, r, "user2", http.MethodPost,
		"/api/vertex/v1/graphs/"+rid+"/share-links", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger create share-link status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// TestGraphsACL_Given_ShareLink_When_StrangerDELETEs_Then_403
func TestGraphsACL_Given_ShareLink_When_StrangerDELETEs_Then_403(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")
	cw := doAsUser(t, r, "user1", http.MethodPost,
		"/api/vertex/v1/graphs/"+rid+"/share-links", nil)
	var created map[string]any
	_ = json.Unmarshal(cw.Body.Bytes(), &created)
	token := created["token"].(string)

	w := doAsUser(t, r, "user2", http.MethodDelete,
		"/api/vertex/v1/share-links/"+token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger revoke status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}
