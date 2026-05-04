//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oms"
)

func setupRepo(t testing.TB) *oms.PGRepository {
	t.Helper()
	pg := testutil.StartPGContainer(t)

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	return oms.NewPGRepository(pg.Pool)
}

func seedOntology(t *testing.T, repo *oms.PGRepository) *oms.Ontology {
	t.Helper()
	o := &oms.Ontology{
		RID:         "ri.ontology.main.ontology.seed-1",
		APIName:     "test-ontology",
		DisplayName: "Test Ontology",
		Description: "For testing",
	}
	if err := repo.CreateOntology(context.Background(), o); err != nil {
		t.Fatalf("seed ontology failed: %v", err)
	}
	return o
}

func seedObjectType(t *testing.T, repo *oms.PGRepository, ontologyRID string) *oms.ObjectType {
	t.Helper()
	ot := &oms.ObjectType{
		RID:         "ri.ontology.main.object-type.seed-1",
		OntologyRID: ontologyRID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(context.Background(), ot); err != nil {
		t.Fatalf("seed object type failed: %v", err)
	}
	return ot
}

// --- Ontology CRUD (4 tests) ---

func TestOntology_Create(t *testing.T) {
	repo := setupRepo(t)
	o := &oms.Ontology{
		RID:         "ri.ontology.main.ontology.o1",
		APIName:     "my-ontology",
		DisplayName: "My Ontology",
		Description: "Test",
	}
	err := repo.CreateOntology(context.Background(), o)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
}

func TestOntology_Get(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	got, err := repo.GetOntology(context.Background(), o.RID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.APIName != o.APIName {
		t.Errorf("expected apiName %q, got %q", o.APIName, got.APIName)
	}
}

func TestOntology_GetNotFound(t *testing.T) {
	repo := setupRepo(t)
	_, err := repo.GetOntology(context.Background(), "nonexistent")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestOntology_List(t *testing.T) {
	repo := setupRepo(t)
	seedOntology(t, repo)

	list, err := repo.ListOntologies(context.Background())
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 ontology, got %d", len(list))
	}
}

// --- ObjectType CRUD (8 tests) ---

func TestObjectType_Create(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	ot := &oms.ObjectType{
		RID:         "ri.ontology.main.object-type.ot1",
		OntologyRID: o.RID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	err := repo.CreateObjectType(context.Background(), ot)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
}

func TestObjectType_DuplicateApiName(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	ot1 := &oms.ObjectType{
		RID: "ri.ontology.main.object-type.ot1", OntologyRID: o.RID,
		APIName: "employee", DisplayName: "Employee", PrimaryKey: "id",
		Status: "ACTIVE", Visibility: "NORMAL",
	}
	repo.CreateObjectType(context.Background(), ot1)

	ot2 := &oms.ObjectType{
		RID: "ri.ontology.main.object-type.ot2", OntologyRID: o.RID,
		APIName: "employee", DisplayName: "Employee 2", PrimaryKey: "id",
		Status: "ACTIVE", Visibility: "NORMAL",
	}
	err := repo.CreateObjectType(context.Background(), ot2)
	if !errors.Is(err, oms.ErrDuplicate) {
		t.Errorf("expected ErrDuplicate, got %v", err)
	}
}

func TestObjectType_Get(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	got, err := repo.GetObjectType(context.Background(), ot.RID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.APIName != "employee" {
		t.Errorf("expected 'employee', got %q", got.APIName)
	}
}

func TestObjectType_GetNotFound(t *testing.T) {
	repo := setupRepo(t)
	_, err := repo.GetObjectType(context.Background(), "nonexistent")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestObjectType_List(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	seedObjectType(t, repo, o.RID)

	list, err := repo.ListObjectTypes(context.Background(), o.RID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}
}

func TestObjectType_Update(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	ot.DisplayName = "Updated Employee"
	ot.Status = "DEPRECATED"
	err := repo.UpdateObjectType(context.Background(), ot)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got, _ := repo.GetObjectType(context.Background(), ot.RID)
	if got.DisplayName != "Updated Employee" {
		t.Errorf("expected updated name, got %q", got.DisplayName)
	}
	if got.Status != "DEPRECATED" {
		t.Errorf("expected DEPRECATED, got %q", got.Status)
	}
}

func TestObjectType_Delete(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	err := repo.DeleteObjectType(context.Background(), ot.RID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = repo.GetObjectType(context.Background(), ot.RID)
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestObjectType_GetIncludesProperties(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	prop := &oms.Property{
		RID:           "ri.ontology.main.property.p1",
		ObjectTypeRID: ot.RID,
		APIName:       "fullName",
		BaseType:      "string",
		IsNullable:    true,
		IsSearchable:  true,
		IsSortable:    true,
	}
	repo.CreateProperty(context.Background(), prop)

	got, err := repo.GetObjectType(context.Background(), ot.RID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(got.Properties) != 1 {
		t.Fatalf("expected 1 property, got %d", len(got.Properties))
	}
	if got.Properties[0].APIName != "fullName" {
		t.Errorf("expected property 'fullName', got %q", got.Properties[0].APIName)
	}
}

// --- Property (4 tests) ---

func TestProperty_Create(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	p := &oms.Property{
		RID:           "ri.ontology.main.property.p1",
		ObjectTypeRID: ot.RID,
		APIName:       "name",
		BaseType:      "string",
		IsNullable:    true,
		IsSearchable:  true,
		IsSortable:    true,
	}
	err := repo.CreateProperty(context.Background(), p)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
}

func TestProperty_Duplicate(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	p1 := &oms.Property{
		RID: "ri.ontology.main.property.p1", ObjectTypeRID: ot.RID,
		APIName: "name", BaseType: "string", IsNullable: true, IsSearchable: true, IsSortable: true,
	}
	repo.CreateProperty(context.Background(), p1)

	p2 := &oms.Property{
		RID: "ri.ontology.main.property.p2", ObjectTypeRID: ot.RID,
		APIName: "name", BaseType: "integer", IsNullable: true, IsSearchable: true, IsSortable: true,
	}
	err := repo.CreateProperty(context.Background(), p2)
	if !errors.Is(err, oms.ErrDuplicate) {
		t.Errorf("expected ErrDuplicate, got %v", err)
	}
}

func TestProperty_List(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	for i, name := range []string{"id", "name", "email"} {
		p := &oms.Property{
			RID:           "ri.ontology.main.property.p" + string(rune('1'+i)),
			ObjectTypeRID: ot.RID, APIName: name, BaseType: "string",
			IsNullable: true, IsSearchable: true, IsSortable: true,
		}
		repo.CreateProperty(context.Background(), p)
	}

	list, err := repo.ListProperties(context.Background(), ot.RID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3, got %d", len(list))
	}
}

func TestProperty_Delete(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	p := &oms.Property{
		RID: "ri.ontology.main.property.p1", ObjectTypeRID: ot.RID,
		APIName: "name", BaseType: "string", IsNullable: true, IsSearchable: true, IsSortable: true,
	}
	repo.CreateProperty(context.Background(), p)

	err := repo.DeleteProperty(context.Background(), p.RID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	list, _ := repo.ListProperties(context.Background(), ot.RID)
	if len(list) != 0 {
		t.Errorf("expected 0 properties after delete, got %d", len(list))
	}
}

// --- LinkType (5 tests) ---

func seedTwoObjectTypes(t *testing.T, repo *oms.PGRepository, ontologyRID string) (*oms.ObjectType, *oms.ObjectType) {
	t.Helper()
	src := &oms.ObjectType{
		RID: "ri.ontology.main.object-type.src", OntologyRID: ontologyRID,
		APIName: "employee", DisplayName: "Employee", PrimaryKey: "id",
		Status: "ACTIVE", Visibility: "NORMAL",
	}
	tgt := &oms.ObjectType{
		RID: "ri.ontology.main.object-type.tgt", OntologyRID: ontologyRID,
		APIName: "department", DisplayName: "Department", PrimaryKey: "id",
		Status: "ACTIVE", Visibility: "NORMAL",
	}
	repo.CreateObjectType(context.Background(), src)
	repo.CreateObjectType(context.Background(), tgt)
	return src, tgt
}

func TestLinkType_CreateFK(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)

	lt := &oms.LinkType{
		RID: "ri.ontology.main.link-type.lt1", OntologyRID: o.RID,
		APIName: "employeeDept", DisplayName: "Employee Department",
		SourceObjectType: src.RID, TargetObjectType: tgt.RID,
		Cardinality:      "ONE_TO_MANY",
		ForeignKeyConfig: json.RawMessage(`{"sourceProperty":"deptId","targetProperty":"id"}`),
	}
	err := repo.CreateLinkType(context.Background(), lt)
	if err != nil {
		t.Fatalf("create FK link failed: %v", err)
	}
}

func TestLinkType_CreateM2M(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)

	lt := &oms.LinkType{
		RID: "ri.ontology.main.link-type.lt2", OntologyRID: o.RID,
		APIName: "employeeProject", DisplayName: "Employee Project",
		SourceObjectType: src.RID, TargetObjectType: tgt.RID,
		Cardinality:     "MANY_TO_MANY",
		JoinTableConfig: json.RawMessage(`{"datasetRid":"ds1","sourceColumn":"empId","targetColumn":"projId"}`),
	}
	err := repo.CreateLinkType(context.Background(), lt)
	if err != nil {
		t.Fatalf("create M2M link failed: %v", err)
	}
}

func TestLinkType_Get(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)

	lt := &oms.LinkType{
		RID: "ri.ontology.main.link-type.lt1", OntologyRID: o.RID,
		APIName: "empDept", DisplayName: "Emp Dept",
		SourceObjectType: src.RID, TargetObjectType: tgt.RID,
		Cardinality: "ONE_TO_MANY",
	}
	repo.CreateLinkType(context.Background(), lt)

	got, err := repo.GetLinkType(context.Background(), lt.RID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.APIName != "empDept" {
		t.Errorf("expected 'empDept', got %q", got.APIName)
	}
	if got.Cardinality != "ONE_TO_MANY" {
		t.Errorf("expected ONE_TO_MANY, got %q", got.Cardinality)
	}
}

func TestLinkType_ListOutgoing(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)

	lt := &oms.LinkType{
		RID: "ri.ontology.main.link-type.lt1", OntologyRID: o.RID,
		APIName: "empDept", DisplayName: "Emp Dept",
		SourceObjectType: src.RID, TargetObjectType: tgt.RID,
		Cardinality: "ONE_TO_MANY",
	}
	repo.CreateLinkType(context.Background(), lt)

	list, err := repo.ListOutgoingLinkTypes(context.Background(), src.RID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}
}

func TestLinkType_ListIncoming(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)

	lt := &oms.LinkType{
		RID: "ri.ontology.main.link-type.lt1", OntologyRID: o.RID,
		APIName: "empDept", DisplayName: "Emp Dept",
		SourceObjectType: src.RID, TargetObjectType: tgt.RID,
		Cardinality: "ONE_TO_MANY",
	}
	repo.CreateLinkType(context.Background(), lt)

	// Incoming on the target should surface the link.
	incoming, err := repo.ListIncomingLinkTypes(context.Background(), tgt.RID)
	if err != nil {
		t.Fatalf("list incoming failed: %v", err)
	}
	if len(incoming) != 1 {
		t.Fatalf("expected 1 incoming link, got %d", len(incoming))
	}
	if incoming[0].RID != lt.RID {
		t.Errorf("expected %q, got %q", lt.RID, incoming[0].RID)
	}

	// Incoming on the source should be empty (outgoing, not incoming).
	srcIncoming, err := repo.ListIncomingLinkTypes(context.Background(), src.RID)
	if err != nil {
		t.Fatalf("list incoming on src failed: %v", err)
	}
	if len(srcIncoming) != 0 {
		t.Errorf("expected 0 incoming on source, got %d", len(srcIncoming))
	}
}

func TestLinkType_GetNotFound(t *testing.T) {
	repo := setupRepo(t)
	_, err := repo.GetLinkType(context.Background(), "nonexistent")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- ActionType (5 tests) ---

func TestActionType_Create(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	at := &oms.ActionType{
		RID: "ri.ontology.main.action-type.at1", OntologyRID: o.RID,
		APIName: "createEmployee", DisplayName: "Create Employee",
		Status:     "ACTIVE",
		Parameters: json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
		Rules:      json.RawMessage(`[{"type":"createObject","objectType":"employee"}]`),
	}
	err := repo.CreateActionType(context.Background(), at)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
}

func TestActionType_Get(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	at := &oms.ActionType{
		RID: "ri.ontology.main.action-type.at1", OntologyRID: o.RID,
		APIName: "createEmployee", DisplayName: "Create Employee",
		Status:     "ACTIVE",
		Parameters: json.RawMessage(`[{"id":"name","type":"string"}]`),
		Rules:      json.RawMessage(`[]`),
	}
	repo.CreateActionType(context.Background(), at)

	got, err := repo.GetActionType(context.Background(), at.RID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.APIName != "createEmployee" {
		t.Errorf("expected 'createEmployee', got %q", got.APIName)
	}
}

func TestActionType_GetNotFound(t *testing.T) {
	repo := setupRepo(t)
	_, err := repo.GetActionType(context.Background(), "nonexistent")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestActionType_List(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	for _, name := range []string{"create", "update"} {
		at := &oms.ActionType{
			RID: "ri.ontology.main.action-type." + name, OntologyRID: o.RID,
			APIName: name + "Employee", DisplayName: name + " Employee",
			Status: "ACTIVE", Parameters: json.RawMessage(`[]`), Rules: json.RawMessage(`[]`),
		}
		repo.CreateActionType(context.Background(), at)
	}

	list, err := repo.ListActionTypes(context.Background(), o.RID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestActionType_Update(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	at := &oms.ActionType{
		RID: "ri.ontology.main.action-type.at1", OntologyRID: o.RID,
		APIName: "createEmployee", DisplayName: "Create Employee",
		Status: "ACTIVE", Parameters: json.RawMessage(`[]`), Rules: json.RawMessage(`[]`),
	}
	repo.CreateActionType(context.Background(), at)

	at.DisplayName = "Updated Create Employee"
	at.Status = "DEPRECATED"
	err := repo.UpdateActionType(context.Background(), at)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got, _ := repo.GetActionType(context.Background(), at.RID)
	if got.DisplayName != "Updated Create Employee" {
		t.Errorf("expected updated name, got %q", got.DisplayName)
	}
}

// --- Interface (4 tests) ---

func TestInterface_Create(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	iface := &oms.Interface{
		RID: "ri.ontology.main.interface.i1", OntologyRID: o.RID,
		APIName: "GeoLocatable", DisplayName: "Geo Locatable",
		SharedProperties: json.RawMessage(`[{"apiName":"latitude","type":"double"}]`),
	}
	err := repo.CreateInterface(context.Background(), iface)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
}

func TestInterface_WithExtends(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	parent := &oms.Interface{
		RID: "ri.ontology.main.interface.parent", OntologyRID: o.RID,
		APIName: "Identifiable", DisplayName: "Identifiable",
	}
	repo.CreateInterface(context.Background(), parent)

	child := &oms.Interface{
		RID: "ri.ontology.main.interface.child", OntologyRID: o.RID,
		APIName: "NamedEntity", DisplayName: "Named Entity",
		ExtendsRID: parent.RID,
	}
	err := repo.CreateInterface(context.Background(), child)
	if err != nil {
		t.Fatalf("create with extends failed: %v", err)
	}
}

func TestInterface_List(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	iface := &oms.Interface{
		RID: "ri.ontology.main.interface.i1", OntologyRID: o.RID,
		APIName: "GeoLocatable", DisplayName: "Geo Locatable",
	}
	repo.CreateInterface(context.Background(), iface)

	list, err := repo.ListInterfaces(context.Background(), o.RID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}
}

func TestInterface_Attach(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	iface := &oms.Interface{
		RID: "ri.ontology.main.interface.i1", OntologyRID: o.RID,
		APIName: "GeoLocatable", DisplayName: "Geo Locatable",
	}
	repo.CreateInterface(context.Background(), iface)

	oti := &oms.ObjectTypeInterface{
		ObjectTypeRID:   ot.RID,
		InterfaceRID:    iface.RID,
		PropertyMapping: json.RawMessage(`{"latitude":"lat","longitude":"lng"}`),
	}
	err := repo.AttachInterface(context.Background(), oti)
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
}

// --- Bug fix: ListInterfaceObjectTypes (C2) ---

func TestListInterfaceObjectTypes_ReturnsAttachedObjectTypes(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	iface := &oms.Interface{
		RID: "ri.ontology.main.interface.i1", OntologyRID: o.RID,
		APIName: "GeoLocatable", DisplayName: "Geo Locatable",
	}
	repo.CreateInterface(context.Background(), iface)

	oti := &oms.ObjectTypeInterface{
		ObjectTypeRID:   ot.RID,
		InterfaceRID:    iface.RID,
		PropertyMapping: json.RawMessage(`{}`),
	}
	repo.AttachInterface(context.Background(), oti)

	result, err := repo.ListInterfaceObjectTypes(context.Background(), iface.RID)
	if err != nil {
		t.Fatalf("ListInterfaceObjectTypes failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 object type, got %d", len(result))
	}
	if result[0].RID != ot.RID {
		t.Errorf("expected RID %q, got %q", ot.RID, result[0].RID)
	}
	if result[0].PrimaryKey != "employeeId" {
		t.Errorf("expected PrimaryKey 'employeeId', got %q", result[0].PrimaryKey)
	}
	if result[0].Status != "ACTIVE" {
		t.Errorf("expected Status 'ACTIVE', got %q", result[0].Status)
	}
}

// --- Bug fix: CreateObjectType with deprecated fields ---

func TestCreateObjectType_WithDeprecatedFields(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	deadline := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ot := &oms.ObjectType{
		RID:                "ri.ontology.main.object-type.dep1",
		OntologyRID:        o.RID,
		APIName:            "legacyWidget",
		DisplayName:        "Legacy Widget",
		PrimaryKey:         "widgetId",
		Status:             "DEPRECATED",
		Visibility:         "NORMAL",
		DeprecatedReason:   "Replaced by NewWidget",
		DeprecatedDeadline: &deadline,
	}
	err := repo.CreateObjectType(context.Background(), ot)
	if err != nil {
		t.Fatalf("create with deprecated fields failed: %v", err)
	}

	got, err := repo.GetObjectType(context.Background(), ot.RID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.DeprecatedReason != "Replaced by NewWidget" {
		t.Errorf("expected deprecatedReason 'Replaced by NewWidget', got %q", got.DeprecatedReason)
	}
	if got.DeprecatedDeadline == nil || !got.DeprecatedDeadline.Equal(deadline) {
		t.Errorf("expected deprecatedDeadline %v, got %v", deadline, got.DeprecatedDeadline)
	}
}

// --- Bug fix: InsertActionLog (C3) ---

func TestInsertActionLog(t *testing.T) {
	repo := setupRepo(t)

	al := &oms.ActionLog{
		ActionTypeRID: "ri.ontology.main.action-type.at1",
		UserID:        "user-123",
		Parameters:    json.RawMessage(`{"name":"test"}`),
		Edits:         json.RawMessage(`[{"type":"CREATE"}]`),
		Status:        "SUCCESS",
	}
	err := repo.InsertActionLog(context.Background(), al)
	if err != nil {
		t.Fatalf("InsertActionLog failed: %v", err)
	}
	if al.ID == 0 {
		t.Error("expected non-zero ID after insert")
	}
	if al.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt after insert")
	}

	// Verify via ListActionLogs
	logs, err := repo.ListActionLogs(context.Background(), al.ActionTypeRID, 10, 0)
	if err != nil {
		t.Fatalf("ListActionLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].UserID != "user-123" {
		t.Errorf("expected userId 'user-123', got %q", logs[0].UserID)
	}
	if logs[0].Status != "SUCCESS" {
		t.Errorf("expected status 'SUCCESS', got %q", logs[0].Status)
	}
}

func TestInsertActionLog_WithError(t *testing.T) {
	repo := setupRepo(t)

	al := &oms.ActionLog{
		ActionTypeRID: "ri.ontology.main.action-type.at2",
		UserID:        "user-456",
		Parameters:    json.RawMessage(`{}`),
		Edits:         json.RawMessage(`[]`),
		Status:        "FAILED",
		ErrorMessage:  "validation error: missing required field",
	}
	err := repo.InsertActionLog(context.Background(), al)
	if err != nil {
		t.Fatalf("InsertActionLog with error failed: %v", err)
	}

	logs, _ := repo.ListActionLogs(context.Background(), al.ActionTypeRID, 10, 0)
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].ErrorMessage != "validation error: missing required field" {
		t.Errorf("expected error message, got %q", logs[0].ErrorMessage)
	}
}

// --- Bug fix: Ontology CurrentVersion field ---

func TestOntology_CurrentVersion(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)

	got, err := repo.GetOntology(context.Background(), o.RID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.CurrentVersion != 0 {
		t.Errorf("expected CurrentVersion 0 for new ontology, got %d", got.CurrentVersion)
	}

	// Increment version and verify
	newVer, err := repo.IncrementOntologyVersion(context.Background(), o.RID)
	if err != nil {
		t.Fatalf("IncrementOntologyVersion failed: %v", err)
	}
	if newVer != 1 {
		t.Errorf("expected version 1, got %d", newVer)
	}

	got, _ = repo.GetOntology(context.Background(), o.RID)
	if got.CurrentVersion != 1 {
		t.Errorf("expected CurrentVersion 1 after increment, got %d", got.CurrentVersion)
	}
}

func TestOntology_ListIncludesCurrentVersion(t *testing.T) {
	repo := setupRepo(t)
	seedOntology(t, repo)

	list, err := repo.ListOntologies(context.Background())
	if err != nil {
		t.Fatalf("ListOntologies failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].CurrentVersion != 0 {
		t.Errorf("expected CurrentVersion 0, got %d", list[0].CurrentVersion)
	}
}

// --- Bug fix: Property SharedPropertyRID field ---

func TestProperty_WithSharedPropertyRID(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	ot := seedObjectType(t, repo, o.RID)

	// Create a shared property first
	sp := &oms.SharedProperty{
		RID: "ri.ontology.main.shared-property.sp1", OntologyRID: o.RID,
		APIName: "latitude", DisplayName: "Latitude", BaseType: "double",
	}
	err := repo.CreateSharedProperty(context.Background(), sp)
	if err != nil {
		t.Fatalf("create shared property failed: %v", err)
	}

	// Create property linked to shared property
	p := &oms.Property{
		RID:               "ri.ontology.main.property.psp1",
		ObjectTypeRID:     ot.RID,
		APIName:           "lat",
		BaseType:          "double",
		IsNullable:        true,
		IsSearchable:      true,
		IsSortable:        true,
		SharedPropertyRID: sp.RID,
	}
	err = repo.CreateProperty(context.Background(), p)
	if err != nil {
		t.Fatalf("create property with shared_property_rid failed: %v", err)
	}

	got, err := repo.GetProperty(context.Background(), p.RID)
	if err != nil {
		t.Fatalf("get property failed: %v", err)
	}
	if got.SharedPropertyRID != sp.RID {
		t.Errorf("expected SharedPropertyRID %q, got %q", sp.RID, got.SharedPropertyRID)
	}

	// Verify via ListProperties too
	list, _ := repo.ListProperties(context.Background(), ot.RID)
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].SharedPropertyRID != sp.RID {
		t.Errorf("ListProperties: expected SharedPropertyRID %q, got %q", sp.RID, list[0].SharedPropertyRID)
	}
}
