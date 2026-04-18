package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

// fakeMarkingRepo implements MarkingRepository + MarkingGrantAdminRepository
// in memory so the admin handler tests exercise every wired path without a
// PostgreSQL dependency.
type fakeMarkingRepo struct {
	markings []Marking
	grants   map[string]map[string]MarkingGrant // userID -> markingName -> grant
	grantErr error
}

func newFakeMarkingRepo() *fakeMarkingRepo {
	return &fakeMarkingRepo{
		markings: []Marking{
			{Name: "PUBLIC", DisplayName: "Public", Description: "", Color: "#10b981"},
			{Name: "INTERNAL", DisplayName: "Internal", Description: "", Color: "#3b82f6"},
			{Name: "PII", DisplayName: "PII", Description: "", Color: "#ef4444"},
			{Name: "SECRET", DisplayName: "Secret", Description: "", Color: "#dc2626"},
		},
		grants: map[string]map[string]MarkingGrant{},
	}
}

func (f *fakeMarkingRepo) ListMarkings(_ context.Context) ([]Marking, error) {
	out := make([]Marking, len(f.markings))
	copy(out, f.markings)
	return out, nil
}

func (f *fakeMarkingRepo) GetUserMarkings(_ context.Context, userID string) ([]string, error) {
	user := f.grants[userID]
	out := make([]string, 0, len(user))
	now := time.Now()
	for name, g := range user {
		if g.IsExpired(now) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func (f *fakeMarkingRepo) GrantMarking(_ context.Context, userID, markingName, grantedBy string, expiresAt *time.Time) error {
	if f.grantErr != nil {
		return f.grantErr
	}
	if f.grants[userID] == nil {
		f.grants[userID] = map[string]MarkingGrant{}
	}
	f.grants[userID][markingName] = MarkingGrant{
		UserID:      userID,
		MarkingName: markingName,
		GrantedAt:   time.Now().UTC(),
		GrantedBy:   grantedBy,
		ExpiresAt:   expiresAt,
	}
	return nil
}

func (f *fakeMarkingRepo) RevokeMarking(_ context.Context, userID, markingName string) error {
	delete(f.grants[userID], markingName)
	return nil
}

func (f *fakeMarkingRepo) ListGrantsByMarking(_ context.Context, markingName string) ([]MarkingGrant, error) {
	out := make([]MarkingGrant, 0)
	now := time.Now()
	for _, user := range f.grants {
		if g, ok := user[markingName]; ok {
			if g.IsExpired(now) {
				continue
			}
			out = append(out, g)
		}
	}
	return out, nil
}

func (f *fakeMarkingRepo) ListGrantsByUser(_ context.Context, userID string) ([]MarkingGrant, error) {
	user := f.grants[userID]
	out := make([]MarkingGrant, 0, len(user))
	now := time.Now()
	for _, g := range user {
		if g.IsExpired(now) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

// fakeAuditStore is a minimal in-memory audit.Store for verifying the
// grant/revoke handlers emit the expected audit events.
type fakeMarkingAuditStore struct {
	events []audit.AuditEvent
}

func (s *fakeMarkingAuditStore) Insert(_ context.Context, e audit.AuditEvent) error {
	s.events = append(s.events, e)
	return nil
}

func (s *fakeMarkingAuditStore) List(_ context.Context, _ audit.ListFilter) ([]audit.AuditEvent, error) {
	return s.events, nil
}

func newMarkingHandlerHarness(t *testing.T) (*MarkingHandler, *fakeMarkingRepo, *fakeUserRepo, *fakeMarkingAuditStore) {
	t.Helper()
	markingRepo := newFakeMarkingRepo()
	users := newFakeUserRepo()
	users.users["user:alice@example.com"] = &UserRecord{ID: "user:alice@example.com", Email: "alice@example.com"}
	auditStore := &fakeMarkingAuditStore{}
	h := NewMarkingHandler(markingRepo, markingRepo, users, auditStore)
	return h, markingRepo, users, auditStore
}

func TestMarkingHandler_ListMarkings_200(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/markings", nil))
	rec := httptest.NewRecorder()
	h.ListMarkings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp MarkingListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Markings) != 4 {
		t.Errorf("expected 4 markings, got %d", len(resp.Markings))
	}
}

func TestMarkingHandler_ListMarkings_Unauth(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/markings", nil)
	rec := httptest.NewRecorder()
	h.ListMarkings(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMarkingHandler_GrantMarking_200_WritesAudit(t *testing.T) {
	h, repo, _, auditStore := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": "PII"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := repo.grants["user:alice@example.com"]["PII"]; !ok {
		t.Errorf("expected PII grant to be persisted")
	}
	if len(auditStore.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(auditStore.events))
	}
	ev := auditStore.events[0]
	if ev.Action != "marking_grant" {
		t.Errorf("expected action=marking_grant, got %q", ev.Action)
	}
	if ev.ResourceRID != "user:alice@example.com" {
		t.Errorf("expected ResourceRID to be the target user, got %q", ev.ResourceRID)
	}
	if ev.ActorID != "user:admin@example.com" {
		t.Errorf("expected ActorID to be the admin, got %q", ev.ActorID)
	}
}

func TestMarkingHandler_GrantMarking_UnknownMarking_404(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": "GHOST"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMarkingHandler_GrantMarking_UnknownUser_404(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": "PII"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:ghost/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:ghost")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMarkingHandler_GrantMarking_MissingMarking_400(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": ""})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestMarkingHandler_GrantMarking_Unauth(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": "PII"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMarkingHandler_RevokeMarking_204_WritesAudit(t *testing.T) {
	h, repo, _, auditStore := newMarkingHandlerHarness(t)
	_ = repo.GrantMarking(context.Background(), "user:alice@example.com", "PII", "user:admin", nil)

	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/users/user:alice@example.com/markings/PII", nil))
	rec := httptest.NewRecorder()
	h.revokeMarkingFor(rec, req, "user:alice@example.com", "PII")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := repo.grants["user:alice@example.com"]["PII"]; ok {
		t.Errorf("expected PII grant to be removed")
	}
	if len(auditStore.events) != 1 || auditStore.events[0].Action != "marking_revoke" {
		t.Errorf("expected 1 marking_revoke audit event, got %+v", auditStore.events)
	}
}

func TestMarkingHandler_RevokeMarking_Idempotent(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	// No prior grant — revoke should still 204.
	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/users/user:alice@example.com/markings/PII", nil))
	rec := httptest.NewRecorder()
	h.revokeMarkingFor(rec, req, "user:alice@example.com", "PII")

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 (idempotent), got %d", rec.Code)
	}
}

func TestMarkingHandler_ListGrantsByMarking_200(t *testing.T) {
	h, repo, _, _ := newMarkingHandlerHarness(t)
	_ = repo.GrantMarking(context.Background(), "user:alice@example.com", "PII", "user:admin@example.com", nil)
	_ = repo.GrantMarking(context.Background(), "user:bob@example.com", "PII", "user:admin@example.com", nil)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/markings/PII/grants", nil))
	rec := httptest.NewRecorder()
	h.listGrantsByMarkingFor(rec, req, "PII")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp MarkingGrantsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Grants) != 2 {
		t.Errorf("expected 2 grants, got %d", len(resp.Grants))
	}
}

func TestMarkingHandler_ListGrantsByMarking_NoAdmin_500(t *testing.T) {
	// When the admin interface is not wired, we should emit a structured
	// 500 instead of panicking.
	markingRepo := newFakeMarkingRepo()
	h := NewMarkingHandler(markingRepo, nil, nil, nil)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/markings/PII/grants", nil))
	rec := httptest.NewRecorder()
	h.listGrantsByMarkingFor(rec, req, "PII")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMarkingHandler_ListGrantsByUser_200(t *testing.T) {
	h, repo, _, _ := newMarkingHandlerHarness(t)
	_ = repo.GrantMarking(context.Background(), "user:alice@example.com", "PII", "user:admin@example.com", nil)
	_ = repo.GrantMarking(context.Background(), "user:alice@example.com", "SECRET", "user:admin@example.com", nil)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/users/user:alice@example.com/markings", nil))
	rec := httptest.NewRecorder()
	h.listGrantsByUserFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp MarkingGrantsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Grants) != 2 {
		t.Errorf("expected 2 grants, got %d", len(resp.Grants))
	}
	for _, g := range resp.Grants {
		if g.GrantedBy != "user:admin@example.com" {
			t.Errorf("expected grantedBy=user:admin@example.com, got %q", g.GrantedBy)
		}
		if g.GrantedAt == "" {
			t.Errorf("expected grantedAt to be populated")
		}
	}
}

func TestMarkingHandler_GrantMarking_ExpiresInDays_PersistsExpiry(t *testing.T) {
	h, repo, _, auditStore := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": "PII", "expiresInDays": 30})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	grant, ok := repo.grants["user:alice@example.com"]["PII"]
	if !ok {
		t.Fatalf("expected PII grant to be persisted")
	}
	if grant.ExpiresAt == nil {
		t.Fatalf("expected ExpiresAt to be populated")
	}
	// Expires ≈ now + 30d; allow a generous window for scheduling jitter.
	delta := time.Until(*grant.ExpiresAt)
	if delta < 29*24*time.Hour || delta > 31*24*time.Hour {
		t.Errorf("expected ~30 days in the future, got %s", delta)
	}
	if len(auditStore.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(auditStore.events))
	}
	if !bytes.Contains(auditStore.events[0].DiffJSON, []byte("expiresAt")) {
		t.Errorf("expected audit diff to record expiresAt, got %s", auditStore.events[0].DiffJSON)
	}
}

func TestMarkingHandler_GrantMarking_ExpiresAt_ParsesRFC3339(t *testing.T) {
	h, repo, _, _ := newMarkingHandlerHarness(t)

	expected := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	body, _ := json.Marshal(map[string]any{
		"marking":   "PII",
		"expiresAt": expected.Format(time.RFC3339),
	})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	grant := repo.grants["user:alice@example.com"]["PII"]
	if grant.ExpiresAt == nil {
		t.Fatalf("expected ExpiresAt to be populated")
	}
	if !grant.ExpiresAt.Equal(expected) {
		t.Errorf("expected ExpiresAt=%s, got %s", expected, grant.ExpiresAt)
	}
}

func TestMarkingHandler_GrantMarking_InvalidExpiresAt_400(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": "PII", "expiresAt": "not-a-date"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestMarkingHandler_GrantMarking_NegativeExpiresInDays_400(t *testing.T) {
	h, _, _, _ := newMarkingHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"marking": "PII", "expiresInDays": -5})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestMarkingHandler_ListGrantsByUser_IncludesExpiresAt(t *testing.T) {
	h, repo, _, _ := newMarkingHandlerHarness(t)
	future := time.Now().Add(48 * time.Hour)
	_ = repo.GrantMarking(context.Background(), "user:alice@example.com", "PII", "user:admin@example.com", &future)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/users/user:alice@example.com/markings", nil))
	rec := httptest.NewRecorder()
	h.listGrantsByUserFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp MarkingGrantsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Grants) != 1 || resp.Grants[0].ExpiresAt == "" {
		t.Errorf("expected ExpiresAt populated on the wire, got %+v", resp.Grants)
	}
}

func TestMarkingHandler_GetUserMarkings_FiltersExpired(t *testing.T) {
	// Stub fake repo's expiry filter directly — expired grants must be
	// invisible both to GetUserMarkings (MarkingFilter hot path) and to
	// the admin ListGrantsByUser surface.
	h, repo, _, _ := newMarkingHandlerHarness(t)
	past := time.Now().Add(-1 * time.Hour)
	_ = repo.GrantMarking(context.Background(), "user:alice@example.com", "PII", "user:admin@example.com", &past)

	names, err := repo.GetUserMarkings(context.Background(), "user:alice@example.com")
	if err != nil {
		t.Fatalf("GetUserMarkings: %v", err)
	}
	for _, n := range names {
		if n == "PII" {
			t.Errorf("expired PII must not appear in GetUserMarkings, got %v", names)
		}
	}

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/users/user:alice@example.com/markings", nil))
	rec := httptest.NewRecorder()
	h.listGrantsByUserFor(rec, req, "user:alice@example.com")

	var resp MarkingGrantsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Grants) != 0 {
		t.Errorf("expected expired grant filtered, got %+v", resp.Grants)
	}
}

func TestMarkingHandler_GrantMarking_RepoError_500(t *testing.T) {
	h, repo, _, _ := newMarkingHandlerHarness(t)
	repo.grantErr = errors.New("boom")

	body, _ := json.Marshal(map[string]any{"marking": "PII"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/markings", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantMarkingFor(rec, req, "user:alice@example.com")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}
