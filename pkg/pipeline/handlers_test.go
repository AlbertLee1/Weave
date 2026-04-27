package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newHandlerWithStore() (*Handler, *MemoryStore) {
	store := NewMemoryStore()
	return NewHandler(store), store
}

func withAuthContext(r *http.Request, userID string, roles ...string) *http.Request {
	user := &auth.User{ID: userID, Roles: roles}
	ctx := auth.WithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func newRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func samplePipelineBody(t *testing.T, id string) string {
	t.Helper()
	body := map[string]any{
		"name":        "Daily ETL",
		"description": "demo pipeline",
		"inputs": []any{
			map[string]any{
				"name":   "src",
				"type":   "objectset",
				"config": map[string]any{"objectType": "Customer"},
			},
		},
		"transforms": []any{
			map[string]any{
				"name":   "filter_active",
				"type":   "filter",
				"inputs": []any{"src"},
				"config": map[string]any{"where": "active = true"},
			},
		},
		"outputs": []any{
			map[string]any{
				"name":   "warehouse",
				"type":   "jdbc",
				"input":  "filter_active",
				"config": map[string]any{"table": "active_customers"},
			},
		},
		"schedule": "0 9 * * *",
	}
	if id != "" {
		body["id"] = id
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestHandler_CreatePipeline_Success(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines",
		strings.NewReader(samplePipelineBody(t, "demo")))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var p Pipeline
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if p.ID != "demo" {
		t.Errorf("ID = %q, want demo", p.ID)
	}
	if p.CreatedBy != "user:alice" {
		t.Errorf("CreatedBy = %q, want user:alice", p.CreatedBy)
	}
	if !p.Enabled {
		t.Error("Enabled defaulted to false; want true")
	}
	if _, err := store.GetPipeline(context.Background(), "demo"); err != nil {
		t.Fatalf("expected stored pipeline: %v", err)
	}
}

func TestHandler_CreatePipeline_AutoID(t *testing.T) {
	h, _ := newHandlerWithStore()
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines",
		strings.NewReader(samplePipelineBody(t, "")))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var p Pipeline
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if !strings.HasPrefix(p.ID, "pipeline_") {
		t.Errorf("expected pipeline_-prefixed id, got %q", p.ID)
	}
}

func TestHandler_CreatePipeline_RejectsBadDefinition(t *testing.T) {
	h, _ := newHandlerWithStore()
	r := newRouter(h)

	body := `{"id":"bad","inputs":[],"outputs":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["errorName"].(string); got != "InvalidPipelineDefinition" {
		t.Errorf("errorName=%q, want InvalidPipelineDefinition", got)
	}
}

func TestHandler_CreatePipeline_RejectsBadID(t *testing.T) {
	h, _ := newHandlerWithStore()
	r := newRouter(h)

	body := `{"id":"bad id","inputs":[{"name":"a","type":"x"}],"outputs":[{"name":"b","type":"y"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["errorName"].(string); got != "InvalidPipelineID" {
		t.Errorf("errorName=%q, want InvalidPipelineID", got)
	}
}

func TestHandler_CreatePipeline_Conflict(t *testing.T) {
	h, _ := newHandlerWithStore()
	r := newRouter(h)
	body := samplePipelineBody(t, "demo")

	req1 := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines", strings.NewReader(body))
	req1 = withAuthContext(req1, "user:alice")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create status=%d", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines", strings.NewReader(body))
	req2 = withAuthContext(req2, "user:alice")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d body=%s", w2.Code, w2.Body.String())
	}
}

func TestHandler_GetPipeline_OwnershipEnforced(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	if err := store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Other user gets 403.
	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo", nil)
	req = withAuthContext(req, "user:bob")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner GET status=%d body=%s", w.Code, w.Body.String())
	}

	// Admin sees everything.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo", nil)
	req = withAuthContext(req, "user:admin", auth.RoleAdmin)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin GET status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_ListPipelines_ScopedToCaller(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	for _, id := range []string{"a", "b"} {
		_ = store.CreatePipeline(context.Background(), &Pipeline{
			ID:        id,
			CreatedBy: "user:alice",
			Inputs:    []Input{{Name: "src", Type: "objectset"}},
			Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
		})
	}
	_ = store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "c",
		CreatedBy: "user:bob",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var resp listPipelinesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Pipelines) != 2 {
		t.Fatalf("alice sees %d, want 2", len(resp.Pipelines))
	}

	// Admin sees all.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/pipelines", nil)
	req = withAuthContext(req, "user:admin", auth.RoleAdmin)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Pipelines) != 3 {
		t.Fatalf("admin sees %d, want 3", len(resp.Pipelines))
	}
}

func TestHandler_UpdatePipeline_Partial(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	if err := store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "demo",
		Name:      "Old",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
		Enabled:   true,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	body := `{"name":"Renamed","schedule":"0 9 * * *"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/pipelines/demo", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := store.GetPipeline(context.Background(), "demo")
	if got.Name != "Renamed" {
		t.Errorf("Name = %q, want Renamed", got.Name)
	}
	if got.Schedule != "0 9 * * *" {
		t.Errorf("Schedule = %q, want %q", got.Schedule, "0 9 * * *")
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Name != "src" {
		t.Errorf("Inputs lost on partial update: %+v", got.Inputs)
	}
}

func TestHandler_UpdatePipeline_RejectsBadDefinition(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	_ = store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})

	body := `{"schedule":"bad"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/pipelines/demo", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_DeletePipeline(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	_ = store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/pipelines/demo", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	if _, err := store.GetPipeline(context.Background(), "demo"); err == nil {
		t.Fatal("pipeline still present after delete")
	}
}

func TestHandler_RequiresAuth(t *testing.T) {
	h, _ := newHandlerWithStore()
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_StoreUnavailable(t *testing.T) {
	h := NewHandler(nil)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["errorName"].(string); got != "PipelinesUnavailable" {
		t.Errorf("errorName=%q, want PipelinesUnavailable", got)
	}
}
