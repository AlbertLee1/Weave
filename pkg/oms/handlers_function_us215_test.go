package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-215: handler round-trips signature + runtime, defaults runtime to goja
// when omitted, and rejects unknown runtimes / malformed signatures.

func TestCreateFunction_AcceptsSignatureAndRuntime(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	body := `{
        "name": "addNumbers",
        "sourceCode": "function add(a, b) { return a + b; }",
        "runtime": "goja",
        "signature": {
            "params": [
                {"name":"a","type":"integer","required":true},
                {"name":"b","type":"integer","required":true}
            ],
            "returns": {"type":"integer"}
        }
    }`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.Runtime != "goja" {
		t.Errorf("expected runtime=goja, got %q", fn.Runtime)
	}
	if len(fn.Signature) == 0 {
		t.Fatal("expected signature to round-trip")
	}
	var sig struct {
		Params []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"params"`
	}
	if err := json.Unmarshal(fn.Signature, &sig); err != nil {
		t.Fatalf("unmarshal signature: %v", err)
	}
	if len(sig.Params) != 2 || sig.Params[0].Name != "a" {
		t.Errorf("unexpected signature shape: %+v", sig)
	}
}

func TestCreateFunction_DefaultsRuntimeToGoja(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	body := `{"name":"f","sourceCode":"return 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.Runtime != "goja" {
		t.Errorf("expected runtime=goja by default, got %q", fn.Runtime)
	}
}

func TestCreateFunction_RejectsUnknownRuntime(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	body := `{"name":"f","sourceCode":"return 1","runtime":"wasm"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFunction_RejectsBadSignature(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	body := `{"name":"f","sourceCode":"return 1","signature":{"params":{"x":1}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateFunction_PreservesSignatureWhenOmitted(t *testing.T) {
	existing := oms.Function{
		RID:         "ri.ontology.main.function.f1",
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "addNumbers",
		Version:     1,
		SourceCode:  "return a + b",
		Runtime:     "goja",
		Signature:   json.RawMessage(`{"params":[{"name":"a","type":"integer","required":true}],"returns":{"type":"integer"}}`),
	}
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{existing},
	}
	router := setupFunctionRouter(repo)

	body := `{"sourceCode":"return a + b + 1","version":2}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.f1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.Version != 2 {
		t.Errorf("expected version=2, got %d", fn.Version)
	}
	if fn.Runtime != "goja" {
		t.Errorf("expected runtime preserved as goja, got %q", fn.Runtime)
	}
	if len(fn.Signature) == 0 {
		t.Fatal("expected signature preserved when omitted from update")
	}
}

func TestUpdateFunction_RejectsUnknownRuntime(t *testing.T) {
	existing := oms.Function{
		RID:         "ri.ontology.main.function.f1",
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "f",
		Version:     1,
		SourceCode:  "return 1",
		Runtime:     "goja",
	}
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{existing},
	}
	router := setupFunctionRouter(repo)

	body := `{"runtime":"wasm"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.f1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
