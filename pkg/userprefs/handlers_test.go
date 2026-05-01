package userprefs

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

func newTestRouter(t *testing.T, store Store) (*chi.Mux, *auth.User) {
	t.Helper()
	r := chi.NewRouter()
	user := &auth.User{ID: "user-alice", Email: "alice@example.com"}
	// Inject the user into the request context — defence-in-depth so
	// the handler's requireAuth check exercises the real path.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req = req.WithContext(auth.WithUser(req.Context(), user))
			next.ServeHTTP(w, req)
		})
	})
	NewHandler(store).RegisterRoutes(r)
	return r, user
}

func TestHandler_Get_ReturnsVirtualDefaultWhenAbsent(t *testing.T) {
	r, user := newTestRouter(t, NewMemoryStore())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/user-preferences", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var got Preferences
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("UserID: want %q, got %q", user.ID, got.UserID)
	}
	if got.Theme != "" || got.Language != "" {
		t.Errorf("virtual default should have empty theme/language: %+v", got)
	}
	if string(got.Notifications) != "{}" || string(got.Hotkeys) != "{}" {
		t.Errorf("virtual default should have empty envelopes: notif=%s hk=%s",
			got.Notifications, got.Hotkeys)
	}
}

func TestHandler_PutThenGet_PersistsAndReturns(t *testing.T) {
	store := NewMemoryStore()
	r, _ := newTestRouter(t, store)
	body := `{"theme":"dark","language":"en","notifications":{"mentions":true}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v2/user-preferences", strings.NewReader(body))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var put Preferences
	if err := json.Unmarshal(rec.Body.Bytes(), &put); err != nil {
		t.Fatalf("decode put: %v", err)
	}
	if put.Theme != "dark" || put.Language != "en" {
		t.Errorf("PUT response lost values: %+v", put)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v2/user-preferences", nil)
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET status: want 200, got %d", rec2.Code)
	}
	var got Preferences
	if err := json.Unmarshal(rec2.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme not persisted: got %q", got.Theme)
	}
	if !bytes.Equal(got.Notifications, []byte(`{"mentions":true}`)) {
		t.Errorf("Notifications round-trip lost: %s", got.Notifications)
	}
}

func TestHandler_Put_RejectsInvalidTheme(t *testing.T) {
	r, _ := newTestRouter(t, NewMemoryStore())
	body := `{"theme":"chartreuse"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v2/user-preferences", strings.NewReader(body))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "InvalidUserPreferenceTheme") {
		t.Errorf("expected InvalidUserPreferenceTheme code in body: %s", rec.Body.String())
	}
}

func TestHandler_Put_RejectsOverlongLanguage(t *testing.T) {
	r, _ := newTestRouter(t, NewMemoryStore())
	long := strings.Repeat("a", MaxLanguageLength+1)
	body := `{"language":"` + long + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v2/user-preferences", strings.NewReader(body))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "InvalidUserPreferenceLanguage") {
		t.Errorf("expected InvalidUserPreferenceLanguage code in body: %s", rec.Body.String())
	}
}

func TestHandler_Put_PartialPreservesUnchangedFields(t *testing.T) {
	store := NewMemoryStore()
	// seed with full prefs
	theme := "dark"
	lang := "en"
	if _, err := store.Upsert(context.Background(), "user-alice", Update{
		Theme: &theme, Language: &lang,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	r, _ := newTestRouter(t, store)
	body := `{"language":"zh-CN"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v2/user-preferences", strings.NewReader(body))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var got Preferences
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme should be preserved when not in body: %q", got.Theme)
	}
	if got.Language != "zh-CN" {
		t.Errorf("Language not updated: %q", got.Language)
	}
}

func TestHandler_RequiresAuth(t *testing.T) {
	r := chi.NewRouter()
	NewHandler(NewMemoryStore()).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/user-preferences", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET should 401, got %d", rec.Code)
	}
}

func TestHandler_DegradedModeReturns500WhenStoreNil(t *testing.T) {
	r := chi.NewRouter()
	user := &auth.User{ID: "alice"}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req = req.WithContext(auth.WithUser(req.Context(), user))
			next.ServeHTTP(w, req)
		})
	})
	NewHandler(nil).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/user-preferences", nil)
	r.ServeHTTP(rec, req)
	if rec.Code < 500 {
		t.Errorf("nil store should produce 5xx, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "UserPreferencesUnavailable") {
		t.Errorf("expected UserPreferencesUnavailable: %s", rec.Body.String())
	}
}
