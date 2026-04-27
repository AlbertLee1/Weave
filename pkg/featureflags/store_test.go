package featureflags

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStore_CreateGetUpdateDelete(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	flag := &Flag{
		Name:        "new-ui",
		Description: "enable new UI",
		Enabled:     true,
		Realms:      []string{"main"},
		Users:       []string{"u1"},
	}
	if err := s.CreateFlag(ctx, flag); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}

	got, err := s.GetFlag(ctx, "new-ui")
	if err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	if got.Name != "new-ui" || !got.Enabled || len(got.Realms) != 1 || got.Realms[0] != "main" {
		t.Fatalf("GetFlag returned wrong value: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be stamped on create, got %+v", got)
	}

	// Duplicate create → error.
	if err := s.CreateFlag(ctx, &Flag{Name: "new-ui"}); !errors.Is(err, ErrFlagAlreadyExists) {
		t.Fatalf("expected ErrFlagAlreadyExists, got %v", err)
	}

	// Update: flip enabled, change description, change scopes.
	disabled := false
	desc := "turned off"
	upd := FlagUpdate{
		Description: &desc,
		Enabled:     &disabled,
		Realms:      &[]string{},
		Users:       &[]string{"u9"},
	}
	time.Sleep(time.Millisecond) // ensure updated_at monotonic on systems without sub-ms resolution
	if err := s.UpdateFlag(ctx, "new-ui", upd); err != nil {
		t.Fatalf("UpdateFlag: %v", err)
	}
	got, err = s.GetFlag(ctx, "new-ui")
	if err != nil {
		t.Fatalf("GetFlag after update: %v", err)
	}
	if got.Enabled || got.Description != "turned off" || len(got.Realms) != 0 || len(got.Users) != 1 || got.Users[0] != "u9" {
		t.Fatalf("UpdateFlag did not apply: %+v", got)
	}

	// Update missing → ErrFlagNotFound.
	if err := s.UpdateFlag(ctx, "does-not-exist", FlagUpdate{}); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("expected ErrFlagNotFound, got %v", err)
	}

	// List returns the entry.
	flags, err := s.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(flags) != 1 || flags[0].Name != "new-ui" {
		t.Fatalf("ListFlags returned %+v", flags)
	}

	// Delete then Get returns ErrFlagNotFound.
	if err := s.DeleteFlag(ctx, "new-ui"); err != nil {
		t.Fatalf("DeleteFlag: %v", err)
	}
	if _, err := s.GetFlag(ctx, "new-ui"); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("expected ErrFlagNotFound after delete, got %v", err)
	}
	if err := s.DeleteFlag(ctx, "new-ui"); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("expected ErrFlagNotFound on repeat delete, got %v", err)
	}
}

func TestMemoryStore_ListFlagsSortedByName(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	names := []string{"zebra", "apple", "mango"}
	for _, n := range names {
		if err := s.CreateFlag(ctx, &Flag{Name: n}); err != nil {
			t.Fatalf("CreateFlag %q: %v", n, err)
		}
	}
	flags, err := s.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(flags) != 3 {
		t.Fatalf("want 3 flags, got %d", len(flags))
	}
	want := []string{"apple", "mango", "zebra"}
	for i, f := range flags {
		if f.Name != want[i] {
			t.Fatalf("flags[%d].Name=%q want %q", i, f.Name, want[i])
		}
	}
}
