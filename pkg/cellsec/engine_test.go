package cellsec

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/masking"
)

func newCellMask(rid, otRID, pk, prop string, rule masking.MaskRule, applies masking.AppliesTo) *CellMask {
	return &CellMask{
		RID:             rid,
		ObjectTypeRID:   otRID,
		PrimaryKey:      pk,
		PropertyAPIName: prop,
		MaskRule:        rule,
		AppliesTo:       applies,
	}
}

type stubGroupLookup struct {
	groups map[string][]string
}

func (s *stubGroupLookup) UserGroups(_ context.Context, userID string) ([]string, error) {
	if s == nil || s.groups == nil {
		return nil, nil
	}
	return s.groups[userID], nil
}

func TestEngine_Compile_NoMasks_ReturnsNil(t *testing.T) {
	engine := New(NewMemoryStore(), nil)
	if err := engine.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	user := &auth.User{ID: "u:alice", Roles: []string{"editor"}}
	tr, err := engine.Compile(context.Background(), user, "ri.ontology.main.object-type.Customer", "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("expected nil transforms when no masks exist, got %v", tr)
	}
}

func TestEngine_Compile_AdminBypass(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleHash, masking.AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	admin := &auth.User{ID: "u:admin", Roles: []string{auth.RoleAdmin}}
	tr, err := engine.Compile(ctx, admin, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("admin should bypass, got %v", tr)
	}
}

func TestEngine_Compile_NilUser(t *testing.T) {
	store := NewMemoryStore()
	_ = store.Create(context.Background(), newCellMask("m1", "ot", "c-1", "ssn", masking.MaskRuleHash, masking.AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(context.Background())

	tr, err := engine.Compile(context.Background(), nil, "ot", "c-1")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("nil user should not produce masks, got %v", tr)
	}
}

// Cell mask scoped to pk-A must NOT leak to pk-B on the same OT.
func TestEngine_Compile_ScopedByPrimaryKey(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleRedact, masking.AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	// Targeted row → masked.
	tr, err := engine.Compile(ctx, user, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr["ssn"] != masking.MaskRuleRedact {
		t.Fatalf("expected ssn=redact for c-100, got %v", tr)
	}
	// Different PK → no mask.
	tr, err = engine.Compile(ctx, user, otRID, "c-200")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("expected nil for unscoped PK, got %v", tr)
	}
}

// Cell mask scoped to ObjectType A must NOT leak to ObjectType B.
func TestEngine_Compile_ScopedByObjectType(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, newCellMask("m1", "ri.ontology.main.object-type.Customer", "c-100", "ssn", masking.MaskRuleHash, masking.AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	tr, err := engine.Compile(ctx, user, "ri.ontology.main.object-type.Order", "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("expected nil for unscoped OT, got %v", tr)
	}
}

// AppliesTo carries the ALLOWED identities. A mask with a non-empty
// AppliesTo that covers the caller MUST NOT be applied.
func TestEngine_Compile_AppliesTo_AllowsCaller(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleHash,
		masking.AppliesTo{Roles: []string{"finance"}}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	finance := &auth.User{ID: "u:fin", Roles: []string{"finance"}}
	tr, err := engine.Compile(ctx, finance, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tr) != 0 {
		t.Fatalf("finance caller should see clear data, got %v", tr)
	}

	outsider := &auth.User{ID: "u:ops", Roles: []string{"ops"}}
	tr, err = engine.Compile(ctx, outsider, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr["ssn"] != masking.MaskRuleHash {
		t.Fatalf("outsider should be hash-masked, got %v", tr)
	}
}

// Empty AppliesTo ⇒ mask applies to everyone (except admin).
func TestEngine_Compile_EmptyAppliesTo_MasksEveryone(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleRedact, masking.AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	tr, err := engine.Compile(ctx, user, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr["ssn"] != masking.MaskRuleRedact {
		t.Fatalf("expected ssn=redact, got %v", tr)
	}
}

// Multiple masks on the same cell across distinct properties compose.
func TestEngine_Compile_MultipleProperties(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleHash, masking.AppliesTo{}))
	_ = store.Create(ctx, newCellMask("m2", otRID, "c-100", "email", masking.MaskRulePartial, masking.AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	tr, err := engine.Compile(ctx, user, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tr) != 2 {
		t.Fatalf("expected 2 transforms, got %d (%v)", len(tr), tr)
	}
	if tr["ssn"] != masking.MaskRuleHash || tr["email"] != masking.MaskRulePartial {
		t.Fatalf("transforms mismatch: %v", tr)
	}
}

func TestEngine_Compile_GroupScope(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleHash,
		masking.AppliesTo{Groups: []string{"finance"}}))
	engine := New(store, &stubGroupLookup{groups: map[string][]string{
		"u:alice": {"finance"},
		"u:bob":   {"ops"},
	}})
	_ = engine.Reload(ctx)

	alice := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	tr, err := engine.Compile(ctx, alice, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tr) != 0 {
		t.Fatalf("alice (finance) should see clear, got %v", tr)
	}
	bob := &auth.User{ID: "u:bob", Roles: []string{"viewer"}}
	tr, err = engine.Compile(ctx, bob, otRID, "c-100")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr["ssn"] != masking.MaskRuleHash {
		t.Fatalf("bob should be hash-masked, got %v", tr)
	}
}

func TestEngine_Size(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleHash, masking.AppliesTo{}))
	_ = store.Create(ctx, newCellMask("m2", otRID, "c-100", "email", masking.MaskRulePartial, masking.AppliesTo{}))
	_ = store.Create(ctx, newCellMask("m3", otRID, "c-200", "ssn", masking.MaskRuleHash, masking.AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	if got := engine.Size(otRID); got != 3 {
		t.Fatalf("Size: got %d, want 3", got)
	}
	if got := engine.Size("ri.other"); got != 0 {
		t.Fatalf("Size: unknown OT should be 0, got %d", got)
	}
}

func TestEngine_SetMasks(t *testing.T) {
	engine := New(NewMemoryStore(), nil)
	otRID := "ri.ontology.main.object-type.Customer"

	engine.SetMasks(otRID, "c-100", []*CellMask{
		newCellMask("m1", otRID, "c-100", "ssn", masking.MaskRuleHash, masking.AppliesTo{}),
	})
	if engine.Size(otRID) != 1 {
		t.Fatalf("expected 1 cached mask after SetMasks, got %d", engine.Size(otRID))
	}
	// Empty slice drops the entry.
	engine.SetMasks(otRID, "c-100", nil)
	if engine.Size(otRID) != 0 {
		t.Fatalf("expected 0 after clearing, got %d", engine.Size(otRID))
	}
}
