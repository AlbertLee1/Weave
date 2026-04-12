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

// TestMarkingE2E_ThreeUsersThreeObjects is the US-052 acceptance scenario:
// Foundry-style mandatory access control (subset / AND semantics) is proven
// end-to-end against a real PG-backed OMS + Bleve index through
// oss.ServiceImpl.ListObjects.
//
// Seed matrix (3 users × 3 objects):
//
//	                     ACME        ACME2      ACME+ACME2
//	alice   (ACME)        ✓            ✗            ✗
//	bob     (ACME2)       ✗            ✓            ✗
//	carol   (ACME+ACME2)  ✓            ✓            ✓
//
// The third column is the decisive test — should-terms semantics alone
// would leak docACME2only to alice and docACMEonly to bob, so US-052 proves
// the engine enforces SUBSET (user.markings ⊇ object.markings), not simple
// overlap. Un-marked objects (len(_markings)==0) would stay public under
// EvaluateMarkings's empty-input rule; none are seeded here because the
// matrix is explicitly about marked objects.
func TestMarkingE2E_ThreeUsersThreeObjects(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "marking_e2e",
		DisplayName: "Marking E2E",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	documentOT := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "document",
		DisplayName: "Document",
		PrimaryKey:  "documentId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, documentOT); err != nil {
		t.Fatalf("create object type: %v", err)
	}

	// `_markings` is a reserved keyword field on every ObjectType index
	// (pkg/index.BuildMapping hard-reserves it under MarkingsField), so
	// schema-authored properties only need to cover documentId + title.
	// Both stay not_analyzed so the test's exact-match expectations do not
	// depend on stemming behaviour.
	props := []index.Property{
		{APIName: "documentId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "title", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	if _, err := mgr.EnsureIndex("document", props); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	docs := []struct {
		pk       string
		title    string
		markings []string
	}{
		{"docACMEonly", "Acme Proposal", []string{"ACME"}},
		{"docACME2only", "Acme2 Whitepaper", []string{"ACME2"}},
		{"docBothRequired", "Joint Venture", []string{"ACME", "ACME2"}},
	}
	for _, d := range docs {
		doc := map[string]interface{}{
			"documentId":          d.pk,
			"title":               d.title,
			security.MarkingField: d.markings,
		}
		if err := mgr.IndexDocument("document", d.pk, doc); err != nil {
			t.Fatalf("index %s: %v", d.pk, err)
		}
	}
	// Short settle window so the first ListObjects call sees the freshly
	// committed segment — matches the policy_matrix_test.go pattern.
	time.Sleep(200 * time.Millisecond)

	// Users — attribute keys must match the fixed
	// `security.userMarkingsKey` ("markings") that RuleTypeMarkingSubset
	// reads. alice / bob / carol mirror the three rows in the matrix.
	users := map[string]*auth.User{
		"alice": {
			ID:    "alice",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"ACME"},
			},
		},
		"bob": {
			ID:    "bob",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"ACME2"},
			},
		},
		"carol": {
			ID:    "carol",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"ACME", "ACME2"},
			},
		},
	}

	svc := oss.NewService(repo, mgr, nil)
	engine := security.NewEngine()
	svc.SetPolicyEngine(engine)
	// Opt the document ObjectType into marking enforcement. The engine
	// synthesises a marking-subset clause on top of whatever explicit rules
	// are attached; none are attached here so the auto clause is the only
	// filter in play.
	engine.SetMarkingsEnabled(documentOT.RID, true)

	cases := []struct {
		user    string
		visible []string
	}{
		{"alice", []string{"docACMEonly"}},
		{"bob", []string{"docACME2only"}},
		{"carol", []string{"docACME2only", "docACMEonly", "docBothRequired"}},
	}

	for _, c := range cases {
		user := users[c.user]
		cellCtx := auth.WithUser(ctx, user)
		page, err := svc.ListObjects(cellCtx, oss.ListObjectsRequest{
			OntologyRID: ont.RID,
			ObjectType:  "document",
			PageSize:    50,
		})
		if err != nil {
			t.Fatalf("user=%s ListObjects: %v", c.user, err)
		}

		got := make([]string, 0, len(page.Data))
		for _, o := range page.Data {
			pk, ok := o.PrimaryKey.(string)
			if !ok {
				t.Fatalf("user=%s: non-string PK %T", c.user, o.PrimaryKey)
			}
			got = append(got, pk)
		}
		sort.Strings(got)
		want := append([]string(nil), c.visible...)
		sort.Strings(want)

		if !equalPKSets(got, want) {
			t.Errorf("user=%s: visible set mismatch\n  got:  %v\n  want: %v",
				c.user, got, want)
		}
	}
}

func equalPKSets(a, b []string) bool {
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
