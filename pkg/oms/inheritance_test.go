package oms_test

import (
	"context"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-212 Object Type Inheritance: ResolveInheritedObjectType walks the
// `extends_rid` chain, merging parent properties + outgoing links into the
// child. Child entries override matching api_name on parents. Cycle in the
// chain returns ErrInheritanceCycle.

func TestResolveInheritedObjectType_NoParent(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.standalone", APIName: "standalone", DisplayName: "Standalone", Status: "ACTIVE", Visibility: "NORMAL"},
	}
	repo.properties = []oms.Property{
		{RID: "ri.p.1", ObjectTypeRID: "ri.ot.standalone", APIName: "name", BaseType: "string"},
	}
	resolved, err := oms.ResolveInheritedObjectType(context.Background(), repo, &repo.objectTypes[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ExtendsChain != nil {
		t.Errorf("expected nil ExtendsChain for root type, got %v", resolved.ExtendsChain)
	}
	if len(resolved.Properties) != 1 || resolved.Properties[0].APIName != "name" {
		t.Errorf("expected props=[name], got %v", resolved.Properties)
	}
}

func TestResolveInheritedObjectType_SingleParent_MergesProperties(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.parent", APIName: "person", DisplayName: "Person"},
		{RID: "ri.ot.child", APIName: "employee", DisplayName: "Employee", ExtendsRID: "ri.ot.parent"},
	}
	repo.properties = []oms.Property{
		// Parent owns name + age
		{RID: "ri.p.parent.name", ObjectTypeRID: "ri.ot.parent", APIName: "name", BaseType: "string"},
		{RID: "ri.p.parent.age", ObjectTypeRID: "ri.ot.parent", APIName: "age", BaseType: "integer"},
		// Child adds salary
		{RID: "ri.p.child.salary", ObjectTypeRID: "ri.ot.child", APIName: "salary", BaseType: "double"},
	}
	resolved, err := oms.ResolveInheritedObjectType(context.Background(), repo, &repo.objectTypes[1])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := propAPINames(resolved.Properties); !containsAll(got, []string{"name", "age", "salary"}) {
		t.Errorf("expected merged props to include name/age/salary, got %v", got)
	}
	if len(resolved.ExtendsChain) != 1 || resolved.ExtendsChain[0] != "ri.ot.parent" {
		t.Errorf("expected ExtendsChain=[ri.ot.parent], got %v", resolved.ExtendsChain)
	}
}

func TestResolveInheritedObjectType_ChildOverridesParentProperty(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.parent", APIName: "person"},
		{RID: "ri.ot.child", APIName: "employee", ExtendsRID: "ri.ot.parent"},
	}
	repo.properties = []oms.Property{
		{RID: "ri.p.parent.name", ObjectTypeRID: "ri.ot.parent", APIName: "name", BaseType: "string", DisplayName: "Person Name"},
		{RID: "ri.p.child.name", ObjectTypeRID: "ri.ot.child", APIName: "name", BaseType: "string", DisplayName: "Employee Name"},
	}
	resolved, err := oms.ResolveInheritedObjectType(context.Background(), repo, &repo.objectTypes[1])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved.Properties) != 1 {
		t.Fatalf("expected 1 property after override, got %d (%v)", len(resolved.Properties), resolved.Properties)
	}
	if resolved.Properties[0].DisplayName != "Employee Name" {
		t.Errorf("child override missing: got DisplayName=%q, want 'Employee Name'", resolved.Properties[0].DisplayName)
	}
	if resolved.Properties[0].ObjectTypeRID != "ri.ot.child" {
		t.Errorf("child override should win: got ObjectTypeRID=%q, want ri.ot.child", resolved.Properties[0].ObjectTypeRID)
	}
}

func TestResolveInheritedObjectType_MultiLevelChain(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.living", APIName: "living"},
		{RID: "ri.ot.person", APIName: "person", ExtendsRID: "ri.ot.living"},
		{RID: "ri.ot.employee", APIName: "employee", ExtendsRID: "ri.ot.person"},
	}
	repo.properties = []oms.Property{
		{RID: "ri.p.l", ObjectTypeRID: "ri.ot.living", APIName: "isAlive", BaseType: "boolean"},
		{RID: "ri.p.p", ObjectTypeRID: "ri.ot.person", APIName: "name", BaseType: "string"},
		{RID: "ri.p.e", ObjectTypeRID: "ri.ot.employee", APIName: "salary", BaseType: "double"},
	}
	resolved, err := oms.ResolveInheritedObjectType(context.Background(), repo, &repo.objectTypes[2])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := propAPINames(resolved.Properties); !containsAll(got, []string{"isAlive", "name", "salary"}) {
		t.Errorf("expected isAlive/name/salary, got %v", got)
	}
	if len(resolved.ExtendsChain) != 2 || resolved.ExtendsChain[0] != "ri.ot.person" || resolved.ExtendsChain[1] != "ri.ot.living" {
		t.Errorf("expected ExtendsChain=[ri.ot.person ri.ot.living] (immediate→root), got %v", resolved.ExtendsChain)
	}
}

func TestResolveInheritedObjectType_CycleDetected(t *testing.T) {
	// A→B→A cycle. The resolver should refuse the chain rather than spin
	// forever. ValidateInheritanceCandidate is the write-time guard but the
	// read path still must protect itself in case stale data slipped in.
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.a", APIName: "a", ExtendsRID: "ri.ot.b"},
		{RID: "ri.ot.b", APIName: "b", ExtendsRID: "ri.ot.a"},
	}
	_, err := oms.ResolveInheritedObjectType(context.Background(), repo, &repo.objectTypes[0])
	if !errors.Is(err, oms.ErrInheritanceCycle) {
		t.Fatalf("expected ErrInheritanceCycle, got %v", err)
	}
}

func TestResolveInheritedObjectType_MergesOutgoingLinks(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.parent", APIName: "person"},
		{RID: "ri.ot.child", APIName: "employee", ExtendsRID: "ri.ot.parent"},
	}
	repo.linkTypes = []oms.LinkType{
		{RID: "ri.lt.parent.friends", OntologyRID: "o1", APIName: "friends", SourceObjectType: "ri.ot.parent", TargetObjectType: "ri.ot.parent", Cardinality: "MANY_TO_MANY"},
		{RID: "ri.lt.child.manager", OntologyRID: "o1", APIName: "manager", SourceObjectType: "ri.ot.child", TargetObjectType: "ri.ot.child", Cardinality: "MANY_TO_ONE"},
	}
	resolved, err := oms.ResolveInheritedObjectType(context.Background(), repo, &repo.objectTypes[1])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := linkAPINames(resolved.OutgoingLinkTypes)
	if !containsAll(got, []string{"friends", "manager"}) {
		t.Errorf("expected merged links to include friends + manager, got %v", got)
	}
}

func TestValidateInheritanceCandidate_SelfReferenceRejected(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.x", APIName: "x"},
	}
	if err := oms.ValidateInheritanceCandidate(context.Background(), repo, "ri.ot.x", "ri.ot.x"); !errors.Is(err, oms.ErrInheritanceCycle) {
		t.Fatalf("expected ErrInheritanceCycle for self-reference, got %v", err)
	}
}

func TestValidateInheritanceCandidate_AcyclicChainAccepted(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.root", APIName: "root"},
		{RID: "ri.ot.mid", APIName: "mid", ExtendsRID: "ri.ot.root"},
	}
	if err := oms.ValidateInheritanceCandidate(context.Background(), repo, "ri.ot.leaf", "ri.ot.mid"); err != nil {
		t.Fatalf("expected nil error for acyclic chain, got %v", err)
	}
}

func TestValidateInheritanceCandidate_CycleViaParentChainRejected(t *testing.T) {
	// We want to add: leaf.ExtendsRID = parent. parent.ExtendsRID = leaf.
	// So the candidate (parent) reaches back to leaf — must be flagged.
	repo := &mockRepo{}
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.leaf", APIName: "leaf"},
		{RID: "ri.ot.parent", APIName: "parent", ExtendsRID: "ri.ot.leaf"},
	}
	if err := oms.ValidateInheritanceCandidate(context.Background(), repo, "ri.ot.leaf", "ri.ot.parent"); !errors.Is(err, oms.ErrInheritanceCycle) {
		t.Fatalf("expected cycle, got %v", err)
	}
}

func TestValidateInheritanceCandidate_EmptyParentIsNoop(t *testing.T) {
	repo := &mockRepo{}
	if err := oms.ValidateInheritanceCandidate(context.Background(), repo, "ri.ot.x", ""); err != nil {
		t.Fatalf("expected nil for empty parent, got %v", err)
	}
}

// ---- helpers ----

func propAPINames(ps []oms.Property) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.APIName
	}
	return out
}

func linkAPINames(ls []oms.LinkType) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.APIName
	}
	return out
}

func containsAll(got, want []string) bool {
	have := make(map[string]bool, len(got))
	for _, g := range got {
		have[g] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}
