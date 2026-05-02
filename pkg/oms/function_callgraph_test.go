package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// Direct unit tests for the static call-graph scanner. The scanner is
// regex-based on purpose — JS parsers are heavyweight relative to the
// `weave.callFunction("ref")` shape we need to recognise — so the suite
// captures the literal-shape, comment-skip, and dynamic-fallback contracts
// as table-driven cases.
func TestExtractCallTargets_Variants(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "empty source returns nil",
			source: "",
			want:   nil,
		},
		{
			name:   "double quoted",
			source: `function main(input) { return weave.callFunction("helper", input); }`,
			want:   []string{"helper"},
		},
		{
			name:   "single quoted",
			source: `function main(input) { return weave.callFunction('helper', input); }`,
			want:   []string{"helper"},
		},
		{
			name:   "name@version",
			source: `function main(input) { return weave.callFunction("scoreOrder@2.1.0", input); }`,
			want:   []string{"scoreOrder@2.1.0"},
		},
		{
			name:   "rid form",
			source: `function main(input) { return weave.callFunction("ri.main.main.function.abc", {}); }`,
			want:   []string{"ri.main.main.function.abc"},
		},
		{
			name:   "two distinct callees deduped",
			source: `function main(input) { var a = weave.callFunction("alpha", input); var b = weave.callFunction("beta", input); var c = weave.callFunction("alpha", input); return [a, b, c]; }`,
			want:   []string{"alpha", "beta"},
		},
		{
			name: "line comment masking ignored",
			source: `function main(input) {
				// weave.callFunction("ghost") — should NOT count
				return weave.callFunction("real", input);
			}`,
			want: []string{"real"},
		},
		{
			name: "block comment masking ignored",
			source: `function main(input) {
				/* weave.callFunction("ghost") inside a block comment */
				return weave.callFunction("real", input);
			}`,
			want: []string{"real"},
		},
		{
			name:   "string literal containing callFunction text ignored",
			source: `function main(input) { var note = "weave.callFunction(\"ghost\")"; return weave.callFunction("real", {note: note}); }`,
			want:   []string{"real"},
		},
		{
			name:   "dynamic ref invisible to scanner",
			source: `function main(input) { var ref = input.target; return weave.callFunction(ref, input); }`,
			want:   nil,
		},
		{
			name:   "template literal ref invisible to scanner",
			source: "function main(input) { return weave.callFunction(`helper-${input.suffix}`, input); }",
			want:   nil,
		},
		{
			name:   "bare callFunction(...) call (e.g. when shimmed locally)",
			source: `function main(input) { return callFunction("helper", input); }`,
			want:   []string{"helper"},
		},
		{
			name: "whitespace + multiline arguments",
			source: `function main(input) {
				return weave.callFunction(
					"helper",
					input
				);
			}`,
			want: []string{"helper"},
		},
		{
			name:   "empty string ref is ignored",
			source: `function main(input) { return weave.callFunction("", input); }`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := oms.ExtractCallTargets(tc.source)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("index %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// callgraphLookup is a minimal in-memory FunctionCallGraphLookup so the
// detection algorithm can be exercised without booting the OMS handler stack.
// Entries are stored by both their RID and `name@version` so the scanner sees
// the same row through every reference shape.
type callgraphLookup struct {
	byRID  map[string]*oms.Function
	byName map[string]map[string]*oms.Function // name → version → fn
}

func newCallgraphLookup(fns ...*oms.Function) *callgraphLookup {
	out := &callgraphLookup{
		byRID:  map[string]*oms.Function{},
		byName: map[string]map[string]*oms.Function{},
	}
	for _, fn := range fns {
		out.put(fn)
	}
	return out
}

func (l *callgraphLookup) put(fn *oms.Function) {
	if fn.RID != "" {
		l.byRID[fn.RID] = fn
	}
	if fn.Name == "" {
		return
	}
	if l.byName[fn.Name] == nil {
		l.byName[fn.Name] = map[string]*oms.Function{}
	}
	l.byName[fn.Name][fn.NormalisedVersion()] = fn
}

func (l *callgraphLookup) GetFunction(_ context.Context, rid string) (*oms.Function, error) {
	if fn, ok := l.byRID[rid]; ok {
		return fn, nil
	}
	return nil, oms.ErrNotFound
}

func (l *callgraphLookup) GetFunctionByName(_ context.Context, _, name string) (*oms.Function, error) {
	versions := l.byName[name]
	if len(versions) == 0 {
		return nil, oms.ErrNotFound
	}
	// Return any deterministic version — the scanner only cares about the
	// callee's source, and tests register one row per name@version.
	for _, fn := range versions {
		return fn, nil
	}
	return nil, oms.ErrNotFound
}

func (l *callgraphLookup) GetFunctionByNameVersion(_ context.Context, _, name, version string) (*oms.Function, error) {
	versions := l.byName[name]
	if len(versions) == 0 {
		return nil, oms.ErrNotFound
	}
	if fn, ok := versions[version]; ok {
		return fn, nil
	}
	return nil, oms.ErrNotFound
}

func makeFunc(name, source string) *oms.Function {
	return &oms.Function{
		RID:         "ri.main.main.function." + name,
		OntologyRID: "ri.main.main.ontology.demo",
		Name:        name,
		Version:     "1.0.0",
		SourceCode:  source,
	}
}

func TestDetectCallCycle_DirectSelfRecursion(t *testing.T) {
	a := makeFunc("self", `function main(input) { return weave.callFunction("self", input); }`)
	lookup := newCallgraphLookup(a)

	err := oms.DetectCallCycle(context.Background(), lookup, a.OntologyRID, a)
	if err == nil {
		t.Fatal("expected cycle error for direct self-recursion")
	}
	var cyc *oms.FunctionCallCycleError
	if !errors.As(err, &cyc) {
		t.Fatalf("expected *FunctionCallCycleError, got %T (%v)", err, err)
	}
	if len(cyc.Cycle) < 2 {
		t.Fatalf("expected cycle path with at least 2 nodes, got %v", cyc.Cycle)
	}
	if cyc.Cycle[0] != cyc.Cycle[len(cyc.Cycle)-1] {
		t.Fatalf("expected cycle to close on first node, got %v", cyc.Cycle)
	}
}

func TestDetectCallCycle_IndirectCycleAToBToA(t *testing.T) {
	a := makeFunc("A", `function main(input) { return weave.callFunction("B", input); }`)
	b := makeFunc("B", `function main(input) { return weave.callFunction("A", input); }`)
	lookup := newCallgraphLookup(a, b)

	err := oms.DetectCallCycle(context.Background(), lookup, a.OntologyRID, a)
	if err == nil {
		t.Fatal("expected cycle error for A→B→A")
	}
	var cyc *oms.FunctionCallCycleError
	if !errors.As(err, &cyc) {
		t.Fatalf("expected *FunctionCallCycleError, got %T", err)
	}
	if len(cyc.Cycle) != 3 {
		t.Fatalf("expected 3-node cycle path, got %v", cyc.Cycle)
	}
	if cyc.Cycle[0] != cyc.Cycle[2] {
		t.Fatalf("expected cycle to close on node 0, got %v", cyc.Cycle)
	}
}

func TestDetectCallCycle_LongerCycleAToBToCToA(t *testing.T) {
	a := makeFunc("A", `function main(input) { return weave.callFunction("B", input); }`)
	b := makeFunc("B", `function main(input) { return weave.callFunction("C", input); }`)
	c := makeFunc("C", `function main(input) { return weave.callFunction("A", input); }`)
	lookup := newCallgraphLookup(a, b, c)

	err := oms.DetectCallCycle(context.Background(), lookup, a.OntologyRID, a)
	if err == nil {
		t.Fatal("expected cycle error for A→B→C→A")
	}
	var cyc *oms.FunctionCallCycleError
	if !errors.As(err, &cyc) {
		t.Fatalf("expected *FunctionCallCycleError, got %T", err)
	}
	if len(cyc.Cycle) != 4 {
		t.Fatalf("expected 4-node cycle path, got %v", cyc.Cycle)
	}
}

// PRD requirement: 非环嵌套 3 层 must be accepted.
func TestDetectCallCycle_AcyclicThreeLevelNesting(t *testing.T) {
	a := makeFunc("A", `function main(input) { return weave.callFunction("B", input); }`)
	b := makeFunc("B", `function main(input) { return weave.callFunction("C", input); }`)
	c := makeFunc("C", `function main(input) { return input.value * 2; }`)
	lookup := newCallgraphLookup(a, b, c)

	if err := oms.DetectCallCycle(context.Background(), lookup, a.OntologyRID, a); err != nil {
		t.Fatalf("expected no cycle for acyclic 3-level chain, got %v", err)
	}
}

func TestDetectCallCycle_DiamondGraphIsAcyclic(t *testing.T) {
	a := makeFunc("A", `function main(input) {
		var l = weave.callFunction("L", input);
		var r = weave.callFunction("R", input);
		return l + r;
	}`)
	l := makeFunc("L", `function main(input) { return weave.callFunction("Leaf", input); }`)
	r := makeFunc("R", `function main(input) { return weave.callFunction("Leaf", input); }`)
	leaf := makeFunc("Leaf", `function main(input) { return input.value; }`)
	lookup := newCallgraphLookup(a, l, r, leaf)

	if err := oms.DetectCallCycle(context.Background(), lookup, a.OntologyRID, a); err != nil {
		t.Fatalf("expected no cycle for diamond graph (Leaf reachable via L and R), got %v", err)
	}
}

func TestDetectCallCycle_UnknownCalleeIsTolerated(t *testing.T) {
	// A references "ghost" which does not exist in the lookup. The runtime
	// will surface this as "function not found" at execute time; the static
	// scan must not block the publish.
	a := makeFunc("A", `function main(input) { return weave.callFunction("ghost", input); }`)
	lookup := newCallgraphLookup(a)

	if err := oms.DetectCallCycle(context.Background(), lookup, a.OntologyRID, a); err != nil {
		t.Fatalf("expected no cycle when callee is unknown, got %v", err)
	}
}

func TestDetectCallCycle_DynamicCalleeFallsThrough(t *testing.T) {
	// `weave.callFunction(ref, ...)` with a variable first argument is
	// invisible to the static scanner. Even if the runtime would loop on it,
	// the publish path must not reject — runtime guards (depth + visited
	// stack) catch the dynamic case at execute time.
	a := makeFunc("self", `function main(input) {
		var ref = "self";
		return weave.callFunction(ref, input);
	}`)
	lookup := newCallgraphLookup(a)

	if err := oms.DetectCallCycle(context.Background(), lookup, a.OntologyRID, a); err != nil {
		t.Fatalf("expected no cycle for dynamic ref, got %v", err)
	}
}

// HTTP-level integration: a publish that introduces a cycle returns 422 with
// the WEAVE_FUNCTION_CALL_CYCLE wire-format code so SDK consumers can branch.
func TestCreateFunction_RejectsCallCycleWithWeaveCode(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{
			{
				RID:         "ri.ontology.main.function.b",
				OntologyRID: "ri.ontology.main.ontology.o1",
				Name:        "B",
				Version:     "1.0.0",
				SourceCode:  `function main(input) { return weave.callFunction("A", input); }`,
			},
		},
	}
	router := setupFunctionRouter(repo)

	body := `{"name":"A","sourceCode":"function main(input) { return weave.callFunction(\"B\", input); }"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.ErrorCode != "WEAVE_FUNCTION_CALL_CYCLE" {
		t.Fatalf("expected errorCode=WEAVE_FUNCTION_CALL_CYCLE, got %q", envelope.ErrorCode)
	}
	if envelope.Parameters["name"] != "A" {
		t.Fatalf("expected parameters.name=A, got %q", envelope.Parameters["name"])
	}
	if !strings.Contains(envelope.Parameters["cycle"], "A") || !strings.Contains(envelope.Parameters["cycle"], "B") {
		t.Fatalf("expected cycle path to include A and B, got %q", envelope.Parameters["cycle"])
	}
}

// HTTP-level integration: an Update that introduces a cycle (B already exists
// and references A; we now PUT a new body for A that calls B) is rejected at
// publish time, NOT at runtime.
func TestUpdateFunction_RejectsCallCycle(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{
			{
				RID:         "ri.ontology.main.function.a",
				OntologyRID: "ri.ontology.main.ontology.o1",
				Name:        "A",
				Version:     "1.0.0",
				SourceCode:  `function main(input) { return input.value; }`,
			},
			{
				RID:         "ri.ontology.main.function.b",
				OntologyRID: "ri.ontology.main.ontology.o1",
				Name:        "B",
				Version:     "1.0.0",
				SourceCode:  `function main(input) { return weave.callFunction("A", input); }`,
			},
		},
	}
	router := setupFunctionRouter(repo)

	body := `{"sourceCode":"function main(input) { return weave.callFunction(\"B\", input); }"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.a", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		ErrorCode string `json:"errorCode"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.ErrorCode != "WEAVE_FUNCTION_CALL_CYCLE" {
		t.Fatalf("expected errorCode=WEAVE_FUNCTION_CALL_CYCLE, got %q", envelope.ErrorCode)
	}
}

