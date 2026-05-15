package graphsvc_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// TestInstantiate_Given_OnePath_When_LeafParamProvided_Then_PayloadSubstituted
func TestInstantiate_Given_OnePath_When_LeafParamProvided_Then_PayloadSubstituted(t *testing.T) {
	payload := json.RawMessage(`{"layers":[{"id":"L1","filter":{"objectRid":""}}]}`)
	out, err := graphsvc.Instantiate(payload,
		[]string{"layers[0].filter.objectRid"},
		map[string]json.RawMessage{
			"objectRid": json.RawMessage(`"ri.ontology.main.object.airport.JFK"`),
		})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	layers, ok := got["layers"].([]any)
	if !ok || len(layers) != 1 {
		t.Fatalf("layers shape wrong: %v", got["layers"])
	}
	layer0 := layers[0].(map[string]any)
	filter := layer0["filter"].(map[string]any)
	if filter["objectRid"] != "ri.ontology.main.object.airport.JFK" {
		t.Errorf("leaf objectRid = %v, want substituted RID", filter["objectRid"])
	}
}

// TestInstantiate_Given_MissingLeafParam_When_Instantiate_Then_FieldLeftUntouched
func TestInstantiate_Given_MissingLeafParam_When_Instantiate_Then_FieldLeftUntouched(t *testing.T) {
	payload := json.RawMessage(`{"layers":[{"filter":{"objectRid":"PLACEHOLDER"}}]}`)
	out, err := graphsvc.Instantiate(payload,
		[]string{"layers[0].filter.objectRid"},
		map[string]json.RawMessage{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if !strings.Contains(string(out), `"PLACEHOLDER"`) {
		t.Errorf("expected placeholder preserved when param missing, got %s", out)
	}
}

// TestInstantiate_Given_NumericValue_When_LeafParamProvided_Then_RawJSONSubstituted
// raw json.RawMessage support: numeric, bool, object, array all substitute verbatim
func TestInstantiate_Given_NumericValue_When_LeafParamProvided_Then_RawJSONSubstituted(t *testing.T) {
	payload := json.RawMessage(`{"layers":[{"limit":0}]}`)
	out, err := graphsvc.Instantiate(payload,
		[]string{"layers[0].limit"},
		map[string]json.RawMessage{"limit": json.RawMessage(`100`)})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	layer0 := got["layers"].([]any)[0].(map[string]any)
	if v, _ := layer0["limit"].(float64); v != 100 {
		t.Errorf("limit = %v, want 100", layer0["limit"])
	}
}

// TestInstantiate_Given_MalformedPath_When_Instantiate_Then_ReturnsError
func TestInstantiate_Given_MalformedPath_When_Instantiate_Then_ReturnsError(t *testing.T) {
	_, err := graphsvc.Instantiate(json.RawMessage(`{}`),
		[]string{"layers[].x"},
		map[string]json.RawMessage{"x": json.RawMessage(`1`)})
	if err == nil {
		t.Errorf("expected error for malformed path layers[].x")
	}
}

// TestInstantiate_Given_PathPointsToMissingNode_When_Instantiate_Then_ReturnsError
func TestInstantiate_Given_PathPointsToMissingNode_When_Instantiate_Then_ReturnsError(t *testing.T) {
	_, err := graphsvc.Instantiate(json.RawMessage(`{"layers":[]}`),
		[]string{"layers[3].filter.objectRid"},
		map[string]json.RawMessage{"objectRid": json.RawMessage(`"x"`)})
	if err == nil {
		t.Errorf("expected error for out-of-bound path layers[3]")
	}
}

// TestInstantiate_Given_SearchAroundFn_When_ObjectRidsProvided_Then_PlanGenerated
// VTX-012 BDD: "Given 模板含 search around 函数 RID When instantiate 时传入新对象
// Then 自动调用 search around 扩展图" — backend produces a plan of calls keyed by
// each new object RID; the actual fetch lives client-side (mirrors the existing
// web/src/features/vertex/templates/parameterizedSearchAround.ts shape).
func TestInstantiate_Given_SearchAroundFn_When_ObjectRidsProvided_Then_PlanGenerated(t *testing.T) {
	payload := json.RawMessage(`{"layers":[],"searchAroundFnRid":"ri.functions.main.function.fn1"}`)
	out, err := graphsvc.Instantiate(payload,
		nil,
		map[string]json.RawMessage{
			"objectRids": json.RawMessage(`["ri.ontology.main.object.airport.JFK","ri.ontology.main.object.airport.LAX"]`),
		})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	calls, ok := got["searchAroundCalls"].([]any)
	if !ok {
		t.Fatalf("missing searchAroundCalls in output: %s", out)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 search-around calls, got %d", len(calls))
	}
	first := calls[0].(map[string]any)
	if first["functionRid"] != "ri.functions.main.function.fn1" {
		t.Errorf("functionRid = %v, want fn1", first["functionRid"])
	}
	if first["objectRid"] != "ri.ontology.main.object.airport.JFK" {
		t.Errorf("objectRid = %v, want JFK", first["objectRid"])
	}
}

// TestInstantiate_Given_NoSearchAroundFn_When_ObjectRidsProvided_Then_NoPlanAdded
func TestInstantiate_Given_NoSearchAroundFn_When_ObjectRidsProvided_Then_NoPlanAdded(t *testing.T) {
	payload := json.RawMessage(`{"layers":[]}`)
	out, err := graphsvc.Instantiate(payload, nil,
		map[string]json.RawMessage{"objectRids": json.RawMessage(`["ri.ontology.main.object.airport.JFK"]`)})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if strings.Contains(string(out), "searchAroundCalls") {
		t.Errorf("did not expect searchAroundCalls (no fn rid), got %s", out)
	}
}
