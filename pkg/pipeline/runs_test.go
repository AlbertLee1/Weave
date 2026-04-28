package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

func newRunFixture(pipelineID string) *PipelineRun {
	return &PipelineRun{
		PipelineID:  pipelineID,
		Status:      "success",
		StartedAt:   time.Now().UTC(),
		TriggeredBy: "user:alice",
	}
}

func TestMemoryStore_AppendPipelineRun_StampsID(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if err := s.CreatePipeline(ctx, newTestPipeline("demo")); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	run := newRunFixture("demo")
	if err := s.AppendPipelineRun(ctx, run); err != nil {
		t.Fatalf("AppendPipelineRun: %v", err)
	}
	if run.ID == 0 {
		t.Fatal("AppendPipelineRun did not stamp the row id")
	}
	if run.CreatedAt.IsZero() {
		t.Fatal("AppendPipelineRun did not stamp CreatedAt")
	}
}

func TestMemoryStore_AppendPipelineRun_UnknownPipeline(t *testing.T) {
	s := NewMemoryStore()
	err := s.AppendPipelineRun(context.Background(), newRunFixture("missing"))
	if !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("err = %v, want ErrPipelineNotFound", err)
	}
}

func TestMemoryStore_GetPipelineRun(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	run := newRunFixture("demo")
	run.ErrorMessage = "boom"
	run.Status = "failed"
	if err := s.AppendPipelineRun(ctx, run); err != nil {
		t.Fatalf("AppendPipelineRun: %v", err)
	}
	got, err := s.GetPipelineRun(ctx, "demo", run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
	if got.ErrorMessage != "boom" {
		t.Fatalf("ErrorMessage = %q, want boom", got.ErrorMessage)
	}
}

func TestMemoryStore_GetPipelineRun_NotFound(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	_, err := s.GetPipelineRun(ctx, "demo", 999)
	if !errors.Is(err, ErrPipelineRunNotFound) {
		t.Fatalf("err = %v, want ErrPipelineRunNotFound", err)
	}
}

func TestMemoryStore_GetPipelineRun_RejectsCrossPipeline(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	other := newTestPipeline("other")
	_ = s.CreatePipeline(ctx, other)
	run := newRunFixture("demo")
	if err := s.AppendPipelineRun(ctx, run); err != nil {
		t.Fatalf("AppendPipelineRun: %v", err)
	}
	if _, err := s.GetPipelineRun(ctx, "other", run.ID); !errors.Is(err, ErrPipelineRunNotFound) {
		t.Fatalf("cross-pipeline lookup err = %v, want ErrPipelineRunNotFound", err)
	}
}

func TestMemoryStore_ListPipelineRuns_NewestFirstPagination(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	for i := 0; i < 5; i++ {
		r := newRunFixture("demo")
		r.TriggeredBy = fmt.Sprintf("u-%d", i)
		if err := s.AppendPipelineRun(ctx, r); err != nil {
			t.Fatalf("AppendPipelineRun: %v", err)
		}
	}

	page1, err := s.ListPipelineRuns(ctx, "demo", ListRunsOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListPipelineRuns page1: %v", err)
	}
	if len(page1.Runs) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1.Runs))
	}
	if page1.NextCursor == 0 {
		t.Fatal("page1 NextCursor should be non-zero")
	}
	// Newest-first ordering.
	if page1.Runs[0].ID < page1.Runs[1].ID {
		t.Fatalf("expected newest first, got %d, %d", page1.Runs[0].ID, page1.Runs[1].ID)
	}

	page2, err := s.ListPipelineRuns(ctx, "demo", ListRunsOptions{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("ListPipelineRuns page2: %v", err)
	}
	if len(page2.Runs) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2.Runs))
	}
	if page2.Runs[0].ID >= page1.Runs[1].ID {
		t.Fatalf("page2 first id (%d) must be older than page1 last (%d)",
			page2.Runs[0].ID, page1.Runs[1].ID)
	}

	page3, err := s.ListPipelineRuns(ctx, "demo", ListRunsOptions{Limit: 2, Cursor: page2.NextCursor})
	if err != nil {
		t.Fatalf("ListPipelineRuns page3: %v", err)
	}
	if len(page3.Runs) != 1 {
		t.Fatalf("page3 len = %d, want 1", len(page3.Runs))
	}
	if page3.NextCursor != 0 {
		t.Fatalf("page3 NextCursor = %d, want 0 (final page)", page3.NextCursor)
	}
}

func TestMemoryStore_ListPipelineRuns_UnknownPipeline(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.ListPipelineRuns(context.Background(), "missing", ListRunsOptions{Limit: 10})
	if !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("err = %v, want ErrPipelineNotFound", err)
	}
}

func TestMemoryStore_ListPipelineRuns_EmptyOk(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	page, err := s.ListPipelineRuns(ctx, "demo", ListRunsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if len(page.Runs) != 0 || page.NextCursor != 0 {
		t.Fatalf("expected empty page, got %+v", page)
	}
}

func TestMemoryStore_DeletePipeline_CascadesRuns(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	_ = s.AppendPipelineRun(ctx, newRunFixture("demo"))
	if err := s.DeletePipeline(ctx, "demo"); err != nil {
		t.Fatalf("DeletePipeline: %v", err)
	}
	// Re-create a pipeline with the same id; previous runs must not leak in.
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	page, err := s.ListPipelineRuns(ctx, "demo", ListRunsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if len(page.Runs) != 0 {
		t.Fatalf("expected 0 runs after re-create, got %d", len(page.Runs))
	}
}

func TestHandler_ListPipelineRuns_Paginated(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	ctx := context.Background()
	if err := store.CreatePipeline(ctx, &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	}); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	for i := 0; i < 3; i++ {
		run := newRunFixture("demo")
		run.TriggeredBy = fmt.Sprintf("u-%d", i)
		if err := store.AppendPipelineRun(ctx, run); err != nil {
			t.Fatalf("AppendPipelineRun: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo/runs?limit=2", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var page listPipelineRunsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(page.Runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(page.Runs))
	}
	if page.NextCursor == "" {
		t.Fatal("NextCursor should be non-empty when more pages remain")
	}

	// Follow the cursor.
	url := "/api/v2/pipelines/demo/runs?limit=2&cursor=" + page.NextCursor
	req = httptest.NewRequest(http.MethodGet, url, nil)
	req = withAuthContext(req, "user:alice")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var page2 listPipelineRunsResponse
	_ = json.Unmarshal(w.Body.Bytes(), &page2)
	if len(page2.Runs) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2.Runs))
	}
	if page2.NextCursor != "" {
		t.Fatalf("page2 NextCursor = %q, want empty", page2.NextCursor)
	}
}

func TestHandler_ListPipelineRuns_OwnershipEnforced(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	ctx := context.Background()
	_ = store.CreatePipeline(ctx, &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})
	_ = store.AppendPipelineRun(ctx, newRunFixture("demo"))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo/runs", nil)
	req = withAuthContext(req, "user:bob")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner status=%d body=%s", w.Code, w.Body.String())
	}

	// Admin sees runs.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo/runs", nil)
	req = withAuthContext(req, "user:admin", auth.RoleAdmin)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_ListPipelineRuns_MissingPipeline(t *testing.T) {
	h, _ := newHandlerWithStore()
	r := newRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/missing/runs", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_ListPipelineRuns_RejectsBadCursor(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	_ = store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo/runs?cursor=oops", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["errorName"].(string); got != "InvalidPipelineRunCursor" {
		t.Errorf("errorName=%q, want InvalidPipelineRunCursor", got)
	}
}

func TestHandler_GetPipelineRun_Detail(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	ctx := context.Background()
	_ = store.CreatePipeline(ctx, &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})
	run := newRunFixture("demo")
	run.Status = "failed"
	run.ErrorMessage = "boom"
	finished := time.Now().UTC()
	run.FinishedAt = &finished
	if err := store.AppendPipelineRun(ctx, run); err != nil {
		t.Fatalf("AppendPipelineRun: %v", err)
	}

	url := fmt.Sprintf("/api/v2/pipelines/demo/runs/%d", run.ID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got PipelineRun
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if got.ID != run.ID {
		t.Errorf("ID = %d, want %d", got.ID, run.ID)
	}
	if got.Status != "failed" || got.ErrorMessage != "boom" {
		t.Errorf("got Status=%q ErrorMessage=%q, want failed/boom", got.Status, got.ErrorMessage)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt should round-trip via the API")
	}
}

func TestHandler_GetPipelineRun_NotFound(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	_ = store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo/runs/999", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_GetPipelineRun_BadRunID(t *testing.T) {
	h, store := newHandlerWithStore()
	r := newRouter(h)
	_ = store.CreatePipeline(context.Background(), &Pipeline{
		ID:        "demo",
		CreatedBy: "user:alice",
		Inputs:    []Input{{Name: "src", Type: "objectset"}},
		Outputs:   []Output{{Name: "sink", Type: "jdbc", Input: "src"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/pipelines/demo/runs/notanumber", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
