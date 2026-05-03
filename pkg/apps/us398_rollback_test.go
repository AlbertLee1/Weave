package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// US-398 — version rollback. Restoring a historical snapshot bumps the
// live row's Version and inserts a new history row attributed to the
// caller; rollback to a missing version returns ErrNotFound; rollback
// from a non-owner returns ErrNotFound; the rollback'd row's layout
// must byte-match the targeted snapshot.

const v1Layout = `{"type":"row","children":[{"type":"col","width":12,"child":{"type":"component","componentType":"text","props":{"text":"v1"}}}]}`
const v2Layout = `{"type":"row","children":[{"type":"col","width":12,"child":{"type":"component","componentType":"text","props":{"text":"v2"}}}]}`

func TestMemoryStore_RollbackRestoresHistoricalLayoutAndBumpsVersion(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	a.LayoutJSON = json.RawMessage(v1Layout)
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	v2 := json.RawMessage(v2Layout)
	if err := s.Update(ctx, a.RID, "alice", Update{LayoutJSON: &v2}, "alice"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Sanity — live row reflects v2 with Version=2.
	live, err := s.Get(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("Get pre-rollback: %v", err)
	}
	if live.Version != 2 || string(live.LayoutJSON) != v2Layout {
		t.Fatalf("pre-rollback unexpected: version=%d layout=%s", live.Version, string(live.LayoutJSON))
	}
	out, err := s.Rollback(ctx, a.RID, 1, "alice", "alice")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	// Rollback bumps version to 3 (post v1, v2). Layout matches v1.
	if out.Version != 3 {
		t.Fatalf("Rollback returned Version=%d, want 3", out.Version)
	}
	if string(out.LayoutJSON) != v1Layout {
		t.Fatalf("Rollback layout=%s, want v1Layout", string(out.LayoutJSON))
	}
	// History now has 3 rows; row #3 carries the rolled-back layout.
	versions, err := s.ListVersions(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions after rollback, got %d", len(versions))
	}
	// Newest first.
	if versions[0].Version != 3 || string(versions[0].LayoutJSON) != v1Layout {
		t.Fatalf("v3 mismatch: %+v", versions[0])
	}
	// Old rows unchanged.
	if string(versions[2].LayoutJSON) != v1Layout || string(versions[1].LayoutJSON) != v2Layout {
		t.Fatalf("legacy rows mutated by rollback: %+v", versions)
	}
}

func TestMemoryStore_RollbackOwnerOnly(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Rollback(ctx, a.RID, 1, "bob", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner rollback: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_RollbackUnknownRID(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if _, err := s.Rollback(ctx, "ri.app.main.app.missing", 1, "alice", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rollback missing RID: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_RollbackUnknownVersionReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Rollback(ctx, a.RID, 99, "alice", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rollback unknown version: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_RollbackRestoresName(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	rename := "Console v2"
	if err := s.Update(ctx, a.RID, "alice", Update{Name: &rename}, "alice"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	out, err := s.Rollback(ctx, a.RID, 1, "alice", "alice")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if out.Name != "Console" {
		t.Fatalf("Rollback Name=%q, want Console", out.Name)
	}
	// nameIdx now points to the restored Name on this RID.
	live, err := s.Get(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if live.Name != "Console" {
		t.Fatalf("live Name after rollback=%q, want Console", live.Name)
	}
}

func TestMemoryStore_RollbackToLiveVersionStillBumps(t *testing.T) {
	// Idempotent in payload but every Rollback call lands an audit row,
	// matching the contract on Update.
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	out, err := s.Rollback(ctx, a.RID, 1, "alice", "alice")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if out.Version != 2 {
		t.Fatalf("Rollback to live: Version=%d, want 2", out.Version)
	}
	versions, err := s.ListVersions(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("want 2 history rows, got %d", len(versions))
	}
	if versions[0].CreatedBy != "alice" {
		t.Fatalf("history row attribution=%q, want alice", versions[0].CreatedBy)
	}
}

func TestMemoryStore_RollbackNameConflictRolledBack(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	original := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, original, "alice"); err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	// Rename then create a second App reusing the original name. A
	// rollback now would restore "Console" on app #1 but app #2 already
	// owns that name on alice's namespace — must yield ErrNameConflict
	// and leave both rows untouched.
	rename := "Console v2"
	if err := s.Update(ctx, original.RID, "alice", Update{Name: &rename}, "alice"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := s.Create(ctx, newApp("ri.app.main.app.2", "alice", "Console"), "alice"); err != nil {
		t.Fatalf("Create #2: %v", err)
	}
	_, err := s.Rollback(ctx, original.RID, 1, "alice", "alice")
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("rollback into name-conflict: want ErrNameConflict, got %v", err)
	}
}

// Handler-level tests — ensure the wire shape for US-398 matches the
// PRD: POST /api/v2/apps/{rid}/versions/{version}/rollback returns the
// post-rollback live App and 404s when missing / non-owner / unknown
// version.

func TestHandler_RollbackOwnerOnly(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}
	app := createTestApp(t, store, alice, "Console")

	// Bob cannot rollback Alice's App.
	bobR := newTestRouter(store, bob)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/apps/"+app.RID+"/versions/1/rollback", nil)
	w := httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Bob rollback: want 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandler_RollbackRequiresAuth(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/apps/ri.app.main.app.x/versions/1/rollback", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth rollback: want 401, got %d", w.Code)
	}
}

func TestHandler_RollbackRestoresLayout(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	app := createTestApp(t, store, alice, "Console")
	r := newTestRouter(store, alice)

	// Bump to v2 with a new layout.
	updateBody := mustEncode(t, map[string]any{
		"layoutJson": json.RawMessage(v2Layout),
	})
	updReq := httptest.NewRequest(http.MethodPut,
		"/api/v2/apps/"+app.RID, bytes.NewReader(updateBody))
	updReq.Header.Set("Content-Type", "application/json")
	updW := httptest.NewRecorder()
	r.ServeHTTP(updW, updReq)
	if updW.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d", updW.Code)
	}

	// Rollback to v1.
	rbReq := httptest.NewRequest(http.MethodPost,
		"/api/v2/apps/"+app.RID+"/versions/1/rollback", nil)
	rbW := httptest.NewRecorder()
	r.ServeHTTP(rbW, rbReq)
	if rbW.Code != http.StatusOK {
		t.Fatalf("rollback: want 200, got %d (%s)", rbW.Code, rbW.Body.String())
	}
	var live App
	if err := json.Unmarshal(rbW.Body.Bytes(), &live); err != nil {
		t.Fatalf("decode rollback resp: %v", err)
	}
	if live.Version != 3 {
		t.Fatalf("post-rollback Version=%d, want 3", live.Version)
	}
	// v1 was created via createTestApp() which seeded validLayout
	// ("text":"hello"). After Rollback to v1, the live row's layout
	// must structurally match validLayout, NOT the v2Layout we just
	// wrote.
	if !strings.Contains(string(live.LayoutJSON), `"text":"hello"`) {
		t.Fatalf("post-rollback layout missing v1 marker: %s", string(live.LayoutJSON))
	}
	if strings.Contains(string(live.LayoutJSON), `"text":"v2"`) {
		t.Fatalf("post-rollback layout still carries v2 content: %s", string(live.LayoutJSON))
	}
}

func TestHandler_RollbackUnknownVersion(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	app := createTestApp(t, store, alice, "Console")
	r := newTestRouter(store, alice)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/apps/"+app.RID+"/versions/42/rollback", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("rollback unknown version: want 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandler_RollbackInvalidVersionParam(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	app := createTestApp(t, store, alice, "Console")
	r := newTestRouter(store, alice)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/apps/"+app.RID+"/versions/zero/rollback", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rollback non-numeric version: want 400, got %d", w.Code)
	}
}

func TestHandler_RollbackHistoryRowAttributionIsCaller(t *testing.T) {
	// The new history row inserted by Rollback must be attributed to
	// the authenticated caller, not to the user who originally
	// authored the targeted snapshot.
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	app := createTestApp(t, store, alice, "Console")
	r := newTestRouter(store, alice)

	rbReq := httptest.NewRequest(http.MethodPost,
		"/api/v2/apps/"+app.RID+"/versions/1/rollback", nil)
	rbW := httptest.NewRecorder()
	r.ServeHTTP(rbW, rbReq)
	if rbW.Code != http.StatusOK {
		t.Fatalf("rollback: want 200, got %d", rbW.Code)
	}

	// Pull versions list and ensure newest carries createdBy=alice.
	listReq := httptest.NewRequest(http.MethodGet,
		"/api/v2/apps/"+app.RID+"/versions", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("versions list: want 200, got %d", listW.Code)
	}
	var resp listVersionsResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	if len(resp.Versions) < 1 {
		t.Fatalf("expected at least one version row")
	}
	if resp.Versions[0].CreatedBy != alice.ID {
		t.Fatalf("rollback history row CreatedBy=%q, want %q",
			resp.Versions[0].CreatedBy, alice.ID)
	}
}
