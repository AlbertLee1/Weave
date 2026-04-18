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
	for _, existing := range f.roles[userID] {
		if existing == role {
			return nil
		}
	}
	f.roles[userID] = append(f.roles[userID], role)
	return nil
}

// RevokeUserRole satisfies UserRoleRevoker for the fake. Idempotent.
func (f *fakeUserRepo) RevokeUserRole(_ context.Context, userID, role string) error {
	existing := f.roles[userID]
	filtered := existing[:0]
	for _, r := range existing {
		if r != role {
			filtered = append(filtered, r)
		}
	}
	f.roles[userID] = filtered
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

func TestRoleResolver_LRU_EvictsOldest(t *testing.T) {
	repo := newFakeUserRepo()

	// Build a resolver with a tiny capacity so the test runs fast.
	const capacity = 3
	resolver := NewRoleResolverWithSize(repo, 5*time.Minute, capacity)

	// Insert capacity+1 distinct users; the first one inserted should be the
	// oldest and therefore evicted.
	users := []string{"u1", "u2", "u3", "u4"}
	for _, id := range users {
		repo.users[id] = &UserRecord{ID: id}
		repo.roles[id] = []string{"viewer"}
		if _, _, err := resolver.Resolve(context.Background(), id); err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
	}

	if got := resolver.CacheSize(); got != capacity {
		t.Errorf("expected cache size %d after eviction, got %d", capacity, got)
	}

	// Verify the OLDEST entry (u1) was evicted: re-resolving must hit the repo
	// again, incrementing the call counter.
	rolesCallsBefore := repo.rolesCalls
	if _, _, err := resolver.Resolve(context.Background(), "u1"); err != nil {
		t.Fatalf("resolve u1: %v", err)
	}
	if repo.rolesCalls != rolesCallsBefore+1 {
		t.Errorf("expected u1 to be evicted (repo hit), but got cached result")
	}

	// Verify the most recently used entry (u4) is still in the cache: it must
	// NOT increment the call counter.
	rolesCallsBefore = repo.rolesCalls
	if _, _, err := resolver.Resolve(context.Background(), "u4"); err != nil {
		t.Fatalf("resolve u4: %v", err)
	}
	if repo.rolesCalls != rolesCallsBefore {
		t.Errorf("expected u4 to still be cached, but repo was hit (calls=%d)", repo.rolesCalls)
	}
}

func TestRoleResolver_LRU_RefreshesOnAccess(t *testing.T) {
	repo := newFakeUserRepo()
	for _, id := range []string{"u1", "u2", "u3"} {
		repo.users[id] = &UserRecord{ID: id}
		repo.roles[id] = []string{"viewer"}
	}

	const capacity = 3
	resolver := NewRoleResolverWithSize(repo, 5*time.Minute, capacity)

	// Fill the cache.
	for _, id := range []string{"u1", "u2", "u3"} {
		if _, _, err := resolver.Resolve(context.Background(), id); err != nil {
			t.Fatalf("seed resolve %s: %v", id, err)
		}
	}

	// Touch u1 so it becomes the most recently used.
	if _, _, err := resolver.Resolve(context.Background(), "u1"); err != nil {
		t.Fatalf("touch u1: %v", err)
	}

	// Insert u4 — this should evict u2 (now the oldest), NOT u1.
	repo.users["u4"] = &UserRecord{ID: "u4"}
	repo.roles["u4"] = []string{"viewer"}
	if _, _, err := resolver.Resolve(context.Background(), "u4"); err != nil {
		t.Fatalf("insert u4: %v", err)
	}

	// u1 should still be cached (no repo hit).
	rolesBefore := repo.rolesCalls
	if _, _, err := resolver.Resolve(context.Background(), "u1"); err != nil {
		t.Fatalf("re-resolve u1: %v", err)
	}
	if repo.rolesCalls != rolesBefore {
		t.Errorf("expected u1 to remain cached after touch, but repo was hit")
	}

	// u2 should be evicted (repo hit).
	rolesBefore = repo.rolesCalls
	if _, _, err := resolver.Resolve(context.Background(), "u2"); err != nil {
		t.Fatalf("re-resolve u2: %v", err)
	}
	if repo.rolesCalls != rolesBefore+1 {
		t.Errorf("expected u2 to be evicted (repo hit), but got cached result")
	}
}

func TestRoleResolver_DefaultMaxSize(t *testing.T) {
	// NewRoleResolver (no explicit size) should default to a sane bound.
	resolver := NewRoleResolver(newFakeUserRepo(), time.Minute)
	if resolver.MaxSize() <= 0 {
		t.Errorf("expected default max size > 0, got %d", resolver.MaxSize())
	}
}
