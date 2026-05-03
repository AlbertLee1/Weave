package apps

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHandler_CreateRequiresAuth(t *testing.T) {
	r := newTestRouter(NewMemoryStore(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/apps",
		strings.NewReader(`{"name":"X"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestHandler_CreateGetUpdateDelete(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, alice)

	// CREATE
	body := mustEncode(t, map[string]any{
		"name":       "Console",
		"layoutJson": json.RawMessage(validLayout),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/apps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created App
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.RID == "" || created.Name != "Console" || created.OwnerID != alice.ID || created.Version != 1 {
		t.Fatalf("create returned wrong shape: %+v", created)
	}

	// duplicate name → 409
	req = httptest.NewRequest(http.MethodPost, "/api/v2/apps", bytes.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: want 409, got %d", w.Code)
	}

	// invalid layout → 400 with InvalidAppLayout
	bad := mustEncode(t, map[string]any{
		"name":       "Bad",
		"layoutJson": json.RawMessage(`{"type":"row"}`),
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/apps", bytes.NewReader(bad))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid layout POST: want 400, got %d (%s)", w.Code, w.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &errBody)
	if errBody["errorName"] != "InvalidAppLayout" {
		t.Fatalf("expected InvalidAppLayout errorName, got %v", errBody["errorName"])
	}

	// missing name → 400 InvalidAppName
	noName := mustEncode(t, map[string]any{
		"layoutJson": json.RawMessage(validLayout),
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/apps", bytes.NewReader(noName))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty-name POST: want 400, got %d", w.Code)
	}

	// GET
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+created.RID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", w.Code)
	}

	// Cross-user GET → 404
	bobRouter := newTestRouter(store, &auth.User{ID: "user:bob"})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+created.RID, nil)
	w = httptest.NewRecorder()
	bobRouter.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user GET: want 404, got %d", w.Code)
	}

	// LIST
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("LIST: want 200, got %d", w.Code)
	}
	var listed listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(listed.Apps))
	}

	// UPDATE — bumps version
	updateBody := mustEncode(t, map[string]any{
		"name": "Console v2",
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/apps/"+created.RID, bytes.NewReader(updateBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var updated App
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Version != 2 || updated.Name != "Console v2" {
		t.Fatalf("update should bump version to 2, got %+v", updated)
	}

	// PUT bad layout → 400, version unchanged
	badPut := mustEncode(t, map[string]any{"layoutJson": json.RawMessage(`{"type":"banana"}`)})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/apps/"+created.RID, bytes.NewReader(badPut))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad PUT: want 400, got %d", w.Code)
	}

	// LIST VERSIONS
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+created.RID+"/versions", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListVersions: want 200, got %d", w.Code)
	}
	var lv listVersionsResponse
	_ = json.Unmarshal(w.Body.Bytes(), &lv)
	if len(lv.Versions) != 2 || lv.Versions[0].Version != 2 || lv.Versions[1].Version != 1 {
		t.Fatalf("expected 2 history rows, got %+v", lv.Versions)
	}

	// GET single version
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+created.RID+"/versions/1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetVersion: want 200, got %d", w.Code)
	}
	var v1 AppVersion
	_ = json.Unmarshal(w.Body.Bytes(), &v1)
	if v1.Version != 1 || v1.Name != "Console" {
		t.Fatalf("v1 wrong shape: %+v", v1)
	}

	// GET unknown version → 404
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+created.RID+"/versions/99", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing version: want 404, got %d", w.Code)
	}

	// GET non-int version → 400
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+created.RID+"/versions/abc", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad version: want 400, got %d", w.Code)
	}

	// DELETE — owner-only
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/apps/"+created.RID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+created.RID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: want 404, got %d", w.Code)
	}
}

func TestHandler_DegradedNoStore(t *testing.T) {
	// nil store → every endpoint reports AppsUnavailable so the SPA's
	// router can keep its /api/v2 prefix mounted in dev.
	r := newTestRouter(nil, &auth.User{ID: "user:alice"})
	req := httptest.NewRequest(http.MethodGet, "/api/v2/apps", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("LIST degraded: want 500, got %d", w.Code)
	}
	var errBody map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &errBody)
	if errBody["errorName"] != "AppsUnavailable" {
		t.Fatalf("expected AppsUnavailable errorName, got %v", errBody["errorName"])
	}
}
