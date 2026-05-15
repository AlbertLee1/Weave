package graphsvc_test

// VTX-024 — manual drag + position persistence. Behavioural surface of
// PATCH /api/vertex/v1/graphs/{rid}/layout:
//
//   1. PATCH layout MUST honour the owner-based ACL applied to GET. A stranger
//      who cannot read the graph must not be able to mutate its positions.
//      Ownerless ("legacy") graphs stay anonymously patchable to preserve
//      pre-VTX-024 test fixtures + degraded-mode boots.
//   2. PATCH layout MUST merge incoming positions into payload.positions —
//      keys present in the request overwrite, unrelated keys are preserved.
//      A wholesale replacement would clobber every other node's pinned coord
//      the moment a single drag fires, defeating the persistence story.
//   3. pinned=true / pinned=false on a per-node position is the canonical
//      flag the layout algorithms read to bypass repositioning. The PATCH
//      handler must round-trip the boolean unchanged.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

// doAsAdmin sends the request with an authenticated user holding the "admin"
// role attached to the context. Used to exercise the admin-bypass branch of
// the owner ACL on mutating endpoints.
func doAsAdmin(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithUser(req.Context(),
		&auth.User{ID: "admin-user", Roles: []string{"admin"}}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestPatchLayout_Given_OwnedGraph_When_StrangerPATCHes_Then_403 — VTX-024 ACL.
func TestPatchLayout_Given_OwnedGraph_When_StrangerPATCHes_Then_403(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	w := doAsUser(t, r, "user2", http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"ri.ontology.main.object.airport.JFK": map[string]any{
					"x": 10.0, "y": 20.0, "pinned": true,
				},
			},
		})
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger PATCH layout status = %d, want 403; body: %s",
			w.Code, w.Body.String())
	}
}

// TestPatchLayout_Given_OwnedGraph_When_OwnerPATCHes_Then_200 — owner is allowed.
func TestPatchLayout_Given_OwnedGraph_When_OwnerPATCHes_Then_200(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	w := doAsUser(t, r, "user1", http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"ri.ontology.main.object.airport.JFK": map[string]any{
					"x": 10.0, "y": 20.0, "pinned": true,
				},
			},
		})
	if w.Code != http.StatusOK {
		t.Fatalf("owner PATCH layout status = %d, want 200; body: %s",
			w.Code, w.Body.String())
	}
}

// TestPatchLayout_Given_OwnedGraph_When_AdminPATCHes_Then_200 — admins bypass owner check.
func TestPatchLayout_Given_OwnedGraph_When_AdminPATCHes_Then_200(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	w := doAsAdmin(t, r, http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"ri.ontology.main.object.airport.JFK": map[string]any{
					"x": 10.0, "y": 20.0, "pinned": true,
				},
			},
		})
	if w.Code != http.StatusOK {
		t.Fatalf("admin PATCH layout status = %d, want 200; body: %s",
			w.Code, w.Body.String())
	}
}

// TestPatchLayout_Given_LegacyGraphNoOwner_When_AnonPATCHes_Then_200 —
// ownerless graphs stay anonymously patchable for back-compat.
func TestPatchLayout_Given_LegacyGraphNoOwner_When_AnonPATCHes_Then_200(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	cw := doAsUser(t, r, "", http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Legacy",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	if cw.Code != http.StatusCreated {
		t.Fatalf("create legacy graph status = %d, want 201", cw.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(cw.Body.Bytes(), &created)
	rid := created["rid"].(string)

	pw := doAsUser(t, r, "", http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"n1": map[string]any{"x": 1.0, "y": 2.0, "pinned": true},
			},
		})
	if pw.Code != http.StatusOK {
		t.Fatalf("anon PATCH legacy layout status = %d, want 200; body: %s",
			pw.Code, pw.Body.String())
	}
}

// TestPatchLayout_Given_ExistingPositions_When_PATCHSubset_Then_OtherKeysPreserved
// — drag of one node must NOT clobber the others' stored positions.
func TestPatchLayout_Given_ExistingPositions_When_PATCHSubset_Then_OtherKeysPreserved(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	// Seed two positions.
	w1 := doAsUser(t, r, "user1", http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"A": map[string]any{"x": 1.0, "y": 2.0, "pinned": true},
				"B": map[string]any{"x": 3.0, "y": 4.0, "pinned": false},
			},
		})
	if w1.Code != http.StatusOK {
		t.Fatalf("seed PATCH status = %d, want 200; body: %s", w1.Code, w1.Body.String())
	}

	// Drag node A only — must not clobber B.
	w2 := doAsUser(t, r, "user1", http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"A": map[string]any{"x": 99.0, "y": 99.0, "pinned": true},
			},
		})
	if w2.Code != http.StatusOK {
		t.Fatalf("partial PATCH status = %d, want 200; body: %s", w2.Code, w2.Body.String())
	}

	// Read back via GET and check both keys are still there with the right values.
	gw := doAsUser(t, r, "user1", http.MethodGet, "/api/vertex/v1/graphs/"+rid, nil)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body: %s", gw.Code, gw.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(gw.Body.Bytes(), &got)
	payload, _ := got["payload"].(map[string]any)
	positions, ok := payload["positions"].(map[string]any)
	if !ok {
		t.Fatalf("positions not an object: %v (payload %v)", payload["positions"], payload)
	}
	posA, ok := positions["A"].(map[string]any)
	if !ok {
		t.Fatalf("position A missing or wrong shape: %v", positions["A"])
	}
	if posA["x"].(float64) != 99.0 || posA["y"].(float64) != 99.0 {
		t.Errorf("position A = %v, want x=99 y=99 (drag should update)", posA)
	}
	posB, ok := positions["B"].(map[string]any)
	if !ok {
		t.Fatalf("position B missing — drag of A clobbered B: %v", positions)
	}
	if posB["x"].(float64) != 3.0 || posB["y"].(float64) != 4.0 {
		t.Errorf("position B = %v, want x=3 y=4 (must be preserved)", posB)
	}
}

// TestPatchLayout_Given_NodePosition_When_PATCHPinnedFlag_Then_RoundTrips
// — pinned boolean must round-trip exactly so the layout algorithms can
// read it to skip repositioning.
func TestPatchLayout_Given_NodePosition_When_PATCHPinnedFlag_Then_RoundTrips(t *testing.T) {
	r, _, _ := newACLTestHandler(t)
	rid := createOwnedGraph(t, r, "user1")

	doAsUser(t, r, "user1", http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"A": map[string]any{"x": 5.0, "y": 6.0, "pinned": true},
			},
		})
	doAsUser(t, r, "user1", http.MethodPatch,
		"/api/vertex/v1/graphs/"+rid+"/layout",
		map[string]any{
			"positions": map[string]any{
				"A": map[string]any{"x": 5.0, "y": 6.0, "pinned": false},
			},
		})

	gw := doAsUser(t, r, "user1", http.MethodGet, "/api/vertex/v1/graphs/"+rid, nil)
	var got map[string]any
	_ = json.Unmarshal(gw.Body.Bytes(), &got)
	payload := got["payload"].(map[string]any)
	positions := payload["positions"].(map[string]any)
	posA := positions["A"].(map[string]any)
	if pinned, ok := posA["pinned"].(bool); !ok || pinned {
		t.Errorf("pinned = %v (ok=%v), want false after toggle", posA["pinned"], ok)
	}
}
