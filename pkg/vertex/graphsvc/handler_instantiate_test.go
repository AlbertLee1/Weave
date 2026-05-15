package graphsvc_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestTemplatesHandler_Given_TemplateWithLeafParam_When_POSTInstantiate_Then_PayloadSubstituted
// Covers VTX-012 BDD acceptance #2: "Given 模板 + 参数 {objectRid:'ri...JFK'}
// When POST /templates/{rid}/instantiate Then 返回 instantiated graph payload
// （参数替换完成）".
func TestTemplatesHandler_Given_TemplateWithLeafParam_When_POSTInstantiate_Then_PayloadSubstituted(t *testing.T) {
	r, _, _ := newTestHandler(t)

	// 1. create graph carrying a parameterizable field
	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Hub Template Source",
		"versioned":   true,
		"payload": map[string]any{
			"layers": []any{
				map[string]any{
					"id":     "L1",
					"filter": map[string]any{"objectRid": ""},
				},
			},
			"edges": []any{},
		},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	graphRid := created["rid"].(string)

	// 2. save as template with parameterizedFields
	tplResp := doRequest(t, r, http.MethodPost,
		"/api/vertex/v1/graphs/"+graphRid+"/save-as-template", map[string]any{
			"name":                "Hub & Spoke",
			"parameterizedFields": []string{"layers[0].filter.objectRid"},
		})
	if tplResp.Code != http.StatusCreated {
		t.Fatalf("save-as-template status = %d, want 201", tplResp.Code)
	}
	var tpl map[string]any
	_ = json.Unmarshal(tplResp.Body.Bytes(), &tpl)
	templateRID := tpl["rid"].(string)

	// 3. instantiate with parameter
	w := doRequest(t, r, http.MethodPost,
		"/api/vertex/v1/templates/"+templateRID+"/instantiate", map[string]any{
			"parameters": map[string]any{
				"objectRid": "ri.ontology.main.object.airport.JFK",
			},
		})
	if w.Code != http.StatusOK {
		t.Fatalf("instantiate status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	payload, ok := resp["payload"].(map[string]any)
	if !ok {
		t.Fatalf("response missing payload: %s", w.Body.String())
	}
	layers := payload["layers"].([]any)
	layer0 := layers[0].(map[string]any)
	filter := layer0["filter"].(map[string]any)
	if filter["objectRid"] != "ri.ontology.main.object.airport.JFK" {
		t.Errorf("instantiated filter.objectRid = %v, want JFK", filter["objectRid"])
	}
	if resp["sourceTemplateRid"] != templateRID {
		t.Errorf("response sourceTemplateRid = %v, want %q", resp["sourceTemplateRid"], templateRID)
	}
}

// TestTemplatesHandler_Given_UnknownTemplateRID_When_POSTInstantiate_Then_404
func TestTemplatesHandler_Given_UnknownTemplateRID_When_POSTInstantiate_Then_404(t *testing.T) {
	r, _, _ := newTestHandler(t)
	w := doRequest(t, r, http.MethodPost,
		"/api/vertex/v1/templates/ri.vertex.main.graph-template.00000000-0000-0000-0000-000000000000/instantiate",
		map[string]any{"parameters": map[string]any{}})
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestTemplatesHandler_Given_TemplateWithSearchAroundFn_When_POSTInstantiateWithObjectRids_Then_PlanReturned
// Covers VTX-012 BDD acceptance #3.
func TestTemplatesHandler_Given_TemplateWithSearchAroundFn_When_POSTInstantiateWithObjectRids_Then_PlanReturned(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "SA template",
		"versioned":   true,
		"payload": map[string]any{
			"layers":            []any{},
			"edges":             []any{},
			"searchAroundFnRid": "ri.functions.main.function.fn1",
		},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	graphRid := created["rid"].(string)

	tplResp := doRequest(t, r, http.MethodPost,
		"/api/vertex/v1/graphs/"+graphRid+"/save-as-template", map[string]any{
			"name":                "SearchAround template",
			"parameterizedFields": []string{},
		})
	var tpl map[string]any
	_ = json.Unmarshal(tplResp.Body.Bytes(), &tpl)
	templateRID := tpl["rid"].(string)

	w := doRequest(t, r, http.MethodPost,
		"/api/vertex/v1/templates/"+templateRID+"/instantiate", map[string]any{
			"parameters": map[string]any{
				"objectRids": []string{
					"ri.ontology.main.object.airport.JFK",
					"ri.ontology.main.object.airport.LAX",
				},
			},
		})
	if w.Code != http.StatusOK {
		t.Fatalf("instantiate status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	payload := resp["payload"].(map[string]any)
	calls, ok := payload["searchAroundCalls"].([]any)
	if !ok {
		t.Fatalf("payload missing searchAroundCalls: %s", w.Body.String())
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 search-around calls, got %d (body: %s)", len(calls), w.Body.String())
	}
	first := calls[0].(map[string]any)
	if first["functionRid"] != "ri.functions.main.function.fn1" {
		t.Errorf("functionRid = %v, want fn1", first["functionRid"])
	}
}

// TestTemplatesHandler_Given_MalformedParameterPath_When_POSTInstantiate_Then_400
func TestTemplatesHandler_Given_MalformedParameterPath_When_POSTInstantiate_Then_400(t *testing.T) {
	r, _, _ := newTestHandler(t)

	createResp := doRequest(t, r, http.MethodPost, "/api/vertex/v1/graphs", map[string]any{
		"ontologyRid": "ri.ontology.main.ontology.vtx",
		"name":        "Bad path template source",
		"versioned":   true,
		"payload":     map[string]any{"layers": []any{}, "edges": []any{}},
	})
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	graphRid := created["rid"].(string)

	tplResp := doRequest(t, r, http.MethodPost,
		"/api/vertex/v1/graphs/"+graphRid+"/save-as-template", map[string]any{
			"name":                "Bad",
			"parameterizedFields": []string{"layers[].x"},
		})
	var tpl map[string]any
	_ = json.Unmarshal(tplResp.Body.Bytes(), &tpl)
	templateRID := tpl["rid"].(string)

	w := doRequest(t, r, http.MethodPost,
		"/api/vertex/v1/templates/"+templateRID+"/instantiate", map[string]any{
			"parameters": map[string]any{"x": 1},
		})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "InvalidTemplateField") &&
		!strings.Contains(w.Body.String(), "field") {
		t.Errorf("expected error to mention field path, got: %s", w.Body.String())
	}
}
