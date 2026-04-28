package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// fakeActionLogStore is a minimal in-memory ActionLogStore that filters its
// rows the same way the PG impl does. The tests inject a fixed slice and
// verify that the handler maps query params into oms.ActionLogQuery and
// surfaces total / nextOffset correctly.
type fakeActionLogStore struct {
	rows []oms.ActionLog
}

func (s *fakeActionLogStore) match(row oms.ActionLog, ontologyRID string, q oms.ActionLogQuery, ridToOnt map[string]string) bool {
	if ridToOnt[row.ActionTypeRID] != ontologyRID {
		return false
	}
	if q.ActionTypeRID != "" && row.ActionTypeRID != q.ActionTypeRID {
		return false
	}
	if q.Status != "" && row.Status != q.Status {
		return false
	}
	if q.UserID != "" && row.UserID != q.UserID {
		return false
	}
	if !q.Since.IsZero() && row.CreatedAt.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && !row.CreatedAt.Before(q.Until) {
		return false
	}
	return true
}

// ridToOntology overlays the test fixture's ActionType → ontology mapping so
// the fake store can answer ontology-scoped queries without a real JOIN.
var ridToOntology = map[string]string{
	"rid:at:create": "ont1",
	"rid:at:delete": "ont1",
	"rid:at:other":  "ont2",
}

func (s *fakeActionLogStore) ListActionLogsByOntology(_ context.Context, ontologyRID string, q oms.ActionLogQuery) ([]oms.ActionLog, error) {
	var matched []oms.ActionLog
	for _, row := range s.rows {
		if s.match(row, ontologyRID, q, ridToOntology) {
			matched = append(matched, row)
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = oms.DefaultActionHistoryLimit
	}
	start := q.Offset
	if start > len(matched) {
		start = len(matched)
	}
	end := start + limit
	if end > len(matched) {
		end = len(matched)
	}
	return append([]oms.ActionLog(nil), matched[start:end]...), nil
}

func (s *fakeActionLogStore) CountActionLogsByOntology(_ context.Context, ontologyRID string, q oms.ActionLogQuery) (int, error) {
	n := 0
	for _, row := range s.rows {
		if s.match(row, ontologyRID, q, ridToOntology) {
			n++
		}
	}
	return n, nil
}

func (s *fakeActionLogStore) GetActionLogByOntology(_ context.Context, ontologyRID string, id int64) (*oms.ActionLog, error) {
	for i := range s.rows {
		if s.rows[i].ID == id && ridToOntology[s.rows[i].ActionTypeRID] == ontologyRID {
			row := s.rows[i]
			return &row, nil
		}
	}
	return nil, oms.ErrNotFound
}

func newHistoryHandlerWithRows(rows []oms.ActionLog, repo *mockOmsRepo) (*Handler, *fakeActionLogStore) {
	exec := NewExecutor(repo, nil)
	store := &fakeActionLogStore{rows: rows}
	exec.SetActionLogStore(store)
	return NewHandler(exec), store
}

func newHistoryRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/history", h.ListHistory)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/history/{logId}", h.GetHistoryEntry)
	return r
}

func actionTypeFixture() *mockOmsRepo {
	return &mockOmsRepo{
		actionTypes: []oms.ActionType{
			{RID: "rid:at:create", APIName: "createEmployee", DisplayName: "Create Employee"},
			{RID: "rid:at:delete", APIName: "deleteEmployee", DisplayName: "Delete Employee"},
			{RID: "rid:at:other", APIName: "otherAction", DisplayName: "Other"},
		},
	}
}

func sampleHistoryRows() []oms.ActionLog {
	t0 := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	return []oms.ActionLog{
		// ont1 rows
		{ID: 1, ActionTypeRID: "rid:at:create", UserID: "user:alice", Status: "SUCCESS", CreatedAt: t0.Add(2 * time.Hour), Parameters: json.RawMessage(`{"name":"Alice"}`), Edits: json.RawMessage(`[{"type":"create"}]`)},
		{ID: 2, ActionTypeRID: "rid:at:delete", UserID: "user:bob", Status: "FAILED", ErrorMessage: "boom", CreatedAt: t0.Add(1 * time.Hour), Parameters: json.RawMessage(`{}`), Edits: json.RawMessage(`[]`)},
		{ID: 3, ActionTypeRID: "rid:at:create", UserID: "user:alice", Status: "SUCCESS", CreatedAt: t0, Parameters: json.RawMessage(`{}`), Edits: json.RawMessage(`[]`)},
		// ont2 row — must be filtered out by ontology scope
		{ID: 99, ActionTypeRID: "rid:at:other", UserID: "user:eve", Status: "SUCCESS", CreatedAt: t0.Add(3 * time.Hour), Parameters: json.RawMessage(`{}`), Edits: json.RawMessage(`[]`)},
	}
}

func decodeHistoryResponse(t *testing.T, body []byte) ActionHistoryResponse {
	t.Helper()
	var resp ActionHistoryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, string(body))
	}
	return resp
}

func TestListHistory_OntologyScoped(t *testing.T) {
	repo := actionTypeFixture()
	rows := sampleHistoryRows()
	h, _ := newHistoryHandlerWithRows(rows, repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeHistoryResponse(t, w.Body.Bytes())
	if got.Total != 3 {
		t.Errorf("total=%d, want 3", got.Total)
	}
	if len(got.Data) != 3 {
		t.Fatalf("len(data)=%d, want 3", len(got.Data))
	}
	for _, r := range got.Data {
		if r.ActionTypeRID == "rid:at:other" {
			t.Errorf("ont2 row leaked into ont1 page: %+v", r)
		}
	}
	// Cross-ontology row stays invisible to ont1 callers.
}

func TestListHistory_FilterByActionType(t *testing.T) {
	repo := actionTypeFixture()
	rows := sampleHistoryRows()
	h, _ := newHistoryHandlerWithRows(rows, repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?actionType=createEmployee", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeHistoryResponse(t, w.Body.Bytes())
	if got.Total != 2 {
		t.Fatalf("total=%d, want 2", got.Total)
	}
	for _, r := range got.Data {
		if r.ActionTypeRID != "rid:at:create" {
			t.Errorf("non-matching row: %+v", r)
		}
	}
}

func TestListHistory_FilterByStatus(t *testing.T) {
	repo := actionTypeFixture()
	rows := sampleHistoryRows()
	h, _ := newHistoryHandlerWithRows(rows, repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?status=failed", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeHistoryResponse(t, w.Body.Bytes())
	if got.Total != 1 || len(got.Data) != 1 || got.Data[0].Status != "FAILED" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestListHistory_InvalidStatus(t *testing.T) {
	repo := actionTypeFixture()
	h, _ := newHistoryHandlerWithRows(sampleHistoryRows(), repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?status=banana", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode err: %v", err)
	}
	if body["errorName"] != "InvalidStatus" {
		t.Fatalf("errorName=%v body=%s", body["errorName"], w.Body.String())
	}
}

func TestListHistory_FilterByUser(t *testing.T) {
	repo := actionTypeFixture()
	h, _ := newHistoryHandlerWithRows(sampleHistoryRows(), repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?userId=user:bob", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeHistoryResponse(t, w.Body.Bytes())
	if got.Total != 1 || got.Data[0].UserID != "user:bob" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestListHistory_FilterByTimeWindow(t *testing.T) {
	repo := actionTypeFixture()
	h, _ := newHistoryHandlerWithRows(sampleHistoryRows(), repo)
	router := newHistoryRouter(h)

	since := url("2026-04-28T13:30:00Z")
	until := url("2026-04-28T15:00:00Z")
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont1/actions/history?since="+since+"&until="+until, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeHistoryResponse(t, w.Body.Bytes())
	// 14:00 (id=1) is the only ont1 row inside [13:30, 15:00).
	if got.Total != 1 || len(got.Data) != 1 || got.Data[0].ID != 1 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestListHistory_PaginationNextOffset(t *testing.T) {
	repo := actionTypeFixture()
	rows := sampleHistoryRows()
	h, _ := newHistoryHandlerWithRows(rows, repo)
	router := newHistoryRouter(h)

	// limit=2 → first page returns 2 rows + nextOffset=2.
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?limit=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	got := decodeHistoryResponse(t, w.Body.Bytes())
	if len(got.Data) != 2 || got.Total != 3 {
		t.Fatalf("page1 unexpected: %+v", got)
	}
	if got.NextOffset == nil || *got.NextOffset != 2 {
		t.Fatalf("page1 nextOffset=%v, want 2", got.NextOffset)
	}

	// page2 (offset=2, limit=2) → 1 row, nextOffset omitted.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?limit=2&offset=2", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	got = decodeHistoryResponse(t, w.Body.Bytes())
	if len(got.Data) != 1 || got.NextOffset != nil {
		t.Fatalf("page2 unexpected: %+v", got)
	}
}

func TestListHistory_UnknownActionTypeReturnsEmpty(t *testing.T) {
	repo := actionTypeFixture()
	h, _ := newHistoryHandlerWithRows(sampleHistoryRows(), repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?actionType=ghost", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeHistoryResponse(t, w.Body.Bytes())
	if len(got.Data) != 0 {
		t.Fatalf("expected empty page, got %d rows", len(got.Data))
	}
}

func TestListHistory_NoStoreWiredReturnsEmpty(t *testing.T) {
	repo := actionTypeFixture()
	exec := NewExecutor(repo, nil)
	h := NewHandler(exec)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeHistoryResponse(t, w.Body.Bytes())
	if len(got.Data) != 0 || got.Total != 0 {
		t.Fatalf("expected empty/0, got %+v", got)
	}
	// `data` MUST be a non-nil JSON array on the wire so SDKs reading
	// data.length / data.flatMap don't blow up. Re-decode via map to assert
	// the [] shape over null.
	var raw map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["data"].([]interface{}); !ok {
		t.Fatalf("data is not a JSON array: %s", w.Body.String())
	}
}

func TestGetHistoryEntry_Success(t *testing.T) {
	repo := actionTypeFixture()
	rows := sampleHistoryRows()
	h, _ := newHistoryHandlerWithRows(rows, repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got oms.ActionLog
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 1 || got.UserID != "user:alice" {
		t.Fatalf("unexpected row: %+v", got)
	}
	// Parameters should round-trip as a JSON object the FE can render directly.
	if !strings.Contains(string(got.Parameters), "Alice") {
		t.Errorf("parameters not preserved: %s", got.Parameters)
	}
}

func TestGetHistoryEntry_CrossOntology404(t *testing.T) {
	repo := actionTypeFixture()
	rows := sampleHistoryRows()
	h, _ := newHistoryHandlerWithRows(rows, repo)
	router := newHistoryRouter(h)

	// id=99 belongs to ont2, request says ont1 → 404 (not 200, not 403).
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history/99", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGetHistoryEntry_InvalidID(t *testing.T) {
	repo := actionTypeFixture()
	h, _ := newHistoryHandlerWithRows(sampleHistoryRows(), repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGetHistoryEntry_NoStoreWired404(t *testing.T) {
	repo := actionTypeFixture()
	exec := NewExecutor(repo, nil)
	router := newHistoryRouter(NewHandler(exec))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestListHistory_InvalidLimit(t *testing.T) {
	repo := actionTypeFixture()
	h, _ := newHistoryHandlerWithRows(sampleHistoryRows(), repo)
	router := newHistoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/actions/history?limit=999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func url(s string) string { return strings.ReplaceAll(s, ":", "%3A") }
