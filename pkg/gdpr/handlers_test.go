package gdpr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
)

func TestErase_RejectsAnonymousCaller(t *testing.T) {
	h := newTestHandler(t, NewMemoryJobStore(), simpleEraser(NewMemoryJobStore()))
	rec := doJSON(h, http.MethodPost, "/api/admin/gdpr/erase",
		`{"userId":"user:bob"}`, "" /* anonymous */)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}
}

func TestErase_RejectsEmptyUserID(t *testing.T) {
	jobStore := NewMemoryJobStore()
	h := newTestHandler(t, jobStore, simpleEraser(jobStore))
	rec := doJSON(h, http.MethodPost, "/api/admin/gdpr/erase",
		`{}`, "user:admin")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "MissingUserID") {
		t.Errorf("expected MissingUserID errorName, got %s", rec.Body.String())
	}
}

func TestErase_AcceptsAndKicksWorker(t *testing.T) {
	jobStore := NewMemoryJobStore()
	called := make(chan string, 1)
	steps := []Step{
		StepFunc{StepName: "noop", Fn: func(_ context.Context, uid string) (int, error) {
			called <- uid
			return 0, nil
		}},
	}
	eraser := NewEraser(jobStore, steps)
	auditStore := audit.NewMemoryStore()
	h := newTestHandler(t, jobStore, eraser)
	h.auditStore = auditStore

	rec := doJSON(h, http.MethodPost, "/api/admin/gdpr/erase",
		`{"userId":"user:bob"}`, "user:admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	var resp EraseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.JobID == "" {
		t.Fatal("expected jobId in response")
	}
	if resp.Status != JobStatusPending {
		t.Errorf("status = %s, want PENDING", resp.Status)
	}
	// Worker should have been invoked with the userId.
	select {
	case uid := <-called:
		if uid != "user:bob" {
			t.Errorf("step called with %q, want user:bob", uid)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not run within 1s")
	}

	// Audit row for the request should be present.
	events, _ := auditStore.List(context.Background(), audit.ListFilter{})
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Action != "gdpr_erase_request" || events[0].ResourceRID != "user:bob" {
		t.Errorf("audit event wrong: %#v", events[0])
	}
}

func TestErase_DegradedModeWhenUnconfigured(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	r := chi.NewRouter()
	r.Route("/", func(r chi.Router) {
		r.Use(testInjectUser("user:admin"))
		h.RegisterRoutes(r)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/gdpr/erase",
		strings.NewReader(`{"userId":"user:bob"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GDPREraseUnavailable") {
		t.Errorf("expected GDPREraseUnavailable errorName, got %s", rec.Body.String())
	}
}

func TestGetJob_ReturnsCurrentState(t *testing.T) {
	store := NewMemoryJobStore()
	job := &ErasureJob{JobID: "j-1", UserID: "user:bob", Status: JobStatusSucceeded, Progress: 100}
	if err := store.CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, store, simpleEraser(store))

	rec := doRequest(h, http.MethodGet, "/api/admin/gdpr/erase/j-1", nil, "user:admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got ErasureJob
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.JobID != "j-1" || got.Status != JobStatusSucceeded {
		t.Errorf("unexpected job: %#v", got)
	}
}

func TestGetJob_404ForUnknownID(t *testing.T) {
	store := NewMemoryJobStore()
	h := newTestHandler(t, store, simpleEraser(store))
	rec := doRequest(h, http.MethodGet, "/api/admin/gdpr/erase/nope", nil, "user:admin")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetJob_RejectsAnonymous(t *testing.T) {
	store := NewMemoryJobStore()
	h := newTestHandler(t, store, simpleEraser(store))
	rec := doRequest(h, http.MethodGet, "/api/admin/gdpr/erase/abc", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}
}

func TestErase_StepFailureSurfacesAsFailedJob(t *testing.T) {
	jobStore := NewMemoryJobStore()
	doneCh := make(chan struct{})
	steps := []Step{
		StepFunc{StepName: "boom", Fn: func(context.Context, string) (int, error) {
			defer close(doneCh)
			return 0, errors.New("boom")
		}},
	}
	eraser := NewEraser(jobStore, steps)
	h := newTestHandler(t, jobStore, eraser)

	rec := doJSON(h, http.MethodPost, "/api/admin/gdpr/erase",
		`{"userId":"user:carol"}`, "user:admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	var resp EraseResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("worker did not finish")
	}
	// Allow the orchestrator's terminal write to land.
	deadline := time.Now().Add(time.Second)
	for {
		stored, err := jobStore.GetJob(context.Background(), resp.JobID)
		if err == nil && stored.Status == JobStatusFailed {
			if !strings.Contains(stored.ErrorMessage, "boom") {
				t.Errorf("error message = %q", stored.ErrorMessage)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not transition to FAILED: %#v err=%v", stored, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// --- helpers ---

func newTestHandler(t *testing.T, store JobStore, e *Eraser) *Handler {
	t.Helper()
	return NewHandler(store, e, audit.NewMemoryStore())
}

func simpleEraser(store JobStore) *Eraser {
	return NewEraser(store, []Step{
		StepFunc{StepName: "noop", Fn: func(context.Context, string) (int, error) { return 0, nil }},
	})
}

func doJSON(h *Handler, method, target, body, callerID string) *httptest.ResponseRecorder {
	return doRequest(h, method, target, bytes.NewBufferString(body), callerID)
}

func doRequest(h *Handler, method, target string, body interface {
	Read(p []byte) (int, error)
}, callerID string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(testInjectUser(callerID))
		h.RegisterRoutes(r)
	})
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, body)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// testInjectUser is a chi middleware that stamps an auth.User onto the
// request context. callerID="" leaves the request anonymous.
func testInjectUser(callerID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if callerID != "" {
				ctx := auth.WithUser(r.Context(), &auth.User{ID: callerID})
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}
