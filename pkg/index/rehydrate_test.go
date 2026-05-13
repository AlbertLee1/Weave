package index_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// stubRehydrateRepo implements the subset of oms.Repository that
// EnsureAllIndexes touches: ListOntologies, ListObjectTypes, ListProperties.
// Every other method satisfies the interface with a no-op so the type can be
// passed where oms.Repository is required.
type stubRehydrateRepo struct {
	ontologies     []oms.Ontology
	objectTypes    map[string][]oms.ObjectType        // ontologyRID -> ObjectTypes
	properties     map[string][]oms.Property          // objectTypeRID -> Properties
	listOntologyErr error
	listObjectErr  error
	listPropertyErr error
}

func (s *stubRehydrateRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	if s.listOntologyErr != nil {
		return nil, s.listOntologyErr
	}
	return s.ontologies, nil
}

func (s *stubRehydrateRepo) ListObjectTypes(_ context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	if s.listObjectErr != nil {
		return nil, s.listObjectErr
	}
	return s.objectTypes[ontologyRID], nil
}

func (s *stubRehydrateRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	if s.listPropertyErr != nil {
		return nil, s.listPropertyErr
	}
	return s.properties[objectTypeRID], nil
}

// --- Boilerplate to satisfy the rest of oms.Repository ---

func (s *stubRehydrateRepo) CreateOntology(context.Context, *oms.Ontology) error  { return nil }
func (s *stubRehydrateRepo) GetOntology(context.Context, string) (*oms.Ontology, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateOntology(context.Context, *oms.Ontology) error { return nil }
func (s *stubRehydrateRepo) CreateObjectType(context.Context, *oms.ObjectType) error {
	return nil
}
func (s *stubRehydrateRepo) GetObjectType(context.Context, string) (*oms.ObjectType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetObjectTypeByAPIName(context.Context, string, string) (*oms.ObjectType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateObjectType(context.Context, *oms.ObjectType) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteObjectType(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) CreateProperty(context.Context, *oms.Property) error {
	return nil
}
func (s *stubRehydrateRepo) GetProperty(context.Context, string) (*oms.Property, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateProperty(context.Context, *oms.Property) error { return nil }
func (s *stubRehydrateRepo) DeleteProperty(context.Context, string) error        { return nil }
func (s *stubRehydrateRepo) CreateLinkType(context.Context, *oms.LinkType) error {
	return nil
}
func (s *stubRehydrateRepo) GetLinkType(context.Context, string) (*oms.LinkType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListOutgoingLinkTypes(context.Context, string) ([]oms.LinkType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListIncomingLinkTypes(context.Context, string) ([]oms.LinkType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListLinkTypes(context.Context, string) ([]oms.LinkType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateLinkType(context.Context, *oms.LinkType) error { return nil }
func (s *stubRehydrateRepo) DeleteLinkType(context.Context, string) error        { return nil }
func (s *stubRehydrateRepo) CreateActionType(context.Context, *oms.ActionType) error {
	return nil
}
func (s *stubRehydrateRepo) GetActionType(context.Context, string) (*oms.ActionType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetActionTypeByAPIName(context.Context, string, string) (*oms.ActionType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListActionTypes(context.Context, string) ([]oms.ActionType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateActionType(context.Context, *oms.ActionType) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteActionType(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) CreateInterface(context.Context, *oms.Interface) error {
	return nil
}
func (s *stubRehydrateRepo) GetInterface(context.Context, string) (*oms.Interface, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetInterfaceByAPIName(context.Context, string, string) (*oms.Interface, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListInterfaces(context.Context, string) ([]oms.Interface, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateInterface(context.Context, *oms.Interface) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteInterface(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) AttachInterface(context.Context, *oms.ObjectTypeInterface) error {
	return nil
}
func (s *stubRehydrateRepo) DetachInterface(context.Context, string, string) error { return nil }
func (s *stubRehydrateRepo) ListObjectTypeInterfaces(context.Context, string) ([]oms.ObjectTypeInterface, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListInterfaceObjectTypes(context.Context, string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) CreateSharedProperty(context.Context, *oms.SharedProperty) error {
	return nil
}
func (s *stubRehydrateRepo) GetSharedProperty(context.Context, string) (*oms.SharedProperty, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListSharedProperties(context.Context, string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateSharedProperty(context.Context, *oms.SharedProperty) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteSharedProperty(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) CreateTypeGroup(context.Context, *oms.TypeGroup) error {
	return nil
}
func (s *stubRehydrateRepo) GetTypeGroup(context.Context, string) (*oms.TypeGroup, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListTypeGroups(context.Context, string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateTypeGroup(context.Context, *oms.TypeGroup) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteTypeGroup(context.Context, string) error      { return nil }
func (s *stubRehydrateRepo) AssignTypeGroup(context.Context, string, string) error { return nil }
func (s *stubRehydrateRepo) RemoveTypeGroup(context.Context, string, string) error { return nil }
func (s *stubRehydrateRepo) ListTypeGroupsForObjectType(context.Context, string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) CreateValueType(context.Context, *oms.ValueType) error {
	return nil
}
func (s *stubRehydrateRepo) GetValueType(context.Context, string) (*oms.ValueType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetValueTypeByAPIName(context.Context, string) (*oms.ValueType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListValueTypes(context.Context) ([]oms.ValueType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateValueType(context.Context, *oms.ValueType) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteValueType(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) ListPropertyUsagesByBaseType(context.Context, string) ([]oms.PropertyUsage, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) CreateSecurityPolicy(context.Context, *oms.SecurityPolicy) error {
	return nil
}
func (s *stubRehydrateRepo) GetSecurityPolicy(context.Context, string) (*oms.SecurityPolicy, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListSecurityPolicies(context.Context, string) ([]oms.SecurityPolicy, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateSecurityPolicy(context.Context, *oms.SecurityPolicy) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteSecurityPolicy(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) CreateDatasourceBinding(context.Context, *oms.DatasourceBinding) error {
	return nil
}
func (s *stubRehydrateRepo) GetDatasourceBinding(context.Context, string) (*oms.DatasourceBinding, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListDatasourceBindings(context.Context, string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateDatasourceBinding(context.Context, *oms.DatasourceBinding) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteDatasourceBinding(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) CreateQueryType(context.Context, *oms.QueryType) error {
	return nil
}
func (s *stubRehydrateRepo) GetQueryType(context.Context, string) (*oms.QueryType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetQueryTypeByAPIName(context.Context, string, string) (*oms.QueryType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListQueryTypes(context.Context, string) ([]oms.QueryType, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateQueryType(context.Context, *oms.QueryType) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteQueryType(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) InsertActionLog(context.Context, *oms.ActionLog) error {
	return nil
}
func (s *stubRehydrateRepo) GetActionLog(context.Context, int64) (*oms.ActionLog, error) {
	return nil, oms.ErrNotFound
}
func (s *stubRehydrateRepo) ListActionLogs(context.Context, string, int, int) ([]oms.ActionLog, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) CountActionLogs(context.Context, string) (int, error)       { return 0, nil }
func (s *stubRehydrateRepo) UpdateActionLogStatus(context.Context, int64, string) error { return nil }
func (s *stubRehydrateRepo) SearchOntologyResources(context.Context, string, string) ([]oms.SearchResult, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) CreateSnapshot(context.Context, *oms.OntologySnapshot) error {
	return nil
}
func (s *stubRehydrateRepo) ListSnapshots(context.Context, string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetSnapshot(context.Context, string, int) (*oms.OntologySnapshot, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetOntologyVersion(context.Context, string) (int, error) {
	return 0, nil
}
func (s *stubRehydrateRepo) IncrementOntologyVersion(context.Context, string) (int, error) {
	return 1, nil
}
func (s *stubRehydrateRepo) CreateFunction(context.Context, *oms.Function) error { return nil }
func (s *stubRehydrateRepo) GetFunction(context.Context, string) (*oms.Function, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetFunctionByName(context.Context, string, string) (*oms.Function, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) GetFunctionByNameVersion(context.Context, string, string, string) (*oms.Function, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListFunctions(context.Context, string) ([]oms.Function, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListFunctionVersionsByName(context.Context, string, string) ([]oms.Function, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateFunction(context.Context, *oms.Function) error { return nil }
func (s *stubRehydrateRepo) DeleteFunction(context.Context, string) error        { return nil }

// Branch stubs
func (s *stubRehydrateRepo) CreateBranch(context.Context, *oms.OntologyBranch) error {
	return nil
}
func (s *stubRehydrateRepo) GetBranch(context.Context, string) (*oms.OntologyBranch, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListBranches(context.Context, string) ([]oms.OntologyBranch, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) CloseBranch(context.Context, string) error              { return nil }
func (s *stubRehydrateRepo) UpdateBranchStatus(context.Context, string, string) error { return nil }
func (s *stubRehydrateRepo) UpdateBranchBaseVersion(context.Context, string, int64) error {
	return nil
}
func (s *stubRehydrateRepo) CreateBranchChange(context.Context, *oms.BranchChange) error {
	return nil
}
func (s *stubRehydrateRepo) ListBranchChanges(context.Context, string) ([]oms.BranchChange, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateBranchChangeBeforeState(context.Context, string, json.RawMessage) error {
	return nil
}

// Proposal stubs
func (s *stubRehydrateRepo) CreateProposal(context.Context, *oms.OntologyProposal) error {
	return nil
}
func (s *stubRehydrateRepo) GetProposal(context.Context, string) (*oms.OntologyProposal, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListProposals(context.Context, string) ([]oms.OntologyProposal, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateProposalStatus(context.Context, string, string) error { return nil }
func (s *stubRehydrateRepo) CreateProposalReview(context.Context, *oms.ProposalReview) error {
	return nil
}
func (s *stubRehydrateRepo) ListProposalReviews(context.Context, string) ([]oms.ProposalReview, error) {
	return nil, nil
}

// Automation stubs
func (s *stubRehydrateRepo) CreateAutomationRule(context.Context, *oms.AutomationRule) error {
	return nil
}
func (s *stubRehydrateRepo) GetAutomationRule(context.Context, string) (*oms.AutomationRule, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) ListAutomationRules(context.Context, string) ([]oms.AutomationRule, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) UpdateAutomationRule(context.Context, *oms.AutomationRule) error {
	return nil
}
func (s *stubRehydrateRepo) DeleteAutomationRule(context.Context, string) error { return nil }
func (s *stubRehydrateRepo) InsertExecution(context.Context, *oms.AutomationExecution) error {
	return nil
}
func (s *stubRehydrateRepo) UpdateExecution(context.Context, *oms.AutomationExecution) error {
	return nil
}
func (s *stubRehydrateRepo) ListExecutions(context.Context, string) ([]oms.AutomationExecution, error) {
	return nil, nil
}

// Notification stubs
func (s *stubRehydrateRepo) CreateNotification(context.Context, *oms.Notification) error {
	return nil
}
func (s *stubRehydrateRepo) ListNotifications(context.Context, string, bool) ([]oms.Notification, error) {
	return nil, nil
}
func (s *stubRehydrateRepo) MarkNotificationRead(context.Context, string) error { return nil }

// --- Tests ---

func newRehydrateRepo() *stubRehydrateRepo {
	repo := &stubRehydrateRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.test", APIName: "test"},
		},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.main.ontology.test": {
				{
					RID:         "ri.ontology.main.objectType.employee",
					OntologyRID: "ri.ontology.main.ontology.test",
					APIName:     "Employee",
					PrimaryKey:  "id",
				},
				{
					RID:         "ri.ontology.main.objectType.customer",
					OntologyRID: "ri.ontology.main.ontology.test",
					APIName:     "Customer",
					PrimaryKey:  "id",
				},
				{
					RID:         "ri.ontology.main.objectType.order",
					OntologyRID: "ri.ontology.main.ontology.test",
					APIName:     "Order",
					PrimaryKey:  "id",
				},
			},
		},
		properties: map[string][]oms.Property{
			"ri.ontology.main.objectType.employee": {
				{APIName: "id", BaseType: "string", IsSearchable: true},
				{APIName: "name", BaseType: "string", IsSearchable: true},
				{APIName: "salary", BaseType: "double", IsSearchable: true},
			},
			"ri.ontology.main.objectType.customer": {
				{APIName: "id", BaseType: "string", IsSearchable: true},
				{APIName: "company", BaseType: "string", IsSearchable: true},
			},
			"ri.ontology.main.objectType.order": {
				{APIName: "id", BaseType: "string", IsSearchable: true},
				{APIName: "total", BaseType: "double", IsSearchable: true},
			},
		},
	}
	return repo
}

func TestEnsureAllIndexes_CreatesMissingIndexes(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := newRehydrateRepo()
	if err := index.EnsureAllIndexes(context.Background(), mgr, repo); err != nil {
		t.Fatalf("EnsureAllIndexes: %v", err)
	}

	// All three ObjectTypes should now have a per-ontology scoped index
	// (US-044): the rehydrator keys indexes as "{ontologyApiName}__{objectType}".
	for _, name := range []string{"Employee", "Customer", "Order"} {
		key := index.ScopedKey("test", name)
		if idx := mgr.GetIndex(key); idx == nil {
			t.Errorf("expected index %q to exist after EnsureAllIndexes", key)
		}
	}

	// Each empty index should have DocCount == 0 (verifies the shell is real,
	// not a placeholder map entry).
	count, err := mgr.DocCount(index.ScopedKey("test", "Employee"))
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected fresh index DocCount=0, got %d", count)
	}
}

func TestEnsureAllIndexes_IdempotentOnExistingIndexes(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := newRehydrateRepo()
	if err := index.EnsureAllIndexes(context.Background(), mgr, repo); err != nil {
		t.Fatalf("first EnsureAllIndexes: %v", err)
	}
	// Second call should not error and should not duplicate indexes.
	if err := index.EnsureAllIndexes(context.Background(), mgr, repo); err != nil {
		t.Fatalf("second EnsureAllIndexes: %v", err)
	}

	// Same indexes should still be reachable under the scoped key.
	if mgr.GetIndex(index.ScopedKey("test", "Employee")) == nil {
		t.Error("Employee index missing after second call")
	}
}

func TestEnsureAllIndexes_EmptyRepo(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &stubRehydrateRepo{}
	// No ontologies, no object types — should be a clean no-op.
	if err := index.EnsureAllIndexes(context.Background(), mgr, repo); err != nil {
		t.Fatalf("EnsureAllIndexes on empty repo: %v", err)
	}
}

func TestEnsureAllIndexes_NilManager(t *testing.T) {
	// Defensive: nil manager should be a no-op, not a panic.
	repo := newRehydrateRepo()
	if err := index.EnsureAllIndexes(context.Background(), nil, repo); err != nil {
		t.Fatalf("expected nil manager to be no-op, got: %v", err)
	}
}

func TestEnsureAllIndexes_NilRepo(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	// Defensive: nil repo should be a no-op.
	if err := index.EnsureAllIndexes(context.Background(), mgr, nil); err != nil {
		t.Fatalf("expected nil repo to be no-op, got: %v", err)
	}
}

// TestEnsureAllIndexes_PropagatesAnalyzerNotIndexed is the end-to-end half
// of US-010. It exercises the real bootstrap path: a PG-backed ObjectType
// whose property row stores {"analyzer":"not_indexed"} in TypeConfig must,
// after EnsureAllIndexes, produce a Bleve index that excludes that property
// from field-scoped search while still preserving the stored value.
func TestEnsureAllIndexes_PropagatesAnalyzerNotIndexed(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &stubRehydrateRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.test", APIName: "test"},
		},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.main.ontology.test": {
				{
					RID:         "ri.ontology.main.objectType.patent",
					OntologyRID: "ri.ontology.main.ontology.test",
					APIName:     "Patent",
					PrimaryKey:  "id",
				},
			},
		},
		properties: map[string][]oms.Property{
			"ri.ontology.main.objectType.patent": {
				{APIName: "id", BaseType: "string", IsSearchable: true},
				{
					APIName:      "abstract",
					BaseType:     "string",
					IsSearchable: true,
					TypeConfig:   []byte(`{"analyzer":"not_indexed"}`),
				},
			},
		},
	}

	if err := index.EnsureAllIndexes(context.Background(), mgr, repo); err != nil {
		t.Fatalf("EnsureAllIndexes: %v", err)
	}

	key := index.ScopedKey("test", "Patent")
	if err := mgr.IndexDocument(key, "p1", map[string]interface{}{
		"id":       "p1",
		"abstract": "quantum compute entanglement",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// Field query on the not_indexed property must miss.
	absQ := bleve.NewMatchQuery("quantum")
	absQ.SetField("abstract")
	absRes, err := mgr.Search(key, bleve.NewSearchRequest(absQ))
	if err != nil {
		t.Fatalf("search abstract: %v", err)
	}
	if absRes.Total != 0 {
		t.Errorf("abstract search after rehydrate got total=%d, want 0 (not_indexed)", absRes.Total)
	}

	// Stored value must still come back via id lookup + Fields projection.
	idQ := bleve.NewMatchQuery("p1")
	idQ.SetField("id")
	req := bleve.NewSearchRequest(idQ)
	req.Fields = []string{"abstract"}
	idRes, err := mgr.Search(key, req)
	if err != nil {
		t.Fatalf("search id: %v", err)
	}
	if idRes.Total != 1 {
		t.Fatalf("id search got total=%d, want 1", idRes.Total)
	}
	if got := idRes.Hits[0].Fields["abstract"]; got != "quantum compute entanglement" {
		t.Errorf("stored abstract = %v, want original", got)
	}
}

func TestEnsureAllIndexes_ListOntologiesError(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &stubRehydrateRepo{listOntologyErr: errors.New("boom")}
	err := index.EnsureAllIndexes(context.Background(), mgr, repo)
	if err == nil {
		t.Fatal("expected error when ListOntologies fails")
	}
}
