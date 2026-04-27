package featureflags

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func TestManager_HasFlag(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.CreateFlag(ctx, &Flag{Name: "global-on", Enabled: true})
	_ = store.CreateFlag(ctx, &Flag{Name: "off", Enabled: false})
	_ = store.CreateFlag(ctx, &Flag{Name: "per-user", Enabled: true, Users: []string{"u1"}})
	_ = store.CreateFlag(ctx, &Flag{Name: "per-realm", Enabled: true, Realms: []string{"main"}})

	mgr := NewManager(store)

	cases := []struct {
		name string
		flag string
		user *auth.User
		want bool
	}{
		{"global on for any user", "global-on", &auth.User{ID: "u1"}, true},
		{"global on for nil user", "global-on", nil, true},
		{"disabled always false", "off", &auth.User{ID: "u1"}, false},
		{"per-user allowlist match", "per-user", &auth.User{ID: "u1"}, true},
		{"per-user allowlist miss", "per-user", &auth.User{ID: "u2"}, false},
		{"per-realm match", "per-realm",
			&auth.User{ID: "u1", Attributes: map[string]any{"realm": "main"}}, true},
		{"per-realm miss", "per-realm",
			&auth.User{ID: "u1", Attributes: map[string]any{"realm": "other"}}, false},
		{"unknown flag is false", "missing", &auth.User{ID: "u1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mgr.HasFlag(ctx, tc.flag, tc.user)
			if got != tc.want {
				t.Fatalf("HasFlag(%q, %+v) = %v; want %v", tc.flag, tc.user, got, tc.want)
			}
		})
	}
}

func TestManager_NilSafety(t *testing.T) {
	var mgr *Manager
	if mgr.HasFlag(context.Background(), "x", &auth.User{ID: "u1"}) {
		t.Fatal("nil manager must always return false")
	}

	mgr = NewManager(nil)
	if mgr.HasFlag(context.Background(), "x", &auth.User{ID: "u1"}) {
		t.Fatal("manager with nil store must always return false")
	}
}

func TestContextHelpers(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.CreateFlag(ctx, &Flag{Name: "on", Enabled: true})
	mgr := NewManager(store)

	// Without a manager in context, HasFlag returns false.
	if HasFlag(ctx, "on", &auth.User{ID: "u1"}) {
		t.Fatal("HasFlag on bare context must return false")
	}

	ctxWithMgr := WithManager(ctx, mgr)
	if !HasFlag(ctxWithMgr, "on", &auth.User{ID: "u1"}) {
		t.Fatal("HasFlag on manager-carrying context must return true for enabled flag")
	}

	// ManagerFromContext returns the wired manager.
	got := ManagerFromContext(ctxWithMgr)
	if got == nil || got != mgr {
		t.Fatalf("ManagerFromContext returned %v; want %v", got, mgr)
	}
	if ManagerFromContext(ctx) != nil {
		t.Fatal("ManagerFromContext on bare ctx should be nil")
	}
}
