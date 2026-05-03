package quiver

import (
	"bytes"
	"context"
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

func TestHandler_SaveListGetViewDelete(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}

	rAlice := newTestRouter(store, alice)
	rBob := newTestRouter(store, bob)

	// SAVE (create)
	body := mustEncode(t, map[string]any{
		"name":   "CPU Trend",
		"config": map[string]any{"series": []any{}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/quiver/save", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rAlice.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST save: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created Dashboard
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "CPU Trend" || created.Owner != alice.ID || created.RID == "" {
		t.Fatalf("save returned wrong shape: %+v", created)
	}

	// SAVE again with same name → 409 (no rid means create branch, name collision).
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v2/quiver/save", bytes.NewReader(body))
	rAlice.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: want 409, got %d", w.Code)
	}

	// SAVE with rid (update existing) — bumps updatedAt + replaces config.
	updateBody := mustEncode(t, map[string]any{
		"rid":    created.RID,
		"name":   "CPU Trend",
		"config": map[string]any{"series": []any{map[string]any{"id": "a"}}},
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v2/quiver/save", bytes.NewReader(updateBody))
	rAlice.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update save: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var updated Dashboard
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.RID != created.RID {
		t.Fatalf("update should keep rid: got %q want %q", updated.RID, created.RID)
	}
	if !bytes.Contains(updated.Config, []byte(`"id":"a"`)) {
		t.Fatalf("update did not persist config: %s", string(updated.Config))
	}

	// SAVE with empty name → 400.
	bad := mustEncode(t, map[string]any{"name": ""})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v2/quiver/save", bytes.NewReader(bad))
	rAlice.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty name POST: want 400, got %d", w.Code)
	}

	// LIST (alice sees one row)
	w = httptest.NewRecorder()
	rAlice.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET list: want 200, got %d", w.Code)
	}
	var listResp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Dashboards) != 1 || listResp.Dashboards[0].RID != created.RID {
		t.Fatalf("list returned %+v", listResp.Dashboards)
	}

	// LIST (bob sees zero rows — share is RID-only, not enumerable)
	w = httptest.NewRecorder()
	rBob.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards", nil))
	var bobList listResponse
	_ = json.Unmarshal(w.Body.Bytes(), &bobList)
	if len(bobList.Dashboards) != 0 {
		t.Fatalf("bob should not see alice's dashboard: %+v", bobList.Dashboards)
	}

	// GET (owner) — works
	w = httptest.NewRecorder()
	rAlice.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards/"+created.RID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("owner GET: want 200, got %d", w.Code)
	}

	// GET (non-owner) — 404 (private)
	w = httptest.NewRecorder()
	rBob.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards/"+created.RID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner GET: want 404, got %d", w.Code)
	}

	// VIEW (non-owner with RID) — 200 (share)
	w = httptest.NewRecorder()
	rBob.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards/"+created.RID+"/view", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("non-owner VIEW: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var viewed Dashboard
	if err := json.Unmarshal(w.Body.Bytes(), &viewed); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if viewed.RID != created.RID || viewed.Owner != alice.ID {
		t.Fatalf("view returned wrong shape: %+v", viewed)
	}

	// VIEW unknown RID → 404
	w = httptest.NewRecorder()
	rBob.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards/ri.quiver.main.dashboard.does-not-exist/view", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("VIEW unknown rid: want 404, got %d", w.Code)
	}

	// DELETE (non-owner) — 404
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/quiver/dashboards/"+created.RID, nil)
	rBob.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner DELETE: want 404, got %d", w.Code)
	}

	// DELETE (owner) — 204
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/quiver/dashboards/"+created.RID, nil)
	rAlice.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("owner DELETE: want 204, got %d", w.Code)
	}

	// VIEW after delete — 404
	w = httptest.NewRecorder()
	rBob.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards/"+created.RID+"/view", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("VIEW after delete: want 404, got %d", w.Code)
	}
}

func TestHandler_DegradedMode_NoStore(t *testing.T) {
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(nil, alice)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards", nil))
	// nil-store path returns a structured 5xx APIError envelope so the SPA
	// can hide the dashboard panel rather than render an unhandled error.
	if w.Code < 500 {
		t.Fatalf("degraded list: want 5xx, got %d", w.Code)
	}
}

func TestHandler_Unauthenticated(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/quiver/dashboards", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list: want 401, got %d", w.Code)
	}
}

// TestStore_PersistsBetweenSessions covers the PRD's "save → re-load
// returns identical config" expectation: round-trip through the
// MemoryStore preserves the JSONB envelope byte-for-byte.
func TestStore_PersistsBetweenSessions(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	original := newRow("rid-x", "alice", "demo")
	original.Config = json.RawMessage(`{"series":[{"id":"a","color":"#fff","label":"A"}],"selection":{"start":1,"end":2}}`)
	if err := s.Save(ctx, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.GetByRID(ctx, "rid-x")
	if err != nil {
		t.Fatalf("GetByRID: %v", err)
	}
	if string(got.Config) != string(original.Config) {
		t.Fatalf("config drifted on round-trip:\nwant %s\ngot  %s", string(original.Config), string(got.Config))
	}
}
