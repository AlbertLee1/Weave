package graphsvc_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// newTestHandler wires a Handler with in-memory graph + template stores.
// Returns the chi router (ready for httptest), the GraphRepo, and the
// TemplateStore so tests can assert side effects directly.
func newTestHandler(t *testing.T) (chi.Router, *graphsvc.MemRepo, *graphsvc.MemTemplateStore) {
	t.Helper()
	repo := graphsvc.NewMemRepo()
	templates := graphsvc.NewMemTemplateStore()
	h := graphsvc.NewHandler(repo, templates)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, repo, templates
}

func doRequest(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestGraphsHandler_Given_EmptyService_When_POSTValidPayload_Then_201WithRID
func TestGraphsHandler_Given_EmptyService_When_POSTValidPayload_Then_201WithRID(t *testing.T) {
	r, _, _ := newTestHandler(t)

	body := map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "JFK Map",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	}
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	rid, _ := resp["rid"].(string)
	if !strings.HasPrefix(rid, "ri.vertex.main.graph.") {
		t.Errorf("rid = %q, want ri.vertex.main.graph.* prefix", rid)
	}
	if v, _ := resp["version"].(float64); v != 1 {
		t.Errorf("version = %v, want 1", resp["version"])
	}
}

// TestGraphsHandler_Given_Graph_When_GET_Then_FullPayloadReturned
func TestGraphsHandler_Given_Graph_When_GET_Then_FullPayloadReturned(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{map[string]any{"id": "L1"}}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRequest(t, r, http.MethodGet, "/api/vertex/v1/graphs/"+rid, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["rid"] != rid {
		t.Errorf("rid = %v, want %v", got["rid"], rid)
	}
	payload, ok := got["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing or not object: %v", got["payload"])
	}
	if layers, _ := payload["layers"].([]any); len(layers) != 1 {
		t.Errorf("expected 1 layer in payload, got %v", payload["layers"])
	}
}

// TestGraphsHandler_Given_Graph_When_PUT_Then_VersionBumps
func TestGraphsHandler_Given_Graph_When_PUT_Then_VersionBumps(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRequest(t, r, http.MethodPut, "/api/vertex/v1/graphs/"+rid, map[string]any{
		"payload": map[string]any{"layers": []any{map[string]any{"id": "L2"}}, "edges": []any{}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if v, _ := resp["version"].(float64); v != 2 {
		t.Errorf("version = %v after PUT, want 2", resp["version"])
	}
}

// TestGraphsHandler_Given_Graph_When_PATCHLayout_Then_PositionsUpdatedNoVersionBump
func TestGraphsHandler_Given_Graph_When_PATCHLayout_Then_PositionsUpdatedNoVersionBump(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRequest(t, r, http.MethodPatch, "/api/vertex/v1/graphs/"+rid+"/layout", map[string]any{
		"positions": map[string]any{"n1": map[string]any{"x": 1.0, "y": 2.0}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH layout status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	getResp := doRequest(t, r, http.MethodGet, "/api/vertex/v1/graphs/"+rid, nil)
	var got map[string]any
	_ = json.Unmarshal(getResp.Body.Bytes(), &got)
	if v, _ := got["version"].(float64); v != 1 {
		t.Errorf("version = %v after PATCH layout, want 1 (no bump)", got["version"])
	}
	payload := got["payload"].(map[string]any)
	if _, ok := payload["positions"]; !ok {
		t.Errorf("payload missing positions field after PATCH: %v", payload)
	}
}

// TestGraphsHandler_Given_Graph_When_POSTDuplicate_Then_NewRIDReturned
func TestGraphsHandler_Given_Graph_When_POSTDuplicate_Then_NewRIDReturned(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Orig",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs/"+rid+"/duplicate", nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var dup map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &dup)
	newRid, _ := dup["rid"].(string)
	if newRid == rid {
		t.Errorf("duplicate returned same rid %q", newRid)
	}
	if !strings.HasPrefix(newRid, "ri.vertex.main.graph.") {
		t.Errorf("duplicate rid = %q, want ri.vertex.main.graph.* prefix", newRid)
	}
}

// TestGraphsHandler_Given_Graph_When_POSTSaveAsTemplate_Then_TemplateWritten
func TestGraphsHandler_Given_Graph_When_POSTSaveAsTemplate_Then_TemplateWritten(t *testing.T) {
	r, _, templates := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs/"+rid+"/save-as-template", map[string]any{
		"name":                "Hub & Spoke",
		"parameterizedFields": []string{"layers[0].filter.objectRid"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("save-as-template status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	templateRID, _ := resp["rid"].(string)
	if !strings.HasPrefix(templateRID, "ri.vertex.main.graph-template.") {
		t.Errorf("template rid = %q, want ri.vertex.main.graph-template.* prefix", templateRID)
	}
	if templates.Count() != 1 {
		t.Errorf("template store count = %d, want 1", templates.Count())
	}
}

// TestGraphsHandler_Given_GraphWith3Versions_When_GETHistory_Then_VersionsListed
func TestGraphsHandler_Given_GraphWith3Versions_When_GETHistory_Then_VersionsListed(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)
	for i := 0; i < 2; i++ {
		doRequest(t, r, http.MethodPut, "/api/vertex/v1/graphs/"+rid, map[string]any{
			"payload": map[string]any{"layers": []any{}, "edges": []any{}, "i": i},
		})
	}

	w := doRequest(t, r, http.MethodGet, "/api/vertex/v1/graphs/"+rid+"/history", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("history status = %d, want 200", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	versions, _ := resp["versions"].([]any)
	if len(versions) != 3 {
		t.Errorf("history has %d versions, want 3", len(versions))
	}
}

// TestGraphsHandler_Given_GraphWith3Versions_When_GETVersionN_Then_SnapshotReturned
func TestGraphsHandler_Given_GraphWith3Versions_When_GETVersionN_Then_SnapshotReturned(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{map[string]any{"id": "L1"}}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)
	doRequest(t, r, http.MethodPut, "/api/vertex/v1/graphs/"+rid, map[string]any{
		"payload": map[string]any{"layers": []any{map[string]any{"id": "L2"}}, "edges": []any{}},
	})

	w := doRequest(t, r, http.MethodGet, "/api/vertex/v1/graphs/"+rid+"/versions/1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get version 1 status = %d, want 200", w.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if v, _ := got["version"].(float64); v != 1 {
		t.Errorf("version = %v, want 1", got["version"])
	}
	payload := got["payload"].(map[string]any)
	layers := payload["layers"].([]any)
	if l0, _ := layers[0].(map[string]any); l0["id"] != "L1" {
		t.Errorf("v1 payload layer = %v, want L1", layers[0])
	}

	// v99 → 404
	w404 := doRequest(t, r, http.MethodGet, "/api/vertex/v1/graphs/"+rid+"/versions/99", nil)
	if w404.Code != http.StatusNotFound {
		t.Errorf("version 99 status = %d, want 404", w404.Code)
	}
}

// TestGraphsHandler_Given_UnknownRID_When_GET_Then_404
func TestGraphsHandler_Given_UnknownRID_When_GET_Then_404(t *testing.T) {
	r, _, _ := newTestHandler(t)
	w := doRequest(t, r, http.MethodGet,
		"/api/vertex/v1/graphs/ri.vertex.main.graph.00000000-0000-0000-0000-000000000000", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestGraphsHandler_Given_MissingFields_When_POST_Then_400
func TestGraphsHandler_Given_MissingFields_When_POST_Then_400(t *testing.T) {
	r, _, _ := newTestHandler(t)
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		// missing ontologyRid
		"name": "x",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// VTX-058: layer.extendedLabels[] discriminator wiring on POST + PUT.

// TestGraphsHandler_Given_AllKnownLabelKinds_When_POST_Then_201
func TestGraphsHandler_Given_AllKnownLabelKinds_When_POST_Then_201(t *testing.T) {
	r, _, _ := newTestHandler(t)
	body := map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Labels Map",
		"payload": map[string]any{
			"layers": []any{
				map[string]any{
					"objectType":  "Airport",
					"ontologyRid": "ri.ontology.main.ontology.vtx",
					"extendedLabels": []any{
						map[string]any{"kind": "property", "property": "onTimePct"},
						map[string]any{"kind": "timeSeries", "property": "throughput"},
						map[string]any{"kind": "measure", "measureRid": "ri.functions.measure.total-alerts"},
					},
				},
			},
			"edges": []any{},
		},
	}
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

// TestGraphsHandler_Given_UnknownLabelKind_When_POST_Then_422
func TestGraphsHandler_Given_UnknownLabelKind_When_POST_Then_422(t *testing.T) {
	r, _, _ := newTestHandler(t)
	body := map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Bad Map",
		"payload": map[string]any{
			"layers": []any{
				map[string]any{
					"objectType":     "Airport",
					"extendedLabels": []any{map[string]any{"kind": "histogram"}},
				},
			},
			"edges": []any{},
		},
	}
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", body)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if code, _ := resp["errorCode"].(string); code != "WEAVE_VALIDATION_SCHEMA" {
		t.Errorf("errorCode = %q, want WEAVE_VALIDATION_SCHEMA; body: %s", code, w.Body.String())
	}
}

// TestGraphsHandler_Given_Graph_When_PUTUnknownLabelKind_Then_422
func TestGraphsHandler_Given_Graph_When_PUTUnknownLabelKind_Then_422(t *testing.T) {
	r, _, _ := newTestHandler(t)
	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRequest(t, r, http.MethodPut, "/api/vertex/v1/graphs/"+rid, map[string]any{
		"payload": map[string]any{
			"layers": []any{
				map[string]any{
					"objectType":     "Airport",
					"extendedLabels": []any{map[string]any{"kind": "badge"}},
				},
			},
			"edges": []any{},
		},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if code, _ := resp["errorCode"].(string); code != "WEAVE_VALIDATION_SCHEMA" {
		t.Errorf("errorCode = %q, want WEAVE_VALIDATION_SCHEMA", code)
	}
}
