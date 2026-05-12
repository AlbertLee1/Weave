package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// US-044 (PC-A08): backend coverage for the saga / job monitoring read
// paths consumed by the new /actions/:ontology/jobs UI. The handlers
// share an in-memory SagaStore (memSagaStore from saga_us369_test.go)
// so the tests exercise the actual HTTP marshalling + chi URL routing
// the production binary registers in cmd/server/main.go.

func newSagaRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/sagas", h.ListSagas)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/sagas/{sagaId}", h.GetSaga)
	return r
}

func seedSagaUS044(t *testing.T, store *memSagaStore, ontology string) []*Saga {
	t.Helper()
	now := time.Now()
	sagas := []*Saga{
		{SagaID: "saga-success", Ontology: ontology, Status: SagaStatusSuccess,
			RequestedBy: "alice", CreatedAt: now.Add(-3 * time.Minute), UpdatedAt: now.Add(-3 * time.Minute)},
		{SagaID: "saga-running", Ontology: ontology, Status: SagaStatusRunning,
			RequestedBy: "bob", CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
		{SagaID: "saga-compensated", Ontology: ontology, Status: SagaStatusCompensated,
			RequestedBy: "carol", FailureMessage: "step B prepare failed",
			CreatedAt: now.Add(-1 * time.Minute), UpdatedAt: now.Add(-1 * time.Minute)},
		// Different ontology — must NOT appear in the per-ontology list.
		{SagaID: "saga-other-ont", Ontology: "other-ont", Status: SagaStatusSuccess,
			CreatedAt: now, UpdatedAt: now},
	}
	for _, sg := range sagas {
		if err := store.CreateSaga(context.Background(), sg); err != nil {
			t.Fatalf("seed saga %s: %v", sg.SagaID, err)
		}
	}
	return sagas
}

func TestHandler_ListSagas_US044_ScopesToOntologyAndOrdersByCreatedDesc(t *testing.T) {
	store := newMemSagaStore()
	exec := NewExecutor(&mockOmsRepo{}, &fakePublisher{})
	exec.SetSagaStore(store)
	handler := NewHandler(exec)
	router := newSagaRouter(handler)
	seedSagaUS044(t, store, "ont-1")

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont-1/actions/sagas", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []*Saga `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 sagas scoped to ont-1, got %d", len(resp.Data))
	}
	// Ordered by created_at DESC: compensated (newest) → running → success.
	if resp.Data[0].SagaID != "saga-compensated" ||
		resp.Data[1].SagaID != "saga-running" ||
		resp.Data[2].SagaID != "saga-success" {
		t.Fatalf("unexpected order: %v", []string{resp.Data[0].SagaID, resp.Data[1].SagaID, resp.Data[2].SagaID})
	}
	// Status field must round-trip exactly so the UI badge styler can switch on it.
	if resp.Data[0].Status != SagaStatusCompensated {
		t.Fatalf("expected COMPENSATED status preserved, got %q", resp.Data[0].Status)
	}
}

func TestHandler_ListSagas_US044_StatusFilterOnlyReturnsMatchingRows(t *testing.T) {
	store := newMemSagaStore()
	exec := NewExecutor(&mockOmsRepo{}, &fakePublisher{})
	exec.SetSagaStore(store)
	handler := NewHandler(exec)
	router := newSagaRouter(handler)
	seedSagaUS044(t, store, "ont-1")

	for _, tc := range []struct {
		status string
		wantID string
	}{
		{SagaStatusRunning, "saga-running"},
		{SagaStatusSuccess, "saga-success"},
		{SagaStatusCompensated, "saga-compensated"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v2/ontologies/ont-1/actions/sagas?status="+tc.status, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
			}
			var resp struct {
				Data []*Saga `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v body=%s", err, w.Body.String())
			}
			if len(resp.Data) != 1 {
				t.Fatalf("expected 1 row, got %d", len(resp.Data))
			}
			if resp.Data[0].SagaID != tc.wantID {
				t.Fatalf("expected %q, got %q", tc.wantID, resp.Data[0].SagaID)
			}
		})
	}
}

func TestHandler_ListSagas_US044_EmptyResponseWhenStoreNotConfigured(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, &fakePublisher{})
	// Intentionally no SagaStore wired (degraded mode).
	handler := NewHandler(exec)
	router := newSagaRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont-1/actions/sagas", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty list, got %d", len(resp.Data))
	}
}

func TestHandler_GetSaga_US044_ReturnsHeaderAndStepTimeline(t *testing.T) {
	store := newMemSagaStore()
	exec := NewExecutor(&mockOmsRepo{}, &fakePublisher{})
	exec.SetSagaStore(store)
	handler := NewHandler(exec)
	router := newSagaRouter(handler)

	if err := store.CreateSaga(context.Background(), &Saga{
		SagaID: "saga-detail", Ontology: "ont-1", Status: SagaStatusCompensated,
		FailureMessage: "step B prepare failed",
	}); err != nil {
		t.Fatalf("seed saga: %v", err)
	}
	steps := []*SagaStep{
		{StepID: "step-1", SagaID: "saga-detail", StepIndex: 0,
			ActionType: "ri.action.createOrder", Status: SagaStepStatusCompensated,
			EditsJSON:        json.RawMessage(`[{"kind":"CREATE","objectType":"Order"}]`),
			InverseEditsJSON: json.RawMessage(`[{"kind":"DELETE","objectType":"Order"}]`)},
		{StepID: "step-2", SagaID: "saga-detail", StepIndex: 1,
			ActionType: "ri.action.bookResource", Status: SagaStepStatusFailed},
	}
	for _, st := range steps {
		if err := store.CreateSagaStep(context.Background(), st); err != nil {
			t.Fatalf("seed step %s: %v", st.StepID, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/sagas/saga-detail", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Saga  *Saga       `json:"saga"`
		Steps []*SagaStep `json:"steps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp.Saga == nil || resp.Saga.SagaID != "saga-detail" {
		t.Fatalf("expected saga-detail, got %+v", resp.Saga)
	}
	if resp.Saga.Status != SagaStatusCompensated {
		t.Fatalf("expected COMPENSATED, got %q", resp.Saga.Status)
	}
	if resp.Saga.FailureMessage != "step B prepare failed" {
		t.Fatalf("expected failureMessage propagated, got %q", resp.Saga.FailureMessage)
	}
	if len(resp.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(resp.Steps))
	}
	if resp.Steps[0].Status != SagaStepStatusCompensated {
		t.Fatalf("expected step 0 COMPENSATED, got %q", resp.Steps[0].Status)
	}
	if resp.Steps[1].Status != SagaStepStatusFailed {
		t.Fatalf("expected step 1 FAILED, got %q", resp.Steps[1].Status)
	}
	if len(resp.Steps[0].InverseEditsJSON) == 0 {
		t.Fatal("expected inverseEditsJson to round-trip for the compensated step")
	}
}

func TestHandler_GetSaga_US044_Returns404OnMissingSaga(t *testing.T) {
	store := newMemSagaStore()
	exec := NewExecutor(&mockOmsRepo{}, &fakePublisher{})
	exec.SetSagaStore(store)
	handler := NewHandler(exec)
	router := newSagaRouter(handler)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/sagas/no-such-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ErrorName string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ErrorName != "SagaNotFound" {
		t.Fatalf("expected SagaNotFound, got %q", resp.ErrorName)
	}
}
