//go:build integration

package phase7_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
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
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/rid"
	"github.com/liyang/weave/pkg/security"
)

// TestPolicyE2E_RowColumnMarkingMatrix is the US-070 acceptance test:
// a comprehensive cross-US integration test that exercises all three
// policy dimensions — row-level (eq), column-level (PROPERTY scope),
// and marking enforcement (subset semantics) — simultaneously against
// a real PG-backed OMS + Bleve index through oss.ServiceImpl.
//
// The test proves that Load, Search, and Aggregate paths all apply
// the combined policy matrix correctly for 5 user/object combinations:
//
//	Users:
//	  admin      dept=engineering  markings=[ALPHA,BETA]  role=manager
//	  analyst    dept=engineering  markings=[ALPHA]        role=analyst
//	  finMgr     dept=finance      markings=[ALPHA,BETA]  role=manager
//	  intern     dept=engineering  markings=[]             role=intern
//	  auditor    dept=engineering  markings=[ALPHA,BETA,GAMMA] role=auditor
//
//	Objects (5 projects):
//	  proj1  dept=engineering  _markings=[ALPHA]        budget=100000
//	  proj2  dept=engineering  _markings=[BETA]         budget=200000
//	  proj3  dept=finance      _markings=[ALPHA]        budget=150000
//	  proj4  dept=engineering  _markings=[ALPHA,BETA]   budget=300000
//	  proj5  dept=finance      _markings=[GAMMA]        budget=50000
//
//	Policies:
//	  Row-level:    eq on department (user.department == object.department)
//	  Marking:      enabled (subset: user.markings ⊇ object._markings)
//	  Column-level: baseline=[projectId,name,department], manager→+[budget]
//
//	Expected matrix (row+marking → visible set, column → visible fields):
//	  admin     → {proj1,proj2,proj4}  fields: projectId,name,department,budget
//	  analyst   → {proj1}              fields: projectId,name,department
//	  finMgr    → {proj3}              fields: projectId,name,department,budget
//	  intern    → {}                   fields: N/A
//	  auditor   → {proj1,proj2,proj4}  fields: projectId,name,department
func TestPolicyE2E_RowColumnMarkingMatrix(t *testing.T) {
	ctx := context.Background()

	// --- infrastructure: PG + migrations ---
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	// --- OMS: ontology + object type ---
	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "policy_e2e_p7",
		DisplayName: "Phase 7 Policy E2E",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	projectOT := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "project",
		DisplayName: "Project",
		PrimaryKey:  "projectId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, projectOT); err != nil {
		t.Fatalf("create object type: %v", err)
	}

	// --- Bleve index ---
	// All keyword fields use not_analyzed so TermQuery payloads from the
	// policy engine match exactly without stemming. budget is numeric.
	props := []index.Property{
		{APIName: "projectId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "name", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "department", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "budget", BaseType: "double", IsSearchable: true},
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	if _, err := mgr.EnsureIndex("project", props); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// --- seed 5 project objects ---
	docs := []struct {
		pk  string
		doc map[string]interface{}
	}{
		{"proj1", map[string]interface{}{
			"projectId":           "proj1",
			"name":                "Alpha Engine",
			"department":          "engineering",
			security.MarkingField: []string{"ALPHA"},
			"budget":              float64(100000),
		}},
		{"proj2", map[string]interface{}{
			"projectId":           "proj2",
			"name":                "Beta Pipeline",
			"department":          "engineering",
			security.MarkingField: []string{"BETA"},
			"budget":              float64(200000),
		}},
		{"proj3", map[string]interface{}{
			"projectId":           "proj3",
			"name":                "Finance Audit",
			"department":          "finance",
			security.MarkingField: []string{"ALPHA"},
			"budget":              float64(150000),
		}},
		{"proj4", map[string]interface{}{
			"projectId":           "proj4",
			"name":                "Joint Research",
			"department":          "engineering",
			security.MarkingField: []string{"ALPHA", "BETA"},
			"budget":              float64(300000),
		}},
		{"proj5", map[string]interface{}{
			"projectId":           "proj5",
			"name":                "Gamma Initiative",
			"department":          "finance",
			security.MarkingField: []string{"GAMMA"},
			"budget":              float64(50000),
		}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("project", d.pk, d.doc); err != nil {
			t.Fatalf("index %s: %v", d.pk, err)
		}
	}
	// Bleve commits are async; short settle window prevents stale segments.
	time.Sleep(200 * time.Millisecond)

	// --- users: 5 distinct attribute combos ---
	users := map[string]*auth.User{
		"admin": {
			ID:    "admin",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"department": "engineering",
				"markings":   []string{"ALPHA", "BETA"},
				"role":       "manager",
			},
		},
		"analyst": {
			ID:    "analyst",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"department": "engineering",
				"markings":   []string{"ALPHA"},
				"role":       "analyst",
			},
		},
		"finMgr": {
			ID:    "finMgr",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"department": "finance",
				"markings":   []string{"ALPHA", "BETA"},
				"role":       "manager",
			},
		},
		"intern": {
			ID:    "intern",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"department": "engineering",
				"markings":   []string{},
				"role":       "intern",
			},
		},
		"auditor": {
			ID:    "auditor",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"department": "engineering",
				"markings":   []string{"ALPHA", "BETA", "GAMMA"},
				"role":       "auditor",
			},
		},
	}

	// --- policy engine: row + marking + column ---
	engine := security.NewEngine()

	// Row-level policy: eq on department — user.department must match
	// object.department. This filters the object set per user.
	engine.SetPolicies(projectOT.RID, []security.Policy{
		{
			RID:           "ri.ontology.main.security-policy.p7-row",
			ObjectTypeRID: projectOT.RID,
			PolicyType:    security.PolicyTypeObject,
			Rules: []security.Rule{
				{Type: security.RuleTypeEq, UserAttr: "department", ObjectProperty: "department"},
			},
		},
		{
			RID:           "ri.ontology.main.security-policy.p7-column",
			ObjectTypeRID: projectOT.RID,
			PolicyType:    security.PolicyTypeProperty,
			Rules: []security.Rule{
				// Baseline: everyone sees projectId, name, department.
				{Properties: []string{"projectId", "name", "department"}},
				// Managers additionally see budget.
				{
					UserAttr:   "role",
					Values:     []string{"manager"},
					Properties: []string{"budget"},
				},
			},
		},
	})

	// Marking enforcement: subset semantics on _markings field.
	engine.SetMarkingsEnabled(projectOT.RID, true)

	// --- wire service ---
	svc := oss.NewService(repo, mgr, nil)
	svc.SetPolicyEngine(engine)

	// ===================================================================
	// Part 1: ListObjects — verify visible object set + visible fields
	// ===================================================================
	t.Run("ListObjects", func(t *testing.T) {
		cases := []struct {
			userName     string
			wantObjects  []string
			wantFields   []string // expected fields on each visible object
			wantNoBudget bool     // budget must NOT appear
		}{
			{
				userName:    "admin",
				wantObjects: []string{"proj1", "proj2", "proj4"},
				wantFields:  []string{"projectId", "name", "department", "budget"},
			},
			{
				userName:     "analyst",
				wantObjects:  []string{"proj1"},
				wantFields:   []string{"projectId", "name", "department"},
				wantNoBudget: true,
			},
			{
				userName:    "finMgr",
				wantObjects: []string{"proj3"},
				wantFields:  []string{"projectId", "name", "department", "budget"},
			},
			{
				userName:    "intern",
				wantObjects: []string{}, // no markings → nothing visible
			},
			{
				userName:     "auditor",
				wantObjects:  []string{"proj1", "proj2", "proj4"},
				wantFields:   []string{"projectId", "name", "department"},
				wantNoBudget: true,
			},
		}

		for _, c := range cases {
			t.Run(c.userName, func(t *testing.T) {
				user := users[c.userName]
				userCtx := auth.WithUser(ctx, user)
				page, err := svc.ListObjects(userCtx, oss.ListObjectsRequest{
					OntologyRID: ont.RID,
					ObjectType:  "project",
					PageSize:    50,
				})
				if err != nil {
					t.Fatalf("ListObjects: %v", err)
				}

				// Verify visible object set.
				gotPKs := extractPKs(t, page.Data)
				sort.Strings(gotPKs)
				wantPKs := append([]string(nil), c.wantObjects...)
				sort.Strings(wantPKs)

				if !slicesEqual(gotPKs, wantPKs) {
					t.Errorf("visible objects mismatch\n  got:  %v\n  want: %v", gotPKs, wantPKs)
				}

				// Verify visible fields on each object.
				if len(c.wantFields) > 0 {
					for _, obj := range page.Data {
						for _, f := range c.wantFields {
							if _, ok := obj.Properties[f]; !ok {
								t.Errorf("object %v missing expected field %q", obj.PrimaryKey, f)
							}
						}
						if c.wantNoBudget {
							if _, ok := obj.Properties["budget"]; ok {
								t.Errorf("object %v has budget but should not (column policy)", obj.PrimaryKey)
							}
						}
					}
				}
			})
		}
	})

	// ===================================================================
	// Part 2: SearchObjects — verify row+marking filter with where clause
	// ===================================================================
	t.Run("SearchObjects", func(t *testing.T) {
		// Search for objects where department=engineering. Without
		// policy, this would return proj1,proj2,proj4. With marking
		// filter the visible set narrows per user.
		cases := []struct {
			userName    string
			wantObjects []string
		}{
			// admin: engineering + markings=[ALPHA,BETA] → {proj1,proj2,proj4}
			{"admin", []string{"proj1", "proj2", "proj4"}},
			// analyst: engineering + markings=[ALPHA] → {proj1}
			{"analyst", []string{"proj1"}},
			// finMgr: dept row-filter=finance, but searching for engineering
			// → 0 results (row policy restricts to finance, search further
			// narrows to engineering → empty intersection)
			{"finMgr", []string{}},
			// intern: engineering but no markings → 0
			{"intern", []string{}},
			// auditor: engineering + all markings → {proj1,proj2,proj4}
			{"auditor", []string{"proj1", "proj2", "proj4"}},
		}

		for _, c := range cases {
			t.Run(c.userName, func(t *testing.T) {
				user := users[c.userName]
				userCtx := auth.WithUser(ctx, user)

				// Build a where clause that matches department=engineering.
				whereClause := mustParseWhere(t, `{"type":"eq","field":"department","value":"engineering"}`)

				page, err := svc.SearchObjects(userCtx, oss.SearchObjectsRequest{
					OntologyRID: ont.RID,
					ObjectType:  "project",
					Where:       whereClause,
					PageSize:    50,
				})
				if err != nil {
					t.Fatalf("SearchObjects: %v", err)
				}

				gotPKs := extractPKs(t, page.Data)
				sort.Strings(gotPKs)
				wantPKs := append([]string(nil), c.wantObjects...)
				sort.Strings(wantPKs)

				if !slicesEqual(gotPKs, wantPKs) {
					t.Errorf("search results mismatch\n  got:  %v\n  want: %v", gotPKs, wantPKs)
				}
			})
		}
	})

	// ===================================================================
	// Part 3: Aggregate — column policy gates budget field access
	// ===================================================================
	t.Run("Aggregate", func(t *testing.T) {
		// Wire up handler-level aggregation with column policy gate.
		filter := &propertyFilterAdapter{repo: repo, engine: engine}

		h := oss.NewHandler(svc)
		h.SetAggregation(aggregation.NewEngine(), mgr)
		h.SetPropertyFilterProvider(filter)

		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				scopedCtx := index.WithOntologyScope(req.Context(), ont.APIName)
				next.ServeHTTP(w, req.WithContext(scopedCtx))
			})
		})
		h.RegisterRoutes(r)

		doPost := func(t *testing.T, user *auth.User, body string) *httptest.ResponseRecorder {
			t.Helper()
			req := httptest.NewRequest("POST",
				"/api/v2/ontologies/"+ont.APIName+"/objects/project/aggregate",
				bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(auth.WithUser(req.Context(), user))
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			return rr
		}

		// Manager (admin) can aggregate sum(budget).
		t.Run("manager_sum_budget_allowed", func(t *testing.T) {
			body := `{"aggregation":[{"type":"sum","field":"budget","name":"totalBudget"}]}`
			rr := doPost(t, users["admin"], body)
			if rr.Code != http.StatusOK {
				t.Fatalf("manager sum(budget): status=%d body=%s", rr.Code, rr.Body.String())
			}
		})

		// Non-manager (analyst) is rejected on sum(budget) with 403.
		t.Run("analyst_sum_budget_rejected", func(t *testing.T) {
			body := `{"aggregation":[{"type":"sum","field":"budget","name":"totalBudget"}]}`
			rr := doPost(t, users["analyst"], body)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("analyst sum(budget): status=%d want=403 body=%s", rr.Code, rr.Body.String())
			}
			var apiErr struct {
				ErrorName  string            `json:"errorName"`
				Parameters map[string]string `json:"parameters"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if apiErr.ErrorName != "PropertyNotAccessible" {
				t.Errorf("errorName=%q want PropertyNotAccessible", apiErr.ErrorName)
			}
			if apiErr.Parameters["property"] != "budget" {
				t.Errorf("parameters.property=%q want budget", apiErr.Parameters["property"])
			}
		})

		// Auditor (non-manager) can count + groupBy department (allowed fields).
		t.Run("auditor_count_department_allowed", func(t *testing.T) {
			body := `{
				"groupBy":[{"type":"exact","field":"department"}],
				"aggregation":[{"type":"count","name":"c"}]
			}`
			rr := doPost(t, users["auditor"], body)
			if rr.Code != http.StatusOK {
				t.Fatalf("auditor count groupBy department: status=%d body=%s", rr.Code, rr.Body.String())
			}
		})

		// Auditor cannot groupBy budget (not in allow list).
		t.Run("auditor_groupby_budget_rejected", func(t *testing.T) {
			body := `{
				"groupBy":[{"type":"exact","field":"budget"}],
				"aggregation":[{"type":"count","name":"c"}]
			}`
			rr := doPost(t, users["auditor"], body)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("auditor groupBy budget: status=%d want=403 body=%s", rr.Code, rr.Body.String())
			}
		})
	})
}

// --- helpers ---

// propertyFilterAdapter mirrors cmd/server's adapter: ontology scope from
// context → GetOntology → GetObjectType → engine.AllowedProperties.
type propertyFilterAdapter struct {
	repo   oms.Repository
	engine *security.Engine
}

func (a *propertyFilterAdapter) AllowedProperties(ctx context.Context, objectType string) ([]string, error) {
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

func extractPKs(t *testing.T, objs []*oss.WireObject) []string {
	t.Helper()
	pks := make([]string, 0, len(objs))
	for _, o := range objs {
		pk, ok := o.PrimaryKey.(string)
		if !ok {
			t.Fatalf("non-string PK %T", o.PrimaryKey)
		}
		pks = append(pks, pk)
	}
	return pks
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustParseWhere(t *testing.T, raw string) *where.WhereClause {
	t.Helper()
	var clause where.WhereClause
	if err := json.Unmarshal([]byte(raw), &clause); err != nil {
		t.Fatalf("parse where: %v", err)
	}
	return &clause
}
