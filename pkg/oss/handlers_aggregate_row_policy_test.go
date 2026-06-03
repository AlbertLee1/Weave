package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/rls"
)

// staticRowPolicyProvider is a test double for oss.RowPolicyQueryProvider that
// compiles the caller's row policy through the real US-256 rls.Engine. It
// mirrors what cmd/server's *policyQueryAdapter does on the live path, but
// without the ontology-scope→ObjectType resolution (the test wires otRID
// directly), so the assertion isolates the handler's pushdown behaviour.
type staticRowPolicyProvider struct {
	engine *rls.Engine
	otRID  string
}

func (p *staticRowPolicyProvider) PolicyQuery(ctx context.Context, _ string) (query.Query, error) {
	user := auth.UserFromContext(ctx)
	return p.engine.Compile(ctx, user, p.otRID)
}

// sumAggCounts decodes an aggregation response and sums the "c" count metric
// across every leaf row, which equals the total number of rows the engine
// actually scanned for the caller.
func sumAggCounts(t *testing.T, body []byte) float64 {
	t.Helper()
	var resp struct {
		Data []struct {
			Metrics []struct {
				Name  string  `json:"name"`
				Value float64 `json:"value"`
			} `json:"metrics"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal aggregation response: %v (body=%s)", err, body)
	}
	total := 0.0
	for _, row := range resp.Data {
		for _, m := range row.Metrics {
			if m.Name == "c" {
				total += m.Value
			}
		}
	}
	return total
}

// TestBDD_Aggregation_RowPolicyScopesCount is the row-level security contract
// for the direct /objects/{objectType}/aggregate endpoint.
//
//	Given an employee ObjectType with 3 rows (emp1,emp2 in deptId=d1; emp3 in d2)
//	  and a US-256 row policy restricting role "eu-reader" to deptId=d1
//	When an eu-reader POSTs a count aggregation grouped by deptId
//	Then the engine only counts the 2 rows the caller may read (not all 3),
//	  so count(group d1)=2 and the d2 row is invisible to the aggregate.
//
// Before the fix AggregateObjects hit Bleve with MatchAll, so the eu-reader's
// count leaked all 3 rows — the existence and size of the policy-hidden d2
// population was recoverable via aggregation.
func TestBDD_Aggregation_RowPolicyScopesCount(t *testing.T) {
	svc, mgr, repo, _ := setupOSSTest(t)
	ctx := context.Background()

	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := rls.NewMemoryStore()
	if err := store.Create(ctx, &rls.RowPolicy{
		RID:           "ri.rls.main.row-policy.agg-d1-only",
		ObjectTypeRID: ot.RID,
		Predicate:     json.RawMessage(`{"type":"eq","field":"deptId","value":"d1"}`),
		AppliesTo:     rls.AppliesTo{Roles: []string{"eu-reader"}},
	}); err != nil {
		t.Fatalf("create row policy: %v", err)
	}
	engine := rls.New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("reload rls engine: %v", err)
	}

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)
	h.SetRowPolicyQueryProvider(&staticRowPolicyProvider{engine: engine, otRID: ot.RID})

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	aggregateAs := func(user *auth.User) float64 {
		body := `{
			"groupBy":[{"type":"exact","field":"deptId"}],
			"aggregation":[{"type":"count","name":"c"}]
		}`
		req := httptest.NewRequest("POST",
			"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.WithUser(req.Context(), user))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
		}
		return sumAggCounts(t, rr.Body.Bytes())
	}

	// eu-reader holds the policy role → aggregation is scoped to deptId=d1.
	euTotal := aggregateAs(&auth.User{ID: "u:eu", Roles: []string{"eu-reader"}})
	if euTotal != 2 {
		t.Errorf("eu-reader aggregate count = %v, want 2 (d1 rows only — d2 must be invisible)", euTotal)
	}

	// A role the policy does not apply to sees the full population (3 rows):
	// confirms the pushdown is permissive when no row policy matches.
	viewerTotal := aggregateAs(&auth.User{ID: "u:viewer", Roles: []string{"viewer"}})
	if viewerTotal != 3 {
		t.Errorf("viewer aggregate count = %v, want 3 (no applicable row policy)", viewerTotal)
	}
}

// TestAggregation_RowPolicyNilProviderAllowsEverything guards the back-compat
// contract: with no RowPolicyQueryProvider wired, the endpoint aggregates over
// the whole index exactly as before (MatchAll), returning all 3 rows.
func TestAggregation_RowPolicyNilProviderAllowsEverything(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	h.SetAggregation(aggregation.NewEngine(), mgr)
	// No SetRowPolicyQueryProvider call — provider is nil.

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body := `{
		"groupBy":[{"type":"exact","field":"deptId"}],
		"aggregation":[{"type":"count","name":"c"}]
	}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if total := sumAggCounts(t, rr.Body.Bytes()); total != 3 {
		t.Errorf("nil-provider aggregate count = %v, want 3 (unrestricted)", total)
	}
}
