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
	"github.com/liyang/weave/pkg/queryexec"
)

// TestBDD_ExecuteQueryValueEnvelope pins the Foundry ExecuteQueryResponse
// contract on POST /api/v2/ontologies/{ont}/queries/{queryApiName}/execute:
// the response body is ALWAYS `{"value": <DataValue>}` — including when the
// backing function produces a struct/map DataValue. OSDK-style clients
// unconditionally unwrap `.value`, so a bare map response breaks them.
//
// Scenario (Given/When/Then), exercising the same router wiring used in
// production (cmd/server/routes.go):
//
//	Given a QueryType backed by a Goja function
//	When  the function returns a bare map (a struct DataValue not wrapped
//	      in the {value: ...} function convention)
//	Then  the HTTP response is {"value": <that map>} — never the bare map
//	When  the function returns the conventional {value: ...} envelope
//	Then  the HTTP response is {"value": ...} exactly once (no double wrap)
//	When  the function returns a scalar
//	Then  the HTTP response is {"value": <scalar>}
func TestBDD_ExecuteQueryValueEnvelope(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.nw"
	const ontAPIName = "northwind"

	newServer := func(t *testing.T, queryAPIName, fnSource string) *chi.Mux {
		t.Helper()
		fnRID := "ri.ontology.main.function." + queryAPIName
		repo := &mockRepo{
			ontologies: []oms.Ontology{
				{RID: ontRID, APIName: ontAPIName, DisplayName: "Northwind"},
			},
			queryTypes: []oms.QueryType{
				{
					RID:         "ri.ontology.main.querytype." + queryAPIName,
					OntologyRID: ontRID,
					APIName:     queryAPIName,
					DisplayName: queryAPIName,
					Status:      "ACTIVE",
					FunctionRID: fnRID,
					Parameters:  json.RawMessage(`[]`),
					Output:      json.RawMessage(`{}`),
					Query:       json.RawMessage(`{}`),
				},
			},
			functions: []oms.Function{
				{RID: fnRID, Name: queryAPIName, Version: "1.0.0", SourceCode: fnSource},
			},
		}
		handler := oms.NewOMSHandler(repo)
		rt := functions.NewRuntime(functions.DefaultConfig())
		handler.SetQueryExecutor(queryexec.NewGojaQueryExecutor(rt, repo))

		r := chi.NewRouter()
		// Mirror exactly the route registered in cmd/server/routes.go.
		r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)
		return r
	}

	execute := func(t *testing.T, r *chi.Mux, queryAPIName string) map[string]interface{} {
		t.Helper()
		body, _ := json.Marshal(map[string]interface{}{
			"parameters": map[string]interface{}{},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontAPIName+"/queries/"+queryAPIName+"/execute",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse response: %v", err)
		}
		return resp
	}

	t.Run("bare map result is wrapped under value", func(t *testing.T) {
		// Given a query function returning a struct DataValue as a bare map
		// (no {value: ...} convention).
		fnSource := `function main(input) {
			return { customerId: "ALFKI", orderCount: 6 };
		}`
		r := newServer(t, "customerSummary", fnSource)

		// When the query is executed over HTTP.
		resp := execute(t, r, "customerSummary")

		// Then the map is delivered under the "value" envelope key…
		value, ok := resp["value"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected response to be {value: <map>}, got: %v", resp)
		}
		if value["customerId"] != "ALFKI" {
			t.Errorf("expected value.customerId 'ALFKI', got %v", value["customerId"])
		}
		if value["orderCount"] != float64(6) {
			t.Errorf("expected value.orderCount 6, got %v", value["orderCount"])
		}
		// …and never leaks bare at the top level.
		if _, leaked := resp["customerId"]; leaked {
			t.Errorf("bare map leaked to top level, response: %v", resp)
		}
		if len(resp) != 1 {
			t.Errorf("ExecuteQueryResponse must have exactly one key 'value', got: %v", resp)
		}
	})

	t.Run("function {value} envelope passes through without double wrap", func(t *testing.T) {
		// Given a query function that follows the {value: ...} convention.
		fnSource := `function main(input) {
			return { value: { customers: ["ALFKI", "ANATR"], totalCount: 2 } };
		}`
		r := newServer(t, "topCustomers", fnSource)

		// When the query is executed over HTTP.
		resp := execute(t, r, "topCustomers")

		// Then the payload sits directly under "value" (single wrap).
		value, ok := resp["value"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected response to be {value: <map>}, got: %v", resp)
		}
		if _, doubleWrapped := value["value"]; doubleWrapped {
			t.Errorf("response was double-wrapped: %v", resp)
		}
		customers, ok := value["customers"].([]interface{})
		if !ok || len(customers) != 2 {
			t.Errorf("expected value.customers with 2 entries, got: %v", value)
		}
		if len(resp) != 1 {
			t.Errorf("ExecuteQueryResponse must have exactly one key 'value', got: %v", resp)
		}
	})

	t.Run("scalar result is wrapped under value", func(t *testing.T) {
		// Given a query function returning a scalar DataValue.
		fnSource := `function main(input) {
			return 42.5;
		}`
		r := newServer(t, "totalRevenue", fnSource)

		// When the query is executed over HTTP.
		resp := execute(t, r, "totalRevenue")

		// Then the scalar sits under "value".
		if resp["value"] != 42.5 {
			t.Errorf("expected {value: 42.5}, got: %v", resp)
		}
		if len(resp) != 1 {
			t.Errorf("ExecuteQueryResponse must have exactly one key 'value', got: %v", resp)
		}
	})
}
