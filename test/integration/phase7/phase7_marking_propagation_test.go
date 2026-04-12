//go:build integration

package phase7_test

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

// TestMarkingPropagation is the US-071 acceptance test:
// a focused cross-US integration test that asserts marking AND (subset)
// semantics across 2 markings × 3 objects × 3 users.
//
// Marking semantics: user.markings ⊇ object._markings (user must hold
// ALL markings the object carries).
//
//	Markings: ALPHA, BETA
//
//	Objects:
//	  item1  _markings=[ALPHA]        — needs ALPHA
//	  item2  _markings=[BETA]         — needs BETA
//	  item3  _markings=[ALPHA,BETA]   — needs BOTH (AND semantics)
//
//	Users:
//	  alphaUser  markings=[ALPHA]        — sees item1 only
//	  betaUser   markings=[BETA]         — sees item2 only
//	  bothUser   markings=[ALPHA,BETA]   — sees item1, item2, item3
//
//	Visibility matrix (✓=visible, ✗=hidden):
//	                 item1(A)  item2(B)  item3(A,B)
//	  alphaUser(A)     ✓         ✗          ✗
//	  betaUser(B)      ✗         ✓          ✗
//	  bothUser(A,B)    ✓         ✓          ✓
func TestMarkingPropagation(t *testing.T) {
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
		APIName:     "marking_prop_p7",
		DisplayName: "Phase 7 Marking Propagation",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	itemOT := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "item",
		DisplayName: "Item",
		PrimaryKey:  "itemId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, itemOT); err != nil {
		t.Fatalf("create object type: %v", err)
	}

	// --- Bleve index ---
	props := []index.Property{
		{APIName: "itemId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "name", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	if _, err := mgr.EnsureIndex("item", props); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// --- seed 3 objects with distinct marking combinations ---
	docs := []struct {
		pk  string
		doc map[string]interface{}
	}{
		{"item1", map[string]interface{}{
			"itemId":              "item1",
			"name":                "Alpha Widget",
			security.MarkingField: []string{"ALPHA"},
		}},
		{"item2", map[string]interface{}{
			"itemId":              "item2",
			"name":                "Beta Gadget",
			security.MarkingField: []string{"BETA"},
		}},
		{"item3", map[string]interface{}{
			"itemId":              "item3",
			"name":                "Combined Device",
			security.MarkingField: []string{"ALPHA", "BETA"},
		}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("item", d.pk, d.doc); err != nil {
			t.Fatalf("index %s: %v", d.pk, err)
		}
	}
	// Bleve commits are async; short settle window.
	time.Sleep(200 * time.Millisecond)

	// --- 3 users with distinct marking sets ---
	users := map[string]*auth.User{
		"alphaUser": {
			ID:    "alphaUser",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"ALPHA"},
			},
		},
		"betaUser": {
			ID:    "betaUser",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"BETA"},
			},
		},
		"bothUser": {
			ID:    "bothUser",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"ALPHA", "BETA"},
			},
		},
	}

	// --- policy engine: marking-only (no row-level department filter) ---
	engine := security.NewEngine()
	engine.SetMarkingsEnabled(itemOT.RID, true)

	// --- wire service ---
	svc := oss.NewService(repo, mgr, nil)
	svc.SetPolicyEngine(engine)

	// ===================================================================
	// Part 1: ListObjects — verify 3×3 marking visibility matrix
	// ===================================================================
	t.Run("ListObjects_MarkingMatrix", func(t *testing.T) {
		cases := []struct {
			userName    string
			wantObjects []string
		}{
			// alphaUser [ALPHA] ⊇ item1 [ALPHA] ✓, ⊉ item2 [BETA] ✗, ⊉ item3 [A,B] ✗
			{"alphaUser", []string{"item1"}},
			// betaUser [BETA] ⊉ item1 [ALPHA] ✗, ⊇ item2 [BETA] ✓, ⊉ item3 [A,B] ✗
			{"betaUser", []string{"item2"}},
			// bothUser [ALPHA,BETA] ⊇ all three
			{"bothUser", []string{"item1", "item2", "item3"}},
		}

		for _, c := range cases {
			t.Run(c.userName, func(t *testing.T) {
				user := users[c.userName]
				userCtx := auth.WithUser(ctx, user)

				page, err := svc.ListObjects(userCtx, oss.ListObjectsRequest{
					OntologyRID: ont.RID,
					ObjectType:  "item",
					PageSize:    50,
				})
				if err != nil {
					t.Fatalf("ListObjects: %v", err)
				}

				gotPKs := extractPKs(t, page.Data)
				sort.Strings(gotPKs)
				wantPKs := append([]string(nil), c.wantObjects...)
				sort.Strings(wantPKs)

				if !slicesEqual(gotPKs, wantPKs) {
					t.Errorf("visible objects mismatch\n  got:  %v\n  want: %v", gotPKs, wantPKs)
				}
			})
		}
	})

	// ===================================================================
	// Part 2: SearchObjects — marking filter applies on top of search
	// ===================================================================
	t.Run("SearchObjects_MarkingMatrix", func(t *testing.T) {
		// Search without a where clause — marking filter alone restricts.
		cases := []struct {
			userName    string
			wantObjects []string
		}{
			{"alphaUser", []string{"item1"}},
			{"betaUser", []string{"item2"}},
			{"bothUser", []string{"item1", "item2", "item3"}},
		}

		for _, c := range cases {
			t.Run(c.userName, func(t *testing.T) {
				user := users[c.userName]
				userCtx := auth.WithUser(ctx, user)

				page, err := svc.SearchObjects(userCtx, oss.SearchObjectsRequest{
					OntologyRID: ont.RID,
					ObjectType:  "item",
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
	// Part 3: Verify AND semantics — object with BOTH markings is only
	// visible when user holds BOTH, not just one.
	// ===================================================================
	t.Run("ANDSemantics_DualMarkedObject", func(t *testing.T) {
		// item3 has [ALPHA, BETA] — only bothUser can see it.
		singleMarkingUsers := []string{"alphaUser", "betaUser"}
		for _, name := range singleMarkingUsers {
			t.Run(name+"_cannot_see_dual_marked", func(t *testing.T) {
				user := users[name]
				userCtx := auth.WithUser(ctx, user)

				page, err := svc.ListObjects(userCtx, oss.ListObjectsRequest{
					OntologyRID: ont.RID,
					ObjectType:  "item",
					PageSize:    50,
				})
				if err != nil {
					t.Fatalf("ListObjects: %v", err)
				}

				gotPKs := extractPKs(t, page.Data)
				for _, pk := range gotPKs {
					if pk == "item3" {
						t.Errorf("user %q should NOT see item3 (requires both ALPHA+BETA)", name)
					}
				}
			})
		}

		t.Run("bothUser_can_see_dual_marked", func(t *testing.T) {
			user := users["bothUser"]
			userCtx := auth.WithUser(ctx, user)

			page, err := svc.ListObjects(userCtx, oss.ListObjectsRequest{
				OntologyRID: ont.RID,
				ObjectType:  "item",
				PageSize:    50,
			})
			if err != nil {
				t.Fatalf("ListObjects: %v", err)
			}

			gotPKs := extractPKs(t, page.Data)
			found := false
			for _, pk := range gotPKs {
				if pk == "item3" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("bothUser should see item3 (has both ALPHA+BETA markings), got: %v", gotPKs)
			}
		})
	})
}
