package oss_test

import (
	"context"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/oss"
)

// TestCellMask_TargetsSpecificInstance verifies US-258: a cell mask that
// targets (emp1, name) only affects the `name` property of that single
// instance. Other rows (emp2, emp3) return their clear `name` value even
// though they are the same ObjectType.
func TestCellMask_TargetsSpecificInstance(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := cellsec.NewMemoryStore()
	// Mask ONLY emp1.name for non-finance callers.
	_ = store.Create(ctx, &cellsec.CellMask{
		RID:             "ri.cellsec.main.cell-mask.emp1-name",
		ObjectTypeRID:   ot.RID,
		PrimaryKey:      "emp1",
		PropertyAPIName: "name",
		MaskRule:        masking.MaskRuleRedact,
		AppliesTo:       masking.AppliesTo{Roles: []string{"finance"}},
	})
	engine := cellsec.New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	svc.SetCellMaskEngine(engine)

	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	ctxV := auth.WithUser(ctx, viewer)
	page, err := svc.ListObjects(ctxV, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects viewer: %v", err)
	}
	byPK := make(map[string]map[string]interface{})
	for _, o := range page.Data {
		pk, _ := o.PrimaryKey.(string)
		byPK[pk] = o.Properties
	}
	if got := byPK["emp1"]["name"]; got != "[REDACTED]" {
		t.Fatalf("expected emp1.name=[REDACTED] for viewer, got %v", got)
	}
	// Other rows must be unaffected — cell mask is targeted.
	if got, _ := byPK["emp2"]["name"].(string); got != "bob" {
		t.Fatalf("emp2.name should stay clear, got %v", got)
	}
	if got, _ := byPK["emp3"]["name"].(string); got != "charlie" {
		t.Fatalf("emp3.name should stay clear, got %v", got)
	}

	// Finance caller bypasses the mask (in the allow list).
	fin := &auth.User{ID: "u:fin", Roles: []string{"finance"}}
	ctxFin := auth.WithUser(ctx, fin)
	page, err = svc.ListObjects(ctxFin, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects fin: %v", err)
	}
	for _, o := range page.Data {
		pk, _ := o.PrimaryKey.(string)
		name, _ := o.Properties["name"].(string)
		if pk == "emp1" && name != "alice" {
			t.Fatalf("finance should see clear emp1.name=alice, got %v", name)
		}
	}

	// Admin bypass.
	admin := &auth.User{ID: "u:admin", Roles: []string{auth.RoleAdmin}}
	ctxAdmin := auth.WithUser(ctx, admin)
	page, err = svc.ListObjects(ctxAdmin, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects admin: %v", err)
	}
	for _, o := range page.Data {
		pk, _ := o.PrimaryKey.(string)
		name, _ := o.Properties["name"].(string)
		if pk == "emp1" && name != "alice" {
			t.Fatalf("admin should see clear emp1.name=alice, got %v", name)
		}
	}
}

// TestCellMask_AppliedOnGetObject verifies the masked cell survives the
// single-object GetObject read path.
func TestCellMask_AppliedOnGetObject(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := cellsec.NewMemoryStore()
	_ = store.Create(ctx, &cellsec.CellMask{
		RID:             "ri.cellsec.main.cell-mask.emp1-age-hash",
		ObjectTypeRID:   ot.RID,
		PrimaryKey:      "emp1",
		PropertyAPIName: "age",
		MaskRule:        masking.MaskRuleHash,
		AppliesTo:       masking.AppliesTo{}, // apply to everyone (admin bypasses)
	})
	engine := cellsec.New(store, nil)
	_ = engine.Reload(ctx)
	svc.SetCellMaskEngine(engine)

	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	ctxV := auth.WithUser(ctx, viewer)

	// emp1 age is hashed.
	obj, err := svc.GetObject(ctxV, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	})
	if err != nil {
		t.Fatalf("GetObject emp1: %v", err)
	}
	ageStr, ok := obj.Properties["age"].(string)
	if !ok {
		t.Fatalf("expected hashed age string, got %T %v", obj.Properties["age"], obj.Properties["age"])
	}
	if !strings.HasPrefix(ageStr, "sha256:") {
		t.Fatalf("expected emp1.age prefixed with sha256:, got %v", ageStr)
	}
	if obj.Properties["name"] != "alice" {
		t.Fatalf("expected name=alice untouched, got %v", obj.Properties["name"])
	}

	// emp2 stays clear (cell mask targets only emp1).
	obj, err = svc.GetObject(ctxV, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp2",
	})
	if err != nil {
		t.Fatalf("GetObject emp2: %v", err)
	}
	if _, ok := obj.Properties["age"].(string); ok {
		t.Fatalf("expected emp2.age numeric (untouched), got string %v", obj.Properties["age"])
	}
}

// TestCellMask_ComposesWithColumnMask verifies the ORDER invariant: column
// masking runs first, then cell masking. When both target the same property
// on the same row, the LAST write (cell) wins — so cell-level rules can
// sharpen a column-wide partial to a row-specific redact.
func TestCellMask_ComposesWithColumnMask(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	// Column mask: all employees' `name` is PARTIAL-masked for viewers.
	colStore := masking.NewMemoryStore()
	_ = colStore.Create(ctx, &masking.ColumnMask{
		RID:             "ri.masking.main.column-mask.name-partial",
		ObjectTypeRID:   ot.RID,
		PropertyAPIName: "name",
		MaskRule:        masking.MaskRulePartial,
		AppliesTo:       masking.AppliesTo{}, // apply to everyone
	})
	colEngine := masking.New(colStore, nil)
	_ = colEngine.Reload(ctx)
	svc.SetColumnMaskEngine(colEngine)

	// Cell mask: emp1's `name` is REDACTED (sharper rule for the sensitive row).
	cellStore := cellsec.NewMemoryStore()
	_ = cellStore.Create(ctx, &cellsec.CellMask{
		RID:             "ri.cellsec.main.cell-mask.emp1-name-redact",
		ObjectTypeRID:   ot.RID,
		PrimaryKey:      "emp1",
		PropertyAPIName: "name",
		MaskRule:        masking.MaskRuleRedact,
		AppliesTo:       masking.AppliesTo{},
	})
	cellEngine := cellsec.New(cellStore, nil)
	_ = cellEngine.Reload(ctx)
	svc.SetCellMaskEngine(cellEngine)

	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	ctxV := auth.WithUser(ctx, viewer)

	page, err := svc.ListObjects(ctxV, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	byPK := make(map[string]map[string]interface{})
	for _, o := range page.Data {
		pk, _ := o.PrimaryKey.(string)
		byPK[pk] = o.Properties
	}
	// emp1: cell mask wins (REDACT fully replaces the partial-masked value).
	if got := byPK["emp1"]["name"]; got != "[REDACTED]" {
		t.Fatalf("expected emp1.name=[REDACTED] (cell wins), got %v", got)
	}
	// emp2: only column mask applies (partial "b*b" for "bob" → "b*b").
	if got, _ := byPK["emp2"]["name"].(string); got != "b*b" {
		t.Fatalf("expected emp2.name partial-mask 'b*b', got %v", got)
	}
	// emp3: only column mask applies (partial for "charlie" → "c*****e").
	if got, _ := byPK["emp3"]["name"].(string); got != "c*****e" {
		t.Fatalf("expected emp3.name partial-mask 'c*****e', got %v", got)
	}
}
