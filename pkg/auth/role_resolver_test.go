package auth

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// fakeUserRepo is an in-memory UserRepository for resolver tests.
type fakeUserRepo struct {
	users         map[string]*UserRecord
	roles         map[string][]string
	scopedRoles   map[string]map[string]string
	rolesCalls    int
	scopedCalls   int
	getUserCalls  int
	rolesError    error
	scopedError   error
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		users:       map[string]*UserRecord{},
		roles:       map[string][]string{},
		scopedRoles: map[string]map[string]string{},
	}
}

func (f *fakeUserRepo) CreateUser(_ context.Context, u *UserRecord) error {
	f.users[u.ID] = u
	return nil
}

func (f *fakeUserRepo) GetUserByID(_ context.Context, id string) (*UserRecord, error) {
	f.getUserCalls++
	u, ok := f.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUserRepo) GetUserByEmail(_ context.Context, email string) (*UserRecord, error) {
	for _, u := range f.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

func (f *fakeUserRepo) ListUserRoles(_ context.Context, userID string) ([]string, error) {
	f.rolesCalls++
	if f.rolesError != nil {
		return nil, f.rolesError
	}
	return append([]string(nil), f.roles[userID]...), nil
}

func (f *fakeUserRepo) ListUserOntologyRoles(_ context.Context, userID string) (map[string]string, error) {
	f.scopedCalls++
	if f.scopedError != nil {
		return nil, f.scopedError
	}
	out := map[string]string{}
	for k, v := range f.scopedRoles[userID] {
		out[k] = v
	}
	return out, nil
}

func (f *fakeUserRepo) UpsertUserRole(_ context.Context, userID, role string) error {
	f.roles[userID] = append(f.roles[userID], role)
	return nil
}

func (f *fakeUserRepo) SetPassword(_ context.Context, userID, passwordHash string) error {
	u, ok := f.users[userID]
	if !ok {
		return ErrUserNotFound
	}
	u.PasswordHash = passwordHash
	return nil
}

func TestRoleResolver_ResolvesGlobalAndScopedRoles(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["alice"] = &UserRecord{ID: "alice"}
	repo.roles["alice"] = []string{"editor"}
	repo.scopedRoles["alice"] = map[string]string{"ri.ontology.main.ontology.northwind": "ontology-owner"}

	resolver := NewRoleResolver(repo, 5*time.Minute)
	global, scoped, err := resolver.Resolve(context.Background(), "alice")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if !slices.Contains(global, "editor") {
		t.Errorf("expected editor in global roles, got %v", global)
	}
	if scoped["ri.ontology.main.ontology.northwind"] != "ontology-owner" {
		t.Errorf("expected ontology-owner scoped role, got %v", scoped)
	}
}

func TestRoleResolver_CacheHits(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["bob"] = &UserRecord{ID: "bob"}
	repo.roles["bob"] = []string{"viewer"}

	resolver := NewRoleResolver(repo, 5*time.Minute)
	for i := 0; i < 3; i++ {
		if _, _, err := resolver.Resolve(context.Background(), "bob"); err != nil {
			t.Fatalf("resolve %d failed: %v", i, err)
		}
	}
	if repo.rolesCalls != 1 {
		t.Errorf("expected 1 roles DB call (cached), got %d", repo.rolesCalls)
	}
	if repo.scopedCalls != 1 {
		t.Errorf("expected 1 scoped DB call (cached), got %d", repo.scopedCalls)
	}
}

func TestRoleResolver_TTLExpiry(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["carol"] = &UserRecord{ID: "carol"}
	repo.roles["carol"] = []string{"viewer"}

	resolver := NewRoleResolver(repo, 10*time.Millisecond)
	if _, _, err := resolver.Resolve(context.Background(), "carol"); err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, _, err := resolver.Resolve(context.Background(), "carol"); err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if repo.rolesCalls != 2 {
		t.Errorf("expected 2 roles DB calls after TTL expiry, got %d", repo.rolesCalls)
	}
}

func TestRoleResolver_Invalidate(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["dan"] = &UserRecord{ID: "dan"}
	repo.roles["dan"] = []string{"viewer"}

	resolver := NewRoleResolver(repo, 5*time.Minute)
	resolver.Resolve(context.Background(), "dan")
	resolver.Invalidate("dan")
	resolver.Resolve(context.Background(), "dan")
	if repo.rolesCalls != 2 {
		t.Errorf("expected 2 roles DB calls after invalidate, got %d", repo.rolesCalls)
	}
}

func TestRoleResolver_PropagatesError(t *testing.T) {
	repo := newFakeUserRepo()
	repo.rolesError = errors.New("db down")
	resolver := NewRoleResolver(repo, time.Minute)
	if _, _, err := resolver.Resolve(context.Background(), "eve"); err == nil {
		t.Error("expected error to propagate from repo")
	}
}

func TestRoleResolver_NilRepoNoOp(t *testing.T) {
	resolver := NewRoleResolver(nil, time.Minute)
	global, scoped, err := resolver.Resolve(context.Background(), "anyone")
	if err != nil {
		t.Fatalf("nil repo should be a no-op, got error: %v", err)
	}
	if global != nil || scoped != nil {
		t.Errorf("expected empty results, got global=%v scoped=%v", global, scoped)
	}
}
