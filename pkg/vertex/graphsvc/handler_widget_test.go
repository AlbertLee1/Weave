package graphsvc_test

// VTX-014 — Workshop-embedded widget surface. BDD acceptance:
//   1. Given Workshop renders vertex_graph widget with graphRid
//      When it calls GET /api/vertex/v1/graphs/{rid}/widget
//      Then the response carries a compact payload — no `savedSelections`,
//      no `history` (and any other widget-noise keys), but layers + edges
//      + positions stay intact so the widget can render.
//   2. Given the widget URL passes overrideGraphRid
//      When the Save action fires (POST /widget/save with body
//      {payload, overrideGraphRid})
//      Then the new state is written to the overrideGraphRid resource, not
//      the source rid. Empty overrideGraphRid falls back to the source rid.
//
// These tests assert the wire-level behaviour through the full chi router.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// newWidgetTestHandler wires a Handler with mem store. Identical to
// newACLTestHandler but returns just the router + repo since widget tests
// don't touch share links.
func newWidgetTestHandler(t *testing.T) (chi.Router, *graphsvc.MemRepo) {
	t.Helper()
	repo := graphsvc.NewMemRepo()
	templates := graphsvc.NewMemTemplateStore()
	h := graphsvc.NewHandler(repo, templates)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, repo
}

// doWidgetRequest dispatches as the given user (empty = anonymous).
func doWidgetRequest(t *testing.T, r chi.Router, user, method, path string, body any) *httptest.ResponseRecorder {
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

// createWidgetGraph creates a graph with savedSelections + the "ambient"
// widget-noise keys that the compact response must strip.
func createWidgetGraph(t *testing.T, r chi.Router, owner string) string {
	t.Helper()
	body := map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Widget Graph",
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
							},
						},
					},
				},
			},
			"edges":     []any{},
			"positions": map[string]any{"n1": map[string]any{"x": 1.0, "y": 2.0}},
			"savedSelections": []any{
				map[string]any{"id": "sel1", "objectRids": []any{"ri.foo.JFK"}},
			},
			"history": []any{map[string]any{"version": 1}},
		},
	}
	w := doWidgetRequest(t, r, owner, http.MethodPost, "/api/vertex/v1/graphs", body)
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

// TestWidget_Given_GraphWithSavedSelectionsAndHistory_When_GETWidget_Then_CompactPayloadReturned
func TestWidget_Given_GraphWithSavedSelectionsAndHistory_When_GETWidget_Then_CompactPayloadReturned(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	rid := createWidgetGraph(t, r, "user1")

	w := doWidgetRequest(t, r, "user1", http.MethodGet,
		"/api/vertex/v1/graphs/"+rid+"/widget", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("widget GET status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode widget response: %v", err)
	}
	if resp["rid"] != rid {
		t.Errorf("rid = %v, want %v", resp["rid"], rid)
	}
	payload, ok := resp["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing or not object: %v", resp["payload"])
	}
	if _, present := payload["savedSelections"]; present {
		t.Errorf("widget payload still carries savedSelections; want it stripped: %v", payload["savedSelections"])
	}
	if _, present := payload["history"]; present {
		t.Errorf("widget payload still carries history; want it stripped: %v", payload["history"])
	}
	layers, ok := payload["layers"].([]any)
	if !ok || len(layers) != 1 {
		t.Fatalf("widget payload lost layers: %v", payload["layers"])
	}
	if _, ok := payload["positions"].(map[string]any); !ok {
		t.Errorf("widget payload should keep positions; got: %v", payload["positions"])
	}
	if _, ok := payload["edges"].([]any); !ok {
		t.Errorf("widget payload should keep edges; got: %v", payload["edges"])
	}
}

// TestWidget_Given_GraphOwner_When_StrangerGETsWidget_Then_403
func TestWidget_Given_GraphOwner_When_StrangerGETsWidget_Then_403(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	rid := createWidgetGraph(t, r, "user1")

	w := doWidgetRequest(t, r, "user2", http.MethodGet,
		"/api/vertex/v1/graphs/"+rid+"/widget", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger widget GET status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// TestWidget_Given_UnknownRID_When_GETWidget_Then_404
func TestWidget_Given_UnknownRID_When_GETWidget_Then_404(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	w := doWidgetRequest(t, r, "user1", http.MethodGet,
		"/api/vertex/v1/graphs/ri.vertex.main.graph.00000000-0000-0000-0000-000000000000/widget", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestWidget_Given_OverrideGraphRid_When_Save_Then_TargetGraphUpdatedNotSource
func TestWidget_Given_OverrideGraphRid_When_Save_Then_TargetGraphUpdatedNotSource(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	sourceRid := createWidgetGraph(t, r, "user1")
	targetRid := createWidgetGraph(t, r, "user1")
	if sourceRid == targetRid {
		t.Fatal("setup error: source and target rid should differ")
	}

	newPayload := map[string]any{
		"layers": []any{map[string]any{"id": "L_NEW"}},
		"edges":  []any{},
	}
	w := doWidgetRequest(t, r, "user1", http.MethodPost,
		"/api/vertex/v1/graphs/"+sourceRid+"/widget/save", map[string]any{
			"payload":          newPayload,
			"overrideGraphRid": targetRid,
		})
	if w.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["rid"] != targetRid {
		t.Errorf("save response rid = %v, want override target %v", resp["rid"], targetRid)
	}

	// Target rid: payload now reflects the new layers.
	targetGet := doWidgetRequest(t, r, "user1", http.MethodGet,
		"/api/vertex/v1/graphs/"+targetRid, nil)
	if targetGet.Code != http.StatusOK {
		t.Fatalf("get target status = %d, want 200", targetGet.Code)
	}
	var targetResp map[string]any
	_ = json.Unmarshal(targetGet.Body.Bytes(), &targetResp)
	targetPayload := targetResp["payload"].(map[string]any)
	targetLayers := targetPayload["layers"].([]any)
	if l0, _ := targetLayers[0].(map[string]any); l0["id"] != "L_NEW" {
		t.Errorf("target rid layers not updated; got %v", targetLayers)
	}

	// Source rid: payload unchanged (still has "Airport" objectType, version 1).
	sourceGet := doWidgetRequest(t, r, "user1", http.MethodGet,
		"/api/vertex/v1/graphs/"+sourceRid, nil)
	var sourceResp map[string]any
	_ = json.Unmarshal(sourceGet.Body.Bytes(), &sourceResp)
	if v, _ := sourceResp["version"].(float64); v != 1 {
		t.Errorf("source rid version = %v, want 1 (override save must not bump source)", v)
	}
	sourcePayload := sourceResp["payload"].(map[string]any)
	sourceLayers := sourcePayload["layers"].([]any)
	if l0, _ := sourceLayers[0].(map[string]any); l0["objectType"] != "Airport" {
		t.Errorf("source rid layers were unexpectedly overwritten; got %v", sourceLayers)
	}
}

// TestWidget_Given_NoOverride_When_Save_Then_SourceGraphUpdated
func TestWidget_Given_NoOverride_When_Save_Then_SourceGraphUpdated(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	sourceRid := createWidgetGraph(t, r, "user1")

	newPayload := map[string]any{
		"layers": []any{map[string]any{"id": "L_FROM_WIDGET"}},
		"edges":  []any{},
	}
	w := doWidgetRequest(t, r, "user1", http.MethodPost,
		"/api/vertex/v1/graphs/"+sourceRid+"/widget/save", map[string]any{
			"payload": newPayload,
		})
	if w.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["rid"] != sourceRid {
		t.Errorf("save response rid = %v, want source %v (no override)", resp["rid"], sourceRid)
	}
	if v, _ := resp["version"].(float64); v != 2 {
		t.Errorf("save version = %v, want 2 (bump from 1)", v)
	}
}

// TestWidget_Given_OverrideToUnknownRID_When_Save_Then_404
func TestWidget_Given_OverrideToUnknownRID_When_Save_Then_404(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	sourceRid := createWidgetGraph(t, r, "user1")

	w := doWidgetRequest(t, r, "user1", http.MethodPost,
		"/api/vertex/v1/graphs/"+sourceRid+"/widget/save", map[string]any{
			"payload":          map[string]any{"layers": []any{}, "edges": []any{}},
			"overrideGraphRid": "ri.vertex.main.graph.00000000-0000-0000-0000-000000000000",
		})
	if w.Code != http.StatusNotFound {
		t.Errorf("override-unknown status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestWidget_Given_StrangerOnSource_When_Save_Then_403
//
// The Save action targets a graph the caller must have write access to —
// piggyback on canReadGraph (owner-or-admin) for now; full RBAC is out of
// scope for VTX-014.
func TestWidget_Given_StrangerOnSource_When_Save_Then_403(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	sourceRid := createWidgetGraph(t, r, "user1")

	w := doWidgetRequest(t, r, "user2", http.MethodPost,
		"/api/vertex/v1/graphs/"+sourceRid+"/widget/save", map[string]any{
			"payload": map[string]any{"layers": []any{}, "edges": []any{}},
		})
	if w.Code != http.StatusForbidden {
		t.Errorf("stranger save status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// TestWidget_Given_StrangerOnOverrideTarget_When_Save_Then_403
//
// Save must check ACL on the *target* graph too — otherwise an attacker
// with read on graph A could trigger an override save into graph B they
// don't own. Belt-and-braces against IDOR.
func TestWidget_Given_StrangerOnOverrideTarget_When_Save_Then_403(t *testing.T) {
	r, _ := newWidgetTestHandler(t)
	mySourceRid := createWidgetGraph(t, r, "user2")
	otherTargetRid := createWidgetGraph(t, r, "user1")

	w := doWidgetRequest(t, r, "user2", http.MethodPost,
		"/api/vertex/v1/graphs/"+mySourceRid+"/widget/save", map[string]any{
			"payload":          map[string]any{"layers": []any{}, "edges": []any{}},
			"overrideGraphRid": otherTargetRid,
		})
	if w.Code != http.StatusForbidden {
		t.Errorf("override into stranger graph status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}
