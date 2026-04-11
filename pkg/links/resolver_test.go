package links_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
)

// mockRepo implements a minimal oms.Repository for testing link resolution.
type mockRepo struct {
	objectTypes map[string]*oms.ObjectType // rid -> ObjectType
	linkTypes   map[string]*oms.LinkType   // rid -> LinkType
	outgoing    map[string][]oms.LinkType  // sourceObjectType RID -> []LinkType
	incoming    map[string][]oms.LinkType  // targetObjectType RID -> []LinkType (optional override)
	ontologies  map[string]*oms.Ontology   // rid or apiName -> Ontology (dual-accept)
	otByAPIName map[string]*oms.ObjectType // "ontologyRID|apiName" -> ObjectType
}

func (m *mockRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	ot, ok := m.objectTypes[rid]
	if !ok {
		return nil, fmt.Errorf("object type %q not found", rid)
	}
	return ot, nil
}

func (m *mockRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	lt, ok := m.linkTypes[rid]
	if !ok {
		return nil, fmt.Errorf("link type %q not found", rid)
	}
	return lt, nil
}

func (m *mockRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	lts, ok := m.outgoing[objectTypeRID]
	if !ok {
		return nil, nil
	}
	return lts, nil
}

func (m *mockRepo) ListIncomingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	if m.incoming != nil {
		return m.incoming[objectTypeRID], nil
	}
	// Fallback: derive incoming by filtering outgoing across all sources.
	var result []oms.LinkType
	for _, lts := range m.outgoing {
		for _, lt := range lts {
			if lt.TargetObjectType == objectTypeRID {
				result = append(result, lt)
			}
		}
	}
	return result, nil
}

// Unused Repository methods — satisfy the interface.
func (m *mockRepo) CreateOntology(context.Context, *oms.Ontology) error { return nil }
func (m *mockRepo) GetOntology(_ context.Context, key string) (*oms.Ontology, error) {
	if m.ontologies == nil {
		return nil, nil
	}
	if o, ok := m.ontologies[key]; ok {
		return o, nil
	}
	return nil, nil
}
func (m *mockRepo) ListOntologies(context.Context) ([]oms.Ontology, error)  { return nil, nil }
func (m *mockRepo) UpdateOntology(context.Context, *oms.Ontology) error     { return nil }
func (m *mockRepo) CreateObjectType(context.Context, *oms.ObjectType) error { return nil }
func (m *mockRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	if m.otByAPIName == nil {
		return nil, nil
	}
	if ot, ok := m.otByAPIName[ontologyRID+"|"+apiName]; ok {
		return ot, nil
	}
	return nil, nil
}
func (m *mockRepo) ListObjectTypes(context.Context, string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (m *mockRepo) UpdateObjectType(context.Context, *oms.ObjectType) error { return nil }
func (m *mockRepo) DeleteObjectType(context.Context, string) error          { return nil }
func (m *mockRepo) CreateProperty(context.Context, *oms.Property) error     { return nil }
func (m *mockRepo) GetProperty(context.Context, string) (*oms.Property, error) {
	return nil, nil
}
func (m *mockRepo) ListProperties(context.Context, string) ([]oms.Property, error) {
	return nil, nil
}
func (m *mockRepo) UpdateProperty(context.Context, *oms.Property) error           { return nil }
func (m *mockRepo) DeleteProperty(context.Context, string) error                  { return nil }
func (m *mockRepo) CreateLinkType(context.Context, *oms.LinkType) error           { return nil }
func (m *mockRepo) ListLinkTypes(context.Context, string) ([]oms.LinkType, error) { return nil, nil }
func (m *mockRepo) UpdateLinkType(context.Context, *oms.LinkType) error           { return nil }
func (m *mockRepo) DeleteLinkType(context.Context, string) error                  { return nil }
func (m *mockRepo) CreateActionType(context.Context, *oms.ActionType) error       { return nil }
func (m *mockRepo) GetActionType(context.Context, string) (*oms.ActionType, error) {
	return nil, nil
}
func (m *mockRepo) GetActionTypeByAPIName(context.Context, string, string) (*oms.ActionType, error) {
	return nil, nil
}
func (m *mockRepo) ListActionTypes(context.Context, string) ([]oms.ActionType, error) {
	return nil, nil
}
func (m *mockRepo) UpdateActionType(context.Context, *oms.ActionType) error      { return nil }
func (m *mockRepo) DeleteActionType(context.Context, string) error               { return nil }
func (m *mockRepo) CreateInterface(context.Context, *oms.Interface) error        { return nil }
func (m *mockRepo) GetInterface(context.Context, string) (*oms.Interface, error) { return nil, nil }
func (m *mockRepo) GetInterfaceByAPIName(context.Context, string, string) (*oms.Interface, error) {
	return nil, nil
}
func (m *mockRepo) ListInterfaces(context.Context, string) ([]oms.Interface, error) {
	return nil, nil
}
func (m *mockRepo) UpdateInterface(context.Context, *oms.Interface) error           { return nil }
func (m *mockRepo) DeleteInterface(context.Context, string) error                   { return nil }
func (m *mockRepo) AttachInterface(context.Context, *oms.ObjectTypeInterface) error { return nil }
func (m *mockRepo) DetachInterface(context.Context, string, string) error           { return nil }
func (m *mockRepo) ListInterfaceObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}

func (m *mockRepo) ListObjectTypeInterfaces(context.Context, string) ([]oms.ObjectTypeInterface, error) {
	return nil, nil
}

// SharedProperty stubs
func (m *mockRepo) CreateSharedProperty(context.Context, *oms.SharedProperty) error { return nil }
func (m *mockRepo) GetSharedProperty(context.Context, string) (*oms.SharedProperty, error) {
	return nil, nil
}
func (m *mockRepo) ListSharedProperties(context.Context, string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (m *mockRepo) UpdateSharedProperty(context.Context, *oms.SharedProperty) error { return nil }
func (m *mockRepo) DeleteSharedProperty(context.Context, string) error              { return nil }

// TypeGroup stubs
func (m *mockRepo) CreateTypeGroup(context.Context, *oms.TypeGroup) error           { return nil }
func (m *mockRepo) GetTypeGroup(context.Context, string) (*oms.TypeGroup, error)    { return nil, nil }
func (m *mockRepo) ListTypeGroups(context.Context, string) ([]oms.TypeGroup, error) { return nil, nil }
func (m *mockRepo) UpdateTypeGroup(context.Context, *oms.TypeGroup) error           { return nil }
func (m *mockRepo) DeleteTypeGroup(context.Context, string) error                   { return nil }
func (m *mockRepo) AssignTypeGroup(context.Context, string, string) error           { return nil }
func (m *mockRepo) RemoveTypeGroup(context.Context, string, string) error           { return nil }
func (m *mockRepo) ListTypeGroupsForObjectType(context.Context, string) ([]oms.TypeGroup, error) {
	return nil, nil
}

// ValueType stubs
func (m *mockRepo) CreateValueType(context.Context, *oms.ValueType) error        { return nil }
func (m *mockRepo) GetValueType(context.Context, string) (*oms.ValueType, error) { return nil, nil }
func (m *mockRepo) GetValueTypeByAPIName(context.Context, string) (*oms.ValueType, error) {
	return nil, nil
}
func (m *mockRepo) ListValueTypes(context.Context) ([]oms.ValueType, error) { return nil, nil }
func (m *mockRepo) UpdateValueType(context.Context, *oms.ValueType) error   { return nil }
func (m *mockRepo) DeleteValueType(context.Context, string) error           { return nil }

// DatasourceBinding stubs
func (m *mockRepo) CreateDatasourceBinding(context.Context, *oms.DatasourceBinding) error {
	return nil
}
func (m *mockRepo) GetDatasourceBinding(context.Context, string) (*oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *mockRepo) ListDatasourceBindings(context.Context, string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *mockRepo) UpdateDatasourceBinding(context.Context, *oms.DatasourceBinding) error {
	return nil
}
func (m *mockRepo) DeleteDatasourceBinding(context.Context, string) error { return nil }

// QueryType stubs
func (m *mockRepo) CreateQueryType(context.Context, *oms.QueryType) error { return nil }
func (m *mockRepo) GetQueryType(context.Context, string) (*oms.QueryType, error) {
	return nil, nil
}
func (m *mockRepo) GetQueryTypeByAPIName(context.Context, string, string) (*oms.QueryType, error) {
	return nil, nil
}
func (m *mockRepo) ListQueryTypes(context.Context, string) ([]oms.QueryType, error) { return nil, nil }
func (m *mockRepo) UpdateQueryType(context.Context, *oms.QueryType) error           { return nil }
func (m *mockRepo) DeleteQueryType(context.Context, string) error                   { return nil }

// ActionLog stubs
func (m *mockRepo) InsertActionLog(context.Context, *oms.ActionLog) error { return nil }
func (m *mockRepo) ListActionLogs(context.Context, string, int, int) ([]oms.ActionLog, error) {
	return nil, nil
}
func (m *mockRepo) CountActionLogs(context.Context, string) (int, error) { return 0, nil }

// Search stubs
func (m *mockRepo) SearchOntologyResources(context.Context, string, string) ([]oms.SearchResult, error) {
	return nil, nil
}

// Snapshot stubs
func (m *mockRepo) CreateSnapshot(context.Context, *oms.OntologySnapshot) error { return nil }
func (m *mockRepo) ListSnapshots(context.Context, string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *mockRepo) GetSnapshot(context.Context, string, int) (*oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *mockRepo) GetOntologyVersion(context.Context, string) (int, error)       { return 0, nil }
func (m *mockRepo) IncrementOntologyVersion(context.Context, string) (int, error) { return 1, nil }

// mustJSON marshals v to json.RawMessage; panics on failure.
func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// setupEmployeeDept creates a resolver with employee -> department FK link.
// Employees: emp1(deptId=d1), emp2(deptId=d1), emp3(deptId=d2)
// Departments: d1, d2
func setupEmployeeDept(t *testing.T) *links.Resolver {
	t.Helper()

	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { mgr.Close() })

	// Create employee index
	empProps := []index.Property{
		{APIName: "employeeid", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", empProps); err != nil {
		t.Fatalf("ensure employee index: %v", err)
	}

	// Create department index
	deptProps := []index.Property{
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
		{APIName: "deptname", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("ensure department index: %v", err)
	}

	// Index employee documents
	employees := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"emp1", map[string]interface{}{"employeeid": "emp1", "name": "alice", "deptid": "d1"}},
		{"emp2", map[string]interface{}{"employeeid": "emp2", "name": "bob", "deptid": "d1"}},
		{"emp3", map[string]interface{}{"employeeid": "emp3", "name": "charlie", "deptid": "d2"}},
	}
	for _, e := range employees {
		if err := mgr.IndexDocument("employee", e.id, e.doc); err != nil {
			t.Fatalf("index employee %s: %v", e.id, err)
		}
	}

	// Index department documents
	departments := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"d1", map[string]interface{}{"deptid": "d1", "deptname": "engineering"}},
		{"d2", map[string]interface{}{"deptid": "d2", "deptname": "marketing"}},
	}
	for _, d := range departments {
		if err := mgr.IndexDocument("department", d.id, d.doc); err != nil {
			t.Fatalf("index department %s: %v", d.id, err)
		}
	}

	// Mock repository
	repo := &mockRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.employee": {
				RID:        "ri.ot.employee",
				APIName:    "employee",
				PrimaryKey: "employeeid",
			},
			"ri.ot.department": {
				RID:        "ri.ot.department",
				APIName:    "department",
				PrimaryKey: "deptid",
			},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.emp-dept": {
				RID:              "ri.lt.emp-dept",
				APIName:          "employeedepartment",
				DisplayName:      "Employee Department",
				SourceObjectType: "ri.ot.employee",
				TargetObjectType: "ri.ot.department",
				Cardinality:      "MANY_TO_ONE",
				ForeignKeyConfig: mustJSON(links.FKConfig{
					SourceProperty: "deptid",
					TargetProperty: "deptid",
				}),
			},
			"ri.lt.dept-emp": {
				RID:              "ri.lt.dept-emp",
				APIName:          "departmentemployees",
				DisplayName:      "Department Employees",
				SourceObjectType: "ri.ot.department",
				TargetObjectType: "ri.ot.employee",
				Cardinality:      "ONE_TO_MANY",
				ForeignKeyConfig: mustJSON(links.FKConfig{
					SourceProperty: "deptid",
					TargetProperty: "deptid",
				}),
			},
		},
		outgoing: map[string][]oms.LinkType{},
	}

	// Set up outgoing links
	repo.outgoing["ri.ot.employee"] = []oms.LinkType{
		*repo.linkTypes["ri.lt.emp-dept"],
	}
	repo.outgoing["ri.ot.department"] = []oms.LinkType{
		*repo.linkTypes["ri.lt.dept-emp"],
	}

	return links.NewResolver(repo, mgr)
}

// --- Constructor / setup tests ---

func TestNewResolver(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &mockRepo{
		objectTypes: make(map[string]*oms.ObjectType),
		linkTypes:   make(map[string]*oms.LinkType),
		outgoing:    make(map[string][]oms.LinkType),
	}

	resolver := links.NewResolver(repo, mgr)
	if resolver == nil {
		t.Fatal("expected non-nil resolver")
	}
}

func TestResolver_ResolveNotFound(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &mockRepo{
		objectTypes: make(map[string]*oms.ObjectType),
		linkTypes:   make(map[string]*oms.LinkType),
		outgoing:    make(map[string][]oms.LinkType),
	}

	resolver := links.NewResolver(repo, mgr)
	_, err := resolver.ResolveLinkedObjects(context.Background(), "nonexistent", []string{"pk1"})
	if err == nil {
		t.Fatal("expected error for nonexistent link type")
	}
}

// --- FK resolution tests ---

func TestResolveFK_SingleSource(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	// emp1 has deptid=d1, so should resolve to department d1
	pks, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", []string{"emp1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 1 {
		t.Fatalf("expected 1 target PK, got %d", len(pks))
	}
	if pks[0] != "d1" {
		t.Fatalf("expected target PK %q, got %q", "d1", pks[0])
	}
}

func TestResolveFK_MultipleSources(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	// emp1(deptid=d1) and emp3(deptid=d2) should resolve to d1 and d2
	pks, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", []string{"emp1", "emp3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 2 {
		t.Fatalf("expected 2 target PKs, got %d", len(pks))
	}

	found := make(map[string]bool)
	for _, pk := range pks {
		found[pk] = true
	}
	if !found["d1"] || !found["d2"] {
		t.Fatalf("expected d1 and d2, got %v", pks)
	}
}

func TestResolveFK_NoMatch(t *testing.T) {
	// Create a setup where the FK value doesn't match any target
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	empProps := []index.Property{
		{APIName: "employeeid", BaseType: "string", IsSearchable: true},
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", empProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	deptProps := []index.Property{
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// Employee references a department that doesn't exist
	if err := mgr.IndexDocument("employee", "emp1", map[string]interface{}{
		"employeeid": "emp1",
		"deptid":     "d999",
	}); err != nil {
		t.Fatalf("index doc: %v", err)
	}

	// Only d1 exists in departments
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{
		"deptid": "d1",
	}); err != nil {
		t.Fatalf("index doc: %v", err)
	}

	repo := &mockRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.employee":   {RID: "ri.ot.employee", APIName: "employee", PrimaryKey: "employeeid"},
			"ri.ot.department": {RID: "ri.ot.department", APIName: "department", PrimaryKey: "deptid"},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.emp-dept": {
				RID:              "ri.lt.emp-dept",
				APIName:          "employeedepartment",
				SourceObjectType: "ri.ot.employee",
				TargetObjectType: "ri.ot.department",
				Cardinality:      "MANY_TO_ONE",
				ForeignKeyConfig: mustJSON(links.FKConfig{SourceProperty: "deptid", TargetProperty: "deptid"}),
			},
		},
		outgoing: make(map[string][]oms.LinkType),
	}

	resolver := links.NewResolver(repo, mgr)
	pks, err := resolver.ResolveLinkedObjects(context.Background(), "ri.lt.emp-dept", []string{"emp1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 0 {
		t.Fatalf("expected 0 target PKs, got %d: %v", len(pks), pks)
	}
}

func TestResolveFK_DuplicateTargets(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	// emp1 and emp2 both have deptid=d1, so should resolve to a single d1
	pks, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", []string{"emp1", "emp2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 1 {
		t.Fatalf("expected 1 deduplicated target PK, got %d: %v", len(pks), pks)
	}
	if pks[0] != "d1" {
		t.Fatalf("expected target PK %q, got %q", "d1", pks[0])
	}
}

func TestResolveFK_EmptySourcePKs(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	pks, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pks != nil {
		t.Fatalf("expected nil result for empty source PKs, got %v", pks)
	}
}

func TestResolveFK_OneToMany(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	// Reverse direction: department d1 -> employees emp1, emp2
	pks, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.dept-emp", []string{"d1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 2 {
		t.Fatalf("expected 2 target PKs, got %d: %v", len(pks), pks)
	}

	found := make(map[string]bool)
	for _, pk := range pks {
		found[pk] = true
	}
	if !found["emp1"] || !found["emp2"] {
		t.Fatalf("expected emp1 and emp2, got %v", pks)
	}
}

// --- By API name tests ---

func TestResolveByAPIName_Found(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	pks, err := resolver.ResolveLinkedObjectsByAPIName(ctx, "ri.ot.employee", "employeedepartment", []string{"emp1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 1 {
		t.Fatalf("expected 1 target PK, got %d", len(pks))
	}
	if pks[0] != "d1" {
		t.Fatalf("expected target PK %q, got %q", "d1", pks[0])
	}
}

// TestResolveByAPIName_AcceptsAPIName verifies that when the source identifier
// passed to ResolveLinkedObjectsByAPIName is an object type APIName (not an
// RID), the resolver transparently resolves it via the ontology scope stamped
// on ctx + oms.Repository.GetOntology / GetObjectTypeByAPIName. This is the
// production wiring that makes withProperties / searchAround work end-to-end
// without the executor having to translate apiname → RID itself.
func TestResolveByAPIName_AcceptsAPIName(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { mgr.Close() })

	empProps := []index.Property{
		{APIName: "employeeid", BaseType: "string", IsSearchable: true},
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	// Scoped index keys match the per-ontology Bleve namespacing used by
	// scopedFKKey once a scope is set on ctx.
	empKey := index.ScopedKey("northwind", "employee")
	deptKey := index.ScopedKey("northwind", "department")
	if _, err := mgr.EnsureIndex(empKey, empProps); err != nil {
		t.Fatalf("ensure employee index: %v", err)
	}
	deptProps := []index.Property{
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(deptKey, deptProps); err != nil {
		t.Fatalf("ensure department index: %v", err)
	}
	if err := mgr.IndexDocument(empKey, "emp1", map[string]interface{}{"employeeid": "emp1", "deptid": "d1"}); err != nil {
		t.Fatalf("index emp: %v", err)
	}
	if err := mgr.IndexDocument(deptKey, "d1", map[string]interface{}{"deptid": "d1"}); err != nil {
		t.Fatalf("index dept: %v", err)
	}

	empOT := &oms.ObjectType{RID: "ri.ot.employee", APIName: "employee", PrimaryKey: "employeeid"}
	deptOT := &oms.ObjectType{RID: "ri.ot.department", APIName: "department", PrimaryKey: "deptid"}
	lt := oms.LinkType{
		RID:              "ri.lt.emp-dept",
		APIName:          "employeedepartment",
		SourceObjectType: "ri.ot.employee",
		TargetObjectType: "ri.ot.department",
		Cardinality:      "MANY_TO_ONE",
		ForeignKeyConfig: mustJSON(links.FKConfig{SourceProperty: "deptid", TargetProperty: "deptid"}),
	}

	repo := &mockRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.employee":   empOT,
			"ri.ot.department": deptOT,
		},
		linkTypes: map[string]*oms.LinkType{"ri.lt.emp-dept": &lt},
		outgoing: map[string][]oms.LinkType{
			"ri.ot.employee": {lt},
		},
		ontologies: map[string]*oms.Ontology{
			"northwind":        {RID: "ri.ont.northwind", APIName: "northwind"},
			"ri.ont.northwind": {RID: "ri.ont.northwind", APIName: "northwind"},
		},
		otByAPIName: map[string]*oms.ObjectType{
			"ri.ont.northwind|employee":   empOT,
			"ri.ont.northwind|department": deptOT,
		},
	}

	resolver := links.NewResolver(repo, mgr)
	ctx := index.WithOntologyScope(context.Background(), "northwind")

	pks, err := resolver.ResolveLinkedObjectsByAPIName(ctx, "employee", "employeedepartment", []string{"emp1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 1 {
		t.Fatalf("expected 1 target PK, got %d", len(pks))
	}
	if pks[0] != "d1" {
		t.Fatalf("expected target PK %q, got %q", "d1", pks[0])
	}
}

func TestResolveByAPIName_NotFound(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	_, err := resolver.ResolveLinkedObjectsByAPIName(ctx, "ri.ot.employee", "nonexistentlink", []string{"emp1"})
	if err == nil {
		t.Fatal("expected error for unknown link type API name")
	}
}

func TestResolveByAPIName_MultipleLinks(t *testing.T) {
	// Set up a source type with multiple outgoing link types and verify the correct one is selected.
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	empProps := []index.Property{
		{APIName: "employeeid", BaseType: "string", IsSearchable: true},
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
		{APIName: "managerid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", empProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	deptProps := []index.Property{
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	managerProps := []index.Property{
		{APIName: "managerid", BaseType: "string", IsSearchable: true},
		{APIName: "managername", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("manager", managerProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// Index data
	if err := mgr.IndexDocument("employee", "emp1", map[string]interface{}{
		"employeeid": "emp1", "deptid": "d1", "managerid": "m1",
	}); err != nil {
		t.Fatalf("index doc: %v", err)
	}
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{
		"deptid": "d1",
	}); err != nil {
		t.Fatalf("index doc: %v", err)
	}
	if err := mgr.IndexDocument("manager", "m1", map[string]interface{}{
		"managerid": "m1", "managername": "jane",
	}); err != nil {
		t.Fatalf("index doc: %v", err)
	}

	ltDept := oms.LinkType{
		RID:              "ri.lt.emp-dept",
		APIName:          "employeedepartment",
		SourceObjectType: "ri.ot.employee",
		TargetObjectType: "ri.ot.department",
		Cardinality:      "MANY_TO_ONE",
		ForeignKeyConfig: mustJSON(links.FKConfig{SourceProperty: "deptid", TargetProperty: "deptid"}),
	}
	ltMgr := oms.LinkType{
		RID:              "ri.lt.emp-mgr",
		APIName:          "employeemanager",
		SourceObjectType: "ri.ot.employee",
		TargetObjectType: "ri.ot.manager",
		Cardinality:      "MANY_TO_ONE",
		ForeignKeyConfig: mustJSON(links.FKConfig{SourceProperty: "managerid", TargetProperty: "managerid"}),
	}

	repo := &mockRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.employee":   {RID: "ri.ot.employee", APIName: "employee", PrimaryKey: "employeeid"},
			"ri.ot.department": {RID: "ri.ot.department", APIName: "department", PrimaryKey: "deptid"},
			"ri.ot.manager":    {RID: "ri.ot.manager", APIName: "manager", PrimaryKey: "managerid"},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.emp-dept": &ltDept,
			"ri.lt.emp-mgr":  &ltMgr,
		},
		outgoing: map[string][]oms.LinkType{
			"ri.ot.employee": {ltDept, ltMgr},
		},
	}

	resolver := links.NewResolver(repo, mgr)

	// Resolve by API name "employeemanager" — should pick the manager link, not department
	pks, err := resolver.ResolveLinkedObjectsByAPIName(context.Background(), "ri.ot.employee", "employeemanager", []string{"emp1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 1 {
		t.Fatalf("expected 1 target PK, got %d", len(pks))
	}
	if pks[0] != "m1" {
		t.Fatalf("expected target PK %q, got %q", "m1", pks[0])
	}
}

// --- M2M test ---

func TestResolveM2M_NotSupported(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &mockRepo{
		objectTypes: make(map[string]*oms.ObjectType),
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.m2m": {
				RID:              "ri.lt.m2m",
				APIName:          "manytomany",
				SourceObjectType: "ri.ot.a",
				TargetObjectType: "ri.ot.b",
				Cardinality:      "MANY_TO_MANY",
				// No ForeignKeyConfig — M2M uses JoinTableConfig
			},
		},
		outgoing: make(map[string][]oms.LinkType),
	}

	resolver := links.NewResolver(repo, mgr)
	_, err := resolver.ResolveLinkedObjects(context.Background(), "ri.lt.m2m", []string{"pk1"})
	if err == nil {
		t.Fatal("expected error for M2M link type")
	}
}

// --- Edge case tests ---

func TestResolve_InvalidFKConfig(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &mockRepo{
		objectTypes: make(map[string]*oms.ObjectType),
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.bad": {
				RID:              "ri.lt.bad",
				APIName:          "badlink",
				SourceObjectType: "ri.ot.a",
				TargetObjectType: "ri.ot.b",
				Cardinality:      "ONE_TO_ONE",
				ForeignKeyConfig: json.RawMessage(`{invalid json`),
			},
		},
		outgoing: make(map[string][]oms.LinkType),
	}

	resolver := links.NewResolver(repo, mgr)
	_, err := resolver.ResolveLinkedObjects(context.Background(), "ri.lt.bad", []string{"pk1"})
	if err == nil {
		t.Fatal("expected error for invalid FK config JSON")
	}
}

func TestResolve_SourceObjectNotInIndex(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	empProps := []index.Property{
		{APIName: "employeeid", BaseType: "string", IsSearchable: true},
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", empProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	deptProps := []index.Property{
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// Index a department but no employees
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{
		"deptid": "d1",
	}); err != nil {
		t.Fatalf("index doc: %v", err)
	}

	repo := &mockRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.employee":   {RID: "ri.ot.employee", APIName: "employee", PrimaryKey: "employeeid"},
			"ri.ot.department": {RID: "ri.ot.department", APIName: "department", PrimaryKey: "deptid"},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.emp-dept": {
				RID:              "ri.lt.emp-dept",
				APIName:          "employeedepartment",
				SourceObjectType: "ri.ot.employee",
				TargetObjectType: "ri.ot.department",
				Cardinality:      "MANY_TO_ONE",
				ForeignKeyConfig: mustJSON(links.FKConfig{SourceProperty: "deptid", TargetProperty: "deptid"}),
			},
		},
		outgoing: make(map[string][]oms.LinkType),
	}

	resolver := links.NewResolver(repo, mgr)

	// Search for a PK that doesn't exist in the index — should get empty FK values and nil result
	pks, err := resolver.ResolveLinkedObjects(context.Background(), "ri.lt.emp-dept", []string{"nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pks != nil {
		t.Fatalf("expected nil result for source PK not in index, got %v", pks)
	}
}

func (m *mockRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (m *mockRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) {
	return nil, nil
}
func (m *mockRepo) ListSecurityPolicies(_ context.Context, _ string) ([]oms.SecurityPolicy, error) {
	return nil, nil
}
func (m *mockRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (m *mockRepo) DeleteSecurityPolicy(_ context.Context, _ string) error              { return nil }
