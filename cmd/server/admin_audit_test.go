package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
)

// seedAuditEvents inserts a known set of events into the store.
func seedAuditEvents(t *testing.T, store audit.Store) {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []audit.AuditEvent{
		{ID: "e1", ActorID: "user-1", Action: "CREATE", ResourceType: "ObjectType", ResourceRID: "ri.1", Timestamp: base},
		{ID: "e2", ActorID: "user-2", Action: "UPDATE", ResourceType: "Property", ResourceRID: "ri.2", Timestamp: base.Add(1 * time.Minute)},
		{ID: "e3", ActorID: "user-1", Action: "DELETE", ResourceType: "ObjectType", ResourceRID: "ri.3", Timestamp: base.Add(2 * time.Minute)},
		{ID: "e4", ActorID: "user-2", Action: "login_success", ResourceType: "Session", ResourceRID: "user-2", Timestamp: base.Add(3 * time.Minute)},
		{ID: "e5", ActorID: "user-1", Action: "CREATE", ResourceType: "LinkType", ResourceRID: "ri.5", Timestamp: base.Add(4 * time.Minute)},
	}
	for _, e := range events {
		if err := store.Insert(ctx, e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// auditRouter builds a minimal chi router with the audit events endpoint
// protected by RequirePermission(PermUserManage), similar to production wiring.
func auditRouter(store audit.Store) http.Handler {
	r := chi.NewRouter()
	r.With(auth.RequirePermission(auth.PermUserManage)).
		Get("/api/v2/admin/auditEvents", NewAdminAuditEventsHandler(store).ServeHTTP)
	return r
}

// withAdminUser injects an admin auth.User into the request context.
func withAdminUser(r *http.Request) *http.Request {
	u := &auth.User{
		ID:    "admin-1",
		Email: "admin@test.com",
		Roles: []string{auth.RoleAdmin},
	}
	return r.WithContext(auth.WithUser(r.Context(), u))
}

// withViewerUser injects a viewer-only auth.User into the request context.
func withViewerUser(r *http.Request) *http.Request {
	u := &auth.User{
		ID:    "viewer-1",
		Email: "viewer@test.com",
		Roles: []string{auth.RoleViewer},
	}
	return r.WithContext(auth.WithUser(r.Context(), u))
}

func TestAuditEventsEndpoint_ListAll(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}

	var resp struct {
		Data          []audit.AuditEvent `json:"data"`
		NextPageToken string             `json:"nextPageToken"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 5 {
		t.Errorf("got %d events, want 5", len(resp.Data))
	}
	// Events should be ordered by ts DESC (newest first).
	if len(resp.Data) > 0 && resp.Data[0].ID != "e5" {
		t.Errorf("first event ID = %s, want e5 (newest)", resp.Data[0].ID)
	}
	if resp.NextPageToken != "" {
		t.Errorf("nextPageToken should be empty when all events fit, got %q", resp.NextPageToken)
	}
}

func TestAuditEventsEndpoint_FilterByActor(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?actor=user-1", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d", rw.Code)
	}
	var resp struct {
		Data []audit.AuditEvent `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Errorf("got %d events, want 3 (user-1 only)", len(resp.Data))
	}
}

func TestAuditEventsEndpoint_FilterByAction(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?action=CREATE", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d", rw.Code)
	}
	var resp struct {
		Data []audit.AuditEvent `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Errorf("got %d events, want 2 (CREATE only)", len(resp.Data))
	}
}

func TestAuditEventsEndpoint_FilterByResourceType(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?resource_type=ObjectType", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d", rw.Code)
	}
	var resp struct {
		Data []audit.AuditEvent `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Errorf("got %d events, want 2 (ObjectType only)", len(resp.Data))
	}
}

func TestAuditEventsEndpoint_FilterByTimeRange(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	// since=2026-01-01T00:01:00Z should exclude e1 (00:00)
	// until=2026-01-01T00:03:00Z should exclude e5 (00:04)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/admin/auditEvents?since=2026-01-01T00:01:00Z&until=2026-01-01T00:03:00Z", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d", rw.Code)
	}
	var resp struct {
		Data []audit.AuditEvent `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Errorf("got %d events, want 3 (01:00 through 03:00)", len(resp.Data))
	}
}

func TestAuditEventsEndpoint_Pagination(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	// Page 1: pageSize=2
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?pageSize=2", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("page1 status = %d", rw.Code)
	}
	var page1 struct {
		Data          []audit.AuditEvent `json:"data"`
		NextPageToken string             `json:"nextPageToken"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1.Data) != 2 {
		t.Fatalf("page1 count = %d, want 2", len(page1.Data))
	}
	if page1.NextPageToken == "" {
		t.Fatal("page1 nextPageToken should not be empty")
	}

	// Page 2: use pageToken from page 1
	req2 := httptest.NewRequest(http.MethodGet,
		"/api/v2/admin/auditEvents?pageSize=2&pageToken="+page1.NextPageToken, nil)
	req2 = withAdminUser(req2)
	rw2 := httptest.NewRecorder()
	h.ServeHTTP(rw2, req2)

	if rw2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d", rw2.Code)
	}
	var page2 struct {
		Data          []audit.AuditEvent `json:"data"`
		NextPageToken string             `json:"nextPageToken"`
	}
	if err := json.Unmarshal(rw2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2.Data) != 2 {
		t.Fatalf("page2 count = %d, want 2", len(page2.Data))
	}

	// Page 3: last page with 1 event
	req3 := httptest.NewRequest(http.MethodGet,
		"/api/v2/admin/auditEvents?pageSize=2&pageToken="+page2.NextPageToken, nil)
	req3 = withAdminUser(req3)
	rw3 := httptest.NewRecorder()
	h.ServeHTTP(rw3, req3)

	if rw3.Code != http.StatusOK {
		t.Fatalf("page3 status = %d", rw3.Code)
	}
	var page3 struct {
		Data          []audit.AuditEvent `json:"data"`
		NextPageToken string             `json:"nextPageToken"`
	}
	if err := json.Unmarshal(rw3.Body.Bytes(), &page3); err != nil {
		t.Fatalf("decode page3: %v", err)
	}
	if len(page3.Data) != 1 {
		t.Errorf("page3 count = %d, want 1", len(page3.Data))
	}
	if page3.NextPageToken != "" {
		t.Errorf("page3 nextPageToken should be empty, got %q", page3.NextPageToken)
	}

	// Verify no duplicates across pages.
	seen := map[string]bool{}
	for _, e := range page1.Data {
		seen[e.ID] = true
	}
	for _, e := range page2.Data {
		if seen[e.ID] {
			t.Errorf("duplicate event %s across pages", e.ID)
		}
		seen[e.ID] = true
	}
	for _, e := range page3.Data {
		if seen[e.ID] {
			t.Errorf("duplicate event %s across pages", e.ID)
		}
		seen[e.ID] = true
	}
	if len(seen) != 5 {
		t.Errorf("total unique events = %d, want 5", len(seen))
	}
}

func TestAuditEventsEndpoint_NonAdminDenied(t *testing.T) {
	store := audit.NewMemoryStore()
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents", nil)
	req = withViewerUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d, want 403", rw.Code)
	}
}

func TestAuditEventsEndpoint_Unauthenticated(t *testing.T) {
	store := audit.NewMemoryStore()
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents", nil)
	// No user injected
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusUnauthorized {
		t.Errorf("unauth status = %d, want 401", rw.Code)
	}
}

func TestAuditEventsEndpoint_InvalidPageToken(t *testing.T) {
	store := audit.NewMemoryStore()
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?pageToken=not-valid-base64!", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("invalid token status = %d, want 400", rw.Code)
	}
}

func TestAuditEventsEndpoint_InvalidTimestamp(t *testing.T) {
	store := audit.NewMemoryStore()
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?since=not-a-time", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("invalid since status = %d, want 400", rw.Code)
	}
}

func TestAuditEventsEndpoint_StoreNil(t *testing.T) {
	h := auditRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusServiceUnavailable {
		t.Errorf("nil store status = %d, want 503", rw.Code)
	}
}
