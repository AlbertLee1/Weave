//go:build integration

package integration_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/rid"
	"github.com/liyang/weave/pkg/security"
)

// TestPolicyMatrix_RowLevel_NineCellCoverage is the US-047 acceptance
// scenario: a 3-user × 3-policy × 5-object matrix is exercised end-to-end
// against a real PG-backed OMS + Bleve index through oss.ServiceImpl so
// row-level policy filtering is proven against actual query execution
// (not just unit-level engine compilation).
//
// The 5-object fixture is shared across all three policy regimes so the
// engine's Evaluate output drives the Bleve query through ListObjects and
// the per-user visible set assertions cover every matrix cell. Each
// policy regime calls SetPolicies to replace the active rule set on the
// single employee ObjectType — bumping the engine version invalidates any
// cached compilation from the prior cell.
//
//	Policy A (eq on deptId):
//	  alice(d1) → {emp1,emp2}; bob(d2) → {emp3,emp4}; carol(d3) → {emp5}
//	Policy B (in on regions):
//	  alice(us) → {emp1,emp2,emp4}; bob(us,eu) → {emp1,emp2,emp3,emp4};
//	  carol(apac) → {emp4,emp5}
//	Policy C (markingSubset on classification):
//	  alice(public,internal) → {emp1,emp2,emp3,emp5}; bob(public) → {emp1,emp5};
//	  carol(public,internal,restricted) → all 5
func TestPolicyMatrix_RowLevel_NineCellCoverage(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	// Ontology + single ObjectType. Only the ObjectType.RID is required by
	// the policy engine; Property rows are unnecessary because the Bleve
	// mapping is driven by EnsureIndex below, not BuildMapping from OMS.
	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "policy_matrix",
		DisplayName: "Policy Matrix",
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

	// All policy-evaluated fields are indexed as not_analyzed keywords so
	// compileRule's TermQuery payloads (eq / in / markingSubset) match
	// exactly without snowball stemming. Learnings from Phase 6: default
	// English analyzer + TermQuery is a silent zero-hit landmine.
	props := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "name", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "deptId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "regions", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "classification", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
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
			"employeeId":     "emp1",
			"name":           "alice",
			"deptId":         "d1",
			"regions":        []string{"us", "eu"},
			"classification": "public",
		}},
		{"emp2", map[string]interface{}{
			"employeeId":     "emp2",
			"name":           "bob",
			"deptId":         "d1",
			"regions":        []string{"us"},
			"classification": "internal",
		}},
		{"emp3", map[string]interface{}{
			"employeeId":     "emp3",
			"name":           "carol",
			"deptId":         "d2",
			"regions":        []string{"eu"},
			"classification": "internal",
		}},
		{"emp4", map[string]interface{}{
			"employeeId":     "emp4",
			"name":           "dave",
			"deptId":         "d2",
			"regions":        []string{"us", "apac"},
			"classification": "restricted",
		}},
		{"emp5", map[string]interface{}{
			"employeeId":     "emp5",
			"name":           "erin",
			"deptId":         "d3",
			"regions":        []string{"apac"},
			"classification": "public",
		}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.pk, d.doc); err != nil {
			t.Fatalf("index %s: %v", d.pk, err)
		}
	}
	// Bleve commits are async; a short settle window prevents the first
	// ListObjects call from seeing a stale segment count.
	time.Sleep(200 * time.Millisecond)

	// Users spanning every dept / region / marking axis exercised below.
	// Attribute keys must match the rule UserAttr values (plus the fixed
	// "markings" key enforced by RuleTypeMarkingSubset).
	users := map[string]*auth.User{
		"alice": {
			ID:    "alice",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"deptId":   "d1",
				"regions":  []string{"us"},
				"markings": []string{"public", "internal"},
			},
		},
		"bob": {
			ID:    "bob",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"deptId":   "d2",
				"regions":  []string{"us", "eu"},
				"markings": []string{"public"},
			},
		},
		"carol": {
			ID:    "carol",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"deptId":   "d3",
				"regions":  []string{"apac"},
				"markings": []string{"public", "internal", "restricted"},
			},
		},
	}

	svc := oss.NewService(repo, mgr, nil)
	engine := security.NewEngine()
	svc.SetPolicyEngine(engine)

	type cell struct {
		policy  string
		user    string
		visible []string
	}
	matrix := []cell{
		// Policy A — eq on deptId.
		{"eq", "alice", []string{"emp1", "emp2"}},
		{"eq", "bob", []string{"emp3", "emp4"}},
		{"eq", "carol", []string{"emp5"}},
		// Policy B — in on regions.
		{"in", "alice", []string{"emp1", "emp2", "emp4"}},
		{"in", "bob", []string{"emp1", "emp2", "emp3", "emp4"}},
		{"in", "carol", []string{"emp4", "emp5"}},
		// Policy C — markingSubset on classification.
		{"markingSubset", "alice", []string{"emp1", "emp2", "emp3", "emp5"}},
		{"markingSubset", "bob", []string{"emp1", "emp5"}},
		{"markingSubset", "carol", []string{"emp1", "emp2", "emp3", "emp4", "emp5"}},
	}

	policyRules := map[string][]security.Rule{
		"eq": {
			{Type: security.RuleTypeEq, UserAttr: "deptId", ObjectProperty: "deptId"},
		},
		"in": {
			{Type: security.RuleTypeIn, UserAttr: "regions", ObjectProperty: "regions"},
		},
		"markingSubset": {
			{Type: security.RuleTypeMarkingSubset, ObjectProperty: "classification"},
		},
	}

	currentPolicy := ""
	for _, c := range matrix {
		if c.policy != currentPolicy {
			engine.SetPolicies(employeeOT.RID, []security.Policy{{
				RID:           "ri.ontology.main.security-policy.us047-" + c.policy,
				ObjectTypeRID: employeeOT.RID,
				PolicyType:    security.PolicyTypeObject,
				Rules:         policyRules[c.policy],
			}})
			currentPolicy = c.policy
		}

		user := users[c.user]
		cellCtx := auth.WithUser(ctx, user)
		page, err := svc.ListObjects(cellCtx, oss.ListObjectsRequest{
			OntologyRID: ont.RID,
			ObjectType:  "employee",
			PageSize:    50,
		})
		if err != nil {
			t.Fatalf("policy=%s user=%s ListObjects: %v", c.policy, c.user, err)
		}

		got := make([]string, 0, len(page.Data))
		for _, o := range page.Data {
			pk, ok := o.PrimaryKey.(string)
			if !ok {
				t.Fatalf("policy=%s user=%s: non-string PK %T", c.policy, c.user, o.PrimaryKey)
			}
			got = append(got, pk)
		}
		sort.Strings(got)
		want := append([]string(nil), c.visible...)
		sort.Strings(want)

		if !equalStringSlices(got, want) {
			t.Errorf("policy=%s user=%s: visible set mismatch\n  got:  %v\n  want: %v",
				c.policy, c.user, got, want)
		}
	}
}

func equalStringSlices(a, b []string) bool {
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
