package controlpanel_test

// VTX-015 — Vertex Control Panel HTTP surface. The BDD acceptance is:
//   - Given 初始未配置 When GET /api/vertex/v1/admin/control-panel
//     Then 返回默认值
//   - Given admin 调 PUT 修改 pollingIntervalSec=10 When 普通用户 GET Then 看到 10
//   - Given 非 admin When PUT Then 403
//
// These tests assert the wire-level behaviour through the full chi router
// the way the production server mounts it.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/vertex/controlpanel"
)

// newTestRouter wires a Handler with an in-memory store and mounts its routes
// on a fresh chi router. Returns the router + the store so individual tests
// can pre-seed configuration when they need to.
func newTestRouter(t *testing.T) (chi.Router, *controlpanel.MemStore) {
	t.Helper()
	store := controlpanel.NewMemStore()
	h := controlpanel.NewHandler(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store
}

// doAsUser dispatches a request through the router with an authenticated user
// attached to the context. user == "" sends anonymously; roles is a flat
// slice flowed straight to auth.User.Roles.
func doAsUser(t *testing.T, r chi.Router, user string, roles []string, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req = req.WithContext(auth.WithUser(req.Context(), &auth.User{
			ID:    user,
			Roles: roles,
		}))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v (raw=%s)", err, w.Body.String())
	}
	return got
}

// TestControlPanel_Given_NoConfig_When_GET_Then_ReturnsDefaults covers BDD 1:
// out-of-the-box, GET yields the canonical default values.
func TestControlPanel_Given_NoConfig_When_GET_Then_ReturnsDefaults(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doAsUser(t, r, "user1", nil, http.MethodGet, "/api/vertex/v1/admin/control-panel", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["defaultWindowDays"].(float64) != 30 {
		t.Errorf("defaultWindowDays = %v, want 30", got["defaultWindowDays"])
	}
	if got["pollingIntervalSec"].(float64) != 5 {
		t.Errorf("pollingIntervalSec = %v, want 5", got["pollingIntervalSec"])
	}
	if got["searchAroundMaxNodes"].(float64) != 200 {
		t.Errorf("searchAroundMaxNodes = %v, want 200", got["searchAroundMaxNodes"])
	}
	if got["searchAroundMaxDepth"].(float64) != 3 {
		t.Errorf("searchAroundMaxDepth = %v, want 3", got["searchAroundMaxDepth"])
	}
	if got["missingDataWarningHours"].(float64) != 24 {
		t.Errorf("missingDataWarningHours = %v, want 24", got["missingDataWarningHours"])
	}
}

// TestControlPanel_Given_AdminPutPollingInterval_When_UserGet_Then_SeesNewValue
// covers BDD 2: an admin PUT lands in store; an unrelated, non-admin user GET
// sees the updated value.
func TestControlPanel_Given_AdminPutPollingInterval_When_UserGet_Then_SeesNewValue(t *testing.T) {
	r, _ := newTestRouter(t)

	w := doAsUser(t, r, "admin1", []string{"admin"}, http.MethodPut,
		"/api/vertex/v1/admin/control-panel",
		map[string]any{"pollingIntervalSec": 10})
	if w.Code != http.StatusOK {
		t.Fatalf("admin PUT status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["pollingIntervalSec"].(float64) != 10 {
		t.Errorf("PUT response pollingIntervalSec = %v, want 10", got["pollingIntervalSec"])
	}
	// Untouched fields still come back at their default values — partial PUT
	// merges with whatever was previously stored (or, here, defaults).
	if got["defaultWindowDays"].(float64) != 30 {
		t.Errorf("PUT response defaultWindowDays = %v, want 30", got["defaultWindowDays"])
	}

	// Non-admin GET observes the new value.
	w2 := doAsUser(t, r, "user2", nil, http.MethodGet,
		"/api/vertex/v1/admin/control-panel", nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("non-admin GET status = %d, want 200; body=%s", w2.Code, w2.Body.String())
	}
	got2 := decodeBody(t, w2)
	if got2["pollingIntervalSec"].(float64) != 10 {
		t.Errorf("non-admin GET pollingIntervalSec = %v, want 10", got2["pollingIntervalSec"])
	}
}

// TestControlPanel_Given_NonAdmin_When_PUT_Then_403 covers BDD 3: a regular
// user (no admin role) gets PERMISSION_DENIED 403 on PUT.
func TestControlPanel_Given_NonAdmin_When_PUT_Then_403(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doAsUser(t, r, "user1", nil, http.MethodPut,
		"/api/vertex/v1/admin/control-panel",
		map[string]any{"pollingIntervalSec": 99})
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin PUT status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["errorCode"] != "PERMISSION_DENIED" {
		t.Errorf("errorCode = %v, want PERMISSION_DENIED", got["errorCode"])
	}

	// Crucially the rejected PUT must NOT have leaked into the store: a
	// follow-up GET still returns the default value.
	w2 := doAsUser(t, r, "user1", nil, http.MethodGet,
		"/api/vertex/v1/admin/control-panel", nil)
	got2 := decodeBody(t, w2)
	if got2["pollingIntervalSec"].(float64) != 5 {
		t.Errorf("pollingIntervalSec after rejected PUT = %v, want 5 (defaults)", got2["pollingIntervalSec"])
	}
}

// TestControlPanel_Given_AnonymousPUT_When_Send_Then_403 anchors the auth
// edge case: no user in context = no role = no admin = 403. (We surface
// 403, not 401, because the rest of the vertex surface tolerates anonymous
// reads for legacy fixtures.)
func TestControlPanel_Given_AnonymousPUT_When_Send_Then_403(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doAsUser(t, r, "", nil, http.MethodPut,
		"/api/vertex/v1/admin/control-panel",
		map[string]any{"pollingIntervalSec": 99})
	if w.Code != http.StatusForbidden {
		t.Fatalf("anon PUT status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

// TestControlPanel_Given_BadJSONBody_When_AdminPUT_Then_400 covers basic
// input handling: malformed JSON surfaces as BAD_REQUEST.
func TestControlPanel_Given_BadJSONBody_When_AdminPUT_Then_400(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodPut,
		"/api/vertex/v1/admin/control-panel",
		bytes.NewBufferString(`{not valid json`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{
		ID: "admin1", Roles: []string{"admin"},
	}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT bad json status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestControlPanel_Given_NegativeValue_When_AdminPUT_Then_400 ensures the
// store-level validation surfaces to clients as INVALID_ARGUMENT (400) — a
// bad value from an authorised caller is still a bad request.
func TestControlPanel_Given_NegativeValue_When_AdminPUT_Then_400(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doAsUser(t, r, "admin1", []string{"admin"}, http.MethodPut,
		"/api/vertex/v1/admin/control-panel",
		map[string]any{"pollingIntervalSec": -1})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative PUT status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["errorCode"] != "INVALID_ARGUMENT" {
		t.Errorf("errorCode = %v, want INVALID_ARGUMENT", got["errorCode"])
	}
}

// TestControlPanel_Given_AdminPutAllFields_When_UserGet_Then_AllValuesPersist
// covers a full PUT (every field overwritten) — the store should reflect every
// updated value.
func TestControlPanel_Given_AdminPutAllFields_When_UserGet_Then_AllValuesPersist(t *testing.T) {
	r, _ := newTestRouter(t)
	body := map[string]any{
		"defaultWindowDays":       60,
		"pollingIntervalSec":      15,
		"searchAroundMaxNodes":    500,
		"searchAroundMaxDepth":    7,
		"missingDataWarningHours": 48,
	}
	w := doAsUser(t, r, "admin1", []string{"admin"}, http.MethodPut,
		"/api/vertex/v1/admin/control-panel", body)
	if w.Code != http.StatusOK {
		t.Fatalf("full PUT status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	w2 := doAsUser(t, r, "user2", nil, http.MethodGet,
		"/api/vertex/v1/admin/control-panel", nil)
	got := decodeBody(t, w2)
	for k, want := range body {
		if got[k].(float64) != float64(want.(int)) {
			t.Errorf("%s = %v, want %d", k, got[k], want)
		}
	}
}
