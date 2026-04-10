package oss_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
)

// --- Mock OMS Repository ---

type mockOmsRepo struct {
	objectTypes map[string]*oms.ObjectType // key: ontologyRID+":"+apiName
	byRID       map[string]*oms.ObjectType // key: RID
	linkTypes   map[string][]oms.LinkType  // key: objectTypeRID

	// LinkType lookup by (ontologyRID, apiName) for route-removal tests.
	linkTypesByAPIName map[string]*oms.LinkType // key: ontologyRID+":"+apiName

	// securityPolicies maps objectTypeRID -> attached SecurityPolicies. Tests
	// populate this directly to exercise the ABAC filter path.
	securityPolicies map[string][]oms.SecurityPolicy
}

func newMockOmsRepo() *mockOmsRepo {
	return &mockOmsRepo{
		objectTypes:        make(map[string]*oms.ObjectType),
		byRID:              make(map[string]*oms.ObjectType),
		linkTypes:          make(map[string][]oms.LinkType),
		linkTypesByAPIName: make(map[string]*oms.LinkType),
		securityPolicies:   make(map[string][]oms.SecurityPolicy),
	}
}

// addLinkTypeByAPIName registers a link type so the mock can resolve it via
// GetLinkTypeByAPIName during CreateLink / DeleteLink service calls.
func (m *mockOmsRepo) addLinkTypeByAPIName(ontologyRID string, lt *oms.LinkType) {
	m.linkTypesByAPIName[ontologyRID+":"+lt.APIName] = lt
}

func (m *mockOmsRepo) addObjectType(ot *oms.ObjectType) {
	key := ot.OntologyRID + ":" + ot.APIName
	m.objectTypes[key] = ot
	m.byRID[ot.RID] = ot
}

func (m *mockOmsRepo) addLinkType(lt oms.LinkType) {
	m.linkTypes[lt.SourceObjectType] = append(m.linkTypes[lt.SourceObjectType], lt)
}

func (m *mockOmsRepo) CreateOntology(_ context.Context, _ *oms.Ontology) error {
	return nil
}

func (m *mockOmsRepo) GetOntology(_ context.Context, _ string) (*oms.Ontology, error) {
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	return nil, nil
}

func (m *mockOmsRepo) UpdateOntology(_ context.Context, _ *oms.Ontology) error {
	return nil
}

func (m *mockOmsRepo) CreateObjectType(_ context.Context, _ *oms.ObjectType) error {
	return nil
}

func (m *mockOmsRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	if ot, ok := m.byRID[rid]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	key := ontologyRID + ":" + apiName
	if ot, ok := m.objectTypes[key]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) ListObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}

func (m *mockOmsRepo) UpdateObjectType(_ context.Context, _ *oms.ObjectType) error {
	return nil
}

func (m *mockOmsRepo) DeleteObjectType(_ context.Context, _ string) error {
	return nil
}

func (m *mockOmsRepo) CreateProperty(_ context.Context, _ *oms.Property) error {
	return nil
}

func (m *mockOmsRepo) GetProperty(_ context.Context, _ string) (*oms.Property, error) {
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) ListProperties(_ context.Context, _ string) ([]oms.Property, error) {
	return nil, nil
}

func (m *mockOmsRepo) UpdateProperty(_ context.Context, _ *oms.Property) error {
	return nil
}

func (m *mockOmsRepo) DeleteProperty(_ context.Context, _ string) error {
	return nil
}

func (m *mockOmsRepo) CreateLinkType(_ context.Context, _ *oms.LinkType) error {
	return nil
}

func (m *mockOmsRepo) GetLinkType(_ context.Context, _ string) (*oms.LinkType, error) {
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	return m.linkTypes[objectTypeRID], nil
}

func (m *mockOmsRepo) ListIncomingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	var result []oms.LinkType
	for _, lts := range m.linkTypes {
		for _, lt := range lts {
			if lt.TargetObjectType == objectTypeRID {
				result = append(result, lt)
			}
		}
	}
	return result, nil
}

func (m *mockOmsRepo) ListLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}

func (m *mockOmsRepo) UpdateLinkType(_ context.Context, _ *oms.LinkType) error {
	return nil
}

func (m *mockOmsRepo) DeleteLinkType(_ context.Context, _ string) error {
	return nil
}

// LinkEdge stubs — required by oms.Repository interface (used by actions).
func (m *mockOmsRepo) UpsertLinkEdge(_ context.Context, _ *oms.LinkEdge) error { return nil }
func (m *mockOmsRepo) DeleteLinkEdge(_ context.Context, _, _, _ string) error  { return nil }
func (m *mockOmsRepo) DeleteAllLinkEdgesForSource(_ context.Context, _, _ string) error {
	return nil
}

// GetLinkTypeByAPIName resolves a LinkType by ontology + API name.
// Required by oms.Repository interface.
func (m *mockOmsRepo) GetLinkTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.LinkType, error) {
	if lt, ok := m.linkTypesByAPIName[ontologyRID+":"+apiName]; ok {
		return lt, nil
	}
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) CreateActionType(_ context.Context, _ *oms.ActionType) error {
	return nil
}

func (m *mockOmsRepo) GetActionType(_ context.Context, _ string) (*oms.ActionType, error) {
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) GetActionTypeByAPIName(_ context.Context, _, _ string) (*oms.ActionType, error) {
	return nil, oms.ErrNotFound
}

func (m *mockOmsRepo) ListActionTypes(_ context.Context, _ string) ([]oms.ActionType, error) {
	return nil, nil
}

func (m *mockOmsRepo) UpdateActionType(_ context.Context, _ *oms.ActionType) error {
	return nil
}

func (m *mockOmsRepo) DeleteActionType(_ context.Context, _ string) error {
	return nil
}

func (m *mockOmsRepo) CreateInterface(_ context.Context, _ *oms.Interface) error {
	return nil
}

func (m *mockOmsRepo) GetInterface(_ context.Context, _ string) (*oms.Interface, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetInterfaceByAPIName(_ context.Context, _, _ string) (*oms.Interface, error) {
	return nil, nil
}

func (m *mockOmsRepo) ListInterfaces(_ context.Context, _ string) ([]oms.Interface, error) {
	return nil, nil
}

func (m *mockOmsRepo) UpdateInterface(_ context.Context, _ *oms.Interface) error { return nil }

func (m *mockOmsRepo) DeleteInterface(_ context.Context, _ string) error { return nil }

func (m *mockOmsRepo) AttachInterface(_ context.Context, _ *oms.ObjectTypeInterface) error {
	return nil
}

func (m *mockOmsRepo) DetachInterface(_ context.Context, _, _ string) error { return nil }

// SharedProperty stubs
func (m *mockOmsRepo) CreateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (m *mockOmsRepo) GetSharedProperty(_ context.Context, _ string) (*oms.SharedProperty, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListSharedProperties(_ context.Context, _ string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (m *mockOmsRepo) DeleteSharedProperty(_ context.Context, _ string) error { return nil }

// TypeGroup stubs
func (m *mockOmsRepo) CreateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (m *mockOmsRepo) GetTypeGroup(_ context.Context, _ string) (*oms.TypeGroup, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListTypeGroups(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (m *mockOmsRepo) DeleteTypeGroup(_ context.Context, _ string) error         { return nil }
func (m *mockOmsRepo) AssignTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *mockOmsRepo) RemoveTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *mockOmsRepo) ListTypeGroupsForObjectType(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}

// ValueType stubs
func (m *mockOmsRepo) CreateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *mockOmsRepo) GetValueType(_ context.Context, _ string) (*oms.ValueType, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetValueTypeByAPIName(_ context.Context, _ string) (*oms.ValueType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListValueTypes(_ context.Context) ([]oms.ValueType, error) { return nil, nil }
func (m *mockOmsRepo) UpdateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *mockOmsRepo) DeleteValueType(_ context.Context, _ string) error         { return nil }

func (m *mockOmsRepo) ListInterfaceObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}

func (m *mockOmsRepo) ListObjectTypeInterfaces(_ context.Context, _ string) ([]oms.ObjectTypeInterface, error) {
	return nil, nil
}

// DatasourceBinding stubs
func (m *mockOmsRepo) CreateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *mockOmsRepo) GetDatasourceBinding(_ context.Context, _ string) (*oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListDatasourceBindings(_ context.Context, _ string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *mockOmsRepo) DeleteDatasourceBinding(_ context.Context, _ string) error { return nil }

// QueryType stubs
func (m *mockOmsRepo) CreateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (m *mockOmsRepo) GetQueryType(_ context.Context, _ string) (*oms.QueryType, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetQueryTypeByAPIName(_ context.Context, _, _ string) (*oms.QueryType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListQueryTypes(_ context.Context, _ string) ([]oms.QueryType, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (m *mockOmsRepo) DeleteQueryType(_ context.Context, _ string) error         { return nil }

// ActionLog stubs
func (m *mockOmsRepo) InsertActionLog(_ context.Context, _ *oms.ActionLog) error { return nil }
func (m *mockOmsRepo) ListActionLogs(_ context.Context, _ string, _, _ int) ([]oms.ActionLog, error) {
	return nil, nil
}
func (m *mockOmsRepo) CountActionLogs(_ context.Context, _ string) (int, error) { return 0, nil }

// ObjectHistory stubs (Tier 2.3)
func (m *mockOmsRepo) InsertObjectHistory(_ context.Context, _ *oms.ObjectHistory) error {
	return nil
}
func (m *mockOmsRepo) ListObjectHistory(_ context.Context, _, _ string, _ int) ([]oms.ObjectHistory, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetObjectVersionCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

// Search stubs
func (m *mockOmsRepo) SearchOntologyResources(_ context.Context, _, _ string) ([]oms.SearchResult, error) {
	return nil, nil
}

// Snapshot stubs
func (m *mockOmsRepo) CreateSnapshot(_ context.Context, _ *oms.OntologySnapshot) error { return nil }
func (m *mockOmsRepo) ListSnapshots(_ context.Context, _ string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetSnapshot(_ context.Context, _ string, _ int) (*oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetOntologyVersion(_ context.Context, _ string) (int, error)       { return 0, nil }
func (m *mockOmsRepo) IncrementOntologyVersion(_ context.Context, _ string) (int, error) { return 1, nil }

// --- Mock Link Resolver ---

type mockLinkResolver struct {
	results        map[string][]string // linkTypeAPIName or RID -> target PKs
	reverseResults map[string][]string // linkTypeAPIName or RID -> reverse PKs
	err            error
}

func (m *mockLinkResolver) ResolveLinkedObjects(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}

func (m *mockLinkResolver) ResolveLinkedObjectsByAPIName(_ context.Context, _, linkTypeAPIName string, _ []string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results[linkTypeAPIName], nil
}

// ResolveLinked resolves links by RID with direction awareness. The service
// implementation looks up the LinkType first and passes its RID here, so the
// mock keys results on the RID it was stubbed with. For simplicity the mock
// stores its results under the *API name* and also under the *RID*, plus
// optional reverse-direction results via a second map.
func (m *mockLinkResolver) ResolveLinked(_ context.Context, linkTypeKey string, _ []string, dir links.Direction) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	if dir == links.DirectionReverse && m.reverseResults != nil {
		if v, ok := m.reverseResults[linkTypeKey]; ok {
			return v, nil
		}
	}
	return m.results[linkTypeKey], nil
}

// --- Test Setup ---

const testOntologyRID = "ri.ontology.main.ontology.test"

func setupOSSTest(t *testing.T) (*oss.ServiceImpl, *index.Manager, *mockOmsRepo, *mockLinkResolver) {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "age", BaseType: "integer", IsSearchable: true},
		{APIName: "active", BaseType: "boolean", IsSearchable: true},
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("employee", props)
	if err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Seed data
	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"emp1", map[string]interface{}{"employeeId": "emp1", "name": "alice", "age": float64(30), "active": true, "deptId": "d1"}},
		{"emp2", map[string]interface{}{"employeeId": "emp2", "name": "bob", "age": float64(25), "active": false, "deptId": "d1"}},
		{"emp3", map[string]interface{}{"employeeId": "emp3", "name": "charlie", "age": float64(35), "active": true, "deptId": "d2"}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.id, err)
		}
	}

	// Small delay to let the index settle
	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.employee",
		OntologyRID: testOntologyRID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	linkResolver := &mockLinkResolver{
		results: make(map[string][]string),
	}

	svc := oss.NewService(repo, mgr, linkResolver)
	return svc, mgr, repo, linkResolver
}

// --- WireObject Tests (3 tests) ---

func TestFormatObject_Fields(t *testing.T) {
	props := map[string]interface{}{
		"name": "alice",
		"age":  float64(30),
	}
	obj := oss.FormatObject("employee", "emp1", props)

	if obj.APIName != "employee" {
		t.Errorf("expected APIName 'employee', got %q", obj.APIName)
	}
	if obj.PrimaryKey != "emp1" {
		t.Errorf("expected PrimaryKey 'emp1', got %v", obj.PrimaryKey)
	}
	if obj.Properties["name"] != "alice" {
		t.Errorf("expected name 'alice', got %v", obj.Properties["name"])
	}
	if obj.Properties["age"] != float64(30) {
		t.Errorf("expected age 30, got %v", obj.Properties["age"])
	}
	if obj.RID != "ri.phonograph2-objects.main.object.emp1" {
		t.Errorf("expected RID 'ri.phonograph2-objects.main.object.emp1', got %q", obj.RID)
	}
}

func TestFormatObject_JSON(t *testing.T) {
	props := map[string]interface{}{
		"name": "alice",
	}
	obj := oss.FormatObject("employee", "emp1", props)

	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if parsed["__primaryKey"] != "emp1" {
		t.Errorf("expected __primaryKey 'emp1', got %v", parsed["__primaryKey"])
	}
	if parsed["__apiName"] != "employee" {
		t.Errorf("expected __apiName 'employee', got %v", parsed["__apiName"])
	}
	// __rid should be present with the RID format
	rid, ok := parsed["__rid"]
	if !ok {
		t.Fatal("expected __rid to be present")
	}
	if rid != "ri.phonograph2-objects.main.object.emp1" {
		t.Errorf("expected __rid 'ri.phonograph2-objects.main.object.emp1', got %v", rid)
	}
	// Properties should be flattened at the top level (no "properties" key)
	if _, ok := parsed["properties"]; ok {
		t.Error("expected no nested 'properties' key; properties should be flattened")
	}
	if parsed["name"] != "alice" {
		t.Errorf("expected name 'alice' at top level, got %v", parsed["name"])
	}
}

func TestObjectPage_Empty(t *testing.T) {
	page := &oss.ObjectPage{
		Data:       make([]*oss.WireObject, 0),
		TotalCount: "0",
	}

	data, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	arr, ok := parsed["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(arr) != 0 {
		t.Errorf("expected empty data array, got %d items", len(arr))
	}
	tc, ok := parsed["totalCount"].(string)
	if !ok {
		t.Fatalf("expected totalCount to be a string, got %T", parsed["totalCount"])
	}
	if tc != "0" {
		t.Errorf("expected totalCount '0', got %v", tc)
	}
	if _, ok := parsed["nextPageToken"]; ok {
		t.Error("expected nextPageToken to be omitted")
	}
}

// --- GetObject Tests (4 tests) ---

func TestGetObject_Found(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	obj, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}

	if obj.APIName != "employee" {
		t.Errorf("expected APIName 'employee', got %q", obj.APIName)
	}
	if obj.PrimaryKey != "emp1" {
		t.Errorf("expected PrimaryKey 'emp1', got %v", obj.PrimaryKey)
	}
	if obj.Properties == nil {
		t.Fatal("expected non-nil Properties")
	}
	// Bleve returns stored fields
	if obj.Properties["name"] != "alice" {
		t.Errorf("expected name 'alice', got %v", obj.Properties["name"])
	}
}

func TestGetObject_NotFound(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	_, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != oms.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetObject_PrimaryKeyMatch(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	obj, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp2",
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}

	if obj.PrimaryKey != "emp2" {
		t.Errorf("expected PrimaryKey 'emp2', got %v", obj.PrimaryKey)
	}
	if obj.Properties["name"] != "bob" {
		t.Errorf("expected name 'bob', got %v", obj.Properties["name"])
	}
}

func TestGetObject_UnknownObjectType(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	_, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "nonexistent",
		PrimaryKey:  "emp1",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ListObjects Tests (5 tests) ---

func TestListObjects_All(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	if len(page.Data) != 3 {
		t.Errorf("expected 3 objects, got %d", len(page.Data))
	}
	if page.TotalCount != "3" {
		t.Errorf("expected totalCount '3', got %v", page.TotalCount)
	}
	if page.NextPageToken != "" {
		t.Errorf("expected no nextPageToken, got %q", page.NextPageToken)
	}
}

func TestListObjects_Pagination_FirstPage(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PageSize:    2,
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	if len(page.Data) != 2 {
		t.Errorf("expected 2 objects, got %d", len(page.Data))
	}
	if page.NextPageToken == "" {
		t.Error("expected nextPageToken to be set")
	}
	if page.TotalCount != "3" {
		t.Errorf("expected totalCount '3', got %v", page.TotalCount)
	}
}

func TestListObjects_Pagination_SecondPage(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	// Get first page
	firstPage, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PageSize:    2,
	})
	if err != nil {
		t.Fatalf("ListObjects first page: %v", err)
	}
	if firstPage.NextPageToken == "" {
		t.Fatal("expected nextPageToken on first page")
	}

	// Get second page
	secondPage, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PageSize:    2,
		PageToken:   firstPage.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListObjects second page: %v", err)
	}

	if len(secondPage.Data) != 1 {
		t.Errorf("expected 1 object on second page, got %d", len(secondPage.Data))
	}
	if secondPage.NextPageToken != "" {
		t.Errorf("expected no nextPageToken on last page, got %q", secondPage.NextPageToken)
	}
}

func TestListObjects_Empty(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("emptyType", props)
	if err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.empty",
		OntologyRID: testOntologyRID,
		APIName:     "emptyType",
		PrimaryKey:  "id",
	})

	linkResolver := &mockLinkResolver{results: make(map[string][]string)}
	svc := oss.NewService(repo, mgr, linkResolver)
	ctx := context.Background()

	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "emptyType",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	if len(page.Data) != 0 {
		t.Errorf("expected 0 objects, got %d", len(page.Data))
	}
	if page.TotalCount != "0" {
		t.Errorf("expected totalCount '0', got %v", page.TotalCount)
	}
}

func TestListObjects_DefaultPageSize(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	// PageSize=0 should use default (100), which is more than our 3 docs
	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PageSize:    0,
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	if len(page.Data) != 3 {
		t.Errorf("expected 3 objects with default page size, got %d", len(page.Data))
	}
	// No next page since all fit in default page
	if page.NextPageToken != "" {
		t.Errorf("expected no nextPageToken, got %q", page.NextPageToken)
	}
}

// --- SearchObjects Tests (6 tests) ---

func TestSearchObjects_EqString(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "name",
			Value: json.RawMessage(`"alice"`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	if len(page.Data) != 1 {
		t.Fatalf("expected 1 result, got %d", len(page.Data))
	}
	if page.Data[0].PrimaryKey != "emp1" {
		t.Errorf("expected PrimaryKey 'emp1', got %v", page.Data[0].PrimaryKey)
	}
}

func TestSearchObjects_EqNumber(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "age",
			Value: json.RawMessage(`30`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	if len(page.Data) != 1 {
		t.Fatalf("expected 1 result, got %d", len(page.Data))
	}
	if page.Data[0].PrimaryKey != "emp1" {
		t.Errorf("expected PrimaryKey 'emp1', got %v", page.Data[0].PrimaryKey)
	}
}

func TestSearchObjects_EqBoolean(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "active",
			Value: json.RawMessage(`true`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	if len(page.Data) != 2 {
		t.Errorf("expected 2 results for active=true, got %d", len(page.Data))
	}
}

func TestSearchObjects_And(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type: "and",
			Value: json.RawMessage(`[
				{"type":"eq","field":"active","value":true},
				{"type":"gt","field":"age","value":30}
			]`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	if len(page.Data) != 1 {
		t.Fatalf("expected 1 result (active=true AND age>30), got %d", len(page.Data))
	}
	if page.Data[0].PrimaryKey != "emp3" {
		t.Errorf("expected PrimaryKey 'emp3', got %v", page.Data[0].PrimaryKey)
	}
}

func TestSearchObjects_NoMatch(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "name",
			Value: json.RawMessage(`"nobody"`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	if len(page.Data) != 0 {
		t.Errorf("expected 0 results, got %d", len(page.Data))
	}
	if page.TotalCount != "0" {
		t.Errorf("expected totalCount '0', got %v", page.TotalCount)
	}
}

func TestSearchObjects_WithPagination(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	// Search active=true which returns 2, but page size 1
	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "active",
			Value: json.RawMessage(`true`),
		},
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	if len(page.Data) != 1 {
		t.Errorf("expected 1 result with pageSize=1, got %d", len(page.Data))
	}
	if page.NextPageToken == "" {
		t.Error("expected nextPageToken to be set")
	}
	if page.TotalCount != "2" {
		t.Errorf("expected totalCount '2', got %v", page.TotalCount)
	}
}

// --- ListLinkedObjects Tests (4 tests) ---

func TestListLinkedObjects_Found(t *testing.T) {
	svc, mgr, repo, linkResolver := setupOSSTest(t)
	ctx := context.Background()

	// Set up department index and data
	deptProps := []index.Property{
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("department", deptProps)
	if err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{
		"deptId":   "d1",
		"deptName": "engineering",
	}); err != nil {
		t.Fatalf("IndexDocument d1: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Register department object type
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		DisplayName: "Department",
		PrimaryKey:  "deptId",
		Status:      "ACTIVE",
	})

	// Register link type
	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "MANY_TO_ONE",
	})

	// Configure link resolver to return d1 for employeeDept.
	// Service calls ResolveLinked with the link type RID.
	linkResolver.results["employeeDept"] = []string{"d1"}
	linkResolver.results["ri.ontology.main.link-type.empDept"] = []string{"d1"}

	page, err := svc.ListLinkedObjects(ctx, oss.LinkedObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
		LinkType:    "employeeDept",
	})
	if err != nil {
		t.Fatalf("ListLinkedObjects: %v", err)
	}

	if len(page.Data) != 1 {
		t.Fatalf("expected 1 linked object, got %d", len(page.Data))
	}
	if page.Data[0].APIName != "department" {
		t.Errorf("expected APIName 'department', got %q", page.Data[0].APIName)
	}
	if page.Data[0].PrimaryKey != "d1" {
		t.Errorf("expected PrimaryKey 'd1', got %v", page.Data[0].PrimaryKey)
	}
}

func TestListLinkedObjects_NoLinks(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()

	// Register a target object type
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		PrimaryKey:  "deptId",
	})

	// Register link type
	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "MANY_TO_ONE",
	})

	// Link resolver returns no results
	page, err := svc.ListLinkedObjects(ctx, oss.LinkedObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
		LinkType:    "employeeDept",
	})
	if err != nil {
		t.Fatalf("ListLinkedObjects: %v", err)
	}

	if len(page.Data) != 0 {
		t.Errorf("expected 0 linked objects, got %d", len(page.Data))
	}
	if page.TotalCount != "0" {
		t.Errorf("expected totalCount '0', got %v", page.TotalCount)
	}
}

func TestListLinkedObjects_WithPagination(t *testing.T) {
	svc, mgr, repo, linkResolver := setupOSSTest(t)
	ctx := context.Background()

	// Set up department index with 3 departments
	deptProps := []index.Property{
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("department", deptProps)
	if err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("d%d", i)
		if err := mgr.IndexDocument("department", id, map[string]interface{}{
			"deptId":   id,
			"deptName": fmt.Sprintf("dept%d", i),
		}); err != nil {
			t.Fatalf("IndexDocument %s: %v", id, err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		PrimaryKey:  "deptId",
	})

	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDepts",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "ONE_TO_MANY",
	})

	linkResolver.results["employeeDepts"] = []string{"d1", "d2", "d3"}
	linkResolver.results["ri.ontology.main.link-type.empDept"] = []string{"d1", "d2", "d3"}

	// First page
	page, err := svc.ListLinkedObjects(ctx, oss.LinkedObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
		LinkType:    "employeeDepts",
		PageSize:    2,
	})
	if err != nil {
		t.Fatalf("ListLinkedObjects first page: %v", err)
	}

	if len(page.Data) != 2 {
		t.Errorf("expected 2 linked objects on first page, got %d", len(page.Data))
	}
	if page.NextPageToken == "" {
		t.Error("expected nextPageToken to be set")
	}
	if page.TotalCount != "3" {
		t.Errorf("expected totalCount '3', got %v", page.TotalCount)
	}
}

func TestListLinkedObjects_ReverseDirection(t *testing.T) {
	svc, mgr, repo, linkResolver := setupOSSTest(t)
	ctx := context.Background()

	// Set up department index and data. For reverse traversal the caller's
	// ObjectType is the link's TARGET (department), and the response objects
	// are instances of the SOURCE (employee).
	deptProps := []index.Property{
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{
		"deptId": "d1",
	}); err != nil {
		t.Fatalf("IndexDocument d1: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		DisplayName: "Department",
		PrimaryKey:  "deptId",
		Status:      "ACTIVE",
	})

	// Link: employee -> department (forward direction). Querying department
	// with direction=reverse should return employees in department d1.
	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "ONE_TO_MANY",
	})

	// Reverse traversal returns source (employee) PKs.
	linkResolver.reverseResults = map[string][]string{
		"ri.ontology.main.link-type.empDept": {"emp1", "emp2"},
	}

	page, err := svc.ListLinkedObjects(ctx, oss.LinkedObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "department",
		PrimaryKey:  "d1",
		LinkType:    "employeeDept",
		Direction:   "reverse",
	})
	if err != nil {
		t.Fatalf("ListLinkedObjects reverse: %v", err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("expected 2 employees, got %d", len(page.Data))
	}
	if page.Data[0].APIName != "employee" {
		t.Errorf("expected APIName employee, got %q", page.Data[0].APIName)
	}
}

func TestListLinkedObjects_ForwardDefault(t *testing.T) {
	// Omitting the direction field must behave like forward traversal.
	svc, mgr, repo, linkResolver := setupOSSTest(t)
	ctx := context.Background()

	deptProps := []index.Property{{APIName: "deptId", BaseType: "string", IsSearchable: true}}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	_ = mgr.IndexDocument("department", "d1", map[string]interface{}{"deptId": "d1"})
	time.Sleep(200 * time.Millisecond)

	repo.addObjectType(&oms.ObjectType{
		RID: "ri.ontology.main.object-type.department", OntologyRID: testOntologyRID,
		APIName: "department", PrimaryKey: "deptId", Status: "ACTIVE",
	})
	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "ONE_TO_MANY",
	})

	linkResolver.results["ri.ontology.main.link-type.empDept"] = []string{"d1"}

	// No Direction set.
	page, err := svc.ListLinkedObjects(ctx, oss.LinkedObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
		LinkType:    "employeeDept",
	})
	if err != nil {
		t.Fatalf("ListLinkedObjects default dir: %v", err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("expected 1 department, got %d", len(page.Data))
	}
}

func TestListLinkedObjects_InvalidDirection(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	_, err := svc.ListLinkedObjects(ctx, oss.LinkedObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
		LinkType:    "whatever",
		Direction:   "sideways",
	})
	if err == nil {
		t.Fatal("expected error for invalid direction")
	}
}

func TestListLinkedObjects_LinkTypeError(t *testing.T) {
	svc, _, repo, linkResolver := setupOSSTest(t)
	ctx := context.Background()

	// Register a valid link type so the service can locate it, then force
	// the resolver itself to error out.
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		PrimaryKey:  "deptId",
	})
	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.badLink",
		APIName:          "badLink",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "MANY_TO_ONE",
	})

	linkResolver.err = fmt.Errorf("link resolver error")

	_, err := svc.ListLinkedObjects(ctx, oss.LinkedObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
		LinkType:    "badLink",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "link resolver error" {
		t.Errorf("expected 'link resolver error', got %q", err.Error())
	}
}

// --- GetLinkedObject Tests (US-018) ---

func TestGetLinkedObject_Found(t *testing.T) {
	svc, mgr, repo, linkResolver := setupOSSTest(t)
	ctx := context.Background()

	// Set up department index and data
	deptProps := []index.Property{
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("department", deptProps)
	if err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{
		"deptId":   "d1",
		"deptName": "engineering",
	}); err != nil {
		t.Fatalf("IndexDocument d1: %v", err)
	}
	if err := mgr.IndexDocument("department", "d2", map[string]interface{}{
		"deptId":   "d2",
		"deptName": "marketing",
	}); err != nil {
		t.Fatalf("IndexDocument d2: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		DisplayName: "Department",
		PrimaryKey:  "deptId",
		Status:      "ACTIVE",
	})

	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "ONE_TO_MANY",
	})

	linkResolver.results["ri.ontology.main.link-type.empDept"] = []string{"d1", "d2"}

	obj, err := svc.GetLinkedObject(ctx, oss.GetLinkedObjectRequest{
		OntologyRID:            testOntologyRID,
		ObjectType:             "employee",
		PrimaryKey:             "emp1",
		LinkType:               "employeeDept",
		LinkedObjectPrimaryKey: "d1",
	})
	if err != nil {
		t.Fatalf("GetLinkedObject: %v", err)
	}
	if obj.APIName != "department" {
		t.Errorf("expected APIName 'department', got %q", obj.APIName)
	}
	if obj.PrimaryKey != "d1" {
		t.Errorf("expected PrimaryKey 'd1', got %v", obj.PrimaryKey)
	}
	if obj.Properties["deptName"] != "engineering" {
		t.Errorf("expected deptName 'engineering', got %v", obj.Properties["deptName"])
	}
}

func TestGetLinkedObject_NotLinked(t *testing.T) {
	svc, mgr, repo, linkResolver := setupOSSTest(t)
	ctx := context.Background()

	deptProps := []index.Property{
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("department", deptProps)
	if err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		PrimaryKey:  "deptId",
	})

	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "MANY_TO_ONE",
	})

	// Only d1 is linked — d99 is not
	linkResolver.results["ri.ontology.main.link-type.empDept"] = []string{"d1"}

	_, err = svc.GetLinkedObject(ctx, oss.GetLinkedObjectRequest{
		OntologyRID:            testOntologyRID,
		ObjectType:             "employee",
		PrimaryKey:             "emp1",
		LinkType:               "employeeDept",
		LinkedObjectPrimaryKey: "d99",
	})
	if err == nil {
		t.Fatal("expected error for unlinked PK, got nil")
	}
}

func TestGetLinkedObject_NoLinks(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		PrimaryKey:  "deptId",
	})

	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "MANY_TO_ONE",
	})

	// No links resolved (empty result)
	_, err := svc.GetLinkedObject(ctx, oss.GetLinkedObjectRequest{
		OntologyRID:            testOntologyRID,
		ObjectType:             "employee",
		PrimaryKey:             "emp1",
		LinkType:               "employeeDept",
		LinkedObjectPrimaryKey: "d1",
	})
	if err == nil {
		t.Fatal("expected error for no links, got nil")
	}
}

func TestHandler_GetLinkedObject_200(t *testing.T) {
	svc, mgr, repo, linkResolver := setupOSSTest(t)
	handler := oss.NewHandler(svc)

	deptProps := []index.Property{
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("department", deptProps)
	if err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{
		"deptId":   "d1",
		"deptName": "engineering",
	}); err != nil {
		t.Fatalf("IndexDocument d1: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		DisplayName: "Department",
		PrimaryKey:  "deptId",
		Status:      "ACTIVE",
	})

	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "MANY_TO_ONE",
	})

	linkResolver.results["ri.ontology.main.link-type.empDept"] = []string{"d1"}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/links/employeeDept/d1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if body["__apiName"] != "department" {
		t.Errorf("expected __apiName 'department', got %v", body["__apiName"])
	}
	if body["__primaryKey"] != "d1" {
		t.Errorf("expected __primaryKey 'd1', got %v", body["__primaryKey"])
	}
}

func TestHandler_GetLinkedObject_404(t *testing.T) {
	svc, _, repo, linkResolver := setupOSSTest(t)
	handler := oss.NewHandler(svc)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.department",
		OntologyRID: testOntologyRID,
		APIName:     "department",
		PrimaryKey:  "deptId",
	})

	repo.addLinkType(oms.LinkType{
		RID:              "ri.ontology.main.link-type.empDept",
		APIName:          "employeeDept",
		SourceObjectType: "ri.ontology.main.object-type.employee",
		TargetObjectType: "ri.ontology.main.object-type.department",
		Cardinality:      "MANY_TO_ONE",
	})

	linkResolver.results["ri.ontology.main.link-type.empDept"] = []string{"d1"}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/links/employeeDept/d99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- HTTP Handler Tests (3 tests) ---

func TestHandler_GetObject_200(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	handler := oss.NewHandler(svc)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if body["__apiName"] != "employee" {
		t.Errorf("expected __apiName 'employee', got %v", body["__apiName"])
	}
	if body["__primaryKey"] != "emp1" {
		t.Errorf("expected __primaryKey 'emp1', got %v", body["__primaryKey"])
	}
	// Properties should be flattened at top level
	if body["name"] != "alice" {
		t.Errorf("expected name 'alice' at top level, got %v", body["name"])
	}
	// __rid should be present
	if body["__rid"] == nil {
		t.Error("expected __rid to be present")
	}
}

func TestHandler_GetObject_404(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	handler := oss.NewHandler(svc)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+testOntologyRID+"/objects/employee/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if body["errorCode"] != "NOT_FOUND" {
		t.Errorf("expected errorCode 'NOT_FOUND', got %v", body["errorCode"])
	}
}

func TestHandler_ListObjects_200(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	handler := oss.NewHandler(svc)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+testOntologyRID+"/objects/employee?pageSize=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 objects, got %d", len(data))
	}

	if body["nextPageToken"] == nil || body["nextPageToken"] == "" {
		t.Error("expected nextPageToken to be set")
	}

	tc, ok := body["totalCount"].(string)
	if !ok || tc != "3" {
		t.Errorf("expected totalCount '3', got %v", body["totalCount"])
	}

	// Check that chi routing properly handles the GET route
	// (not conflicting with the GET /{primaryKey} route)
	first := data[0].(map[string]interface{})
	if _, ok := first["__apiName"]; !ok {
		t.Error("expected __apiName in object")
	}

	_ = strings.NewReader("") // keep import
}

// --- OrderBy Tests ---

func TestListObjects_OrderByAsc(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		OrderBy:     "age:asc",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	if len(page.Data) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(page.Data))
	}

	// Ages: bob=25, alice=30, charlie=35 => ascending order
	ages := make([]float64, len(page.Data))
	for i, obj := range page.Data {
		ages[i] = obj.Properties["age"].(float64)
	}
	for i := 1; i < len(ages); i++ {
		if ages[i] < ages[i-1] {
			t.Errorf("expected ascending order, got ages %v", ages)
			break
		}
	}
}

func TestListObjects_OrderByDesc(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		OrderBy:     "age:desc",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	if len(page.Data) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(page.Data))
	}

	// Ages: charlie=35, alice=30, bob=25 => descending order
	ages := make([]float64, len(page.Data))
	for i, obj := range page.Data {
		ages[i] = obj.Properties["age"].(float64)
	}
	for i := 1; i < len(ages); i++ {
		if ages[i] > ages[i-1] {
			t.Errorf("expected descending order, got ages %v", ages)
			break
		}
	}
}

func TestSearchObjects_OrderByAsc(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "active",
			Value: json.RawMessage(`true`),
		},
		OrderBy: "age:asc",
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	if len(page.Data) != 2 {
		t.Fatalf("expected 2 results, got %d", len(page.Data))
	}

	// active=true: alice(30), charlie(35) => asc order
	ages := make([]float64, len(page.Data))
	for i, obj := range page.Data {
		ages[i] = obj.Properties["age"].(float64)
	}
	for i := 1; i < len(ages); i++ {
		if ages[i] < ages[i-1] {
			t.Errorf("expected ascending order, got ages %v", ages)
			break
		}
	}
}

func (m *mockOmsRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (m *mockOmsRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) {
	return nil, nil
}

// ListSecurityPolicies returns the policies a test attached to the given
// objectTypeRID. Returns an empty slice when nothing is attached so the
// filter's pass-through path is exercised.
func (m *mockOmsRepo) ListSecurityPolicies(_ context.Context, objectTypeRID string) ([]oms.SecurityPolicy, error) {
	return m.securityPolicies[objectTypeRID], nil
}
func (m *mockOmsRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (m *mockOmsRepo) DeleteSecurityPolicy(_ context.Context, _ string) error              { return nil }

// --- ABAC integration tests on the OSS read path (3 tests) ---

// attachViewerEqualsPolicy attaches a single OBJECT-scope allow policy that
// only matches viewer users when classification == "PUBLIC". Other users and
// objects with non-PUBLIC classification get default-denied.
func attachViewerEqualsPolicy(t *testing.T, repo *mockOmsRepo, objectTypeRID string) {
	t.Helper()
	rules := auth.SecurityPolicyRules{
		Version:  1,
		Effect:   "allow",
		Subjects: auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition: auth.ConditionSpec{
			Op:    "propertyEquals",
			Field: "classification",
			Value: "PUBLIC",
		},
	}
	b, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal rules: %v", err)
	}
	repo.securityPolicies[objectTypeRID] = []oms.SecurityPolicy{{
		RID:           "ri.ontology.main.security-policy.viewer-public",
		ObjectTypeRID: objectTypeRID,
		PolicyType:    "OBJECT",
		Rules:         b,
	}}
}

// setupOSSTestWithPolicies builds the standard 3-employee fixture and seeds
// classification values so policy tests can assert on visibility. Also wires
// in a PolicyFilter on the service.
func setupOSSTestWithPolicies(t *testing.T) (*oss.ServiceImpl, *mockOmsRepo) {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "salary", BaseType: "integer", IsSearchable: true},
		{APIName: "classification", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"e1", map[string]interface{}{"employeeId": "e1", "name": "alice", "salary": float64(100000), "classification": "PUBLIC"}},
		{"e2", map[string]interface{}{"employeeId": "e2", "name": "bob", "salary": float64(150000), "classification": "SECRET"}},
		{"e3", map[string]interface{}{"employeeId": "e3", "name": "charlie", "salary": float64(80000), "classification": "PUBLIC"}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.employee",
		OntologyRID: testOntologyRID,
		APIName:     "employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
	})

	linkResolver := &mockLinkResolver{results: make(map[string][]string)}
	svc := oss.NewService(repo, mgr, linkResolver)
	svc.SetPolicyFilter(oss.NewPolicyFilter(repo))
	return svc, repo
}

func TestListObjects_WithPolicies_FiltersViewer(t *testing.T) {
	svc, repo := setupOSSTestWithPolicies(t)
	attachViewerEqualsPolicy(t, repo, "ri.ontology.main.object-type.employee")

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	ctx := auth.WithUser(context.Background(), user)

	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	// Only the 2 PUBLIC docs should be returned to a viewer.
	if len(page.Data) != 2 {
		t.Fatalf("expected 2 visible objects, got %d", len(page.Data))
	}
	for _, o := range page.Data {
		if o.Properties["classification"] != "PUBLIC" {
			t.Errorf("expected only PUBLIC docs, got %v", o.Properties["classification"])
		}
	}
}

func TestGetObject_WithPolicies_Denied_Returns404(t *testing.T) {
	svc, repo := setupOSSTestWithPolicies(t)
	attachViewerEqualsPolicy(t, repo, "ri.ontology.main.object-type.employee")

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	ctx := auth.WithUser(context.Background(), user)

	// e2 is SECRET so the viewer cannot see it. The service must hide its
	// existence by returning ErrNotFound rather than ErrForbidden.
	_, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "e2",
	})
	if err == nil {
		t.Fatal("expected ErrNotFound for denied object, got nil")
	}
	if err != oms.ErrNotFound {
		t.Errorf("expected ErrNotFound for denied object, got %v", err)
	}

	// Sanity check: the same viewer can read e1 (PUBLIC).
	obj, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "e1",
	})
	if err != nil {
		t.Fatalf("GetObject e1: %v", err)
	}
	if obj.PrimaryKey != "e1" {
		t.Errorf("expected e1, got %v", obj.PrimaryKey)
	}
}

func TestSearchObjects_WithPolicies_RedactsFields(t *testing.T) {
	svc, repo := setupOSSTestWithPolicies(t)

	// Two policies: one OBJECT-scope grant for viewer, plus one PROPERTY-scope
	// mask that hides "salary" from viewers.
	objRules := auth.SecurityPolicyRules{
		Version:   1,
		Effect:    "allow",
		Subjects:  auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition: auth.ConditionSpec{Op: "always"},
	}
	maskRules := auth.SecurityPolicyRules{
		Version:       1,
		Effect:        "allow",
		Subjects:      auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition:     auth.ConditionSpec{Op: "always"},
		PropertyMasks: []string{"salary"},
	}
	objBytes, _ := json.Marshal(objRules)
	maskBytes, _ := json.Marshal(maskRules)
	repo.securityPolicies["ri.ontology.main.object-type.employee"] = []oms.SecurityPolicy{
		{RID: "ri.ontology.main.security-policy.allow", ObjectTypeRID: "ri.ontology.main.object-type.employee", PolicyType: "OBJECT", Rules: objBytes},
		{RID: "ri.ontology.main.security-policy.mask-salary", ObjectTypeRID: "ri.ontology.main.object-type.employee", PolicyType: "PROPERTY", Rules: maskBytes},
	}

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	ctx := auth.WithUser(context.Background(), user)

	page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "classification",
			Value: json.RawMessage(`"PUBLIC"`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if len(page.Data) == 0 {
		t.Fatal("expected at least one PUBLIC result")
	}
	for _, o := range page.Data {
		if _, ok := o.Properties["salary"]; ok {
			t.Errorf("expected salary to be redacted, got %v", o.Properties["salary"])
		}
		if _, ok := o.Properties["name"]; !ok {
			t.Errorf("expected name to be retained, got nil")
		}
	}
}
