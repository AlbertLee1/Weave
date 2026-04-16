package actions

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// In-memory link-edge store for integration tests
// ---------------------------------------------------------------------------

type inMemoryEdgeStore struct {
	mu    sync.Mutex
	edges []*oms.LinkEdge
}

func (s *inMemoryEdgeStore) UpsertLinkEdge(_ context.Context, edge *oms.LinkEdge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Upsert: replace existing edge with same key, or append.
	for i, e := range s.edges {
		if e.LinkTypeRID == edge.LinkTypeRID &&
			e.SourceObjectPK == edge.SourceObjectPK &&
			e.TargetObjectPK == edge.TargetObjectPK {
			s.edges[i] = edge
			return nil
		}
	}
	cp := *edge
	s.edges = append(s.edges, &cp)
	return nil
}

func (s *inMemoryEdgeStore) ListEdgeTargets(_ context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	srcSet := make(map[string]bool, len(sourcePKs))
	for _, pk := range sourcePKs {
		srcSet[pk] = true
	}
	seen := map[string]bool{}
	var result []string
	for _, e := range s.edges {
		if e.LinkTypeRID == linkTypeRID && srcSet[e.SourceObjectPK] && !seen[e.TargetObjectPK] {
			result = append(result, e.TargetObjectPK)
			seen[e.TargetObjectPK] = true
		}
	}
	return result, nil
}

func (s *inMemoryEdgeStore) DeleteLinkEdge(_ context.Context, linkTypeRID, sourcePK, targetPK string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.edges {
		if e.LinkTypeRID == linkTypeRID &&
			e.SourceObjectPK == sourcePK &&
			e.TargetObjectPK == targetPK {
			s.edges = append(s.edges[:i], s.edges[i+1:]...)
			return nil
		}
	}
	return nil // idempotent
}

func (s *inMemoryEdgeStore) ListEdgeSources(_ context.Context, linkTypeRID string, targetPKs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tgtSet := make(map[string]bool, len(targetPKs))
	for _, pk := range targetPKs {
		tgtSet[pk] = true
	}
	seen := map[string]bool{}
	var result []string
	for _, e := range s.edges {
		if e.LinkTypeRID == linkTypeRID && tgtSet[e.TargetObjectPK] && !seen[e.SourceObjectPK] {
			result = append(result, e.SourceObjectPK)
			seen[e.SourceObjectPK] = true
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Extended mock that resolves link types by API name
// ---------------------------------------------------------------------------

type linkAwareMockRepo struct {
	mockOmsRepo
	linkTypesByAPIName map[string]*oms.LinkType
	edgeStore          *inMemoryEdgeStore
}

func (m *linkAwareMockRepo) GetLinkTypeByAPIName(_ context.Context, _, apiName string) (*oms.LinkType, error) {
	if lt, ok := m.linkTypesByAPIName[apiName]; ok {
		return lt, nil
	}
	return nil, oms.ErrNotFound
}

func (m *linkAwareMockRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	for _, lt := range m.linkTypesByAPIName {
		if lt.RID == rid {
			return lt, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *linkAwareMockRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	var result []oms.LinkType
	for _, lt := range m.linkTypesByAPIName {
		if lt.SourceObjectType == objectTypeRID {
			result = append(result, *lt)
		}
	}
	return result, nil
}

func (m *linkAwareMockRepo) UpsertLinkEdge(ctx context.Context, edge *oms.LinkEdge) error {
	return m.edgeStore.UpsertLinkEdge(ctx, edge)
}

// ---------------------------------------------------------------------------
// Integration Test: action with createLink → searchAround finds linked object
// ---------------------------------------------------------------------------

func TestIntegration_CreateLink_SearchAround(t *testing.T) {
	const ontology = "test-ont"

	// 1. Set up in-memory edge store.
	edgeStore := &inMemoryEdgeStore{}

	// 2. Define a M2M link type.
	empDeptLinkType := &oms.LinkType{
		RID:              "ri.ontology.main.link-type.emp-dept",
		APIName:          "employeeDepartment",
		SourceObjectType: "ri.ontology.main.object-type.Employee",
		TargetObjectType: "ri.ontology.main.object-type.Department",
		Cardinality:      "MANY_TO_MANY",
	}

	// 3. Build mock OMS repo with link type.
	repo := &linkAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("linkEmployeeDept", []ParameterDef{
					{ID: "employeeId", Type: "string", Required: true},
					{ID: "departmentId", Type: "string", Required: true},
				}, []Rule{
					{
						Type:                   "createLink",
						LinkTypeAPIName:        "employeeDepartment",
						SourceObjectPrimaryKey: "employeeId",
						TargetObjectPrimaryKey: "departmentId",
					},
				}),
			},
		},
		linkTypesByAPIName: map[string]*oms.LinkType{
			"employeeDepartment": empDeptLinkType,
		},
		edgeStore: edgeStore,
	}

	// 4. Set up Bleve index manager + consumer.
	tmpDir := t.TempDir()
	indexMgr := index.NewManager(tmpDir)
	t.Cleanup(func() { indexMgr.Close() })

	empProps := []index.Property{
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	deptProps := []index.Property{
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	if _, err := indexMgr.EnsureIndex(index.ScopedKey(ontology, "Employee"), empProps); err != nil {
		t.Fatalf("EnsureIndex Employee: %v", err)
	}
	if _, err := indexMgr.EnsureIndex(index.ScopedKey(ontology, "Department"), deptProps); err != nil {
		t.Fatalf("EnsureIndex Department: %v", err)
	}

	consumer := funnel.NewConsumer(nil, indexMgr)
	consumer.SetLinkEdgeWriter(edgeStore)

	// 5. Index source and target objects.
	seedBatch := funnel.EditBatch{
		ID:              "seed-1",
		OntologyAPIName: ontology,
		UserID:          "test",
		Timestamp:       time.Now(),
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: "Employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{"name": "Alice"},
			},
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: "Department",
				PrimaryKey: "dept-1",
				Properties: map[string]interface{}{"deptName": "Engineering"},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), seedBatch); err != nil {
		t.Fatalf("seed batch: %v", err)
	}

	// 6. Execute action with createLink rule.
	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), ontology, &ApplyRequest{
		ActionType: "linkEmployeeDept",
		Parameters: map[string]interface{}{
			"employeeId":   "emp-1",
			"departmentId": "dept-1",
		},
	})
	if err != nil {
		t.Fatalf("Apply createLink: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeLinkCreate {
		t.Fatalf("expected LINK_CREATE, got %s", result.Edits[0].Type)
	}

	// 7. Verify the link type API name was resolved to the actual RID.
	if result.Edits[0].LinkTypeRID != empDeptLinkType.RID {
		t.Fatalf("expected LinkTypeRID resolved to %s, got %s",
			empDeptLinkType.RID, result.Edits[0].LinkTypeRID)
	}

	// 8. Simulate consumer processing the link edit (in real flow, this goes via NATS).
	linkBatch := funnel.EditBatch{
		ID:              "link-batch-1",
		OntologyAPIName: ontology,
		UserID:          "test",
		Timestamp:       time.Now(),
		Edits:           result.Edits,
	}
	if err := consumer.ApplyBatch(context.Background(), linkBatch); err != nil {
		t.Fatalf("apply link batch: %v", err)
	}

	// 9. Use link resolver with edge repo to find linked object (searchAround).
	resolver := links.NewResolverWithEdges(repo, indexMgr, edgeStore)
	targetPKs, err := resolver.ResolveLinked(
		context.Background(),
		empDeptLinkType.RID,
		[]string{"emp-1"},
		links.DirectionForward,
	)
	if err != nil {
		t.Fatalf("ResolveLinked: %v", err)
	}
	if len(targetPKs) != 1 {
		t.Fatalf("expected 1 linked target, got %d", len(targetPKs))
	}
	if targetPKs[0] != "dept-1" {
		t.Fatalf("expected linked target dept-1, got %s", targetPKs[0])
	}
}

// ---------------------------------------------------------------------------
// Integration Test: create link → delete link → searchAround returns empty (US-101)
// ---------------------------------------------------------------------------

func TestIntegration_DeleteLink_SearchAround(t *testing.T) {
	const ontology = "test-ont"

	// 1. Set up in-memory edge store.
	edgeStore := &inMemoryEdgeStore{}

	// 2. Define a M2M link type.
	empDeptLinkType := &oms.LinkType{
		RID:              "ri.ontology.main.link-type.emp-dept",
		APIName:          "employeeDepartment",
		SourceObjectType: "ri.ontology.main.object-type.Employee",
		TargetObjectType: "ri.ontology.main.object-type.Department",
		Cardinality:      "MANY_TO_MANY",
	}

	// 3. Build mock OMS repo with both createLink and deleteLink actions.
	repo := &linkAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("linkEmployeeDept", []ParameterDef{
					{ID: "employeeId", Type: "string", Required: true},
					{ID: "departmentId", Type: "string", Required: true},
				}, []Rule{
					{
						Type:                   "createLink",
						LinkTypeAPIName:        "employeeDepartment",
						SourceObjectPrimaryKey: "employeeId",
						TargetObjectPrimaryKey: "departmentId",
					},
				}),
				newTestActionType("unlinkEmployeeDept", []ParameterDef{
					{ID: "employeeId", Type: "string", Required: true},
					{ID: "departmentId", Type: "string", Required: true},
				}, []Rule{
					{
						Type:                   "deleteLink",
						LinkTypeAPIName:        "employeeDepartment",
						SourceObjectPrimaryKey: "employeeId",
						TargetObjectPrimaryKey: "departmentId",
					},
				}),
			},
		},
		linkTypesByAPIName: map[string]*oms.LinkType{
			"employeeDepartment": empDeptLinkType,
		},
		edgeStore: edgeStore,
	}

	// 4. Set up Bleve index manager + consumer.
	tmpDir := t.TempDir()
	indexMgr := index.NewManager(tmpDir)
	t.Cleanup(func() { indexMgr.Close() })

	empProps := []index.Property{
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	deptProps := []index.Property{
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	if _, err := indexMgr.EnsureIndex(index.ScopedKey(ontology, "Employee"), empProps); err != nil {
		t.Fatalf("EnsureIndex Employee: %v", err)
	}
	if _, err := indexMgr.EnsureIndex(index.ScopedKey(ontology, "Department"), deptProps); err != nil {
		t.Fatalf("EnsureIndex Department: %v", err)
	}

	consumer := funnel.NewConsumer(nil, indexMgr)
	consumer.SetLinkEdgeWriter(edgeStore)
	consumer.SetLinkEdgeDeleter(edgeStore)

	// 5. Index source and target objects.
	seedBatch := funnel.EditBatch{
		ID:              "seed-1",
		OntologyAPIName: ontology,
		UserID:          "test",
		Timestamp:       time.Now(),
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: "Employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{"name": "Alice"},
			},
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: "Department",
				PrimaryKey: "dept-1",
				Properties: map[string]interface{}{"deptName": "Engineering"},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), seedBatch); err != nil {
		t.Fatalf("seed batch: %v", err)
	}

	exec := NewExecutor(repo, nil)

	// 6. Create link: employee → department.
	createResult, err := exec.Apply(context.Background(), ontology, &ApplyRequest{
		ActionType: "linkEmployeeDept",
		Parameters: map[string]interface{}{
			"employeeId":   "emp-1",
			"departmentId": "dept-1",
		},
	})
	if err != nil {
		t.Fatalf("Apply createLink: %v", err)
	}

	// Process the link create through the consumer.
	linkCreateBatch := funnel.EditBatch{
		ID:              "link-create-batch",
		OntologyAPIName: ontology,
		UserID:          "test",
		Timestamp:       time.Now(),
		Edits:           createResult.Edits,
	}
	if err := consumer.ApplyBatch(context.Background(), linkCreateBatch); err != nil {
		t.Fatalf("apply link create batch: %v", err)
	}

	// 7. Verify the link exists via searchAround.
	resolver := links.NewResolverWithEdges(repo, indexMgr, edgeStore)
	targetPKs, err := resolver.ResolveLinked(
		context.Background(),
		empDeptLinkType.RID,
		[]string{"emp-1"},
		links.DirectionForward,
	)
	if err != nil {
		t.Fatalf("ResolveLinked after create: %v", err)
	}
	if len(targetPKs) != 1 || targetPKs[0] != "dept-1" {
		t.Fatalf("expected 1 linked target (dept-1), got %v", targetPKs)
	}

	// 8. Delete the link.
	deleteResult, err := exec.Apply(context.Background(), ontology, &ApplyRequest{
		ActionType: "unlinkEmployeeDept",
		Parameters: map[string]interface{}{
			"employeeId":   "emp-1",
			"departmentId": "dept-1",
		},
	})
	if err != nil {
		t.Fatalf("Apply deleteLink: %v", err)
	}
	if len(deleteResult.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(deleteResult.Edits))
	}
	if deleteResult.Edits[0].Type != funnel.EditTypeLinkDelete {
		t.Fatalf("expected LINK_DELETE, got %s", deleteResult.Edits[0].Type)
	}
	if deleteResult.Edits[0].LinkTypeRID != empDeptLinkType.RID {
		t.Fatalf("expected LinkTypeRID resolved to %s, got %s",
			empDeptLinkType.RID, deleteResult.Edits[0].LinkTypeRID)
	}

	// 9. Process the link delete through the consumer.
	linkDeleteBatch := funnel.EditBatch{
		ID:              "link-delete-batch",
		OntologyAPIName: ontology,
		UserID:          "test",
		Timestamp:       time.Now(),
		Edits:           deleteResult.Edits,
	}
	if err := consumer.ApplyBatch(context.Background(), linkDeleteBatch); err != nil {
		t.Fatalf("apply link delete batch: %v", err)
	}

	// 10. Verify searchAround now returns empty.
	targetPKs, err = resolver.ResolveLinked(
		context.Background(),
		empDeptLinkType.RID,
		[]string{"emp-1"},
		links.DirectionForward,
	)
	if err != nil {
		t.Fatalf("ResolveLinked after delete: %v", err)
	}
	if len(targetPKs) != 0 {
		t.Fatalf("expected 0 linked targets after delete, got %v", targetPKs)
	}
}

// ---------------------------------------------------------------------------
// bleveObjectChecker adapts index.Manager for ObjectExistenceChecker (US-102)
// ---------------------------------------------------------------------------

// bleveObjectChecker checks object existence against a Bleve index.
// ontologyAPIName is used to compute the scoped key.
type bleveObjectChecker struct {
	indexMgr        *index.Manager
	ontologyAPIName string
}

func (c *bleveObjectChecker) ObjectExists(_ context.Context, objectType, primaryKey string) bool {
	scopedKey := index.ScopedKey(c.ontologyAPIName, objectType)
	idx := c.indexMgr.GetIndex(scopedKey)
	if idx == nil {
		return false
	}
	doc, err := idx.Document(primaryKey)
	return err == nil && doc != nil
}

// ---------------------------------------------------------------------------
// Integration Test: createOrModifyObject upsert (US-102)
// ---------------------------------------------------------------------------

func TestIntegration_CreateOrModify_UpsertNonExistent_ThenExisting(t *testing.T) {
	const ontology = "test-ont"

	// 1. Set up Bleve index manager + consumer.
	tmpDir := t.TempDir()
	indexMgr := index.NewManager(tmpDir)
	t.Cleanup(func() { indexMgr.Close() })

	empProps := []index.Property{
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "department", BaseType: "string", IsSearchable: true},
	}
	if _, err := indexMgr.EnsureIndex(index.ScopedKey(ontology, "Employee"), empProps); err != nil {
		t.Fatalf("EnsureIndex Employee: %v", err)
	}

	consumer := funnel.NewConsumer(nil, indexMgr)

	// 2. Build mock OMS repo with createOrModifyObject action.
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("upsertEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
				{ID: "department", Type: "string", Required: false},
			}, []Rule{
				{
					Type:       "createOrModifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name":       {Type: "parameter", Value: "name"},
						"department": {Type: "parameter", Value: "department"},
					},
				},
			}),
		},
	}

	// 3. Create executor with Bleve-backed existence checker.
	exec := NewExecutor(repo, nil)
	exec.SetObjectExistenceChecker(&bleveObjectChecker{
		indexMgr:        indexMgr,
		ontologyAPIName: ontology,
	})

	// 4. Upsert a non-existent object → should produce CREATE.
	result1, err := exec.Apply(context.Background(), ontology, &ApplyRequest{
		ActionType: "upsertEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice",
			"department": "Engineering",
		},
	})
	if err != nil {
		t.Fatalf("Apply upsert (non-existent): %v", err)
	}
	if len(result1.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result1.Edits))
	}
	if result1.Edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE for non-existent object, got %s", result1.Edits[0].Type)
	}
	if result1.Edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected primaryKey emp-1, got %s", result1.Edits[0].PrimaryKey)
	}

	// 5. Process the CREATE through the consumer so the object exists in Bleve.
	createBatch := funnel.EditBatch{
		ID:              "create-batch-1",
		OntologyAPIName: ontology,
		UserID:          "test",
		Timestamp:       time.Now(),
		Edits:           result1.Edits,
	}
	if err := consumer.ApplyBatch(context.Background(), createBatch); err != nil {
		t.Fatalf("apply create batch: %v", err)
	}

	// 6. Upsert the same object again → should produce MODIFY (object now exists).
	result2, err := exec.Apply(context.Background(), ontology, &ApplyRequest{
		ActionType: "upsertEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice Updated",
			"department": "Product",
		},
	})
	if err != nil {
		t.Fatalf("Apply upsert (existing): %v", err)
	}
	if len(result2.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result2.Edits))
	}
	if result2.Edits[0].Type != funnel.EditTypeModify {
		t.Fatalf("expected MODIFY for existing object, got %s", result2.Edits[0].Type)
	}
	if result2.Edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected primaryKey emp-1, got %s", result2.Edits[0].PrimaryKey)
	}
	if result2.Edits[0].Properties["name"] != "Alice Updated" {
		t.Fatalf("expected name='Alice Updated', got %v", result2.Edits[0].Properties["name"])
	}
	if result2.Edits[0].Properties["department"] != "Product" {
		t.Fatalf("expected department='Product', got %v", result2.Edits[0].Properties["department"])
	}
}

// ---------------------------------------------------------------------------
// Integration Test: interface-backed rule creates correct concrete type (US-103)
// ---------------------------------------------------------------------------

// interfaceAwareIntegrationRepo extends linkAwareMockRepo with interface support.
type interfaceAwareIntegrationRepo struct {
	mockOmsRepo
	interfaces           map[string]*oms.Interface
	objectTypesByName    map[string]*oms.ObjectType
	objectTypeInterfaces map[string][]oms.ObjectTypeInterface
}

func (m *interfaceAwareIntegrationRepo) GetInterfaceByAPIName(_ context.Context, _, apiName string) (*oms.Interface, error) {
	if iface, ok := m.interfaces[apiName]; ok {
		return iface, nil
	}
	return nil, oms.ErrNotFound
}

func (m *interfaceAwareIntegrationRepo) GetObjectTypeByAPIName(_ context.Context, _, apiName string) (*oms.ObjectType, error) {
	if ot, ok := m.objectTypesByName[apiName]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (m *interfaceAwareIntegrationRepo) ListObjectTypeInterfaces(_ context.Context, objectTypeRID string) ([]oms.ObjectTypeInterface, error) {
	if otis, ok := m.objectTypeInterfaces[objectTypeRID]; ok {
		return otis, nil
	}
	return nil, nil
}

func TestIntegration_CreateInterfaceObject_CorrectConcreteType(t *testing.T) {
	const ontology = "test-ont"

	// 1. Define interface and implementing object types.
	geoInterface := &oms.Interface{
		RID:     "ri.ontology.main.interface.geo-entity",
		APIName: "GeoEntity",
	}
	buildingOT := &oms.ObjectType{
		RID:     "ri.ontology.main.object-type.Building",
		APIName: "Building",
	}

	// 2. Build mock OMS repo with interface-backed action.
	repo := &interfaceAwareIntegrationRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createGeoEntity", []ParameterDef{
					{ID: "objectType", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
					{ID: "latitude", Type: "double", Required: false},
				}, []Rule{
					{
						Type:             "createInterfaceObject",
						InterfaceAPIName: "GeoEntity",
						PropertyBindings: map[string]PropertyBinding{
							"name":     {Type: "parameter", Value: "name"},
							"latitude": {Type: "parameter", Value: "latitude"},
						},
					},
				}),
			},
		},
		interfaces: map[string]*oms.Interface{
			"GeoEntity": geoInterface,
		},
		objectTypesByName: map[string]*oms.ObjectType{
			"Building": buildingOT,
		},
		objectTypeInterfaces: map[string][]oms.ObjectTypeInterface{
			buildingOT.RID: {
				{ObjectTypeRID: buildingOT.RID, InterfaceRID: geoInterface.RID},
			},
		},
	}

	// 3. Set up Bleve index + consumer.
	tmpDir := t.TempDir()
	indexMgr := index.NewManager(tmpDir)
	t.Cleanup(func() { indexMgr.Close() })

	buildingProps := []index.Property{
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "latitude", BaseType: "double", IsSearchable: false},
	}
	if _, err := indexMgr.EnsureIndex(index.ScopedKey(ontology, "Building"), buildingProps); err != nil {
		t.Fatalf("EnsureIndex Building: %v", err)
	}

	consumer := funnel.NewConsumer(nil, indexMgr)

	// 4. Execute action with createInterfaceObject rule — concrete type is "Building".
	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), ontology, &ApplyRequest{
		ActionType: "createGeoEntity",
		Parameters: map[string]interface{}{
			"objectType": "Building",
			"name":       "Headquarters",
			"latitude":   37.7749,
		},
	})
	if err != nil {
		t.Fatalf("Apply createInterfaceObject: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE, got %s", result.Edits[0].Type)
	}
	if result.Edits[0].ObjectType != "Building" {
		t.Fatalf("expected objectType=Building, got %s", result.Edits[0].ObjectType)
	}

	// 5. Process the create through consumer so it appears in Bleve.
	createBatch := funnel.EditBatch{
		ID:              "interface-create-1",
		OntologyAPIName: ontology,
		UserID:          "test",
		Timestamp:       time.Now(),
		Edits:           result.Edits,
	}
	if err := consumer.ApplyBatch(context.Background(), createBatch); err != nil {
		t.Fatalf("apply create batch: %v", err)
	}

	// 6. Verify the object exists in the Building index with correct properties.
	scopedKey := index.ScopedKey(ontology, "Building")
	idx := indexMgr.GetIndex(scopedKey)
	if idx == nil {
		t.Fatal("Building index not found")
	}
	doc, err := idx.Document(result.Edits[0].PrimaryKey)
	if err != nil || doc == nil {
		t.Fatalf("expected object in Building index, got err=%v doc=%v", err, doc)
	}

	// 7. Verify a non-implementing type is rejected.
	_, err = exec.Apply(context.Background(), ontology, &ApplyRequest{
		ActionType: "createGeoEntity",
		Parameters: map[string]interface{}{
			"objectType": "NonExistent",
			"name":       "Should Fail",
		},
	})
	if err == nil {
		t.Fatal("expected error for non-implementing type, got nil")
	}
}

// TestIntegration_CreateLink_ActionResponse_HasLinkCount verifies the response
// envelope correctly reports addedLinksCount for createLink actions.
func TestIntegration_CreateLink_ActionResponse_HasLinkCount(t *testing.T) {
	edgeStore := &inMemoryEdgeStore{}
	empDeptLinkType := &oms.LinkType{
		RID:     "ri.ontology.main.link-type.emp-dept",
		APIName: "employeeDepartment",
	}
	repo := &linkAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createAndLink", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
					{ID: "employeeId", Type: "string", Required: true},
					{ID: "departmentId", Type: "string", Required: true},
				}, []Rule{
					{
						Type:       "createObject",
						ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						},
					},
					{
						Type:                   "createLink",
						LinkTypeAPIName:        "employeeDepartment",
						SourceObjectPrimaryKey: "employeeId",
						TargetObjectPrimaryKey: "departmentId",
					},
				}),
			},
		},
		linkTypesByAPIName: map[string]*oms.LinkType{
			"employeeDepartment": empDeptLinkType,
		},
		edgeStore: edgeStore,
	}

	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createAndLink",
		Parameters: map[string]interface{}{
			"name":         "Alice",
			"employeeId":   "emp-1",
			"departmentId": "dept-1",
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify edit counts
	counts := countEdits(result.Edits)
	if counts.AddedObjectCount != 1 {
		t.Fatalf("expected addedObjectCount=1, got %d", counts.AddedObjectCount)
	}
	if counts.AddedLinksCount != 1 {
		t.Fatalf("expected addedLinksCount=1, got %d", counts.AddedLinksCount)
	}

	// Verify the ActionLog was written
	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}

	// Verify log contains both edits
	var logEdits []json.RawMessage
	if err := json.Unmarshal(repo.insertedLogs[0].Edits, &logEdits); err != nil {
		t.Fatalf("unmarshal log edits: %v", err)
	}
	if len(logEdits) != 2 {
		t.Fatalf("expected 2 edits in log, got %d", len(logEdits))
	}
}
