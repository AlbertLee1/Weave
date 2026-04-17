package developer

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// newHarness builds a fresh ApplicationHandler with an in-memory repo and
// returns both so tests can poke at persistent state.
func newHarness(t *testing.T) (*ApplicationHandler, *fakeApplicationRepo) {
	t.Helper()
	repo := newFakeApplicationRepo()
	h := NewApplicationHandler(repo)
	return h, repo
}

// withUser attaches an authenticated user to the request context so the
// handler's UserFromContext lookup succeeds.
func withUser(req *http.Request, userID string) *http.Request {
	return req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: userID}))
}

func TestCreateApplication_201_ReturnsClientSecretOnce(t *testing.T) {
	h, repo := newHarness(t)

	body, _ := json.Marshal(map[string]any{
		"name":         "MyApp",
		"description":  "Third-party integration",
		"redirectUris": []string{"https://example.com/callback"},
		"scopes":       []string{"read:objects", "read:ontologies"},
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/v2/developer/applications", bytes.NewReader(body)), "user:alice@example.com")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp CreateApplicationResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected id")
	}
	if !strings.HasPrefix(resp.ClientID, ClientIDPrefix) {
		t.Errorf("clientId prefix: got %q", resp.ClientID)
	}
	if !strings.HasPrefix(resp.ClientSecret, ClientSecretPrefix) {
		t.Errorf("clientSecret prefix: got %q", resp.ClientSecret)
	}
	if resp.Name != "MyApp" {
		t.Errorf("name: got %q", resp.Name)
	}
	if len(resp.Scopes) != 2 {
		t.Errorf("scopes: got %v", resp.Scopes)
	}

	// Verify the row landed in the repo, with the SHA-256 hash (not the plaintext).
	stored, err := repo.GetByClientID(req.Context(), resp.ClientID)
	if err != nil {
		t.Fatalf("GetByClientID: %v", err)
	}
	if stored.CreatedBy != "user:alice@example.com" {
		t.Errorf("createdBy: got %q", stored.CreatedBy)
	}
	if subtle.ConstantTimeCompare(stored.ClientSecretHash, HashClientSecret(resp.ClientSecret)) != 1 {
		t.Error("stored hash does not match SHA-256 of returned clientSecret")
	}
}

func TestCreateApplication_RequiresAuth(t *testing.T) {
	h, _ := newHarness(t)
	body, _ := json.Marshal(map[string]any{"name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/developer/applications", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestCreateApplication_RejectsEmptyName(t *testing.T) {
	h, _ := newHarness(t)
	body, _ := json.Marshal(map[string]any{"name": ""})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/v2/developer/applications", bytes.NewReader(body)), "user:alice@example.com")
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestListApplications_OnlyOwnerRows(t *testing.T) {
	h, repo := newHarness(t)

	// Seed one row owned by alice and one owned by bob.
	mustSeed(t, repo, "user:alice@example.com", "AppA")
	mustSeed(t, repo, "user:bob@example.com", "AppB")

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v2/developer/applications", nil), "user:alice@example.com")
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp ListApplicationsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Applications) != 1 {
		t.Fatalf("expected 1 app, got %d", len(resp.Applications))
	}
	// Critical: List response MUST NOT contain the clientSecret.
	raw, _ := json.Marshal(resp.Applications[0])
	if strings.Contains(string(raw), "clientSecret") {
		t.Errorf("List response leaked clientSecret: %s", raw)
	}
	if resp.Applications[0].Name != "AppA" {
		t.Errorf("expected AppA, got %q", resp.Applications[0].Name)
	}
}

func TestGetApplication_OwnerOnly(t *testing.T) {
	h, repo := newHarness(t)
	id := mustSeed(t, repo, "user:alice@example.com", "AppA")

	// Owner can read.
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v2/developer/applications/"+id, nil), "user:alice@example.com")
	rec := httptest.NewRecorder()
	h.GetFor(rec, req, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner GET: expected 200, got %d", rec.Code)
	}
	raw, _ := json.Marshal(json.RawMessage(rec.Body.Bytes()))
	if strings.Contains(string(raw), "clientSecret") {
		t.Errorf("Get leaked clientSecret: %s", raw)
	}

	// Other user cannot read — must be 403 or 404 (don't leak existence).
	req2 := withUser(httptest.NewRequest(http.MethodGet, "/api/v2/developer/applications/"+id, nil), "user:bob@example.com")
	rec2 := httptest.NewRecorder()
	h.GetFor(rec2, req2, id)
	if rec2.Code != http.StatusForbidden && rec2.Code != http.StatusNotFound {
		t.Errorf("non-owner GET: expected 403/404, got %d", rec2.Code)
	}
}

func TestGetApplication_NotFound(t *testing.T) {
	h, _ := newHarness(t)
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v2/developer/applications/missing", nil), "user:alice@example.com")
	rec := httptest.NewRecorder()
	h.GetFor(rec, req, "missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteApplication_Removes(t *testing.T) {
	h, repo := newHarness(t)
	id := mustSeed(t, repo, "user:alice@example.com", "AppA")

	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/v2/developer/applications/"+id, nil), "user:alice@example.com")
	rec := httptest.NewRecorder()
	h.DeleteFor(rec, req, id)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if _, err := repo.GetByID(req.Context(), id); err == nil {
		t.Error("expected application to be removed after DELETE")
	}
}

func TestDeleteApplication_RejectsNonOwner(t *testing.T) {
	h, repo := newHarness(t)
	id := mustSeed(t, repo, "user:alice@example.com", "AppA")

	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/v2/developer/applications/"+id, nil), "user:bob@example.com")
	rec := httptest.NewRecorder()
	h.DeleteFor(rec, req, id)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Errorf("expected 403/404, got %d", rec.Code)
	}
	// Row should survive — non-owner cannot delete.
	if _, err := repo.GetByID(req.Context(), id); err != nil {
		t.Error("row should still exist after non-owner delete attempt")
	}
}

func TestDeleteApplication_NotFound(t *testing.T) {
	h, _ := newHarness(t)
	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/v2/developer/applications/missing", nil), "user:alice@example.com")
	rec := httptest.NewRecorder()
	h.DeleteFor(rec, req, "missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestCreateThenListThenGetThenDelete(t *testing.T) {
	// Matches the acceptance criterion: create → list → get → delete round-trip.
	h, _ := newHarness(t)
	user := "user:alice@example.com"

	// Create.
	body, _ := json.Marshal(map[string]any{"name": "RoundTrip"})
	createReq := withUser(httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)), user)
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("Create: %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created CreateApplicationResponse
	json.NewDecoder(createRec.Body).Decode(&created)

	// List.
	listReq := withUser(httptest.NewRequest(http.MethodGet, "/", nil), user)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("List: %d", listRec.Code)
	}
	var listed ListApplicationsResponse
	json.NewDecoder(listRec.Body).Decode(&listed)
	if len(listed.Applications) != 1 || listed.Applications[0].ID != created.ID {
		t.Fatalf("expected exactly the created app in list")
	}

	// Get.
	getReq := withUser(httptest.NewRequest(http.MethodGet, "/", nil), user)
	getRec := httptest.NewRecorder()
	h.GetFor(getRec, getReq, created.ID)
	if getRec.Code != http.StatusOK {
		t.Fatalf("Get: %d", getRec.Code)
	}

	// Delete.
	delReq := withUser(httptest.NewRequest(http.MethodDelete, "/", nil), user)
	delRec := httptest.NewRecorder()
	h.DeleteFor(delRec, delReq, created.ID)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("Delete: %d", delRec.Code)
	}

	// Get after delete should 404.
	after := httptest.NewRecorder()
	h.GetFor(after, getReq, created.ID)
	if after.Code != http.StatusNotFound {
		t.Errorf("after delete: expected 404, got %d", after.Code)
	}
}

// mustSeed inserts a minimal application for the given owner and returns
// its ID.
func mustSeed(t *testing.T, repo *fakeApplicationRepo, owner, name string) string {
	t.Helper()
	cid, _ := GenerateClientID()
	sec, _ := GenerateClientSecret()
	a := &Application{
		Name:             name,
		ClientID:         cid,
		ClientSecretHash: HashClientSecret(sec),
		CreatedBy:        owner,
		RedirectURIs:     []string{},
		Scopes:           []string{},
	}
	if err := repo.Create(nil, a); err != nil {
		t.Fatalf("mustSeed: %v", err)
	}
	return a.ID
}
