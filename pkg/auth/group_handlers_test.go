package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeGroupRepo is an in-memory GroupRepository used by handler-level tests.
// It mirrors the PG repo: unique-name invariant, soft FK check
// (AddMember does not validate user existence — the production PG impl
// relies on the FK).
type fakeGroupRepo struct {
	mu          sync.Mutex
	byID        map[string]*Group
	memberships map[string]map[string]bool // groupID -> set of userIDs
	seq         int
}

func newFakeGroupRepo() *fakeGroupRepo {
	return &fakeGroupRepo{byID: map[string]*Group{}, memberships: map[string]map[string]bool{}}
}

func (f *fakeGroupRepo) Create(_ context.Context, g *Group) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.Name == g.Name {
			return ErrGroupNameConflict
		}
	}
	f.seq++
	g.ID = fmt.Sprintf("grp-%04d", f.seq)
	now := time.Now()
	g.CreatedAt = now
	g.UpdatedAt = now
	cp := *g
	f.byID[g.ID] = &cp
	return nil
}

func (f *fakeGroupRepo) GetByID(_ context.Context, id string) (*Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.byID[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	cp := *g
	return &cp, nil
}

func (f *fakeGroupRepo) GetByName(_ context.Context, name string) (*Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.byID {
		if g.Name == name {
			cp := *g
			return &cp, nil
		}
	}
	return nil, ErrGroupNotFound
}

func (f *fakeGroupRepo) List(_ context.Context) ([]*Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*Group, 0, len(f.byID))
	for _, g := range f.byID {
		cp := *g
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeGroupRepo) Update(_ context.Context, id string, upd GroupUpdate) (*Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.byID[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	if upd.Name != nil {
		for otherID, other := range f.byID {
			if otherID != id && other.Name == *upd.Name {
				return nil, ErrGroupNameConflict
			}
		}
		g.Name = *upd.Name
	}
	if upd.Description != nil {
		g.Description = *upd.Description
	}
	g.UpdatedAt = time.Now()
	cp := *g
	return &cp, nil
}

func (f *fakeGroupRepo) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return ErrGroupNotFound
	}
	delete(f.byID, id)
	delete(f.memberships, id)
	return nil
}

func (f *fakeGroupRepo) AddMember(_ context.Context, groupID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.memberships[groupID] == nil {
		f.memberships[groupID] = map[string]bool{}
	}
	f.memberships[groupID][userID] = true
	return nil
}

func (f *fakeGroupRepo) RemoveMember(_ context.Context, groupID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.memberships[groupID]; ok {
		delete(m, userID)
	}
	return nil
}

func (f *fakeGroupRepo) ListMembers(_ context.Context, groupID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.memberships[groupID]
	out := make([]string, 0, len(m))
	for uid := range m {
		out = append(out, uid)
	}
	return out, nil
}

func (f *fakeGroupRepo) ListUserGroups(_ context.Context, userID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for gid, m := range f.memberships {
		if m[userID] {
			out = append(out, gid)
		}
	}
	return out, nil
}

var _ GroupRepository = (*fakeGroupRepo)(nil)

func newGroupHandlerHarness() (*GroupHandler, *fakeGroupRepo) {
	repo := newFakeGroupRepo()
	return NewGroupHandler(repo, nil), repo
}

func TestGroupHandler_Create_201(t *testing.T) {
	h, repo := newGroupHandlerHarness()

	body, _ := json.Marshal(map[string]any{"name": "analysts", "description": "na team"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp GroupResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ID == "" || resp.Name != "analysts" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if len(repo.byID) != 1 {
		t.Error("group not persisted")
	}
}

func TestGroupHandler_Create_RequiresAuth(t *testing.T) {
	h, _ := newGroupHandlerHarness()
	body, _ := json.Marshal(map[string]any{"name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/groups", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestGroupHandler_Create_RejectsInvalidName(t *testing.T) {
	h, _ := newGroupHandlerHarness()
	body, _ := json.Marshal(map[string]any{"name": "has spaces"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestGroupHandler_Create_NameConflict(t *testing.T) {
	h, _ := newGroupHandlerHarness()

	body, _ := json.Marshal(map[string]any{"name": "analysts"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first Create: %d", rec.Code)
	}

	body2, _ := json.Marshal(map[string]any{"name": "analysts"})
	req2 := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups", bytes.NewReader(body2)))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestGroupHandler_List(t *testing.T) {
	h, repo := newGroupHandlerHarness()
	for _, n := range []string{"alpha", "beta", "gamma"} {
		_ = repo.Create(context.Background(), &Group{Name: n})
	}

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/groups", nil))
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp GroupListResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Groups) != 3 {
		t.Errorf("expected 3, got %d", len(resp.Groups))
	}
}

func TestGroupHandler_Get_And_Update(t *testing.T) {
	h, repo := newGroupHandlerHarness()
	g := &Group{Name: "analysts", Description: "initial"}
	_ = repo.Create(context.Background(), g)

	// Get
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/groups/"+g.ID, nil))
	rec := httptest.NewRecorder()
	h.getFor(rec, req, g.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("Get: %d", rec.Code)
	}

	// Update description
	body, _ := json.Marshal(map[string]any{"description": "updated"})
	req = withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/groups/"+g.ID, bytes.NewReader(body)))
	rec = httptest.NewRecorder()
	h.updateFor(rec, req, g.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("Update: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp GroupResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Description != "updated" || resp.Name != "analysts" {
		t.Errorf("Update result: %+v", resp)
	}
}

func TestGroupHandler_Update_NotFound(t *testing.T) {
	h, _ := newGroupHandlerHarness()
	body, _ := json.Marshal(map[string]any{"description": "x"})
	req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/groups/missing", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.updateFor(rec, req, "missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestGroupHandler_Delete(t *testing.T) {
	h, repo := newGroupHandlerHarness()
	g := &Group{Name: "analysts"}
	_ = repo.Create(context.Background(), g)

	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/groups/"+g.ID, nil))
	rec := httptest.NewRecorder()
	h.deleteFor(rec, req, g.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Delete: %d", rec.Code)
	}
	if _, ok := repo.byID[g.ID]; ok {
		t.Error("group still in repo")
	}
}

func TestGroupHandler_MembershipFlow(t *testing.T) {
	h, repo := newGroupHandlerHarness()
	g := &Group{Name: "analysts"}
	_ = repo.Create(context.Background(), g)

	// Add alice
	body, _ := json.Marshal(map[string]any{"userId": "user:alice"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups/"+g.ID+"/members", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.addMemberFor(rec, req, g.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("AddMember: %d body=%s", rec.Code, rec.Body.String())
	}

	// List members
	req = withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/groups/"+g.ID+"/members", nil))
	rec = httptest.NewRecorder()
	h.listMembersFor(rec, req, g.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("List: %d", rec.Code)
	}
	var resp GroupMembersResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Members) != 1 || resp.Members[0] != "user:alice" {
		t.Errorf("expected [user:alice], got %v", resp.Members)
	}

	// Remove
	req = withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/groups/"+g.ID+"/members/user:alice", nil))
	rec = httptest.NewRecorder()
	h.removeMemberFor(rec, req, g.ID, "user:alice")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Remove: %d", rec.Code)
	}

	// Verify empty
	got, _ := repo.ListMembers(context.Background(), g.ID)
	if len(got) != 0 {
		t.Errorf("expected empty members, got %v", got)
	}
}

func TestGroupHandler_AddMember_NotFound(t *testing.T) {
	h, _ := newGroupHandlerHarness()
	body, _ := json.Marshal(map[string]any{"userId": "user:alice"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups/missing/members", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.addMemberFor(rec, req, "missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestGroupHandler_AddMember_MissingUserID(t *testing.T) {
	h, repo := newGroupHandlerHarness()
	g := &Group{Name: "analysts"}
	_ = repo.Create(context.Background(), g)
	body, _ := json.Marshal(map[string]any{})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups/"+g.ID+"/members", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.addMemberFor(rec, req, g.ID)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
