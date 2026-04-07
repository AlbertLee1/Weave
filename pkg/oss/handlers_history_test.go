package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// historyMockRepo is a minimal oms.Repository implementation that only
// supports the methods exercised by the GetObjectHistory handler. All
// other methods return ErrNotFound or zero values. This keeps the history
// handler test independent from the broader mockOmsRepo defined in
// service_test.go (which is being modified by parallel workers).
type historyMockRepo struct {
	getObjectTypeErr error
	objectType       *oms.ObjectType
	listRows         []oms.ObjectHistory
	listErr          error
	count            int64
	countErr         error

	gotObjectTypeRID string
	gotPrimaryKey    string
	gotLimit         int
}

func (m *historyMockRepo) GetObjectTypeByAPIName(_ context.Context, _, _ string) (*oms.ObjectType, error) {
	if m.getObjectTypeErr != nil {
		return nil, m.getObjectTypeErr
	}
	return m.objectType, nil
}

func (m *historyMockRepo) ListObjectHistory(_ context.Context, otRID, pk string, limit int) ([]oms.ObjectHistory, error) {
	m.gotObjectTypeRID = otRID
	m.gotPrimaryKey = pk
	m.gotLimit = limit
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listRows, nil
}

func (m *historyMockRepo) GetObjectVersionCount(_ context.Context, _, _ string) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.count, nil
}

// --- Stubbed-out methods (unused by handler, return zero values) ---
func (m *historyMockRepo) CreateOntology(_ context.Context, _ *oms.Ontology) error { return nil }
func (m *historyMockRepo) GetOntology(_ context.Context, _ string) (*oms.Ontology, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateOntology(_ context.Context, _ *oms.Ontology) error { return nil }
func (m *historyMockRepo) CreateObjectType(_ context.Context, _ *oms.ObjectType) error {
	return nil
}
func (m *historyMockRepo) GetObjectType(_ context.Context, _ string) (*oms.ObjectType, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateObjectType(_ context.Context, _ *oms.ObjectType) error { return nil }
func (m *historyMockRepo) DeleteObjectType(_ context.Context, _ string) error          { return nil }
func (m *historyMockRepo) CreateProperty(_ context.Context, _ *oms.Property) error     { return nil }
func (m *historyMockRepo) GetProperty(_ context.Context, _ string) (*oms.Property, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListProperties(_ context.Context, _ string) ([]oms.Property, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateProperty(_ context.Context, _ *oms.Property) error { return nil }
func (m *historyMockRepo) DeleteProperty(_ context.Context, _ string) error        { return nil }
func (m *historyMockRepo) CreateLinkType(_ context.Context, _ *oms.LinkType) error { return nil }
func (m *historyMockRepo) GetLinkType(_ context.Context, _ string) (*oms.LinkType, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) GetLinkTypeByAPIName(_ context.Context, _, _ string) (*oms.LinkType, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListOutgoingLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}
func (m *historyMockRepo) ListIncomingLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}
func (m *historyMockRepo) ListLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateLinkType(_ context.Context, _ *oms.LinkType) error { return nil }
func (m *historyMockRepo) DeleteLinkType(_ context.Context, _ string) error        { return nil }
func (m *historyMockRepo) UpsertLinkEdge(_ context.Context, _ *oms.LinkEdge) error { return nil }
func (m *historyMockRepo) DeleteLinkEdge(_ context.Context, _, _, _ string) error  { return nil }
func (m *historyMockRepo) DeleteAllLinkEdgesForSource(_ context.Context, _, _ string) error {
	return nil
}
func (m *historyMockRepo) CreateActionType(_ context.Context, _ *oms.ActionType) error { return nil }
func (m *historyMockRepo) GetActionType(_ context.Context, _ string) (*oms.ActionType, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListActionTypes(_ context.Context, _ string) ([]oms.ActionType, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateActionType(_ context.Context, _ *oms.ActionType) error { return nil }
func (m *historyMockRepo) DeleteActionType(_ context.Context, _ string) error          { return nil }
func (m *historyMockRepo) CreateInterface(_ context.Context, _ *oms.Interface) error   { return nil }
func (m *historyMockRepo) GetInterface(_ context.Context, _ string) (*oms.Interface, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) GetInterfaceByAPIName(_ context.Context, _, _ string) (*oms.Interface, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListInterfaces(_ context.Context, _ string) ([]oms.Interface, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateInterface(_ context.Context, _ *oms.Interface) error { return nil }
func (m *historyMockRepo) DeleteInterface(_ context.Context, _ string) error         { return nil }
func (m *historyMockRepo) AttachInterface(_ context.Context, _ *oms.ObjectTypeInterface) error {
	return nil
}
func (m *historyMockRepo) DetachInterface(_ context.Context, _, _ string) error { return nil }
func (m *historyMockRepo) ListObjectTypeInterfaces(_ context.Context, _ string) ([]oms.ObjectTypeInterface, error) {
	return nil, nil
}
func (m *historyMockRepo) ListInterfaceObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (m *historyMockRepo) CreateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (m *historyMockRepo) GetSharedProperty(_ context.Context, _ string) (*oms.SharedProperty, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListSharedProperties(_ context.Context, _ string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (m *historyMockRepo) DeleteSharedProperty(_ context.Context, _ string) error { return nil }
func (m *historyMockRepo) CreateTypeGroup(_ context.Context, _ *oms.TypeGroup) error {
	return nil
}
func (m *historyMockRepo) GetTypeGroup(_ context.Context, _ string) (*oms.TypeGroup, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListTypeGroups(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (m *historyMockRepo) DeleteTypeGroup(_ context.Context, _ string) error         { return nil }
func (m *historyMockRepo) AssignTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *historyMockRepo) RemoveTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *historyMockRepo) ListTypeGroupsForObjectType(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (m *historyMockRepo) CreateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *historyMockRepo) GetValueType(_ context.Context, _ string) (*oms.ValueType, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListValueTypes(_ context.Context) ([]oms.ValueType, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *historyMockRepo) DeleteValueType(_ context.Context, _ string) error         { return nil }
func (m *historyMockRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error {
	return nil
}
func (m *historyMockRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListSecurityPolicies(_ context.Context, _ string) ([]oms.SecurityPolicy, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error {
	return nil
}
func (m *historyMockRepo) DeleteSecurityPolicy(_ context.Context, _ string) error { return nil }
func (m *historyMockRepo) CreateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *historyMockRepo) GetDatasourceBinding(_ context.Context, _ string) (*oms.DatasourceBinding, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListDatasourceBindings(_ context.Context, _ string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *historyMockRepo) DeleteDatasourceBinding(_ context.Context, _ string) error { return nil }
func (m *historyMockRepo) CreateQueryType(_ context.Context, _ *oms.QueryType) error {
	return nil
}
func (m *historyMockRepo) GetQueryType(_ context.Context, _ string) (*oms.QueryType, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) GetQueryTypeByAPIName(_ context.Context, _, _ string) (*oms.QueryType, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) ListQueryTypes(_ context.Context, _ string) ([]oms.QueryType, error) {
	return nil, nil
}
func (m *historyMockRepo) UpdateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (m *historyMockRepo) DeleteQueryType(_ context.Context, _ string) error         { return nil }
func (m *historyMockRepo) InsertActionLog(_ context.Context, _ *oms.ActionLog) error { return nil }
func (m *historyMockRepo) ListActionLogs(_ context.Context, _ string, _, _ int) ([]oms.ActionLog, error) {
	return nil, nil
}
func (m *historyMockRepo) CountActionLogs(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *historyMockRepo) InsertObjectHistory(_ context.Context, _ *oms.ObjectHistory) error {
	return nil
}
func (m *historyMockRepo) SearchOntologyResources(_ context.Context, _, _ string) ([]oms.SearchResult, error) {
	return nil, nil
}
func (m *historyMockRepo) CreateSnapshot(_ context.Context, _ *oms.OntologySnapshot) error {
	return nil
}
func (m *historyMockRepo) ListSnapshots(_ context.Context, _ string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *historyMockRepo) GetSnapshot(_ context.Context, _ string, _ int) (*oms.OntologySnapshot, error) {
	return nil, oms.ErrNotFound
}
func (m *historyMockRepo) GetOntologyVersion(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *historyMockRepo) IncrementOntologyVersion(_ context.Context, _ string) (int, error) {
	return 1, nil
}

// --- Test cases ---

func newHistoryHandler(t *testing.T, repo *historyMockRepo) http.Handler {
	t.Helper()
	h := oss.NewHandler(nil)
	h.SetHistoryRepo(repo)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func TestGetObjectHistory_Success(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &historyMockRepo{
		objectType: &oms.ObjectType{
			RID:        "ri.ontology.main.object-type.employee",
			APIName:    "employee",
			PrimaryKey: "employeeId",
		},
		listRows: []oms.ObjectHistory{
			{ID: "h1", PrimaryKey: "emp-1", Version: 2, EditType: "MODIFY",
				NewState: json.RawMessage(`{"name":"Alice2"}`), RecordedAt: now},
			{ID: "h2", PrimaryKey: "emp-1", Version: 1, EditType: "CREATE",
				NewState: json.RawMessage(`{"name":"Alice"}`), RecordedAt: now.Add(-time.Hour)},
		},
		count: 2,
	}

	h := newHistoryHandler(t, repo)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/objects/employee/emp-1/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var body struct {
		History       []map[string]interface{} `json:"history"`
		TotalVersions int64                    `json:"totalVersions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.TotalVersions != 2 {
		t.Errorf("expected totalVersions 2, got %d", body.TotalVersions)
	}
	if len(body.History) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(body.History))
	}
	if body.History[0]["editType"] != "MODIFY" {
		t.Errorf("expected first row MODIFY, got %v", body.History[0]["editType"])
	}
	if repo.gotObjectTypeRID != "ri.ontology.main.object-type.employee" {
		t.Errorf("expected RID lookup, got %q", repo.gotObjectTypeRID)
	}
	if repo.gotPrimaryKey != "emp-1" {
		t.Errorf("expected primaryKey emp-1, got %q", repo.gotPrimaryKey)
	}
	if repo.gotLimit != 50 {
		t.Errorf("expected default limit 50, got %d", repo.gotLimit)
	}
}

func TestGetObjectHistory_HonorsLimit(t *testing.T) {
	repo := &historyMockRepo{
		objectType: &oms.ObjectType{
			RID:     "ri.ontology.main.object-type.employee",
			APIName: "employee",
		},
		listRows: []oms.ObjectHistory{},
		count:    0,
	}
	h := newHistoryHandler(t, repo)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/objects/employee/emp-1/history?limit=10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if repo.gotLimit != 10 {
		t.Errorf("expected limit 10, got %d", repo.gotLimit)
	}
}

func TestGetObjectHistory_ClampsLimitAt500(t *testing.T) {
	repo := &historyMockRepo{
		objectType: &oms.ObjectType{
			RID:     "ri.ontology.main.object-type.employee",
			APIName: "employee",
		},
	}
	h := newHistoryHandler(t, repo)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/objects/employee/emp-1/history?limit=10000", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if repo.gotLimit != 500 {
		t.Errorf("expected clamped limit 500, got %d", repo.gotLimit)
	}
}

func TestGetObjectHistory_RejectsBadLimit(t *testing.T) {
	repo := &historyMockRepo{
		objectType: &oms.ObjectType{
			RID:     "ri.ontology.main.object-type.employee",
			APIName: "employee",
		},
	}
	h := newHistoryHandler(t, repo)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/objects/employee/emp-1/history?limit=bogus", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body=%s", w.Code, w.Body.String())
	}
}

func TestGetObjectHistory_ObjectTypeNotFound(t *testing.T) {
	repo := &historyMockRepo{
		getObjectTypeErr: oms.ErrNotFound,
	}
	h := newHistoryHandler(t, repo)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/objects/unknown/emp-1/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body=%s", w.Code, w.Body.String())
	}
}

func TestGetObjectHistory_NoHistoryRepo(t *testing.T) {
	h := oss.NewHandler(nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/objects/employee/emp-1/history", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// When the history repo is not wired the handler returns a 5xx
	// "HistoryNotConfigured" error so callers can detect feature absence.
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d; body=%s", w.Code, w.Body.String())
	}
}

func TestGetObjectHistory_EmptyHistoryReturnsEmptyArray(t *testing.T) {
	repo := &historyMockRepo{
		objectType: &oms.ObjectType{
			RID:     "ri.ontology.main.object-type.employee",
			APIName: "employee",
		},
		listRows: nil,
		count:    0,
	}
	h := newHistoryHandler(t, repo)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/objects/employee/emp-1/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !contains(body, `"history":[]`) {
		t.Errorf("expected empty array in body, got %s", body)
	}
	if !contains(body, `"totalVersions":0`) {
		t.Errorf("expected totalVersions 0, got %s", body)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
