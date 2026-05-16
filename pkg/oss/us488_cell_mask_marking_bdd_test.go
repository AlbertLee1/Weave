package oss_test

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/oss"
)

// US-488 — BDD: cell-mask CEL expressions can reference a top-level
// `marking` binding sourced from the row's reserved __markings field.
//
//	Given an Employee object indexed with __markings=["PII","INTERNAL"]
//	And a cell mask on (employee, empPII, name) with expression
//	  `"PII" in marking && !("auditors" in user.roles)`
//	When a non-auditor viewer reads the row via svc.ListObjects
//	Then employee.name is REDACTed
//	But the unmarked rows (emp1, emp2, emp3) are returned clear
//	And the auditor sees the PII row's name in clear text.
func TestBDD_US488_CellMaskMarkingBinding_EndToEnd(t *testing.T) {
	svc, mgr, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	// Seed an additional doc that carries the PII marking on the row.
	piiDoc := map[string]interface{}{
		"employeeId":       "empPII",
		"name":             "dora",
		"age":              float64(45),
		"active":           true,
		"deptId":           "d3",
		auth.MarkingsField: []interface{}{"PII", "INTERNAL"},
	}
	if err := mgr.IndexDocument("employee", "empPII", piiDoc); err != nil {
		t.Fatalf("IndexDocument empPII: %v", err)
	}
	// Let Bleve settle alongside the existing fixture.
	time.Sleep(200 * time.Millisecond)

	store := cellsec.NewMemoryStore()
	if err := store.Create(ctx, &cellsec.CellMask{
		RID:             "ri.cellsec.main.cell-mask.empPII-marking",
		ObjectTypeRID:   ot.RID,
		PrimaryKey:      "empPII",
		PropertyAPIName: "name",
		MaskStrategy:    masking.MaskStrategyRedact,
		Expression:      `"PII" in marking && !("auditors" in user.roles)`,
	}); err != nil {
		t.Fatalf("seed cell mask: %v", err)
	}
	engine := cellsec.New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	svc.SetCellMaskEngine(engine)

	collectByPK := func(t *testing.T, page *oss.ObjectPage) map[string]map[string]interface{} {
		t.Helper()
		out := make(map[string]map[string]interface{})
		for _, o := range page.Data {
			pk, _ := o.PrimaryKey.(string)
			out[pk] = o.Properties
		}
		return out
	}

	// Given: viewer (non-auditor) reads the listing.
	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	ctxV := auth.WithUser(ctx, viewer)
	page, err := svc.ListObjects(ctxV, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PageSize:    50,
	})
	if err != nil {
		t.Fatalf("ListObjects viewer: %v", err)
	}
	byPK := collectByPK(t, page)
	// Then: PII row's name is REDACTed for the viewer.
	if got, ok := byPK["empPII"]; !ok {
		t.Fatalf("expected empPII in viewer's page, got keys=%v", keys(byPK))
	} else if got["name"] != "***" {
		t.Fatalf("expected empPII.name=*** for viewer (PII marking + non-auditor), got %v", got["name"])
	}
	// But: unmarked rows stay clear (no markings → predicate false).
	if got, _ := byPK["emp1"]["name"].(string); got != "alice" {
		t.Fatalf("expected emp1.name=alice (unmarked row), got %v", got)
	}
	if got, _ := byPK["emp2"]["name"].(string); got != "bob" {
		t.Fatalf("expected emp2.name=bob (unmarked row), got %v", got)
	}

	// When: auditor reads the same listing.
	auditor := &auth.User{ID: "u:auditor", Roles: []string{"auditors"}}
	ctxA := auth.WithUser(ctx, auditor)
	page, err = svc.ListObjects(ctxA, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PageSize:    50,
	})
	if err != nil {
		t.Fatalf("ListObjects auditor: %v", err)
	}
	byPK = collectByPK(t, page)
	// Then: auditor sees the PII row's name in clear text — the role gate in
	// the predicate (`!("auditors" in user.roles)`) suppresses the mask.
	if got := byPK["empPII"]; got == nil {
		t.Fatalf("expected empPII in auditor's page")
	} else if got["name"] != "dora" {
		t.Fatalf("expected auditor to see empPII.name=dora (clear), got %v", got["name"])
	}
}

// US-488 negative control — a cell mask whose predicate references the
// `marking` binding does NOT fire on rows that lack the targeted marking
// label, even when the property name is the same. Proves the binding is
// sourced from row data, not hard-coded to fire on every cell.
func TestBDD_US488_CellMaskMarking_NegativeControl_NoMarkingNoMask(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := cellsec.NewMemoryStore()
	if err := store.Create(ctx, &cellsec.CellMask{
		RID:             "ri.cellsec.main.cell-mask.emp1-marking",
		ObjectTypeRID:   ot.RID,
		PrimaryKey:      "emp1",
		PropertyAPIName: "name",
		MaskStrategy:    masking.MaskStrategyRedact,
		Expression:      `"PII" in marking`,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	engine := cellsec.New(store, nil)
	_ = engine.Reload(ctx)
	svc.SetCellMaskEngine(engine)

	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	ctxV := auth.WithUser(ctx, viewer)
	obj, err := svc.GetObject(ctxV, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	})
	if err != nil {
		t.Fatalf("GetObject emp1: %v", err)
	}
	// emp1 has no __markings — the `"PII" in marking` predicate is false, so
	// the cell mask must not fire and `name` stays clear.
	if obj.Properties["name"] != "alice" {
		t.Fatalf("expected emp1.name=alice when row has no marking, got %v", obj.Properties["name"])
	}
}

func keys(m map[string]map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
