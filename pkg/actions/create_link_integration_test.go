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
