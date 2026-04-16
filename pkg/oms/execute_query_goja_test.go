//go:build goja

package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/oms"
)

// TestExecuteQueryDispatch_Scalar verifies that a query backed by a Goja
// function returning {value: ...} is actually executed and the result
// returned as JSON.
func TestExecuteQueryDispatch_Scalar(t *testing.T) {
	fnSource := `function main(input) {
		var params = input.parameters;
		return { value: 42.5 };
	}`

	fnRID := "ri.ontology.main.function.totalRevenue"
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{
				RID:         "ri.ontology.main.querytype.rev",
				OntologyRID: "ri.ontology.main.ontology.1",
				APIName:     "totalRevenue",
				DisplayName: "Total Revenue",
				Status:      "ACTIVE",
				FunctionRID: fnRID,
				Parameters:  json.RawMessage(`[]`),
				Output:      json.RawMessage(`{}`),
				Query:       json.RawMessage(`{}`),
			},
		},
		functions: []oms.Function{
			{
				RID:        fnRID,
				Name:       "totalRevenue",
				Version:    1,
				SourceCode: fnSource,
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	rt := functions.NewRuntime(functions.DefaultConfig())
	handler.SetQueryExecutor(oms.NewGojaQueryExecutor(rt, repo))

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)

	body, _ := json.Marshal(map[string]interface{}{
		"parameters": map[string]interface{}{},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/queries/totalRevenue/execute",
		bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	val, ok := result["value"]
	if !ok {
		t.Fatalf("expected 'value' key in response, got keys: %v", result)
	}
	if val != 42.5 {
		t.Errorf("expected value 42.5, got %v", val)
	}
}

// TestExecuteQueryDispatch_ObjectList verifies that a query function
// returning a list of objects works correctly.
func TestExecuteQueryDispatch_ObjectList(t *testing.T) {
	fnSource := `function main(input) {
		var params = input.parameters;
		var limit = params.limit || 5;
		var customers = [];
		for (var i = 0; i < limit; i++) {
			customers.push({
				customerId: "CUST-" + (i + 1),
				name: "Customer " + (i + 1),
				orderCount: 100 - i * 10
			});
		}
		return { value: { customers: customers, totalCount: limit } };
	}`

	fnRID := "ri.ontology.main.function.topCustomers"
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{
				RID:         "ri.ontology.main.querytype.top",
				OntologyRID: "ri.ontology.main.ontology.1",
				APIName:     "topCustomers",
				DisplayName: "Top Customers",
				Status:      "ACTIVE",
				FunctionRID: fnRID,
				Parameters:  json.RawMessage(`[{"id":"limit","type":"integer"}]`),
				Output:      json.RawMessage(`{}`),
				Query:       json.RawMessage(`{}`),
			},
		},
		functions: []oms.Function{
			{
				RID:        fnRID,
				Name:       "topCustomers",
				Version:    1,
				SourceCode: fnSource,
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	rt := functions.NewRuntime(functions.DefaultConfig())
	handler.SetQueryExecutor(oms.NewGojaQueryExecutor(rt, repo))

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)

	body, _ := json.Marshal(map[string]interface{}{
		"parameters": map[string]interface{}{"limit": 3},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/queries/topCustomers/execute",
		bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	value, ok := result["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'value' map in response, got: %v", result)
	}

	customers, ok := value["customers"].([]interface{})
	if !ok {
		t.Fatalf("expected 'customers' array, got: %v", value)
	}
	if len(customers) != 3 {
		t.Errorf("expected 3 customers, got %d", len(customers))
	}

	tc, ok := value["totalCount"]
	if !ok {
		t.Fatalf("expected 'totalCount' in value")
	}
	if tc != float64(3) {
		t.Errorf("expected totalCount 3, got %v", tc)
	}
}

// TestExecuteQueryDispatch_NoFunction verifies that when a QueryType has no
// FunctionRID, the handler falls back to returning metadata (backward compat).
func TestExecuteQueryDispatch_NoFunction(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{
				RID:         "ri.ontology.main.querytype.1",
				OntologyRID: "ri.ontology.main.ontology.1",
				APIName:     "simpleQuery",
				DisplayName: "Simple Query",
				Status:      "ACTIVE",
				Parameters:  json.RawMessage(`[]`),
				Output:      json.RawMessage(`{}`),
				Query:       json.RawMessage(`{"objectType":"Customer"}`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/queries/simpleQuery/execute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	// Backward compat: returns query metadata
	if result["apiName"] != "simpleQuery" {
		t.Errorf("expected apiName 'simpleQuery', got %v", result["apiName"])
	}
}

// TestExecuteQueryDispatch_FunctionError verifies that a function-level
// error is reported properly.
func TestExecuteQueryDispatch_FunctionError(t *testing.T) {
	fnSource := `function main(input) {
		return { error: "insufficient permissions" };
	}`

	fnRID := "ri.ontology.main.function.failing"
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{
				RID:         "ri.ontology.main.querytype.fail",
				OntologyRID: "ri.ontology.main.ontology.1",
				APIName:     "failingQuery",
				DisplayName: "Failing Query",
				Status:      "ACTIVE",
				FunctionRID: fnRID,
				Parameters:  json.RawMessage(`[]`),
				Output:      json.RawMessage(`{}`),
				Query:       json.RawMessage(`{}`),
			},
		},
		functions: []oms.Function{
			{
				RID:        fnRID,
				Name:       "failing",
				Version:    1,
				SourceCode: fnSource,
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	rt := functions.NewRuntime(functions.DefaultConfig())
	handler.SetQueryExecutor(oms.NewGojaQueryExecutor(rt, repo))

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/queries/failingQuery/execute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Function error → 400
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestExecuteQueryDispatch_HTTPDispatch verifies that query types with
// http:// FunctionRIDs are dispatched via the HTTP path.
func TestExecuteQueryDispatch_HTTPDispatch(t *testing.T) {
	// Stand up a fake function HTTP server that echoes the parameters.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		params := reqBody["parameters"]
		json.NewEncoder(w).Encode(map[string]interface{}{
			"value": params,
		})
	}))
	defer srv.Close()

	fnURL := srv.URL + "/functions/topCustomers"
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{
				RID:         "ri.ontology.main.querytype.http",
				OntologyRID: "ri.ontology.main.ontology.1",
				APIName:     "httpQuery",
				DisplayName: "HTTP Query",
				Status:      "ACTIVE",
				FunctionRID: fnURL,
				Parameters:  json.RawMessage(`[]`),
				Output:      json.RawMessage(`{}`),
				Query:       json.RawMessage(`{}`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	rt := functions.NewRuntime(functions.DefaultConfig())
	handler.SetQueryExecutor(oms.NewRoutingQueryExecutor(rt, repo))

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)

	body, _ := json.Marshal(map[string]interface{}{
		"parameters": map[string]interface{}{"limit": 5},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/queries/httpQuery/execute",
		bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	value, ok := result["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'value' map in response, got: %v", result)
	}
	if value["limit"] != float64(5) {
		t.Errorf("expected limit=5 echoed back, got %v", value["limit"])
	}
}
