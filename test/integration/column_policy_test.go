//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/rid"
	"github.com/liyang/weave/pkg/security"
)

// TestColumnPolicy_AggregationRejectsFilteredField is the US-049 acceptance
// integration test. A PROPERTY-scope policy grants `salary` only to callers
// whose `role` attribute is "manager"; everyone sees `employeeId`, `name`,
// `deptId`. The /objects/employee/aggregate endpoint is exercised through a
// real PG-backed OMS, a real Bleve index, and the same
// propertyFilterAdapter wiring used by the cmd/server main:
//
//   - manager can sum/avg salary and groupBy deptId with a salary metric,
//   - peer (engineer) receives 403 + PropertyNotAccessible on any request
//     referencing salary, whether in groupBy.field or metric.field,
//   - peer can still count + groupBy deptId because deptId is allowed.
//
// The test covers the matrix described in the story's acceptance criteria:
// "manager can aggregate salary, peer cannot".
func TestColumnPolicy_AggregationRejectsFilteredField(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "column_policy",
		DisplayName: "Column Policy",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	employeeOT := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, employeeOT); err != nil {
		t.Fatalf("create object type: %v", err)
	}

	// Not_analyzed on every keyword field so exact-match TermQuery
	// pipelines behave consistently with the US-047 policy matrix fixture.
	props := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "name", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "deptId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "salary", BaseType: "double", IsSearchable: true},
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})
	if _, err := mgr.EnsureIndex("employee", props); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	docs := []struct {
		pk  string
		doc map[string]interface{}
	}{
		{"emp1", map[string]interface{}{
			"employeeId": "emp1", "name": "alice", "deptId": "d1", "salary": float64(100000),
		}},
		{"emp2", map[string]interface{}{
			"employeeId": "emp2", "name": "bob", "deptId": "d1", "salary": float64(90000),
		}},
		{"emp3", map[string]interface{}{
			"employeeId": "emp3", "name": "carol", "deptId": "d2", "salary": float64(120000),
		}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.pk, d.doc); err != nil {
			t.Fatalf("index %s: %v", d.pk, err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	engine := security.NewEngine()
	engine.SetPolicies(employeeOT.RID, []security.Policy{{
		RID:           "ri.ontology.main.security-policy.us049-columns",
		ObjectTypeRID: employeeOT.RID,
		PolicyType:    security.PolicyTypeProperty,
		Rules: []security.Rule{
			// Baseline grant: every caller sees employeeId/name/deptId.
			{Properties: []string{"employeeId", "name", "deptId"}},
			// Managers additionally see salary.
			{
				UserAttr:   "role",
				Values:     []string{"manager"},
				Properties: []string{"salary"},
			},
		},
	}})

	// The filter adapter under test is defined in cmd/server; the
	// integration test mirrors its behaviour inline via a closure so this
	// file doesn't depend on the main package. Keeping the forwarder shape
	// identical (ontology scope from context → GetOntology → GetObjectType
	// → engine.AllowedProperties) proves the handler wiring end-to-end.
	filter := filterAdapter{repo: repo, engine: engine}

	svc := oss.NewService(repo, mgr, nil)
	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)
	h.SetPropertyFilterProvider(&filter)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Every request is stamped with the ontology scope the adapter
			// reads; matches the auth middleware chain in cmd/server main.
			ctx := index.WithOntologyScope(req.Context(), ont.APIName)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h.RegisterRoutes(r)

	manager := &auth.User{
		ID: "u-manager", Roles: []string{auth.RoleViewer},
		Attributes: map[string]any{"role": "manager"},
	}
	peer := &auth.User{
		ID: "u-peer", Roles: []string{auth.RoleViewer},
		Attributes: map[string]any{"role": "engineer"},
	}

	post := func(t *testing.T, user *auth.User, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST",
			"/api/v2/ontologies/"+ont.APIName+"/objects/employee/aggregate",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.WithUser(req.Context(), user))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	// --- Manager: sum(salary) is allowed. ---
	mBody := `{"aggregation":[{"type":"sum","field":"salary","name":"total"}]}`
	if rr := post(t, manager, mBody); rr.Code != http.StatusOK {
		t.Fatalf("manager sum(salary): status=%d body=%s", rr.Code, rr.Body.String())
	}

	// --- Manager: groupBy deptId + avg(salary) is allowed. ---
	mGroupBody := `{
		"groupBy":[{"type":"exact","field":"deptId"}],
		"aggregation":[{"type":"avg","field":"salary","name":"avgPay"}]
	}`
	if rr := post(t, manager, mGroupBody); rr.Code != http.StatusOK {
		t.Fatalf("manager groupBy deptId + avg(salary): status=%d body=%s", rr.Code, rr.Body.String())
	}

	// --- Peer: sum(salary) is rejected with 403 PropertyNotAccessible. ---
	pBody := `{"aggregation":[{"type":"sum","field":"salary","name":"total"}]}`
	rr := post(t, peer, pBody)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("peer sum(salary): status=%d want=403 body=%s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("unmarshal error body: %v (body=%s)", err, rr.Body.String())
	}
	if apiErr.ErrorCode != "PERMISSION_DENIED" {
		t.Errorf("errorCode=%q want PERMISSION_DENIED", apiErr.ErrorCode)
	}
	if apiErr.ErrorName != "PropertyNotAccessible" {
		t.Errorf("errorName=%q want PropertyNotAccessible", apiErr.ErrorName)
	}
	if apiErr.Parameters["property"] != "salary" {
		t.Errorf("parameters.property=%q want salary", apiErr.Parameters["property"])
	}

	// --- Peer: groupBy salary is also rejected. ---
	pGroupBody := `{
		"groupBy":[{"type":"exact","field":"salary"}],
		"aggregation":[{"type":"count","name":"c"}]
	}`
	if rr := post(t, peer, pGroupBody); rr.Code != http.StatusForbidden {
		t.Fatalf("peer groupBy salary: status=%d want=403 body=%s", rr.Code, rr.Body.String())
	}

	// --- Peer: count + groupBy deptId is allowed (only touches allowed fields). ---
	pAllowedBody := `{
		"groupBy":[{"type":"exact","field":"deptId"}],
		"aggregation":[{"type":"count","name":"c"}]
	}`
	if rr := post(t, peer, pAllowedBody); rr.Code != http.StatusOK {
		t.Fatalf("peer groupBy deptId count: status=%d body=%s", rr.Code, rr.Body.String())
	}
}

// filterAdapter is an in-test mirror of cmd/server's propertyFilterAdapter.
// It reads the ontology scope stamped by the request middleware, resolves
// the apiName to an ObjectType, and forwards to *security.Engine so the
// handler sees the same contract the production wiring provides.
type filterAdapter struct {
	repo   oms.Repository
	engine *security.Engine
}

func (a *filterAdapter) AllowedProperties(ctx context.Context, objectType string) ([]string, error) {
	scope := index.OntologyScopeFromContext(ctx)
	if scope == "" {
		return nil, nil
	}
	ont, err := a.repo.GetOntology(ctx, scope)
	if err != nil || ont == nil {
		return nil, err
	}
	ot, err := a.repo.GetObjectTypeByAPIName(ctx, ont.RID, objectType)
	if err != nil || ot == nil {
		return nil, err
	}
	user := auth.UserFromContext(ctx)
	return a.engine.AllowedProperties(ctx, user, *ot), nil
}
