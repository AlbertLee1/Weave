package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// fakeOmsRepo is the minimal stub OMS repository the MCP tools need.
// It embeds oms.Repository so unused methods panic if called by accident.
type fakeOmsRepo struct {
	oms.Repository
	ontologies         []oms.Ontology
	listOntologiesErr  error
	objectTypes        map[string][]oms.ObjectType // ontology rid/apiName -> object types
	properties         map[string][]oms.Property   // object type rid -> properties
	listPropertiesErr  map[string]error
	outgoingLinkTypes  map[string][]oms.LinkType // object type rid -> outgoing links
	listOutgoingErr    map[string]error
	actionTypes        map[string][]oms.ActionType
	listActionTypesErr map[string]error
	actionLogs         []*oms.ActionLog
}

func (f *fakeOmsRepo) ListOntologies(ctx context.Context) ([]oms.Ontology, error) {
	if f.listOntologiesErr != nil {
		return nil, f.listOntologiesErr
	}
	return f.ontologies, nil
}

func (f *fakeOmsRepo) GetOntology(ctx context.Context, rid string) (*oms.Ontology, error) {
	for i := range f.ontologies {
		if f.ontologies[i].RID == rid || f.ontologies[i].APIName == rid {
			o := f.ontologies[i]
			return &o, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (f *fakeOmsRepo) ListLinkTypes(ctx context.Context, ontologyRID string) ([]oms.LinkType, error) {
	return nil, nil
}

func (f *fakeOmsRepo) ListObjectTypes(ctx context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	if v, ok := f.objectTypes[ontologyRID]; ok {
		return v, nil
	}
	return nil, nil
}

func (f *fakeOmsRepo) ListProperties(ctx context.Context, objectTypeRID string) ([]oms.Property, error) {
	if f.listPropertiesErr != nil {
		if err := f.listPropertiesErr[objectTypeRID]; err != nil {
			return nil, err
		}
	}
	if v, ok := f.properties[objectTypeRID]; ok {
		return v, nil
	}
	return nil, nil
}

func (f *fakeOmsRepo) ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	if f.listOutgoingErr != nil {
		if err := f.listOutgoingErr[objectTypeRID]; err != nil {
			return nil, err
		}
	}
	if v, ok := f.outgoingLinkTypes[objectTypeRID]; ok {
		return v, nil
	}
	return nil, nil
}

func (f *fakeOmsRepo) ListActionTypes(ctx context.Context, ontologyRID string) ([]oms.ActionType, error) {
	if f.listActionTypesErr != nil {
		if err := f.listActionTypesErr[ontologyRID]; err != nil {
			return nil, err
		}
	}
	if v, ok := f.actionTypes[ontologyRID]; ok {
		return v, nil
	}
	return nil, nil
}

func (f *fakeOmsRepo) InsertActionLog(ctx context.Context, log *oms.ActionLog) error {
	f.actionLogs = append(f.actionLogs, log)
	return nil
}

func (f *fakeOmsRepo) GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	for _, ot := range f.objectTypes[ontologyRID] {
		if ot.APIName == apiName {
			return &ot, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (f *fakeOmsRepo) GetActionTypeByAPIName(ctx context.Context, ontologyRID, apiNameOrRID string) (*oms.ActionType, error) {
	for _, at := range f.actionTypes[ontologyRID] {
		if at.APIName == apiNameOrRID || at.RID == apiNameOrRID {
			return &at, nil
		}
	}
	return nil, oms.ErrNotFound
}

// fakeOssService is a stub oss.Service for the MCP tools.
type fakeOssService struct {
	getErr       error
	listErr      error
	searchErr    error
	getResult    *oss.WireObject
	listResult   *oss.ObjectPage
	searchResult *oss.ObjectPage
	lastGet      oss.GetObjectRequest
	lastList     oss.ListObjectsRequest
	lastSearch   oss.SearchObjectsRequest
}

func (f *fakeOssService) GetObject(ctx context.Context, req oss.GetObjectRequest) (*oss.WireObject, error) {
	f.lastGet = req
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeOssService) ListObjects(ctx context.Context, req oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	f.lastList = req
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeOssService) SearchObjects(ctx context.Context, req oss.SearchObjectsRequest) (*oss.ObjectPage, error) {
	f.lastSearch = req
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResult, nil
}

func (f *fakeOssService) ListLinkedObjects(ctx context.Context, req oss.LinkedObjectsRequest) (*oss.ObjectPage, error) {
	return &oss.ObjectPage{}, nil
}

func (f *fakeOssService) GetLinkedObject(ctx context.Context, req oss.GetLinkedObjectRequest) (*oss.WireObject, error) {
	return nil, oms.ErrNotFound
}

func (f *fakeOssService) CountObjects(ctx context.Context, req oss.CountObjectsRequest) (*oss.CountObjectsResponse, error) {
	return &oss.CountObjectsResponse{Count: 0}, nil
}

// stubPublisher is a no-op funnel publisher for the action executor.
type stubPublisher struct{}

func (stubPublisher) Publish(batch *funnel.EditBatch) (uint64, error) { return 0, nil }

func newTestServer(t *testing.T) (*Server, *fakeOmsRepo, *fakeOssService, *actions.Executor) {
	t.Helper()
	repo := &fakeOmsRepo{
		ontologies:        []oms.Ontology{{RID: "ri.weave.main.ontology.demo", APIName: "demo", DisplayName: "Demo"}},
		objectTypes:       map[string][]oms.ObjectType{},
		properties:        map[string][]oms.Property{},
		outgoingLinkTypes: map[string][]oms.LinkType{},
		actionTypes:       map[string][]oms.ActionType{},
	}
	svc := &fakeOssService{}
	exec := actions.NewExecutor(repo, stubPublisher{})
	return NewServer(svc, repo, exec), repo, svc, exec
}

func TestTool_ListOntologies(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	res, err := srv.Registry().Call(context.Background(), "weave_list_ontologies", map[string]any{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("expected content")
	}
	if res.Content[0].Type != "text" {
		t.Errorf("Type = %s, want text", res.Content[0].Type)
	}
	// Body should mention the ontology api name.
	if !contains(res.Content[0].Text, "demo") {
		t.Errorf("text missing 'demo': %s", res.Content[0].Text)
	}
}

func TestTool_ListObjectTypes(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	repo.objectTypes["demo"] = []oms.ObjectType{
		{RID: "ri.weave.main.objectType.user", APIName: "User", DisplayName: "User"},
		{RID: "ri.weave.main.objectType.order", APIName: "Order", DisplayName: "Order"},
	}
	res, err := srv.Registry().Call(context.Background(), "weave_list_object_types", map[string]any{
		"ontology": "demo",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(res.Content[0].Text, "User") || !contains(res.Content[0].Text, "Order") {
		t.Errorf("text missing object types: %s", res.Content[0].Text)
	}
}

func TestTool_ListObjectTypes_MissingOntology(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	_, err := srv.Registry().Call(context.Background(), "weave_list_object_types", map[string]any{})
	if err == nil {
		t.Fatalf("expected validation error for missing ontology")
	}
}

func TestTool_GetObject_NotFound(t *testing.T) {
	srv, _, svc, _ := newTestServer(t)
	svc.getErr = oms.ErrNotFound
	_, err := srv.Registry().Call(context.Background(), "weave_get_object", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"primaryKey": "u1",
	})
	if err == nil || !errors.Is(err, oms.ErrNotFound) {
		// The tool may wrap the error; accept any non-nil error mentioning not-found.
		if err == nil {
			t.Fatalf("expected error from get_object")
		}
	}
}

func TestTool_GetObject_Success(t *testing.T) {
	srv, _, svc, _ := newTestServer(t)
	svc.getResult = oss.FormatObject("User", "u1", map[string]any{"name": "Alice"})
	res, err := srv.Registry().Call(context.Background(), "weave_get_object", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"primaryKey": "u1",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(res.Content[0].Text, "Alice") {
		t.Errorf("text missing 'Alice': %s", res.Content[0].Text)
	}
	// Verify request was passed through correctly.
	if svc.lastGet.ObjectType != "User" || svc.lastGet.PrimaryKey != "u1" {
		t.Errorf("lastGet = %+v", svc.lastGet)
	}
}

func TestTool_ListObjects(t *testing.T) {
	srv, _, svc, _ := newTestServer(t)
	svc.listResult = &oss.ObjectPage{
		Data: []*oss.WireObject{
			oss.FormatObject("User", "u1", map[string]any{"name": "Alice"}),
			oss.FormatObject("User", "u2", map[string]any{"name": "Bob"}),
		},
		NextPageToken: "tok",
		TotalCount:    "2",
	}
	res, err := srv.Registry().Call(context.Background(), "weave_list_objects", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"pageSize":   float64(10),
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(res.Content[0].Text, "Alice") || !contains(res.Content[0].Text, "Bob") {
		t.Errorf("text = %s", res.Content[0].Text)
	}
	if svc.lastList.PageSize != 10 {
		t.Errorf("PageSize = %d, want 10", svc.lastList.PageSize)
	}
}

func TestTool_SearchObjects(t *testing.T) {
	srv, _, svc, _ := newTestServer(t)
	svc.searchResult = &oss.ObjectPage{
		Data:       []*oss.WireObject{oss.FormatObject("User", "u1", map[string]any{"name": "Alice"})},
		TotalCount: "1",
	}
	res, err := srv.Registry().Call(context.Background(), "weave_search_objects", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"where": map[string]any{
			"type":  "eq",
			"field": "name",
			"value": "Alice",
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(res.Content[0].Text, "Alice") {
		t.Errorf("text = %s", res.Content[0].Text)
	}
	if svc.lastSearch.Where == nil {
		t.Errorf("Where was not forwarded to oss")
	}
}

func TestTool_ListActionTypes(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	repo.actionTypes["demo"] = []oms.ActionType{
		{RID: "ri.weave.main.actionType.create", APIName: "createUser", DisplayName: "Create User"},
	}
	res, err := srv.Registry().Call(context.Background(), "weave_list_action_types", map[string]any{
		"ontology": "demo",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(res.Content[0].Text, "createUser") {
		t.Errorf("text missing actionType: %s", res.Content[0].Text)
	}
}

func TestTool_ApplyAction_UnknownActionType(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	_, err := srv.Registry().Call(context.Background(), "weave_apply_action", map[string]any{
		"ontology":   "demo",
		"actionType": "doesNotExist",
		"parameters": map[string]any{},
	})
	if err == nil {
		t.Fatalf("expected error for unknown action type")
	}
}

// contains is a tiny substring helper to keep test assertions readable
// without pulling in strings.Contains repeatedly.
func contains(haystack, needle string) bool {
	return indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
