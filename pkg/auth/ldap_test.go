package auth

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubLDAPClient is the in-memory LDAPClient used by the sync tests.
type stubLDAPClient struct {
	users      []LDAPUser
	groups     []LDAPGroup
	usersErr   error
	groupsErr  error
	closeCalls int
}

func (c *stubLDAPClient) SearchUsers(ctx context.Context) ([]LDAPUser, error) {
	if c.usersErr != nil {
		return nil, c.usersErr
	}
	out := make([]LDAPUser, len(c.users))
	copy(out, c.users)
	return out, nil
}

func (c *stubLDAPClient) SearchGroups(ctx context.Context) ([]LDAPGroup, error) {
	if c.groupsErr != nil {
		return nil, c.groupsErr
	}
	out := make([]LDAPGroup, len(c.groups))
	copy(out, c.groups)
	return out, nil
}

func (c *stubLDAPClient) Close() {
	c.closeCalls++
}

// memLDAPSyncStore is the in-memory LDAPSyncStore used by the sync tests.
// Keeps a parallel view onto users / groups / memberships keyed on DN so
// the tests can introspect the post-sync state without touching SQL.
type memLDAPSyncStore struct {
	mu sync.Mutex

	usersByDN     map[string]*memUser
	groupsByDN    map[string]*memGroup
	groupMembers  map[string]map[string]struct{} // groupID -> set(userID)
	groupSeq      int
	syncRuns      []*LDAPSyncRun
	upsertUserErr error
}

type memUser struct {
	id           string
	email        string
	name         string
	dn           string
	disabledAt   *time.Time
	lastSyncedAt time.Time
}

type memGroup struct {
	id           string
	name         string
	description  string
	dn           string
	lastSyncedAt time.Time
}

func newMemLDAPSyncStore() *memLDAPSyncStore {
	return &memLDAPSyncStore{
		usersByDN:    map[string]*memUser{},
		groupsByDN:   map[string]*memGroup{},
		groupMembers: map[string]map[string]struct{}{},
	}
}

func (m *memLDAPSyncStore) UpsertSyncedUser(ctx context.Context, dn, email, name string, syncedAt time.Time) (string, bool, error) {
	if m.upsertUserErr != nil {
		return "", false, m.upsertUserErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, exists := m.usersByDN[dn]
	if !exists {
		u = &memUser{id: "user:" + email, dn: dn}
		m.usersByDN[dn] = u
	}
	u.email = email
	if name != "" {
		u.name = name
	}
	u.lastSyncedAt = syncedAt
	u.disabledAt = nil
	return u.id, !exists, nil
}

func (m *memLDAPSyncStore) DisableOrphanedSyncedUsers(ctx context.Context, syncedAt time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, u := range m.usersByDN {
		if u.disabledAt != nil {
			continue
		}
		if u.lastSyncedAt.Before(syncedAt) {
			t := syncedAt
			u.disabledAt = &t
			count++
		}
	}
	return count, nil
}

func (m *memLDAPSyncStore) UpsertSyncedGroup(ctx context.Context, dn, name, description string, syncedAt time.Time) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, exists := m.groupsByDN[dn]
	if !exists {
		m.groupSeq++
		g = &memGroup{
			id: "group-" + itoa(m.groupSeq),
			dn: dn,
		}
		m.groupsByDN[dn] = g
	}
	g.name = name
	g.description = description
	g.lastSyncedAt = syncedAt
	return g.id, !exists, nil
}

func (m *memLDAPSyncStore) ReplaceGroupMembers(ctx context.Context, groupID string, userIDs []string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := m.groupMembers[groupID]
	if existing == nil {
		existing = map[string]struct{}{}
		m.groupMembers[groupID] = existing
	}
	desired := map[string]struct{}{}
	for _, id := range userIDs {
		desired[id] = struct{}{}
	}
	added := 0
	for id := range desired {
		if _, present := existing[id]; !present {
			existing[id] = struct{}{}
			added++
		}
	}
	for id := range existing {
		if _, present := desired[id]; !present {
			delete(existing, id)
		}
	}
	return added, nil
}

func (m *memLDAPSyncStore) UserIDByDN(ctx context.Context, dn string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.usersByDN[dn]; ok {
		return u.id, nil
	}
	return "", nil
}

func (m *memLDAPSyncStore) RecordSyncRun(ctx context.Context, run *LDAPSyncRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *run
	m.syncRuns = append(m.syncRuns, &cp)
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func newSyncService(t *testing.T, client *stubLDAPClient, store *memLDAPSyncStore, now time.Time) *LDAPSyncService {
	t.Helper()
	cfg := LDAPSyncConfig{
		URL:        "ldap://test",
		UserBaseDN: "ou=users",
	}
	svc := NewLDAPSyncService(cfg, func(ctx context.Context) (LDAPClient, error) {
		return client, nil
	}, store)
	svc.SetNowFunc(func() time.Time { return now })
	svc.SetLogger(func(format string, v ...any) {})
	return svc
}

// --- LDAPSyncConfig tests --------------------------------------------------

func TestLDAPSyncConfig_Validate(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		err := LDAPSyncConfig{UserBaseDN: "ou=users"}.Validate()
		if err == nil || !strings.Contains(err.Error(), "URL") {
			t.Errorf("expected URL missing error, got %v", err)
		}
	})
	t.Run("missing user base dn", func(t *testing.T) {
		err := LDAPSyncConfig{URL: "ldap://x"}.Validate()
		if err == nil || !strings.Contains(err.Error(), "UserBaseDN") {
			t.Errorf("expected UserBaseDN missing error, got %v", err)
		}
	})
	t.Run("happy", func(t *testing.T) {
		err := LDAPSyncConfig{URL: "ldap://x", UserBaseDN: "ou=users"}.Validate()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestLDAPSyncConfig_WithDefaults(t *testing.T) {
	cfg := LDAPSyncConfig{}.withDefaults()
	if cfg.UserFilter != "(objectClass=person)" {
		t.Errorf("UserFilter=%q", cfg.UserFilter)
	}
	if cfg.UserEmailAttribute != "mail" {
		t.Errorf("UserEmailAttribute=%q", cfg.UserEmailAttribute)
	}
	if cfg.GroupFilter != "(objectClass=groupOfNames)" {
		t.Errorf("GroupFilter=%q", cfg.GroupFilter)
	}
	if cfg.GroupMemberAttribute != "member" {
		t.Errorf("GroupMemberAttribute=%q", cfg.GroupMemberAttribute)
	}
	if cfg.DialTimeout != DefaultLDAPDialTimeout {
		t.Errorf("DialTimeout=%v", cfg.DialTimeout)
	}
}

// --- LDAPSyncService.Sync ---------------------------------------------------

func TestSync_HappyPath_CreatesUsersAndGroups(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store := newMemLDAPSyncStore()
	client := &stubLDAPClient{
		users: []LDAPUser{
			{DN: "uid=alice,ou=users", Email: "alice@example.com", DisplayName: "Alice"},
			{DN: "uid=bob,ou=users", Email: "bob@example.com", DisplayName: "Bob"},
		},
		groups: []LDAPGroup{
			{DN: "cn=admins,ou=groups", Name: "admins", MemberDNs: []string{"uid=alice,ou=users"}},
			{DN: "cn=users,ou=groups", Name: "users", MemberDNs: []string{"uid=alice,ou=users", "uid=bob,ou=users"}},
		},
	}
	svc := newSyncService(t, client, store, now)

	run, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync returned error: %v", err)
	}
	if client.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", client.closeCalls)
	}
	if run.UsersSeen != 2 || run.UsersCreated != 2 || run.UsersUpdated != 0 || run.UsersDisabled != 0 {
		t.Errorf("user counters wrong: seen=%d created=%d updated=%d disabled=%d",
			run.UsersSeen, run.UsersCreated, run.UsersUpdated, run.UsersDisabled)
	}
	if run.GroupsSeen != 2 || run.GroupsCreated != 2 || run.GroupsUpdated != 0 {
		t.Errorf("group counters wrong: seen=%d created=%d updated=%d",
			run.GroupsSeen, run.GroupsCreated, run.GroupsUpdated)
	}
	if run.MembershipsAdded != 3 {
		t.Errorf("expected 3 memberships added, got %d", run.MembershipsAdded)
	}
	if run.ErrorMessage != "" {
		t.Errorf("expected empty error, got %q", run.ErrorMessage)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(now) {
		t.Errorf("FinishedAt not stamped: %v", run.FinishedAt)
	}
	if len(store.syncRuns) != 1 {
		t.Errorf("expected 1 recorded run, got %d", len(store.syncRuns))
	}
	if u := store.usersByDN["uid=alice,ou=users"]; u == nil || u.id != "user:alice@example.com" {
		t.Errorf("alice user record missing or wrong: %+v", u)
	}

	// Group membership reconciliation: admins{alice}, users{alice, bob}.
	adminMembers := membersOf(store, "cn=admins,ou=groups")
	if !sameSet(adminMembers, []string{"user:alice@example.com"}) {
		t.Errorf("admins members=%v", adminMembers)
	}
	usersMembers := membersOf(store, "cn=users,ou=groups")
	if !sameSet(usersMembers, []string{"user:alice@example.com", "user:bob@example.com"}) {
		t.Errorf("users members=%v", usersMembers)
	}
}

func TestSync_DisablesOrphanedUsers(t *testing.T) {
	t1 := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store := newMemLDAPSyncStore()
	clientA := &stubLDAPClient{
		users: []LDAPUser{
			{DN: "uid=alice,ou=users", Email: "alice@example.com", DisplayName: "Alice"},
			{DN: "uid=bob,ou=users", Email: "bob@example.com", DisplayName: "Bob"},
		},
	}
	if _, err := newSyncService(t, clientA, store, t1).Sync(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Bob disappears from the directory.
	t2 := t1.Add(time.Hour)
	clientB := &stubLDAPClient{
		users: []LDAPUser{
			{DN: "uid=alice,ou=users", Email: "alice@example.com", DisplayName: "Alice"},
		},
	}
	run, err := newSyncService(t, clientB, store, t2).Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if run.UsersDisabled != 1 {
		t.Errorf("expected 1 disabled, got %d", run.UsersDisabled)
	}
	if run.UsersUpdated != 1 {
		t.Errorf("expected alice updated, got %d", run.UsersUpdated)
	}
	if run.UsersCreated != 0 {
		t.Errorf("expected 0 created on second pass, got %d", run.UsersCreated)
	}

	bob := store.usersByDN["uid=bob,ou=users"]
	if bob == nil || bob.disabledAt == nil {
		t.Fatalf("bob should have disabled_at stamped, got: %+v", bob)
	}
	alice := store.usersByDN["uid=alice,ou=users"]
	if alice == nil || alice.disabledAt != nil {
		t.Errorf("alice should not be disabled: %+v", alice)
	}
}

func TestSync_RecyclesPreviouslyDisabledUser(t *testing.T) {
	t1 := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store := newMemLDAPSyncStore()
	bobOnly := &stubLDAPClient{
		users: []LDAPUser{{DN: "uid=bob,ou=users", Email: "bob@example.com"}},
	}
	if _, err := newSyncService(t, bobOnly, store, t1).Sync(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	// Bob disappears.
	empty := &stubLDAPClient{}
	t2 := t1.Add(time.Hour)
	if _, err := newSyncService(t, empty, store, t2).Sync(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	bob := store.usersByDN["uid=bob,ou=users"]
	if bob.disabledAt == nil {
		t.Fatal("bob should be disabled after second sync")
	}
	// Bob comes back.
	t3 := t2.Add(time.Hour)
	if _, err := newSyncService(t, bobOnly, store, t3).Sync(context.Background()); err != nil {
		t.Fatalf("third sync: %v", err)
	}
	if bob.disabledAt != nil {
		t.Errorf("bob should be re-enabled after returning to directory: %+v", bob)
	}
	if !bob.lastSyncedAt.Equal(t3) {
		t.Errorf("bob last_synced_at not advanced: %v", bob.lastSyncedAt)
	}
}

func TestSync_UnknownMemberDNDoesNotFail(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store := newMemLDAPSyncStore()
	client := &stubLDAPClient{
		users: []LDAPUser{{DN: "uid=alice,ou=users", Email: "alice@example.com"}},
		groups: []LDAPGroup{
			{DN: "cn=team,ou=groups", Name: "team", MemberDNs: []string{
				"uid=alice,ou=users",
				"uid=ghost,ou=users", // never synced
				"",                   // empty DN
				"uid=alice,ou=users", // duplicate
			}},
		},
	}
	run, err := newSyncService(t, client, store, now).Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if run.MembershipsAdded != 1 {
		t.Errorf("ghost / empty / duplicate should have been filtered, got %d added", run.MembershipsAdded)
	}
	members := membersOf(store, "cn=team,ou=groups")
	if !sameSet(members, []string{"user:alice@example.com"}) {
		t.Errorf("team members=%v", members)
	}
}

func TestSync_UpsertUserError_RecordsRunWithError(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store := newMemLDAPSyncStore()
	store.upsertUserErr = errors.New("db down")
	client := &stubLDAPClient{users: []LDAPUser{{DN: "uid=a,ou=users", Email: "a@example.com"}}}

	run, err := newSyncService(t, client, store, now).Sync(context.Background())
	if err == nil {
		t.Fatal("expected sync error")
	}
	if run == nil || run.ErrorMessage == "" {
		t.Errorf("expected populated run with ErrorMessage, got %+v", run)
	}
	if len(store.syncRuns) != 1 {
		t.Fatalf("expected 1 recorded run even on failure, got %d", len(store.syncRuns))
	}
	if !strings.Contains(store.syncRuns[0].ErrorMessage, "db down") {
		t.Errorf("recorded run error mismatch: %q", store.syncRuns[0].ErrorMessage)
	}
}

func TestSync_NoFactoryOrStore_ReturnsError(t *testing.T) {
	svc := &LDAPSyncService{}
	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("expected error from unwired service")
	}
}

func TestSync_GroupMembershipShrinksOnSecondPass(t *testing.T) {
	t1 := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store := newMemLDAPSyncStore()
	first := &stubLDAPClient{
		users: []LDAPUser{
			{DN: "uid=alice,ou=users", Email: "alice@example.com"},
			{DN: "uid=bob,ou=users", Email: "bob@example.com"},
		},
		groups: []LDAPGroup{
			{DN: "cn=team,ou=groups", Name: "team", MemberDNs: []string{
				"uid=alice,ou=users",
				"uid=bob,ou=users",
			}},
		},
	}
	if _, err := newSyncService(t, first, store, t1).Sync(context.Background()); err != nil {
		t.Fatalf("first: %v", err)
	}
	t2 := t1.Add(time.Hour)
	second := &stubLDAPClient{
		users: []LDAPUser{
			{DN: "uid=alice,ou=users", Email: "alice@example.com"},
			{DN: "uid=bob,ou=users", Email: "bob@example.com"},
		},
		groups: []LDAPGroup{
			{DN: "cn=team,ou=groups", Name: "team", MemberDNs: []string{
				"uid=alice,ou=users",
			}},
		},
	}
	run, err := newSyncService(t, second, store, t2).Sync(context.Background())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if run.MembershipsAdded != 0 {
		t.Errorf("nothing new should be added on second pass, got %d", run.MembershipsAdded)
	}
	members := membersOf(store, "cn=team,ou=groups")
	if !sameSet(members, []string{"user:alice@example.com"}) {
		t.Errorf("team should have shrunk to alice only, got %v", members)
	}
}

// --- LDAPSyncScheduler -----------------------------------------------------

func TestScheduler_RunsImmediatelyAndStops(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	store := newMemLDAPSyncStore()
	client := &stubLDAPClient{
		users: []LDAPUser{{DN: "uid=a,ou=users", Email: "a@example.com"}},
	}
	svc := newSyncService(t, client, store, now)
	sched := NewLDAPSyncScheduler(svc, time.Hour)
	sched.SetLogger(func(format string, v ...any) {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	// Wait briefly for the first run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		runs := len(store.syncRuns)
		store.mu.Unlock()
		if runs >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	store.mu.Lock()
	runs := len(store.syncRuns)
	store.mu.Unlock()
	if runs != 1 {
		t.Fatalf("expected 1 sync run within 2s, got %d", runs)
	}

	sched.Stop()

	// Stop is idempotent.
	sched.Stop()
}

func TestScheduler_DefaultIntervalApplied(t *testing.T) {
	sched := NewLDAPSyncScheduler(nil, 0)
	if sched.Interval() != DefaultLDAPSyncInterval {
		t.Errorf("Interval=%s, want %s", sched.Interval(), DefaultLDAPSyncInterval)
	}
}

// --- helpers ---------------------------------------------------------------

func membersOf(store *memLDAPSyncStore, groupDN string) []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	g := store.groupsByDN[groupDN]
	if g == nil {
		return nil
	}
	out := []string{}
	for id := range store.groupMembers[g.id] {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	w := append([]string(nil), want...)
	sort.Strings(w)
	g := append([]string(nil), got...)
	sort.Strings(g)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}
