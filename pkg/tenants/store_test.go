package tenants

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	q := &Quota{Tenant: "acme", MaxObjects: 1000, MaxStorage: 1024 * 1024, MaxQPS: 50, Burst: 100}
	if err := s.CreateQuota(ctx, q); err != nil {
		t.Fatalf("create: %v", err)
	}
	if q.CreatedAt.IsZero() || q.UpdatedAt.IsZero() {
		t.Errorf("expected timestamps stamped on create")
	}

	if err := s.CreateQuota(ctx, &Quota{Tenant: "acme"}); !errors.Is(err, ErrQuotaAlreadyExists) {
		t.Errorf("duplicate create: expected ErrQuotaAlreadyExists, got %v", err)
	}

	got, err := s.GetQuota(ctx, "acme")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MaxObjects != 1000 || got.MaxQPS != 50 {
		t.Errorf("get returned wrong row: %+v", got)
	}

	maxObj := int64(2000)
	if err := s.UpdateQuota(ctx, "acme", QuotaUpdate{MaxObjects: &maxObj}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetQuota(ctx, "acme")
	if got.MaxObjects != 2000 {
		t.Errorf("update did not persist: %d", got.MaxObjects)
	}
	if got.MaxStorage != 1024*1024 {
		t.Errorf("update touched a non-targeted field: %d", got.MaxStorage)
	}

	if err := s.DeleteQuota(ctx, "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetQuota(ctx, "acme"); !errors.Is(err, ErrQuotaNotFound) {
		t.Errorf("get after delete: expected ErrQuotaNotFound, got %v", err)
	}
}

func TestMemoryStore_ListSorted(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	for _, name := range []string{"zeta", "alpha", "mike"} {
		_ = s.CreateQuota(ctx, &Quota{Tenant: name})
	}
	out, err := s.ListQuotas(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"alpha", "mike", "zeta"}
	for i, q := range out {
		if q.Tenant != want[i] {
			t.Errorf("list[%d]: want %q, got %q", i, want[i], q.Tenant)
		}
	}
}

func TestMemoryStore_UpdateMissing(t *testing.T) {
	if err := NewMemoryStore().UpdateQuota(context.Background(), "ghost", QuotaUpdate{}); !errors.Is(err, ErrQuotaNotFound) {
		t.Errorf("expected ErrQuotaNotFound, got %v", err)
	}
}
