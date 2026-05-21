package oss_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/scenarios"
)

func TestBDD_SELF604_AggregateWhereFiltersBackendResults(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	unfiltered := postAggregateBDD(t, r, `{"aggregation":[{"type":"count","name":"n"}]}`)
	unfilteredCount := metricValueBDD(t, unfiltered, "n")
	if unfilteredCount != 3 {
		t.Fatalf("unfiltered count = %v, want 3", unfilteredCount)
	}

	filtered := postAggregateBDD(t, r, `{
		"where":{"type":"eq","field":"active","value":true},
		"aggregation":[{"type":"count","name":"n"}]
	}`)
	filteredCount := metricValueBDD(t, filtered, "n")
	if filteredCount != 2 {
		t.Fatalf("filtered count = %v, want 2", filteredCount)
	}
	if filteredCount == unfilteredCount {
		t.Fatalf("where filter did not change aggregate result: filtered=%v unfiltered=%v", filteredCount, unfilteredCount)
	}
}

func TestBDD_SELF604_AggregateWhereFieldsUsePropertyVisibility(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)
	h.SetPropertyFilterProvider(&staticPropertyFilter{
		allowedByType: map[string][]string{
			"employee": {"employeeId", "name", "deptId"},
		},
	})

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	cases := []struct {
		name string
		body string
	}{
		{
			name: "direct hidden where field",
			body: `{
				"where":{"type":"eq","field":"active","value":true},
				"aggregation":[{"type":"count","name":"n"}]
			}`,
		},
		{
			name: "hidden where field under Palantir not array form",
			body: `{
				"where":{"type":"not","value":[{"type":"eq","field":"active","value":false}]},
				"aggregation":[{"type":"count","name":"n"}]
			}`,
		},
		{
			name: "hidden metric field inside subAggregation",
			body: `{
				"aggregation":[{"type":"count","name":"n"}],
				"subAggregations":[{"name":"hiddenMetric","aggregation":[{"type":"sum","field":"active","name":"s"}]}]
			}`,
		},
		{
			name: "hidden groupBy field inside subAggregation",
			body: `{
				"aggregation":[{"type":"count","name":"n"}],
				"subAggregations":[{"name":"hiddenGroup","aggregation":[{"type":"count","name":"m"}],"groupBy":[{"type":"exact","field":"active"}]}]
			}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
			}
			var apiErr struct {
				ErrorCode  string            `json:"errorCode"`
				ErrorName  string            `json:"errorName"`
				Parameters map[string]string `json:"parameters"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("unmarshal error body: %v", err)
			}
			if apiErr.ErrorCode != "PERMISSION_DENIED" {
				t.Errorf("errorCode = %q, want PERMISSION_DENIED", apiErr.ErrorCode)
			}
			if apiErr.ErrorName != "PropertyNotAccessible" {
				t.Errorf("errorName = %q, want PropertyNotAccessible", apiErr.ErrorName)
			}
			if apiErr.Parameters["property"] != "active" {
				t.Errorf("parameters.property = %q, want active", apiErr.Parameters["property"])
			}
		})
	}
}

func TestBDD_SELF604_ScenarioAggregateWhereFiltersPostOverlayRows(t *testing.T) {
	const ontologyRID = "ri.ontology.main.ontology.northwind"
	const scenarioRID = "ri.vertex.main.scenario.self604"

	svc := &listingService{rows: []*oss.WireObject{
		makeOrder("A", "pending", 10),
		makeOrder("B", "pending", 20),
		makeOrder("C", "shipped", 30),
	}}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			scenarioRID: {RID: scenarioRID, ParentOntologyCommit: ontologyRID},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			scenarioRID: {
				{Seq: 1, Op: "modifyProperty", ObjectType: "Order", ObjectID: "A", Property: "status", NewValue: raw("shipped")},
				{Seq: 2, Op: "createObject", ObjectType: "Order", ObjectID: "N", NewValue: raw(map[string]any{"status": "shipped", "total": float64(40)})},
			},
		},
	}
	router := newTestRouter(svc, reader)

	body := []byte(`{
		"where":{"type":"eq","field":"status","value":"shipped"},
		"aggregation":[{"type":"count","name":"n"},{"type":"sum","field":"total","name":"s"}],
		"groupBy":[{"type":"exact","field":"status"}]
	}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Order/aggregate",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp aggregation.AggregationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("data len = %d, want 1 shipped bucket after where filter; data=%+v", len(resp.Data), resp.Data)
	}
	if resp.Data[0].Group["status"] != "shipped" {
		t.Fatalf("group status = %v, want shipped", resp.Data[0].Group["status"])
	}
	if got := metricValueBDD(t, resp, "n"); got != 3 {
		t.Errorf("shipped count = %v, want 3", got)
	}
	if got := metricValueBDD(t, resp, "s"); got != 80 {
		t.Errorf("shipped sum = %v, want 80", got)
	}
}

func TestBDD_SELF605_AggregateWhereRejectsRegexInsteadOfUnboundedSearch(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
		strings.NewReader(`{
			"where":{"type":"regex","field":"name","value":"A.*"},
			"aggregation":[{"type":"count","name":"n"}]
		}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if apiErr.ErrorCode != "INVALID_ARGUMENT" {
		t.Errorf("errorCode = %q, want INVALID_ARGUMENT", apiErr.ErrorCode)
	}
	if apiErr.ErrorName != "AggregationWhereRegexUnsupported" {
		t.Errorf("errorName = %q, want AggregationWhereRegexUnsupported", apiErr.ErrorName)
	}
	if !strings.Contains(apiErr.Parameters["reason"], "regex") {
		t.Errorf("reason = %q, want it to mention regex", apiErr.Parameters["reason"])
	}
}

func TestBDD_SELF607_AggregateWhereRejectsInvalidWhereInsteadOfInternalError(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body := `{
		"where":{"type":"unsupportedForAggregation","field":"name","value":"alice"},
		"aggregation":[{"type":"count","name":"n"}]
	}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		ErrorCode string `json:"errorCode"`
		ErrorName string `json:"errorName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if apiErr.ErrorCode != "INVALID_ARGUMENT" {
		t.Errorf("errorCode = %q, want INVALID_ARGUMENT", apiErr.ErrorCode)
	}
	if apiErr.ErrorName != "InvalidAggregationWhere" {
		t.Errorf("errorName = %q, want InvalidAggregationWhere", apiErr.ErrorName)
	}
}

func TestBDD_SELF605_ScenarioAggregateWhereContainsAnyTermFiltersPostOverlayRows(t *testing.T) {
	const ontologyRID = "ri.ontology.main.ontology.northwind"
	const scenarioRID = "ri.vertex.main.scenario.self605"

	a := makeOrder("A", "pending", 10)
	a.Properties["notes"] = "standard queue"
	b := makeOrder("B", "pending", 20)
	b.Properties["notes"] = "needs review"
	c := makeOrder("C", "shipped", 30)
	c.Properties["notes"] = "standard delivery"

	svc := &listingService{rows: []*oss.WireObject{a, b, c}}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			scenarioRID: {RID: scenarioRID, ParentOntologyCommit: ontologyRID},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			scenarioRID: {
				{Seq: 1, Op: "modifyProperty", ObjectType: "Order", ObjectID: "B", Property: "notes", NewValue: raw("urgent review")},
				{Seq: 2, Op: "createObject", ObjectType: "Order", ObjectID: "N", NewValue: raw(map[string]any{"status": "shipped", "notes": "urgent delayed", "total": float64(40)})},
			},
		},
	}
	router := newTestRouter(svc, reader)

	body := []byte(`{
		"where":{"type":"containsAnyTerm","field":"notes","value":"urgent missing"},
		"aggregation":[{"type":"count","name":"n"},{"type":"sum","field":"total","name":"s"}],
		"groupBy":[{"type":"exact","field":"status"}]
	}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Order/aggregate",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp aggregation.AggregationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("data len = %d, want 2 buckets after containsAnyTerm filter; data=%+v", len(resp.Data), resp.Data)
	}

	got := map[string]map[string]float64{}
	for _, row := range resp.Data {
		status := row.Group["status"].(string)
		n, _ := metricByName(row, "n")
		s, _ := metricByName(row, "s")
		got[status] = map[string]float64{"n": n, "s": s}
	}
	if got["pending"]["n"] != 1 || got["pending"]["s"] != 20 {
		t.Errorf("pending bucket = %+v, want n=1 s=20", got["pending"])
	}
	if got["shipped"]["n"] != 1 || got["shipped"]["s"] != 40 {
		t.Errorf("shipped bucket = %+v, want n=1 s=40", got["shipped"])
	}
}

func TestBDD_SELF606_ScenarioAggregateWhereContainsAnyTermUsesTokensAndArrayValues(t *testing.T) {
	const ontologyRID = "ri.ontology.main.ontology.northwind"
	const scenarioRID = "ri.vertex.main.scenario.self606"

	a := makeOrder("A", "pending", 10)
	a.Properties["notes"] = "insurgent queue"
	b := makeOrder("B", "pending", 20)
	b.Properties["notes"] = "standard queue"
	c := makeOrder("C", "shipped", 30)
	c.Properties["notes"] = "routine delivery"

	svc := &listingService{rows: []*oss.WireObject{a, b, c}}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			scenarioRID: {RID: scenarioRID, ParentOntologyCommit: ontologyRID},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			scenarioRID: {
				{Seq: 1, Op: "modifyProperty", ObjectType: "Order", ObjectID: "B", Property: "notes", NewValue: raw([]string{"urgent", "review"})},
				{Seq: 2, Op: "createObject", ObjectType: "Order", ObjectID: "N", NewValue: raw(map[string]any{"status": "shipped", "notes": []string{"delayed", "urgent"}, "total": float64(40)})},
			},
		},
	}
	router := newTestRouter(svc, reader)

	body := []byte(`{
		"where":{"type":"containsAnyTerm","field":"notes","value":"urgent"},
		"aggregation":[{"type":"count","name":"n"},{"type":"sum","field":"total","name":"s"}],
		"groupBy":[{"type":"exact","field":"status"}]
	}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Order/aggregate",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp aggregation.AggregationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("data len = %d, want 2 buckets after token containsAnyTerm filter; data=%+v", len(resp.Data), resp.Data)
	}

	got := map[string]map[string]float64{}
	for _, row := range resp.Data {
		status := row.Group["status"].(string)
		n, _ := metricByName(row, "n")
		s, _ := metricByName(row, "s")
		got[status] = map[string]float64{"n": n, "s": s}
	}
	if got["pending"]["n"] != 1 || got["pending"]["s"] != 20 {
		t.Errorf("pending bucket = %+v, want n=1 s=20", got["pending"])
	}
	if got["shipped"]["n"] != 1 || got["shipped"]["s"] != 40 {
		t.Errorf("shipped bucket = %+v, want n=1 s=40", got["shipped"])
	}
}

func TestBDD_SELF605_ScenarioAggregateWhereRejectsUnsupportedMatcherOperator(t *testing.T) {
	const ontologyRID = "ri.ontology.main.ontology.northwind"
	const scenarioRID = "ri.vertex.main.scenario.self605.unsupported"

	a := makeOrder("A", "pending", 10)
	a.Properties["notes"] = "standard queue"

	svc := &listingService{rows: []*oss.WireObject{a}}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			scenarioRID: {RID: scenarioRID, ParentOntologyCommit: ontologyRID},
		},
		edits: map[string][]scenarios.ScenarioEdit{scenarioRID: nil},
	}
	router := newTestRouter(svc, reader)

	body := []byte(`{
		"where":{"type":"wildcard","field":"notes","value":"standard*"},
		"aggregation":[{"type":"count","name":"n"}]
	}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Order/aggregate",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if apiErr.ErrorCode != "INVALID_ARGUMENT" {
		t.Errorf("errorCode = %q, want INVALID_ARGUMENT", apiErr.ErrorCode)
	}
	if apiErr.ErrorName != "ScenarioAggregationFailed" {
		t.Errorf("errorName = %q, want ScenarioAggregationFailed", apiErr.ErrorName)
	}
	if !strings.Contains(apiErr.Parameters["reason"], `unsupported where operator "wildcard"`) {
		t.Errorf("reason = %q, want unsupported wildcard operator", apiErr.Parameters["reason"])
	}
}

func postAggregateBDD(t *testing.T, r http.Handler, body string) aggregation.AggregationResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp aggregation.AggregationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal aggregate response: %v", err)
	}
	return resp
}

func metricValueBDD(t *testing.T, resp aggregation.AggregationResponse, name string) float64 {
	t.Helper()
	if len(resp.Data) == 0 {
		t.Fatalf("response has no data rows")
	}
	value, ok := metricByName(resp.Data[0], name)
	if !ok {
		t.Fatalf("metric %q missing: %+v", name, resp.Data[0].Metrics)
	}
	return value
}
