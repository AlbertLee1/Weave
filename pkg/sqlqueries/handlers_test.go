package sqlqueries_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/sqlqueries"
)

type fakeEngine struct {
	gotQuery string
	err      error
}

func (f *fakeEngine) Execute(_ context.Context, query string) error {
	f.gotQuery = query
	return f.err
}

func newRouter(engine sqlqueries.Engine) *chi.Mux {
	r := chi.NewRouter()
	h := sqlqueries.NewHandler(engine)
	h.RegisterRoutes(r)
	return r
}

func doPost(t *testing.T, r http.Handler, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestExecute_Succeeded(t *testing.T) {
	engine := &fakeEngine{}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT 1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sqlqueries.QueryStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded", resp.Type)
	}
	if resp.QueryID == "" {
		t.Fatalf("queryId is empty")
	}
	if engine.gotQuery != "SELECT 1" {
		t.Fatalf("engine got %q, want %q", engine.gotQuery, "SELECT 1")
	}
}

func TestExecute_FailedFromEngineError(t *testing.T) {
	engine := &fakeEngine{err: errors.New("relation \"missing\" does not exist")}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT * FROM missing",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sqlqueries.QueryStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "failed" {
		t.Fatalf("type = %q, want failed", resp.Type)
	}
	if resp.QueryID == "" {
		t.Fatalf("queryId is empty")
	}
	if resp.FailureReason != "ExecutionError" {
		t.Fatalf("failureReason = %q, want ExecutionError", resp.FailureReason)
	}
	if resp.ErrorMessage == "" {
		t.Fatalf("errorMessage is empty")
	}
}

func TestExecute_RejectsNonSelect(t *testing.T) {
	engine := &fakeEngine{}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "DROP TABLE users",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("NonSelectQuery")) {
		t.Fatalf("expected NonSelectQuery error, got %s", rec.Body.String())
	}
	if engine.gotQuery != "" {
		t.Fatalf("engine should not have been called, got %q", engine.gotQuery)
	}
}

func TestExecute_RejectsStackedStatements(t *testing.T) {
	engine := &fakeEngine{}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT 1; DROP TABLE users",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("StackedStatement")) {
		t.Fatalf("expected StackedStatement error, got %s", rec.Body.String())
	}
	if engine.gotQuery != "" {
		t.Fatalf("engine should not have been called, got %q", engine.gotQuery)
	}
}

func TestExecute_RejectsSystemTableAccess(t *testing.T) {
	engine := &fakeEngine{}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT * FROM pg_user",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("SystemTableAccess")) {
		t.Fatalf("expected SystemTableAccess error, got %s", rec.Body.String())
	}
	if engine.gotQuery != "" {
		t.Fatalf("engine should not have been called, got %q", engine.gotQuery)
	}
}

func TestExecute_RejectsMissingQuery(t *testing.T) {
	engine := &fakeEngine{}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("MissingQuery")) {
		t.Fatalf("expected MissingQuery error, got %s", rec.Body.String())
	}
}

func TestExecute_NilEngineDegradedMode(t *testing.T) {
	r := newRouter(nil)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT 1",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("SqlQueryEngineNotConfigured")) {
		t.Fatalf("expected SqlQueryEngineNotConfigured error, got %s", rec.Body.String())
	}
}

func TestExecute_FallbackBranchIDsAccepted(t *testing.T) {
	// fallbackBranchIds is parsed for SDK parity but ignored. The request
	// must still succeed when the field is present.
	engine := &fakeEngine{}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query":             "SELECT 1",
		"fallbackBranchIds": []string{"branch-a", "branch-b"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sqlqueries.QueryStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded", resp.Type)
	}
}
