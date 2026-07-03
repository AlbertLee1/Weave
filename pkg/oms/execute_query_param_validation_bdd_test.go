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

const paramValOntRID = "ri.ontology.main.ontology.nw"
const paramValOntAPIName = "northwind"

// newParamValidationServer wires a chi router around ExecuteQueryType with a
// QueryType declaring a single required integer parameter "limit". A real Goja
// executor is attached so the valid path actually runs the backing function.
func newParamValidationServer(t *testing.T) *chi.Mux {
	t.Helper()
	const queryAPIName = "topCustomers"
	fnRID := "ri.ontology.main.function." + queryAPIName
	// The function echoes the validated parameter back under the {value}
	// envelope so the happy path can assert the value round-trips.
	fnSource := `function main(input) {
		return { value: input.parameters.limit };
	}`
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: paramValOntRID, APIName: paramValOntAPIName, DisplayName: "Northwind"},
		},
		queryTypes: []oms.QueryType{
			{
				RID:         "ri.ontology.main.querytype." + queryAPIName,
				OntologyRID: paramValOntRID,
				APIName:     queryAPIName,
				DisplayName: queryAPIName,
				Status:      "ACTIVE",
				FunctionRID: fnRID,
				Parameters:  json.RawMessage(`[{"id":"limit","type":"integer","required":true}]`),
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
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)
	return r
}

func postExecuteQuery(t *testing.T, r *chi.Mux, params map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"parameters": params})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+paramValOntAPIName+"/queries/topCustomers/execute",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestExecuteQueryTypeParameterValidation is the handler-level unit test for the
// Foundry-parity pre-execution parameter check: a missing required parameter
// surfaces as 400 MissingParameter, and a value that violates the declared base
// type surfaces as 400 InvalidParameterValue — both name the offending
// parameter. Neither error should reach the backing function.
func TestExecuteQueryTypeParameterValidation(t *testing.T) {
	tests := []struct {
		name          string
		params        map[string]interface{}
		wantErrorName string
	}{
		{
			name:          "missing required parameter -> MissingParameter",
			params:        map[string]interface{}{},
			wantErrorName: "MissingParameter",
		},
		{
			name:          "wrong type -> InvalidParameterValue",
			params:        map[string]interface{}{"limit": "not-a-number"},
			wantErrorName: "InvalidParameterValue",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newParamValidationServer(t)
			rec := postExecuteQuery(t, r, tc.params)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("parse error response: %v", err)
			}
			if resp["errorName"] != tc.wantErrorName {
				t.Errorf("expected errorName %q, got %v", tc.wantErrorName, resp["errorName"])
			}
			if resp["errorCode"] != "INVALID_ARGUMENT" {
				t.Errorf("expected errorCode INVALID_ARGUMENT, got %v", resp["errorCode"])
			}
			params, ok := resp["parameters"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected parameters object, got %v", resp["parameters"])
			}
			if params["parameter"] != "limit" {
				t.Errorf("expected parameters.parameter 'limit', got %v", params["parameter"])
			}
		})
	}
}

// TestBDD_ExecuteQueryParameterValidation pins the Foundry pre-execution
// parameter contract on POST .../queries/{queryApiName}/execute, exercised end
// to end through the production chi wiring and a real Goja-backed function:
//
//	Given a QueryType declaring a required integer parameter "limit"
//	When  the caller omits "limit"
//	Then  the response is 400 MissingParameter naming "limit"
//	When  the caller sends "limit" as a non-integer
//	Then  the response is 400 InvalidParameterValue naming "limit"
//	When  the caller sends a valid "limit"
//	Then  the query executes and returns {"value": <limit>}
func TestBDD_ExecuteQueryParameterValidation(t *testing.T) {
	t.Run("missing required parameter is rejected before execution", func(t *testing.T) {
		r := newParamValidationServer(t)
		rec := postExecuteQuery(t, r, map[string]interface{}{})

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["errorName"] != "MissingParameter" {
			t.Errorf("expected errorName MissingParameter, got %v", resp["errorName"])
		}
		params, _ := resp["parameters"].(map[string]interface{})
		if params["parameter"] != "limit" {
			t.Errorf("expected offending parameter 'limit', got %v", params["parameter"])
		}
	})

	t.Run("type-mismatched parameter is rejected before execution", func(t *testing.T) {
		r := newParamValidationServer(t)
		rec := postExecuteQuery(t, r, map[string]interface{}{"limit": "not-a-number"})

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["errorName"] != "InvalidParameterValue" {
			t.Errorf("expected errorName InvalidParameterValue, got %v", resp["errorName"])
		}
		params, _ := resp["parameters"].(map[string]interface{})
		if params["parameter"] != "limit" {
			t.Errorf("expected offending parameter 'limit', got %v", params["parameter"])
		}
	})

	t.Run("valid parameters execute and return the value envelope", func(t *testing.T) {
		r := newParamValidationServer(t)
		rec := postExecuteQuery(t, r, map[string]interface{}{"limit": 5})

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse response: %v", err)
		}
		if resp["value"] != float64(5) {
			t.Errorf("expected {value: 5}, got %v", resp)
		}
		if len(resp) != 1 {
			t.Errorf("ExecuteQueryResponse must have exactly one key 'value', got: %v", resp)
		}
	})
}
