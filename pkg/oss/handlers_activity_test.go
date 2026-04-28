package oss_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// fakeActivityStore is an in-memory implementation of oms.ObjectActivityStore
// for handler-level tests. Rows are returned in the same order as inserted —
// the seed code in each test arranges them version-DESC to mirror the PG
// implementation's ORDER BY clause.
type fakeActivityStore struct {
	rows map[string][]oms.ObjectHistory // key: objectTypeRID + "|" + primaryKey
	err  error
}

func newFakeActivityStore() *fakeActivityStore {
	return &fakeActivityStore{rows: make(map[string][]oms.ObjectHistory)}
}

func (f *fakeActivityStore) seed(objectTypeRID, primaryKey string, rows []oms.ObjectHistory) {
	f.rows[objectTypeRID+"|"+primaryKey] = rows
}

func (f *fakeActivityStore) ListObjectHistoryPage(_ context.Context, objectTypeRID, primaryKey string, beforeVersion int64, limit int) ([]oms.ObjectHistory, error) {
	if f.err != nil {
		return nil, f.err
	}
	all := f.rows[objectTypeRID+"|"+primaryKey]
	out := make([]oms.ObjectHistory, 0, len(all))
	for _, r := range all {
		if beforeVersion > 0 && r.Version >= beforeVersion {
			continue
		}
		out = append(out, r)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func setupActivityHandler(t *testing.T) (*oss.Handler, *fakeActivityStore, *chi.Mux) {
	t.Helper()
	svc, _, repo, _ := setupOSSTest(t)
	h := oss.NewHandler(svc)
	h.SetOmsRepo(repo)
	store := newFakeActivityStore()
	h.SetActivityStore(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return h, store, r
}

func seedHistoryDescending(store *fakeActivityStore, otRID, pk string, n int) {
	rows := make([]oms.ObjectHistory, 0, n)
	for v := n; v >= 1; v-- {
		rows = append(rows, oms.ObjectHistory{
			ID:            "id-" + strconv.Itoa(v),
			ObjectTypeRID: otRID,
			PrimaryKey:    pk,
			Version:       int64(v),
			EditType:      "MODIFY",
			UserID:        "user-" + strconv.Itoa(v),
			RecordedAt:    time.Date(2026, 4, 28, 10, 0, v, 0, time.UTC),
		})
	}
	store.seed(otRID, pk, rows)
}

func TestObjectActivity_ReturnsTimelineDescending(t *testing.T) {
	_, store, r := setupActivityHandler(t)
	otRID := "ri.ontology.main.object-type.employee"
	seedHistoryDescending(store, otRID, "emp1", 3)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/activity", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp oss.ObjectActivityResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("len(data) = %d, want 3", len(resp.Data))
	}
	if resp.Data[0].Version != 3 || resp.Data[1].Version != 2 || resp.Data[2].Version != 1 {
		t.Errorf("versions = %d/%d/%d, want 3/2/1",
			resp.Data[0].Version, resp.Data[1].Version, resp.Data[2].Version)
	}
	if resp.NextPageToken != "" {
		t.Errorf("nextPageToken = %q, want empty (no more pages)", resp.NextPageToken)
	}
}

func TestObjectActivity_PaginatesWithCursor(t *testing.T) {
	_, store, r := setupActivityHandler(t)
	otRID := "ri.ontology.main.object-type.employee"
	seedHistoryDescending(store, otRID, "emp1", 5)

	// First page: pageSize=2 should return v5,v4 + cursor pointing at v4.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/activity?pageSize=2", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("page1 status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var page1 oss.ObjectActivityResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &page1)
	if len(page1.Data) != 2 || page1.Data[0].Version != 5 || page1.Data[1].Version != 4 {
		t.Fatalf("page1 versions = %+v", page1.Data)
	}
	if page1.NextPageToken == "" {
		t.Fatalf("page1 nextPageToken should be non-empty")
	}

	// Second page: feed back the cursor; expect v3,v2.
	req2 := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+
			"/objects/employee/emp1/activity?pageSize=2&pageToken="+page1.NextPageToken, nil)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d, body = %s", rr2.Code, rr2.Body.String())
	}
	var page2 oss.ObjectActivityResponse
	_ = json.Unmarshal(rr2.Body.Bytes(), &page2)
	if len(page2.Data) != 2 || page2.Data[0].Version != 3 || page2.Data[1].Version != 2 {
		t.Fatalf("page2 versions = %+v", page2.Data)
	}
	if page2.NextPageToken == "" {
		t.Fatalf("page2 nextPageToken should still be non-empty (1 row left)")
	}

	// Third page: only v1 remains and it fits in pageSize=2 → no cursor.
	req3 := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+
			"/objects/employee/emp1/activity?pageSize=2&pageToken="+page2.NextPageToken, nil)
	rr3 := httptest.NewRecorder()
	r.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("page3 status = %d, body = %s", rr3.Code, rr3.Body.String())
	}
	var page3 oss.ObjectActivityResponse
	_ = json.Unmarshal(rr3.Body.Bytes(), &page3)
	if len(page3.Data) != 1 || page3.Data[0].Version != 1 {
		t.Fatalf("page3 versions = %+v", page3.Data)
	}
	if page3.NextPageToken != "" {
		t.Errorf("page3 nextPageToken = %q, want empty", page3.NextPageToken)
	}
}

func TestObjectActivity_NoHistoryReturnsEmptyArray(t *testing.T) {
	_, _, r := setupActivityHandler(t)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/missing/activity", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	// `data` must marshal as a non-nil JSON array so the SDK doesn't see null.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &raw)
	if string(raw["data"]) != "[]" {
		t.Errorf("data = %s, want []", string(raw["data"]))
	}
}

func TestObjectActivity_ObjectTypeNotFoundReturns404(t *testing.T) {
	_, _, r := setupActivityHandler(t)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/widget/anyPk/activity", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "ObjectTypeNotFound" {
		t.Errorf("errorName = %q, want ObjectTypeNotFound", apiErr.ErrorName)
	}
}

func TestObjectActivity_StoreNotConfiguredReturns500(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	h := oss.NewHandler(svc) // no SetActivityStore
	h.SetOmsRepo(repo)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/activity", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "ActivityStoreNotConfigured" {
		t.Errorf("errorName = %q, want ActivityStoreNotConfigured", apiErr.ErrorName)
	}
}

func TestObjectActivity_InvalidPageSizeReturns400(t *testing.T) {
	_, _, r := setupActivityHandler(t)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/activity?pageSize=abc", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
}

func TestObjectActivity_InvalidPageTokenReturns400(t *testing.T) {
	_, _, r := setupActivityHandler(t)
	// Non-base64 garbage.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/activity?pageToken=!!!", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}

	// Valid base64 but non-numeric payload.
	tok := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	req2 := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/activity?pageToken="+tok, nil)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric token: status = %d, want 400", rr2.Code)
	}
}

func TestObjectActivity_PageSizeIsCappedToMax(t *testing.T) {
	_, store, r := setupActivityHandler(t)
	otRID := "ri.ontology.main.object-type.employee"
	// Seed 250 rows but request pageSize=1000 — handler must clamp to 200.
	seedHistoryDescending(store, otRID, "emp1", 250)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/emp1/activity?pageSize=1000", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp oss.ObjectActivityResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Data) != 200 {
		t.Errorf("len(data) = %d, want 200 (capped)", len(resp.Data))
	}
	if resp.NextPageToken == "" {
		t.Errorf("nextPageToken should be set since 50 rows remain")
	}
}
