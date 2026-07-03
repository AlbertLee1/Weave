package actions

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// TestBuildObjectEdits_ObjectVariants covers the Foundry ObjectEdit union for
// the three object-level edit kinds: CREATE→addObject, MODIFY→modifyObject,
// DELETE→deleteObject. deleteObject only appears in the detail array under
// returnEdits=ALL_V2_WITH_DELETIONS (osdk-ts #1271); under ALL the object is
// still counted but omitted from edits[].
func TestBuildObjectEdits_ObjectVariants(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1"},
		{Type: funnel.EditTypeModify, ObjectType: "Employee", PrimaryKey: "emp-2"},
		{Type: funnel.EditTypeDelete, ObjectType: "Employee", PrimaryKey: "emp-3"},
	}

	t.Run("ALL omits deleteObject variant", func(t *testing.T) {
		got := buildObjectEdits(edits, false)
		if len(got) != 2 {
			t.Fatalf("ALL: expected 2 detail edits (add+modify), got %d: %+v", len(got), got)
		}
		if got[0].Type != "addObject" || got[0].PrimaryKey != "emp-1" || got[0].ObjectType != "Employee" {
			t.Errorf("ALL[0] = %+v, want addObject/emp-1/Employee", got[0])
		}
		if got[1].Type != "modifyObject" || got[1].PrimaryKey != "emp-2" {
			t.Errorf("ALL[1] = %+v, want modifyObject/emp-2", got[1])
		}
		for _, e := range got {
			if e.Type == "deleteObject" {
				t.Errorf("ALL must not include deleteObject variant: %+v", got)
			}
		}
	})

	t.Run("ALL_V2_WITH_DELETIONS includes deleteObject variant", func(t *testing.T) {
		got := buildObjectEdits(edits, true)
		if len(got) != 3 {
			t.Fatalf("V2: expected 3 detail edits, got %d: %+v", len(got), got)
		}
		if got[2].Type != "deleteObject" || got[2].PrimaryKey != "emp-3" || got[2].ObjectType != "Employee" {
			t.Errorf("V2[2] = %+v, want deleteObject/emp-3/Employee", got[2])
		}
	})
}

// TestBuildObjectEdits_LinkVariants covers the Foundry addLink / deleteLink
// union: linkTypeApiNameAtoB / BtoA plus aSide / bSide LinkSideObject
// descriptors. deleteLink follows the same ALL vs V2 deletion gating.
func TestBuildObjectEdits_LinkVariants(t *testing.T) {
	edits := []funnel.Edit{
		{
			Type:                 funnel.EditTypeLinkCreate,
			PrimaryKey:           "emp-1",
			TargetPrimaryKey:     "dept-9",
			LinkTypeRID:          "ri.ontology.main.link-type.worksIn",
			LinkTypeAPINameAtoB:  "worksIn",
			LinkTypeAPINameBtoA:  "employees",
			LinkSourceObjectType: "Employee",
			LinkTargetObjectType: "Department",
		},
		{
			Type:                 funnel.EditTypeLinkDelete,
			PrimaryKey:           "emp-2",
			TargetPrimaryKey:     "dept-8",
			LinkTypeRID:          "ri.ontology.main.link-type.worksIn",
			LinkTypeAPINameAtoB:  "worksIn",
			LinkTypeAPINameBtoA:  "employees",
			LinkSourceObjectType: "Employee",
			LinkTargetObjectType: "Department",
		},
	}

	t.Run("ALL renders addLink, omits deleteLink", func(t *testing.T) {
		got := buildObjectEdits(edits, false)
		if len(got) != 1 {
			t.Fatalf("ALL: expected 1 detail edit (addLink), got %d: %+v", len(got), got)
		}
		e := got[0]
		if e.Type != "addLink" {
			t.Fatalf("ALL[0].Type = %q, want addLink", e.Type)
		}
		if e.LinkTypeAPINameAtoB != "worksIn" || e.LinkTypeAPINameBtoA != "employees" {
			t.Errorf("addLink names = %q/%q, want worksIn/employees", e.LinkTypeAPINameAtoB, e.LinkTypeAPINameBtoA)
		}
		if e.ASideObject == nil || e.ASideObject.PrimaryKey != "emp-1" || e.ASideObject.ObjectType != "Employee" {
			t.Errorf("addLink aSide = %+v, want emp-1/Employee", e.ASideObject)
		}
		if e.BSideObject == nil || e.BSideObject.PrimaryKey != "dept-9" || e.BSideObject.ObjectType != "Department" {
			t.Errorf("addLink bSide = %+v, want dept-9/Department", e.BSideObject)
		}
	})

	t.Run("ALL_V2_WITH_DELETIONS renders deleteLink too", func(t *testing.T) {
		got := buildObjectEdits(edits, true)
		if len(got) != 2 {
			t.Fatalf("V2: expected 2 detail edits, got %d: %+v", len(got), got)
		}
		if got[1].Type != "deleteLink" || got[1].ASideObject == nil || got[1].ASideObject.PrimaryKey != "emp-2" {
			t.Errorf("V2[1] = %+v, want deleteLink aSide emp-2", got[1])
		}
	})
}

// TestResolveLinkEdits_EnrichesFoundryMetadata locks the executor-level
// enrichment: resolveLinkEdits must stamp linkTypeApiNameAtoB (forward),
// linkTypeApiNameBtoA (inverse partner), and the source/target object types
// onto a LINK_CREATE edit BEFORE it overwrites LinkTypeRID with the resolved
// RID (gap #2 — the forward api name would otherwise be lost).
func TestResolveLinkEdits_EnrichesFoundryMetadata(t *testing.T) {
	repo := &mockOmsRepo{
		linkTypesByAPIName: map[string]*oms.LinkType{
			"worksIn": {
				RID:              "ri.ontology.main.link-type.worksIn",
				APIName:          "worksIn",
				SourceObjectType: "Employee",
				TargetObjectType: "Department",
				InverseLinkRID:   "ri.ontology.main.link-type.employees",
			},
		},
		linkTypesByRID: map[string]*oms.LinkType{
			"ri.ontology.main.link-type.employees": {
				RID:     "ri.ontology.main.link-type.employees",
				APIName: "employees",
			},
		},
	}
	exec := NewExecutor(repo, nil)

	edits := []funnel.Edit{
		{
			Type:             funnel.EditTypeLinkCreate,
			PrimaryKey:       "emp-1",
			TargetPrimaryKey: "dept-9",
			LinkTypeRID:      "worksIn", // api name pre-resolution
		},
	}
	exec.resolveLinkEdits(context.Background(), "ont-1", edits)

	e := edits[0]
	if e.LinkTypeRID != "ri.ontology.main.link-type.worksIn" {
		t.Errorf("LinkTypeRID = %q, want resolved RID", e.LinkTypeRID)
	}
	if e.LinkTypeAPINameAtoB != "worksIn" {
		t.Errorf("LinkTypeAPINameAtoB = %q, want worksIn", e.LinkTypeAPINameAtoB)
	}
	if e.LinkTypeAPINameBtoA != "employees" {
		t.Errorf("LinkTypeAPINameBtoA = %q, want employees (inverse partner)", e.LinkTypeAPINameBtoA)
	}
	if e.LinkSourceObjectType != "Employee" || e.LinkTargetObjectType != "Department" {
		t.Errorf("source/target = %q/%q, want Employee/Department", e.LinkSourceObjectType, e.LinkTargetObjectType)
	}
}
