package compliance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
)

func newTestGen() *Generator {
	g := New()
	g.Audit = &stubAudit{
		events: []audit.AuditEvent{
			{ActorID: "user:a", Action: "login"},
			{ActorID: "user:b", Action: "read"},
		},
	}
	g.Markings = &stubMarkings{
		markings: []MarkingInfo{{Name: "PUBLIC"}},
		counts:   map[string]int{"PUBLIC": 5},
	}
	g.ObjectTypes = &stubObjectTypes{count: 2}
	g.Policies = &stubPolicies{rowTotal: 1, rowOTs: []string{"ri.oms.main.ot.1"}}
	return g
}

func asAdmin(req *http.Request) *http.Request {
	ctx := auth.WithUser(req.Context(), &auth.User{
		ID:    "user:admin@example.com",
		Email: "admin@example.com",
		Roles: []string{"admin"},
	})
	return req.WithContext(ctx)
}

func TestHandler_GenerateReport_JSON(t *testing.T) {
	h := NewHandler(newTestGen(), audit.NewMemoryStore())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: want JSON, got %q", ct)
	}
	var got Report
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Access.Total != 2 {
		t.Errorf("Access.Total: want 2, got %d", got.Access.Total)
	}
	if got.Markings.Total != 1 {
		t.Errorf("Markings.Total: want 1, got %d", got.Markings.Total)
	}
	if got.Policies.ObjectTypesTotal != 2 {
		t.Errorf("ObjectTypesTotal: want 2, got %d", got.Policies.ObjectTypesTotal)
	}
}

func TestHandler_GenerateReport_HTML(t *testing.T) {
	h := NewHandler(newTestGen(), nil)
	body := `{"format":"html"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type: want text/html, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "Weave Compliance Report") {
		t.Errorf("HTML body missing report title, got: %s", rec.Body.String())
	}
}

func TestHandler_GenerateReport_EmptyBody(t *testing.T) {
	h := NewHandler(newTestGen(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", nil)
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_GenerateReport_Unauthenticated(t *testing.T) {
	h := NewHandler(newTestGen(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", rec.Code)
	}
}

func TestHandler_GenerateReport_UnconfiguredGenerator(t *testing.T) {
	h := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", strings.NewReader("{}"))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ComplianceReportUnavailable") {
		t.Errorf("expected ComplianceReportUnavailable, got %s", rec.Body.String())
	}
}

func TestHandler_GenerateReport_InvalidWindow(t *testing.T) {
	h := NewHandler(newTestGen(), nil)
	body := `{"from":"2026-04-19T00:00:00Z","to":"2026-04-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", strings.NewReader(body))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_GenerateReport_InvalidFormat(t *testing.T) {
	h := NewHandler(newTestGen(), nil)
	body := `{"format":"xml"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", strings.NewReader(body))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", rec.Code)
	}
}

func TestHandler_GenerateReport_WritesAuditRow(t *testing.T) {
	store := audit.NewMemoryStore()
	h := NewHandler(newTestGen(), store)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report", strings.NewReader("{}"))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	evts, err := store.List(context.Background(), audit.ListFilter{})
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("want 1 audit event, got %d", len(evts))
	}
	if evts[0].Action != "compliance_report_generated" {
		t.Errorf("Action: want compliance_report_generated, got %q", evts[0].Action)
	}
	if evts[0].ActorID != "user:admin@example.com" {
		t.Errorf("ActorID: want user:admin@example.com, got %q", evts[0].ActorID)
	}
}

func TestHandler_GenerateReport_FormatFromQueryString(t *testing.T) {
	h := NewHandler(newTestGen(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compliance/report?format=html", nil)
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	h.GenerateReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type: want text/html, got %q", ct)
	}
}
