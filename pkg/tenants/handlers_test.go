package tenants

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func authedReq(method, path string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	r = r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: "user:admin"}))
	return r
}

func authedRawReq(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: "user:admin"}))
	return r
}

func newHandlerRouter() (*Handler, *chi.Mux, *MemoryStore, *Manager) {
	store := NewMemoryStore()
	mgr := NewManager(store)
	h := NewHandler(store, mgr)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return h, r, store, mgr
}

func TestHandler_CRUD(t *testing.T) {
	_, r, _, _ := newHandlerRouter()

	// Create.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-quotas", map[string]interface{}{
		"tenant":     "acme",
		"maxObjects": 1000,
		"maxQPS":     50.0,
		"burst":      100,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", w.Code, w.Body.String())
	}

	// Duplicate create.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-quotas", map[string]interface{}{"tenant": "acme"}))
	if w.Code != http.StatusConflict {
		t.Errorf("dup create: want 409, got %d", w.Code)
	}

	// Get.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodGet, "/api/admin/tenant-quotas/acme", nil))
	if w.Code != http.StatusOK {
		t.Errorf("get: want 200, got %d", w.Code)
	}

	// Update.
	w = httptest.NewRecorder()
	maxObj := int64(5000)
	r.ServeHTTP(w, authedReq(http.MethodPut, "/api/admin/tenant-quotas/acme", map[string]interface{}{
		"maxObjects": maxObj,
	}))
	if w.Code != http.StatusOK {
		t.Errorf("update: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var got Quota
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got.MaxObjects != 5000 {
		t.Errorf("update: maxObjects=%d, want 5000", got.MaxObjects)
	}

	// List.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodGet, "/api/admin/tenant-quotas", nil))
	if w.Code != http.StatusOK {
		t.Errorf("list: want 200, got %d", w.Code)
	}
	var ll listResponse
	_ = json.NewDecoder(w.Body).Decode(&ll)
	if len(ll.Quotas) != 1 {
		t.Errorf("list: want 1 quota, got %d", len(ll.Quotas))
	}

	// Delete.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodDelete, "/api/admin/tenant-quotas/acme", nil))
	if w.Code != http.StatusNoContent {
		t.Errorf("delete: want 204, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodGet, "/api/admin/tenant-quotas/acme", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete: want 404, got %d", w.Code)
	}
}

func TestBDD_HandlerRejectsAmbiguousJSONBodies_P2A002(t *testing.T) {
	t.Run("create rejects concatenated JSON without creating quota", func(t *testing.T) {
		_, r, store, _ := newHandlerRouter()
		body := `{"tenant":"acme","maxObjects":1000} {"tenant":"globex","maxObjects":2000}`
		w := httptest.NewRecorder()
		r.ServeHTTP(w, authedRawReq(http.MethodPost, "/api/admin/tenant-quotas", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("create concatenated JSON: want 400, got %d (%s)", w.Code, w.Body.String())
		}
		var apiErr struct {
			ErrorName string `json:"errorName"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &apiErr); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if apiErr.ErrorName != "InvalidRequestBody" {
			t.Fatalf("errorName: want InvalidRequestBody, got %q", apiErr.ErrorName)
		}
		rows, err := store.ListQuotas(context.Background())
		if err != nil {
			t.Fatalf("list quotas: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("ambiguous create must not persist quotas, got %+v", rows)
		}
	})

	t.Run("update rejects concatenated JSON without mutating quota", func(t *testing.T) {
		_, r, store, _ := newHandlerRouter()
		if err := store.CreateQuota(context.Background(), &Quota{
			Tenant:     "acme",
			MaxObjects: 100,
			MaxStorage: 200,
			MaxQPS:     10,
			Burst:      5,
		}); err != nil {
			t.Fatalf("seed quota: %v", err)
		}
		body := `{"maxObjects":1000,"description":"accepted first"} {"maxObjects":2000,"description":"ignored second"}`
		w := httptest.NewRecorder()
		r.ServeHTTP(w, authedRawReq(http.MethodPut, "/api/admin/tenant-quotas/acme", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("update concatenated JSON: want 400, got %d (%s)", w.Code, w.Body.String())
		}
		var apiErr struct {
			ErrorName string `json:"errorName"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &apiErr); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if apiErr.ErrorName != "InvalidRequestBody" {
			t.Fatalf("errorName: want InvalidRequestBody, got %q", apiErr.ErrorName)
		}
		got, err := store.GetQuota(context.Background(), "acme")
		if err != nil {
			t.Fatalf("get quota: %v", err)
		}
		if got.MaxObjects != 100 || got.MaxStorage != 200 || got.MaxQPS != 10 || got.Burst != 5 || got.Description != "" {
			t.Fatalf("ambiguous update must not mutate quota, got %+v", got)
		}
	})
}

func TestHandler_UnauthorisedWhenNoUser(t *testing.T) {
	_, r, _, _ := newHandlerRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/tenant-quotas", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous list: want 401, got %d", w.Code)
	}
}

func TestHandler_InvalidTenantName(t *testing.T) {
	_, r, _, _ := newHandlerRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-quotas", map[string]interface{}{
		"tenant": "bad name with spaces",
	}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad tenant: want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandler_UpdateInvalidatesLimiterCache(t *testing.T) {
	ctx := authedReq(http.MethodGet, "/", nil).Context()
	_, r, store, mgr := newHandlerRouter()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxQPS: 1, Burst: 1})

	// Burn the burst directly via manager.
	if !mgr.CheckQPS(ctx, "acme") {
		t.Fatal("first CheckQPS should pass")
	}
	if mgr.CheckQPS(ctx, "acme") {
		t.Fatal("second CheckQPS should fail (burst=1)")
	}

	// Bump the quota via the admin API.
	w := httptest.NewRecorder()
	bigQPS := 100.0
	bigBurst := 50
	r.ServeHTTP(w, authedReq(http.MethodPut, "/api/admin/tenant-quotas/acme", map[string]interface{}{
		"maxQPS": bigQPS,
		"burst":  bigBurst,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	// After the update, the cached limiter should be gone — fresh CheckQPS passes.
	if !mgr.CheckQPS(ctx, "acme") {
		t.Errorf("after admin update + reload, CheckQPS should pass again")
	}
}
