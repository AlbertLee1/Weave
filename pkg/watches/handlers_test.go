package watches

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newTestRouter(store Store, user *auth.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			if user != nil {
				ctx = auth.WithUser(ctx, user)
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	NewHandler(store).RegisterRoutes(r)
	return r
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestHandler_CreateListStatusDelete(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, alice)

	target := "ri.weave.main.object.42"

	// CREATE
	body := mustEncode(t, map[string]any{"targetRid": target})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/watches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created Watch
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.UserID != alice.ID || created.TargetRID != target || created.ID == "" {
		t.Fatalf("create returned wrong shape: %+v", created)
	}

	// CREATE with bad RID → 400
	bad := mustEncode(t, map[string]any{"targetRid": "not-a-rid"})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/watches", bytes.NewReader(bad))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad POST: want 400, got %d", w.Code)
	}

	// Idempotent re-CREATE → 201 with the same id
	req = httptest.NewRequest(http.MethodPost, "/api/v2/watches", bytes.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("re-POST: want 201, got %d", w.Code)
	}
	var second Watch
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if second.ID != created.ID {
		t.Fatalf("Re-POST should return same id: first=%s second=%s", created.ID, second.ID)
	}

	// STATUS true
	req = httptest.NewRequest(http.MethodGet, "/api/v2/watches/status?targetRid="+target, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var status statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Watching {
		t.Fatalf("Status should be watching=true")
	}

	// STATUS false (different target)
	req = httptest.NewRequest(http.MethodGet, "/api/v2/watches/status?targetRid=ri.weave.main.object.99", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status (other): want 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Watching {
		t.Fatalf("Status for unwatched target should be false")
	}

	// LIST shows alice's row
	req = httptest.NewRequest(http.MethodGet, "/api/v2/watches", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", w.Code)
	}
	var list listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Watches) != 1 || list.Watches[0].TargetRID != target {
		t.Fatalf("List unexpected: %+v", list)
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/watches?targetRid="+target, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d (%s)", w.Code, w.Body.String())
	}

	// Re-DELETE → 404
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/watches?targetRid="+target, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("re-delete: want 404, got %d", w.Code)
	}

	// LIST is empty
	req = httptest.NewRequest(http.MethodGet, "/api/v2/watches", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list 2: want 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list 2: %v", err)
	}
	if len(list.Watches) != 0 {
		t.Fatalf("List should be empty after delete, got %+v", list)
	}
}

func TestHandler_CrossUserIsolation(t *testing.T) {
	store := NewMemoryStore()
	target := "ri.weave.main.object.42"

	// Alice watches the target.
	r := newTestRouter(store, &auth.User{ID: "user:alice"})
	body := mustEncode(t, map[string]any{"targetRid": target})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/watches", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("alice POST: want 201, got %d", w.Code)
	}

	// Bob should see status=false and an empty list.
	rb := newTestRouter(store, &auth.User{ID: "user:bob"})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/watches/status?targetRid="+target, nil)
	w = httptest.NewRecorder()
	rb.ServeHTTP(w, req)
	var status statusResponse
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if status.Watching {
		t.Fatalf("Bob should not see Alice's watch")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v2/watches", nil)
	w = httptest.NewRecorder()
	rb.ServeHTTP(w, req)
	var list listResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Watches) != 0 {
		t.Fatalf("Bob's list should be empty, got %+v", list)
	}

	// Bob unwatching alice's row → 404 (target+user pair has no row).
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/watches?targetRid="+target, nil)
	w = httptest.NewRecorder()
	rb.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Bob re-delete: want 404, got %d", w.Code)
	}
}

func TestHandler_DegradedModeNoStore(t *testing.T) {
	r := newTestRouter(nil, &auth.User{ID: "user:alice"})
	cases := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/v2/watches", nil},
		{http.MethodGet, "/api/v2/watches/status?targetRid=ri.weave.main.object.42", nil},
		{http.MethodPost, "/api/v2/watches", []byte(`{"targetRid":"ri.weave.main.object.42"}`)},
		{http.MethodDelete, "/api/v2/watches?targetRid=ri.weave.main.object.42", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body *bytes.Reader
			if tc.body != nil {
				body = bytes.NewReader(tc.body)
			}
			var req *http.Request
			if body == nil {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, body)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("%s %s: want 500 WatchesUnavailable, got %d (%s)", tc.method, tc.path, w.Code, w.Body.String())
			}
			var env map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &env)
			if env["errorName"] != "WatchesUnavailable" {
				t.Fatalf("errorName mismatch: %v", env["errorName"])
			}
		})
	}
}

func TestHandler_RequiresAuth(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/watches", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anon GET: want 401, got %d", w.Code)
	}
}
