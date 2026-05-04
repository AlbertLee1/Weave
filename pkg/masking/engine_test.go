package masking

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func newMask(rid, otRID, prop string, rule MaskRule, applies AppliesTo) *ColumnMask {
	return &ColumnMask{
		RID:             rid,
		ObjectTypeRID:   otRID,
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
	tr, err := engine.Compile(context.Background(), user, "ri.ontology.main.object-type.Customer")
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
	_ = store.Create(ctx, newMask("m1", otRID, "ssn", MaskRuleHash, AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	admin := &auth.User{ID: "u:admin", Roles: []string{auth.RoleAdmin}}
	tr, err := engine.Compile(ctx, admin, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("admin should bypass, got %v", tr)
	}
}

func TestEngine_Compile_NilUser(t *testing.T) {
	store := NewMemoryStore()
	_ = store.Create(context.Background(), newMask("m1", "ot", "ssn", MaskRuleHash, AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(context.Background())

	tr, err := engine.Compile(context.Background(), nil, "ot")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("nil user should not produce masks, got %v", tr)
	}
}

// AppliesTo carries the ALLOWED identities. A mask with a non-empty
// AppliesTo that covers the caller MUST NOT be applied — callers in
// the allow list see the clear value.
func TestEngine_Compile_AppliesTo_AllowsCaller(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newMask("m1", otRID, "ssn", MaskRuleHash,
		AppliesTo{Roles: []string{"finance"}}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	// Caller holds the finance role → mask DOES NOT apply (clear view).
	finance := &auth.User{ID: "u:fin", Roles: []string{"finance"}}
	tr, err := engine.Compile(ctx, finance, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tr) != 0 {
		t.Fatalf("finance caller should see clear data, got %v", tr)
	}

	// Caller lacks the role → mask applies.
	outsider := &auth.User{ID: "u:ops", Roles: []string{"ops"}}
	tr, err = engine.Compile(ctx, outsider, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tr) != 1 {
		t.Fatalf("outsider should receive 1 mask, got %d (%v)", len(tr), tr)
	}
	if tr["ssn"] != MaskRuleHash {
		t.Fatalf("expected ssn mask=hash, got %v", tr["ssn"])
	}
}

// AppliesTo empty = mask applies to everyone (except admin bypass).
func TestEngine_Compile_EmptyAppliesTo_MasksEveryone(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newMask("m1", otRID, "ssn", MaskRuleRedact, AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	tr, err := engine.Compile(ctx, user, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr["ssn"] != MaskRuleRedact {
		t.Fatalf("expected ssn=redact, got %v", tr)
	}
}

// Multiple masks on different properties all compose into the same map.
func TestEngine_Compile_MultipleProperties(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newMask("m1", otRID, "ssn", MaskRuleHash, AppliesTo{}))
	_ = store.Create(ctx, newMask("m2", otRID, "email", MaskRulePartial, AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	tr, err := engine.Compile(ctx, user, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tr) != 2 {
		t.Fatalf("expected 2 transforms, got %d", len(tr))
	}
	if tr["ssn"] != MaskRuleHash || tr["email"] != MaskRulePartial {
		t.Fatalf("transforms mismatch: %v", tr)
	}
}

// Masks scoped to a different ObjectType do not leak across.
func TestEngine_Compile_ScopedByObjectType(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, newMask("m1", "ri.ontology.main.object-type.Customer", "ssn", MaskRuleHash, AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	// Different OT — no masks.
	tr, err := engine.Compile(ctx, user, "ri.ontology.main.object-type.Order")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr != nil {
		t.Fatalf("expected nil for unscoped OT, got %v", tr)
	}
}

func TestEngine_Compile_GroupScope(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newMask("m1", otRID, "ssn", MaskRuleHash,
		AppliesTo{Groups: []string{"finance"}}))
	engine := New(store, &stubGroupLookup{groups: map[string][]string{
		"u:alice": {"finance"},
		"u:bob":   {"ops"},
	}})
	_ = engine.Reload(ctx)

	// Finance group member is in allow list → no mask.
	alice := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
	tr, err := engine.Compile(ctx, alice, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tr) != 0 {
		t.Fatalf("alice (finance) should see clear data, got %v", tr)
	}
	// Non-member gets masked.
	bob := &auth.User{ID: "u:bob", Roles: []string{"viewer"}}
	tr, err = engine.Compile(ctx, bob, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if tr["ssn"] != MaskRuleHash {
		t.Fatalf("bob should be masked, got %v", tr)
	}
}

func TestApplyTransforms_MutatesSpecifiedKeysOnly(t *testing.T) {
	props := map[string]interface{}{
		"name":  "alice",
		"ssn":   "123-45-6789",
		"email": "alice@example.com",
	}
	transforms := map[string]MaskRule{
		"ssn":   MaskRuleRedact,
		"email": MaskRulePartial,
	}
	ApplyTransforms(props, transforms)

	if props["name"] != "alice" {
		t.Fatalf("name should be untouched, got %v", props["name"])
	}
	if props["ssn"] != "***" {
		t.Fatalf("ssn should be redacted to ***, got %v", props["ssn"])
	}
	if props["email"] == "alice@example.com" {
		t.Fatalf("email should be partially masked")
	}
}

func TestEngine_Size(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newMask("m1", otRID, "ssn", MaskRuleHash, AppliesTo{}))
	_ = store.Create(ctx, newMask("m2", otRID, "email", MaskRulePartial, AppliesTo{}))
	engine := New(store, nil)
	_ = engine.Reload(ctx)
	if got := engine.Size(otRID); got != 2 {
		t.Fatalf("Size: got %d, want 2", got)
	}
	if got := engine.Size("ri.other"); got != 0 {
		t.Fatalf("Size: unknown OT should be 0, got %d", got)
	}
}
