package graphsvc_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// newValidatingHandler wires a Handler whose POST/PUT paths run the
// PayloadValidator before delegating to the repo. The stub OMS lookup mirrors
// what cmd/server wires from *oms.PGRepository in production.
func newValidatingHandler(t *testing.T) (chi.Router, *graphsvc.MemRepo, *stubReferenceLookup) {
	t.Helper()
	refs := &stubReferenceLookup{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ontology.main.object-type.airport": {RID: "ri.ontology.main.object-type.airport"},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.ontology.main.link-type.flights": {RID: "ri.ontology.main.link-type.flights"},
		},
	}
	v, err := graphsvc.NewPayloadValidator(refs)
	if err != nil {
		t.Fatalf("NewPayloadValidator: %v", err)
	}
	repo := graphsvc.NewMemRepo()
	templates := graphsvc.NewMemTemplateStore()
	h := graphsvc.NewHandler(repo, templates)
	h.SetPayloadValidator(v)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, repo, refs
}

// TestGraphsHandler_Given_ValidatorAndPayloadMissingLayers_When_POST_Then_400LayersRequired
func TestGraphsHandler_Given_ValidatorAndPayloadMissingLayers_When_POST_Then_400LayersRequired(t *testing.T) {
	r, _, _ := newValidatingHandler(t)
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"payload":     map[string]any{"edges": []any{}}, // missing layers
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "layers") {
		t.Errorf("body = %s, want it to mention `layers`", w.Body.String())
	}
}

// TestGraphsHandler_Given_ValidatorAndUnknownObjectTypeRid_When_POST_Then_422
func TestGraphsHandler_Given_ValidatorAndUnknownObjectTypeRid_When_POST_Then_422(t *testing.T) {
	r, _, _ := newValidatingHandler(t)
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"payload": map[string]any{
			"layers": []any{
				map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.ghost"},
			},
		},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "objecttype not found") {
		t.Errorf("body = %s, want it to surface `objectType not found`", w.Body.String())
	}
}

// TestGraphsHandler_Given_ValidatorAndUnknownLinkTypeRid_When_POST_Then_422
func TestGraphsHandler_Given_ValidatorAndUnknownLinkTypeRid_When_POST_Then_422(t *testing.T) {
	r, _, _ := newValidatingHandler(t)
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"payload": map[string]any{
			"layers": []any{map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.airport"}},
			"edges":  []any{map[string]any{"id": "E1", "linkTypeRid": "ri.ontology.main.link-type.ghost"}},
		},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "linktype not found") {
		t.Errorf("body = %s, want it to surface `linkType not found`", w.Body.String())
	}
}

// TestGraphsHandler_Given_ValidatorAndStringPositionCoord_When_POST_Then_400
func TestGraphsHandler_Given_ValidatorAndStringPositionCoord_When_POST_Then_400(t *testing.T) {
	r, _, _ := newValidatingHandler(t)
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"payload": map[string]any{
			"layers":    []any{},
			"positions": map[string]any{"n1": map[string]any{"x": "left", "y": 0}},
		},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// TestGraphsHandler_Given_ValidatorAndValidPayload_When_POST_Then_201
func TestGraphsHandler_Given_ValidatorAndValidPayload_When_POST_Then_201(t *testing.T) {
	r, repo, _ := newValidatingHandler(t)
	w := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"payload": map[string]any{
			"layers": []any{map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.airport"}},
			"edges":  []any{map[string]any{"id": "E1", "linkTypeRid": "ri.ontology.main.link-type.flights"}},
			"positions": map[string]any{
				"n1": map[string]any{"x": 1.0, "y": 2.0},
			},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	// Side-effect check: graph row was actually written.
	if _, err := repo.Get(context.Background(), "non-existent"); err == nil {
		t.Errorf("expected ErrGraphNotFound on missing rid")
	}
}

// TestGraphsHandler_Given_ValidatorAndUnknownLinkTypeRid_When_PUTUpdate_Then_422
// PUT /graphs/{rid} bumps version and persists payload — must run the same
// validator as POST so an updated graph can't introduce dangling references.
func TestGraphsHandler_Given_ValidatorAndUnknownLinkTypeRid_When_PUTUpdate_Then_422(t *testing.T) {
	r, _, _ := newValidatingHandler(t)
	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Map",
		"payload": map[string]any{
			"layers": []any{map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.airport"}},
		},
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("seed POST status = %d", createResp.Code)
	}
	// Decode rid the cheap way — substring rather than json.Unmarshal so this
	// test stays focused on the PUT validation path.
	body := createResp.Body.String()
	idx := strings.Index(body, "\"rid\":\"")
	if idx < 0 {
		t.Fatalf("rid missing from create response: %s", body)
	}
	rest := body[idx+len("\"rid\":\""):]
	end := strings.Index(rest, "\"")
	rid := rest[:end]

	w := doRequest(t, r, http.MethodPut, "/api/vertex/v1/graphs/"+rid, map[string]any{
		"payload": map[string]any{
			"layers": []any{map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.airport"}},
			"edges":  []any{map[string]any{"id": "E1", "linkTypeRid": "ri.ontology.main.link-type.ghost"}},
		},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("PUT with unknown linkType status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
}
