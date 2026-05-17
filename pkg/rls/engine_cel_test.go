package rls

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestUS487_Engine_EvaluateRowCEL_PRDExample exercises the literal PRD
// scenario: a CEL policy that joins user.dept with object.dept and
// gates by user.clearance >= object.level. The user attributes feed
// directly into the user.* binding via the userBindingFor flattening.
func TestUS487_Engine_EvaluateRowCEL_PRDExample(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Document"

	policy := &RowPolicy{
		RID:           "ri.rls.main.row-policy.us487-cel",
		ObjectTypeRID: otRID,
		CELExpression: `user.dept == object.dept && object.level <= user.clearance`,
		AppliesTo:     AppliesTo{Roles: []string{"reader"}},
	}
	if err := store.Create(ctx, policy); err != nil {
		t.Fatalf("Create: %v", err)
	}
	engine := New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	user := &auth.User{
		ID:    "user:alice",
		Roles: []string{"reader"},
		Attributes: map[string]any{
			"dept":      "eng",
			"clearance": 3,
		},
	}

	cases := []struct {
		name string
		row  map[string]any
		want bool
	}{
		{name: "same_dept_level_under_clearance", row: map[string]any{"dept": "eng", "level": 2}, want: true},
		{name: "same_dept_level_equal_clearance", row: map[string]any{"dept": "eng", "level": 3}, want: true},
		{name: "same_dept_level_over_clearance", row: map[string]any{"dept": "eng", "level": 4}, want: false},
		{name: "different_dept", row: map[string]any{"dept": "ops", "level": 1}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.EvaluateRowCEL(ctx, user, otRID, tc.row)
			if err != nil {
				t.Fatalf("EvaluateRowCEL: %v", err)
			}
			if got != tc.want {
				t.Fatalf("verdict = %v, want %v (row=%v)", got, tc.want, tc.row)
			}
		})
	}
}

// TestUS487_Engine_EvaluateRowCEL_NoCELPolicies_GateOpen verifies the
// "no CEL policy attached" path is permissive — the gate should not
// reject rows just because the CEL lane has nothing applicable.
func TestUS487_Engine_EvaluateRowCEL_NoCELPolicies_GateOpen(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	engine := New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	user := &auth.User{ID: "user:alice", Roles: []string{"reader"}}
	got, err := engine.EvaluateRowCEL(ctx, user, "ri.ontology.main.object-type.Order", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("EvaluateRowCEL: %v", err)
	}
	if !got {
		t.Fatalf("expected open gate (no CEL policies), got deny")
	}
}

// TestUS487_Engine_EvaluateRowCEL_RoleNotApplicable_GateOpen verifies
// that a CEL policy whose AppliesTo doesn't cover the caller is silently
// skipped — the gate stays open. Matches Engine.Compile's permissive
// semantics for inapplicable Bleve-side policies.
func TestUS487_Engine_EvaluateRowCEL_RoleNotApplicable_GateOpen(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Document"
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.eu-only",
		ObjectTypeRID: otRID,
		CELExpression: `object.region == "EU"`,
		AppliesTo:     AppliesTo{Roles: []string{"eu-reader"}},
	})
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:bob", Roles: []string{"us-reader"}}
	got, err := engine.EvaluateRowCEL(ctx, user, otRID, map[string]any{"region": "US"})
	if err != nil {
		t.Fatalf("EvaluateRowCEL: %v", err)
	}
	if !got {
		t.Fatalf("policy not applicable to caller: gate must stay open, got deny")
	}
}

// TestUS487_Engine_EvaluateRowCEL_AdminBypass mirrors the Compile-side
// admin bypass: admins see everything regardless of CEL gate.
func TestUS487_Engine_EvaluateRowCEL_AdminBypass(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Document"
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.restrictive",
		ObjectTypeRID: otRID,
		CELExpression: `false`, // always-deny, exposes the bypass path
		AppliesTo:     AppliesTo{Roles: []string{auth.RoleAdmin}},
	})
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	admin := &auth.User{ID: "user:root", Roles: []string{auth.RoleAdmin}}
	got, err := engine.EvaluateRowCEL(ctx, admin, otRID, map[string]any{})
	if err != nil {
		t.Fatalf("EvaluateRowCEL admin: %v", err)
	}
	if !got {
		t.Fatalf("admin must bypass CEL gate even when policy says false")
	}
}

// TestUS487_Engine_EvaluateRowCEL_ORAcrossPolicies verifies that when
// multiple CEL policies apply to the same caller, the row passes if ANY
// returns true (OR semantics, matching the Bleve-side disjunction).
func TestUS487_Engine_EvaluateRowCEL_ORAcrossPolicies(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Document"
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.eu",
		ObjectTypeRID: otRID,
		CELExpression: `object.region == "EU"`,
		AppliesTo:     AppliesTo{Roles: []string{"reader"}},
	})
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.owned",
		ObjectTypeRID: otRID,
		CELExpression: `object.owner == user.id`,
		AppliesTo:     AppliesTo{Roles: []string{"reader"}},
	})
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:alice", Roles: []string{"reader"}}

	// US region but owned by alice → second policy passes.
	got, err := engine.EvaluateRowCEL(ctx, user, otRID, map[string]any{"region": "US", "owner": "user:alice"})
	if err != nil {
		t.Fatalf("EvaluateRowCEL: %v", err)
	}
	if !got {
		t.Fatalf("OR semantics: at least one policy should accept, got deny")
	}

	// US region and owned by someone else → both fail.
	got, err = engine.EvaluateRowCEL(ctx, user, otRID, map[string]any{"region": "US", "owner": "user:bob"})
	if err != nil {
		t.Fatalf("EvaluateRowCEL: %v", err)
	}
	if got {
		t.Fatalf("both policies should fail; expected deny, got allow")
	}
}

// TestUS487_Engine_EvaluateRowCEL_BrokenCELQuarantined verifies that a
// broken CEL policy (parses but throws at eval time) makes the gate
// fail-closed for callers it applies to, while leaving unrelated CEL
// policies intact for other rows.
func TestUS487_Engine_EvaluateRowCEL_BrokenCELQuarantined(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Document"
	// References a field the row will not carry.
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.broken",
		ObjectTypeRID: otRID,
		CELExpression: `object.missing_field == "x"`,
		AppliesTo:     AppliesTo{Roles: []string{"reader"}},
	})
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:alice", Roles: []string{"reader"}}
	_, err := engine.EvaluateRowCEL(ctx, user, otRID, map[string]any{"region": "US"})
	if err == nil {
		t.Fatalf("missing field should surface a runtime error (fail-closed at caller)")
	}
}

// TestUS487_Engine_HasCELForObjectType reports correctly whether any
// policy on the OT carries CEL. OSS uses this to skip the per-row CEL
// pass when no policy needs it.
func TestUS487_Engine_HasCELForObjectType(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otA := "ri.ontology.main.object-type.Document"
	otB := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.cel-a",
		ObjectTypeRID: otA,
		CELExpression: `true`,
		AppliesTo:     AppliesTo{Roles: []string{"reader"}},
	})
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.nocel-b",
		ObjectTypeRID: otB,
		Predicate:     []byte(`{"type":"eq","field":"region","value":"EU"}`),
		AppliesTo:     AppliesTo{Roles: []string{"reader"}},
	})
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	if !engine.HasCELForObjectType(otA) {
		t.Fatalf("expected HasCELForObjectType(otA) = true")
	}
	if engine.HasCELForObjectType(otB) {
		t.Fatalf("expected HasCELForObjectType(otB) = false")
	}
	if engine.HasCELForObjectType("ri.ontology.main.object-type.Unknown") {
		t.Fatalf("expected HasCELForObjectType(Unknown) = false")
	}
}

// TestUS487_RowPolicy_Validate_CELOrPredicateRequired pins the contract
// that a CEL-only policy passes Validate even though Predicate is empty.
func TestUS487_RowPolicy_Validate_CELOrPredicateRequired(t *testing.T) {
	celOnly := &RowPolicy{
		RID:           "ri.rls.main.row-policy.cel-only",
		ObjectTypeRID: "ri.ontology.main.object-type.Document",
		CELExpression: `object.x == 1`,
	}
	if err := celOnly.Validate(); err != nil {
		t.Fatalf("CEL-only policy should pass Validate, got %v", err)
	}

	predOnly := &RowPolicy{
		RID:           "ri.rls.main.row-policy.pred-only",
		ObjectTypeRID: "ri.ontology.main.object-type.Document",
		Predicate:     []byte(`{"type":"eq","field":"x","value":1}`),
	}
	if err := predOnly.Validate(); err != nil {
		t.Fatalf("Predicate-only policy should pass Validate, got %v", err)
	}

	empty := &RowPolicy{
		RID:           "ri.rls.main.row-policy.empty",
		ObjectTypeRID: "ri.ontology.main.object-type.Document",
	}
	if err := empty.Validate(); err == nil {
		t.Fatalf("empty policy should fail Validate")
	}
}

// TestUS487_Engine_EvaluateRowCEL_NilUser_GateOpen mirrors the Compile-
// side contract that a nil user (anonymous read that middleware did not
// already reject) does not get blocked by CEL. The middleware is the
// place to reject anonymous; the engine stays permissive to avoid double
// negatives.
func TestUS487_Engine_EvaluateRowCEL_NilUser_GateOpen(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.cel",
		ObjectTypeRID: "ri.ontology.main.object-type.Document",
		CELExpression: `false`,
		AppliesTo:     AppliesTo{Roles: []string{"reader"}},
	})
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	got, err := engine.EvaluateRowCEL(ctx, nil, "ri.ontology.main.object-type.Document", map[string]any{})
	if err != nil {
		t.Fatalf("EvaluateRowCEL nil user: %v", err)
	}
	if !got {
		t.Fatalf("nil user must not be blocked by CEL gate")
	}
}
