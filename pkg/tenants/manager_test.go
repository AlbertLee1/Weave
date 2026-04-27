package tenants

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func TestTenantFromUser(t *testing.T) {
	if got := TenantFromUser(nil); got != "" {
		t.Errorf("nil user: want empty, got %q", got)
	}
	if got := TenantFromUser(&auth.User{}); got != "" {
		t.Errorf("user with no attrs: want empty, got %q", got)
	}
	u := &auth.User{Attributes: map[string]any{"realm": "acme"}}
	if got := TenantFromUser(u); got != "acme" {
		t.Errorf("want acme, got %q", got)
	}
}

func TestManager_CheckQPS_NoQuota(t *testing.T) {
	mgr := NewManager(NewMemoryStore())
	// Tenant with no quota row → allowed.
	if !mgr.CheckQPS(context.Background(), "anyone") {
		t.Errorf("expected allowed when no quota row")
	}
	// Empty tenant → allowed.
	if !mgr.CheckQPS(context.Background(), "") {
		t.Errorf("expected allowed for empty tenant")
	}
}

func TestManager_CheckQPS_BurstThenDeny(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxQPS: 10, Burst: 3})
	mgr := NewManager(store)

	allowed := 0
	for i := 0; i < 6; i++ {
		if mgr.CheckQPS(ctx, "acme") {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("want 3 allowed (burst), got %d", allowed)
	}
}

func TestManager_CheckObjectQuota(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxObjects: 100})
	mgr := NewManager(store)

	if !mgr.CheckObjectQuota(ctx, "acme", 99, 1) {
		t.Errorf("99+1=100 should fit")
	}
	if mgr.CheckObjectQuota(ctx, "acme", 100, 1) {
		t.Errorf("100+1=101 should exceed cap of 100")
	}
	// Tenant without quota row is unbounded.
	if !mgr.CheckObjectQuota(ctx, "ghost", 99999, 100000) {
		t.Errorf("tenant without quota should be unbounded")
	}
}

func TestManager_CheckStorageQuota(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxStorage: 1024 * 1024})
	mgr := NewManager(store)

	if !mgr.CheckStorageQuota(ctx, "acme", 0, 1024*1024) {
		t.Errorf("exact-fit storage should pass")
	}
	if mgr.CheckStorageQuota(ctx, "acme", 1, 1024*1024) {
		t.Errorf("1B over should be denied")
	}
}

func TestManager_NilSafety(t *testing.T) {
	var m *Manager
	ctx := context.Background()
	if !m.CheckQPS(ctx, "acme") {
		t.Errorf("nil manager should allow")
	}
	if !m.CheckObjectQuota(ctx, "acme", 10, 1) {
		t.Errorf("nil manager should allow")
	}
	if !m.CheckStorageQuota(ctx, "acme", 10, 1) {
		t.Errorf("nil manager should allow")
	}
	m.Reload() // must not panic
}

func TestManager_ReloadInvalidatesLimiters(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxQPS: 1, Burst: 1})
	mgr := NewManager(store)

	if !mgr.CheckQPS(ctx, "acme") {
		t.Fatalf("first call should pass")
	}
	if mgr.CheckQPS(ctx, "acme") {
		t.Fatalf("second call should be denied (burst exhausted)")
	}

	// Bump the quota and reload — next call should pass again.
	bigQPS := 100.0
	bigBurst := 10
	if err := store.UpdateQuota(ctx, "acme", QuotaUpdate{MaxQPS: &bigQPS, Burst: &bigBurst}); err != nil {
		t.Fatalf("update: %v", err)
	}
	mgr.Reload()
	if !mgr.CheckQPS(ctx, "acme") {
		t.Errorf("after reload + bigger quota, call should pass")
	}
}

func TestContextHelpers(t *testing.T) {
	if ManagerFromContext(context.Background()) != nil {
		t.Errorf("empty ctx should return nil manager")
	}
	mgr := NewManager(NewMemoryStore())
	ctx := WithManager(context.Background(), mgr)
	if ManagerFromContext(ctx) != mgr {
		t.Errorf("manager round-trip failed")
	}
}
