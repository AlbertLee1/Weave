package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// fakeServiceAccountRepo is an in-memory ServiceAccountRepository used by
// the handler-level unit tests. It mirrors the behaviour of the PG
// implementation (unique-name invariant scoped to active rows; soft delete
// via DisabledAt) without requiring a live PostgreSQL instance.
type fakeServiceAccountRepo struct {
	mu   sync.Mutex
	byID map[string]*ServiceAccount
	seq  int
}

func newFakeServiceAccountRepo() *fakeServiceAccountRepo {
	return &fakeServiceAccountRepo{byID: map[string]*ServiceAccount{}}
}

func (f *fakeServiceAccountRepo) Create(_ context.Context, sa *ServiceAccount) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.Name == sa.Name && !existing.IsDisabled() {
			return ErrServiceAccountNameConflict
		}
	}
	f.seq++
	sa.ID = uuidLike(f.seq)
	now := time.Now()
	sa.CreatedAt = now
	sa.UpdatedAt = now
	if sa.Scopes == nil {
		sa.Scopes = []string{}
	}
	cp := *sa
	f.byID[sa.ID] = &cp
	return nil
}

func (f *fakeServiceAccountRepo) GetByID(_ context.Context, id string) (*ServiceAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sa, ok := f.byID[id]
	if !ok {
		return nil, ErrServiceAccountNotFound
	}
	cp := *sa
	return &cp, nil
}

func (f *fakeServiceAccountRepo) GetByName(_ context.Context, name string) (*ServiceAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sa := range f.byID {
		if sa.Name == name && !sa.IsDisabled() {
			cp := *sa
			return &cp, nil
		}
	}
	return nil, ErrServiceAccountNotFound
}

func (f *fakeServiceAccountRepo) ListActive(_ context.Context) ([]*ServiceAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*ServiceAccount, 0)
	for _, sa := range f.byID {
		if !sa.IsDisabled() {
			cp := *sa
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeServiceAccountRepo) Update(_ context.Context, id string, upd ServiceAccountUpdate) (*ServiceAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sa, ok := f.byID[id]
	if !ok || sa.IsDisabled() {
		return nil, ErrServiceAccountNotFound
	}
	if upd.Description != nil {
		sa.Description = *upd.Description
	}
	if upd.Scopes != nil {
		scopes := *upd.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		sa.Scopes = scopes
	}
	if upd.ExpiresAt != nil {
		sa.ExpiresAt = *upd.ExpiresAt
	}
	sa.UpdatedAt = time.Now()
	cp := *sa
	return &cp, nil
}

func (f *fakeServiceAccountRepo) Disable(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sa, ok := f.byID[id]
	if !ok {
		return nil
	}
	if sa.IsDisabled() {
		return nil
	}
	now := time.Now()
	sa.DisabledAt = &now
	sa.UpdatedAt = now
	return nil
}

// uuidLike returns a deterministic UUID-shaped string for fake IDs so the
// handler tests can assert exact values when needed.
func uuidLike(seq int) string {
	return strings.Repeat("0", 8) + "-0000-0000-0000-" +
		strings.Repeat("0", 12-lenDigits(seq)) + itoaSeq(seq)
}

func lenDigits(n int) int {
	if n == 0 {
		return 1
	}
	c := 0
	for n > 0 {
		c++
		n /= 10
	}
	return c
}

func itoaSeq(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	p := len(buf)
	for n > 0 {
		p--
		buf[p] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[p:])
}

var _ ServiceAccountRepository = (*fakeServiceAccountRepo)(nil)

// newServiceAccountHandlerHarness builds a fresh handler with an in-memory repo.
func newServiceAccountHandlerHarness(_ *testing.T) (*ServiceAccountHandler, *fakeServiceAccountRepo) {
	repo := newFakeServiceAccountRepo()
	h := NewServiceAccountHandler(repo, nil)
	return h, repo
}

func TestServiceAccountHandler_Create_201(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{
		"name":        "ci-bot",
		"description": "GitHub Actions",
		"scopes":      []string{"read:objects"},
	})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp ServiceAccountResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected id")
	}
	if resp.Name != "ci-bot" {
		t.Errorf("Name: %q", resp.Name)
	}
	if resp.Description != "GitHub Actions" {
		t.Errorf("Description: %q", resp.Description)
	}
	if resp.OwnerUserID != "user:admin@example.com" {
		t.Errorf("Owner: %q", resp.OwnerUserID)
	}
	if len(resp.Scopes) != 1 || resp.Scopes[0] != "read:objects" {
		t.Errorf("Scopes: %v", resp.Scopes)
	}
	if resp.DisabledAt != nil {
		t.Error("new SA should not be disabled")
	}

	// Verify it landed in the repo.
	row, err := repo.GetByID(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row.OwnerUserID != "user:admin@example.com" {
		t.Errorf("persisted owner: %q", row.OwnerUserID)
	}
}

func TestServiceAccountHandler_Create_RequiresAuth(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	body, _ := json.Marshal(map[string]any{"name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestServiceAccountHandler_Create_RejectsEmptyName(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	body, _ := json.Marshal(map[string]any{"name": ""})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestServiceAccountHandler_Create_RejectsInvalidName(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	body, _ := json.Marshal(map[string]any{"name": "has spaces"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServiceAccountHandler_Create_RejectsBadExpiry(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	body, _ := json.Marshal(map[string]any{"name": "ci-bot", "expiresAt": "not-a-date"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestServiceAccountHandler_Create_AcceptsExpiry(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	exp := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	body, _ := json.Marshal(map[string]any{
		"name":      "ci-bot",
		"expiresAt": exp.Format(time.RFC3339),
	})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp ServiceAccountResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ExpiresAt == nil || !resp.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt mismatch: got %v want %v", resp.ExpiresAt, exp)
	}
}

func TestServiceAccountHandler_Create_NameConflict(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"name": "ci-bot"})
	req1 := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body)))
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first Create: %d", rec1.Code)
	}

	body2, _ := json.Marshal(map[string]any{"name": "ci-bot"})
	req2 := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(body2)))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestServiceAccountHandler_List(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)

	// Seed 2 active + 1 disabled via the repo directly.
	for _, name := range []string{"alpha", "beta", "gamma"} {
		repo.Create(context.Background(), &ServiceAccount{Name: name, OwnerUserID: "user:admin@example.com"})
	}
	// Disable "beta".
	betaID := ""
	for id, sa := range repo.byID {
		if sa.Name == "beta" {
			betaID = id
		}
	}
	repo.Disable(context.Background(), betaID)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/service-accounts", nil))
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp ServiceAccountListResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.ServiceAccounts) != 2 {
		t.Errorf("expected 2 active, got %d", len(resp.ServiceAccounts))
	}
	for _, sa := range resp.ServiceAccounts {
		if sa.Name == "beta" {
			t.Error("List returned disabled service account")
		}
	}
}

func TestServiceAccountHandler_Get_Found(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)

	sa := &ServiceAccount{Name: "ci-bot", OwnerUserID: "user:admin@example.com"}
	repo.Create(context.Background(), sa)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/service-accounts/"+sa.ID, nil))
	rec := httptest.NewRecorder()
	h.getFor(rec, req, sa.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp ServiceAccountResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Name != "ci-bot" {
		t.Errorf("Name: %q", resp.Name)
	}
}

func TestServiceAccountHandler_Get_NotFound(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/service-accounts/nope", nil))
	rec := httptest.NewRecorder()
	h.getFor(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestServiceAccountHandler_Update_PartialPatch(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)

	exp := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	sa := &ServiceAccount{
		Name:        "ci-bot",
		Description: "initial",
		OwnerUserID: "user:admin@example.com",
		Scopes:      []string{"read:objects"},
		ExpiresAt:   &exp,
	}
	repo.Create(context.Background(), sa)

	body, _ := json.Marshal(map[string]any{"description": "updated"})
	req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/service-accounts/"+sa.ID, bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.updateFor(rec, req, sa.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp ServiceAccountResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Description != "updated" {
		t.Errorf("Description not updated: %q", resp.Description)
	}
	if len(resp.Scopes) != 1 || resp.Scopes[0] != "read:objects" {
		t.Errorf("Scopes clobbered: %v", resp.Scopes)
	}
	if resp.ExpiresAt == nil || !resp.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt clobbered: got %v want %v", resp.ExpiresAt, exp)
	}
}

func TestServiceAccountHandler_Update_ClearExpiry(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)

	exp := time.Now().Add(24 * time.Hour)
	sa := &ServiceAccount{Name: "ci-bot", OwnerUserID: "user:admin@example.com", ExpiresAt: &exp}
	repo.Create(context.Background(), sa)

	body, _ := json.Marshal(map[string]any{"expiresAt": ""})
	req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/service-accounts/"+sa.ID, bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.updateFor(rec, req, sa.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp ServiceAccountResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ExpiresAt != nil {
		t.Errorf("expected ExpiresAt cleared, got %v", resp.ExpiresAt)
	}
}

func TestServiceAccountHandler_Update_NotFound(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	body, _ := json.Marshal(map[string]any{"description": "x"})
	req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/service-accounts/nope", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.updateFor(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestServiceAccountHandler_Update_BadExpiry(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)
	sa := &ServiceAccount{Name: "ci-bot", OwnerUserID: "user:admin@example.com"}
	repo.Create(context.Background(), sa)

	body, _ := json.Marshal(map[string]any{"expiresAt": "not-a-date"})
	req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/service-accounts/"+sa.ID, bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.updateFor(rec, req, sa.ID)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestServiceAccountHandler_Delete_SoftDisables(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)

	sa := &ServiceAccount{Name: "ci-bot", OwnerUserID: "user:admin@example.com"}
	repo.Create(context.Background(), sa)

	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/service-accounts/"+sa.ID, nil))
	rec := httptest.NewRecorder()
	h.deleteFor(rec, req, sa.ID)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
	row, err := repo.GetByID(context.Background(), sa.ID)
	if err != nil {
		t.Fatalf("row disappeared: %v", err)
	}
	if !row.IsDisabled() {
		t.Error("expected DisabledAt set")
	}
}

func TestServiceAccountHandler_Delete_NotFound(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/service-accounts/nope", nil))
	rec := httptest.NewRecorder()
	h.deleteFor(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestServiceAccountHandler_Delete_RequiresAuth(t *testing.T) {
	h, _ := newServiceAccountHandlerHarness(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/service-accounts/abc", nil)
	rec := httptest.NewRecorder()
	h.deleteFor(rec, req, "abc")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestServiceAccountHandler_RegisterRoutes_MountsAll verifies that
// RegisterRoutes wires the 5-endpoint CRUD surface under the expected paths.
// Uses a real chi router + httptest recorder so the path-param extraction
// via chi.URLParam gets exercised alongside the route registration.
func TestServiceAccountHandler_RegisterRoutes_MountsAll(t *testing.T) {
	h, repo := newServiceAccountHandlerHarness(t)
	r := chi.NewRouter()
	// Inject admin user on every request so handlers pass auth.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, withAdmin(req))
		})
	})
	h.RegisterRoutes(r)

	// Create via router so chi parses the body end-to-end.
	createBody := strings.NewReader(`{"name":"ci-bot"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", createBody)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST: %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created ServiceAccountResponse
	json.NewDecoder(createRec.Body).Decode(&created)

	// List
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/service-accounts", nil)
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Errorf("GET list: %d", listRec.Code)
	}

	// Get by id
	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/service-accounts/"+created.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Errorf("GET id: %d", getRec.Code)
	}

	// Patch
	patchBody := strings.NewReader(`{"description":"updated"}`)
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/service-accounts/"+created.ID, patchBody)
	patchRec := httptest.NewRecorder()
	r.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Errorf("PATCH: %d body=%s", patchRec.Code, patchRec.Body.String())
	}

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete, "/api/admin/service-accounts/"+created.ID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Errorf("DELETE: %d", delRec.Code)
	}

	// Verify disabled via repo.
	row, err := repo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if !row.IsDisabled() {
		t.Error("DELETE did not disable the row")
	}
}
