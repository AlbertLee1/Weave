package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/audit"
)

// TestUS493_AuditEndpoint_FilterByResourceRid_CamelCase asserts the PRD
// literal query parameter `resourceRid` filters audit events to a single
// resource. Acceptance: "GET /api/admin/audit 支持按 actor / resourceRid /
// 时间范围筛选". The handler must accept the camelCase form documented in
// the PRD; we also accept the snake_case `resource_rid` form alongside it
// for consistency with existing query params (resource_type, page_size...
// the rest use snake_case + camelCase mix).
func TestUS493_AuditEndpoint_FilterByResourceRid_CamelCase(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	// Seed has e1 (RID=ri.1) and e3 (RID=ri.3) under ObjectType, plus
	// other RIDs. Filtering by resourceRid=ri.1 must return exactly one
	// event.
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?resourceRid=ri.1", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Data []audit.AuditEvent `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("got %d events for resourceRid=ri.1, want 1; events=%+v", len(resp.Data), resp.Data)
	}
	if resp.Data[0].ID != "e1" {
		t.Errorf("got event ID = %q, want e1", resp.Data[0].ID)
	}
}

// TestUS493_AuditEndpoint_FilterByResourceRid_SnakeCaseAlias verifies the
// snake_case `resource_rid` form is accepted equivalently to camelCase
// (mirrors the existing `resource_type` snake_case parameter naming).
func TestUS493_AuditEndpoint_FilterByResourceRid_SnakeCaseAlias(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/auditEvents?resource_rid=ri.3", nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Data []audit.AuditEvent `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("got %d events for resource_rid=ri.3, want 1", len(resp.Data))
	}
	if resp.Data[0].ID != "e3" {
		t.Errorf("got event ID = %q, want e3", resp.Data[0].ID)
	}
}

// TestUS493_AuditEndpoint_ComposesFilters verifies the three PRD-literal
// filter dimensions (actor / resourceRid / time-range) compose correctly:
// a request that combines all three returns only the events satisfying
// every clause.
func TestUS493_AuditEndpoint_ComposesFilters(t *testing.T) {
	store := audit.NewMemoryStore()
	seedAuditEvents(t, store)
	h := auditRouter(store)

	// Seed e1: user-1, ri.1, ts=00:00; e3: user-1, ri.3, ts=00:02; e5:
	// user-1, ri.5, ts=00:04. Asking for actor=user-1 +
	// since=00:00..until=00:10 returns all three user-1 events; adding
	// resourceRid=ri.3 must narrow it to exactly e3 — proving the
	// resourceRid clause is composed (not silently ignored when other
	// clauses already narrow the set).
	url := "/api/v2/admin/auditEvents?actor=user-1&resourceRid=ri.3" +
		"&since=2026-01-01T00:00:00Z&until=2026-01-01T00:10:00Z"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = withAdminUser(req)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Data []audit.AuditEvent `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "e3" {
		t.Fatalf("got events=%+v, want exactly [e3]", resp.Data)
	}
}
