package apps

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func createTestApp(t *testing.T, store Store, owner *auth.User, name string) *App {
	t.Helper()
	r := newTestRouter(store, owner)
	body := mustEncode(t, map[string]any{
		"name":       name,
		"layoutJson": json.RawMessage(validLayout),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/apps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created App
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	return &created
}

func TestHandler_PublishOwnerOnly(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}
	app := createTestApp(t, store, alice, "Console")

	// Bob cannot publish Alice's App.
	bobR := newTestRouter(store, bob)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/publish", nil)
	w := httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Bob publish Alice's App: want 404, got %d (%s)", w.Code, w.Body.String())
	}

	// Alice can.
	aliceR := newTestRouter(store, alice)
	req = httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/publish", nil)
	w = httptest.NewRecorder()
	aliceR.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Alice publish own App: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var view PublishedAppView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view.PublishedVersion != 1 || view.RID != app.RID {
		t.Fatalf("unexpected view: %+v", view)
	}
}

func TestHandler_PublishMissing(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, alice)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/apps/ri.app.main.app.missing/publish", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("publish missing: want 404, got %d", w.Code)
	}
}

func TestHandler_PublishRequiresAuth(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/apps/ri.app.main.app.x/publish", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth publish: want 401, got %d", w.Code)
	}
}

func TestHandler_ViewReturnsPublishedSnapshotForAnyAuthUser(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}
	app := createTestApp(t, store, alice, "Console")

	// Publish as owner.
	aliceR := newTestRouter(store, alice)
	pubReq := httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/publish", nil)
	pubW := httptest.NewRecorder()
	aliceR.ServeHTTP(pubW, pubReq)
	if pubW.Code != http.StatusOK {
		t.Fatalf("publish: want 200, got %d", pubW.Code)
	}

	// Bob (non-owner viewer) can read /view.
	bobR := newTestRouter(store, bob)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+app.RID+"/view", nil)
	w := httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Bob view published App: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var view PublishedAppView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view.RID != app.RID || view.PublishedVersion != 1 || view.OwnerID != alice.ID {
		t.Fatalf("unexpected view: %+v", view)
	}
	if len(view.LayoutJSON) == 0 {
		t.Fatalf("view should carry layoutJson payload")
	}

	// Bob still cannot read the live owner-only GET.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+app.RID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Bob direct GET: want 404, got %d", w.Code)
	}
}

func TestHandler_ViewUnpublished(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	app := createTestApp(t, store, alice, "Console")

	r := newTestRouter(store, alice)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+app.RID+"/view", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("view unpublished: want 404, got %d", w.Code)
	}
	var errBody map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &errBody)
	if errBody["errorName"] != "AppNotPublished" {
		t.Fatalf("expected AppNotPublished, got %v", errBody["errorName"])
	}
}

func TestHandler_ViewMissing(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, alice)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/apps/ri.app.main.app.missing/view", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("view missing: want 404, got %d", w.Code)
	}
	var errBody map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &errBody)
	if errBody["errorName"] != "AppNotFound" {
		t.Fatalf("expected AppNotFound, got %v", errBody["errorName"])
	}
}

func TestHandler_ViewRequiresAuth(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/apps/ri.app.main.app.x/view", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth view: want 401, got %d", w.Code)
	}
}

func TestHandler_UnpublishOwnerOnly(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}
	app := createTestApp(t, store, alice, "Console")

	// Publish.
	aliceR := newTestRouter(store, alice)
	pubReq := httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/publish", nil)
	pubW := httptest.NewRecorder()
	aliceR.ServeHTTP(pubW, pubReq)
	if pubW.Code != http.StatusOK {
		t.Fatalf("publish: want 200, got %d", pubW.Code)
	}

	// Bob cannot unpublish.
	bobR := newTestRouter(store, bob)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/unpublish", nil)
	w := httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Bob unpublish: want 404, got %d", w.Code)
	}

	// Alice can; subsequent /view returns 404 AppNotPublished.
	req = httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/unpublish", nil)
	w = httptest.NewRecorder()
	aliceR.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Alice unpublish: want 204, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+app.RID+"/view", nil)
	w = httptest.NewRecorder()
	aliceR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("view after unpublish: want 404, got %d", w.Code)
	}
}

func TestHandler_PublishViewIntegration_PinsSnapshotAcrossUpdates(t *testing.T) {
	// PRD acceptance: a published App's /view stays on the snapshot
	// version even when the owner keeps editing the live row. Re-
	// publish advances the pin.
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	app := createTestApp(t, store, alice, "Console")
	aliceR := newTestRouter(store, alice)

	// Publish v1.
	pubReq := httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/publish", nil)
	pubW := httptest.NewRecorder()
	aliceR.ServeHTTP(pubW, pubReq)
	if pubW.Code != http.StatusOK {
		t.Fatalf("publish v1: want 200, got %d", pubW.Code)
	}

	// Update to v2.
	updateBody := mustEncode(t, map[string]any{"name": "Console v2"})
	updReq := httptest.NewRequest(http.MethodPut, "/api/v2/apps/"+app.RID, bytes.NewReader(updateBody))
	updW := httptest.NewRecorder()
	aliceR.ServeHTTP(updW, updReq)
	if updW.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d", updW.Code)
	}

	// /view still returns v1 / "Console".
	viewReq := httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+app.RID+"/view", nil)
	viewW := httptest.NewRecorder()
	aliceR.ServeHTTP(viewW, viewReq)
	if viewW.Code != http.StatusOK {
		t.Fatalf("view after update: want 200, got %d", viewW.Code)
	}
	var v1 PublishedAppView
	if err := json.Unmarshal(viewW.Body.Bytes(), &v1); err != nil {
		t.Fatalf("decode v1 view: %v", err)
	}
	if v1.PublishedVersion != 1 || v1.Name != "Console" {
		t.Fatalf("view should still be v1 Console, got %+v", v1)
	}

	// Re-publish; /view advances to v2 / "Console v2".
	pubReq = httptest.NewRequest(http.MethodPost, "/api/v2/apps/"+app.RID+"/publish", nil)
	pubW = httptest.NewRecorder()
	aliceR.ServeHTTP(pubW, pubReq)
	if pubW.Code != http.StatusOK {
		t.Fatalf("re-publish: want 200, got %d", pubW.Code)
	}
	viewReq = httptest.NewRequest(http.MethodGet, "/api/v2/apps/"+app.RID+"/view", nil)
	viewW = httptest.NewRecorder()
	aliceR.ServeHTTP(viewW, viewReq)
	var v2 PublishedAppView
	_ = json.Unmarshal(viewW.Body.Bytes(), &v2)
	if v2.PublishedVersion != 2 || v2.Name != "Console v2" {
		t.Fatalf("view should be v2 Console v2 after re-publish, got %+v", v2)
	}
}
