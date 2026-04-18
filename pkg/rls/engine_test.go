package rls

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/liyang/weave/pkg/auth"
)

func newTestPolicy(t *testing.T, rid, otRID, predicate string, applies AppliesTo) *RowPolicy {
	t.Helper()
	return &RowPolicy{
		RID:           rid,
		ObjectTypeRID: otRID,
		Predicate:     json.RawMessage(predicate),
		AppliesTo:     applies,
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

func TestEngine_Compile_NoPolicies_ReturnsNil(t *testing.T) {
	store := NewMemoryStore()
	engine := New(store, nil)
	if err := engine.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	user := &auth.User{ID: "user:alice", Roles: []string{"editor"}}
	q, err := engine.Compile(context.Background(), user, "ri.ontology.main.object-type.Customer")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q != nil {
		t.Fatalf("expected nil query when no policies exist, got %T", q)
	}
}

func TestEngine_Compile_SinglePolicy_RoleMatch(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	policy := newTestPolicy(t, "ri.rls.main.row-policy.eu",
		otRID,
		`{"type":"eq","field":"region","value":"EU"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	)
	if err := store.Create(ctx, policy); err != nil {
		t.Fatalf("Create: %v", err)
	}
	engine := New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	user := &auth.User{ID: "user:alice", Roles: []string{"eu-reader"}}
	q, err := engine.Compile(ctx, user, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q == nil {
		t.Fatalf("expected non-nil query when role matches")
	}
	if _, ok := q.(*query.MatchQuery); !ok {
		t.Fatalf("expected *query.MatchQuery, got %T", q)
	}
}

func TestEngine_Compile_SinglePolicy_RoleNotMatched_ReturnsNil(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	policy := newTestPolicy(t, "ri.rls.main.row-policy.eu",
		otRID,
		`{"type":"eq","field":"region","value":"EU"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	)
	_ = store.Create(ctx, policy)
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:bob", Roles: []string{"us-reader"}}
	q, err := engine.Compile(ctx, user, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q != nil {
		t.Fatalf("expected nil query when role does not match, got %T", q)
	}
}

func TestEngine_Compile_MultiplePolicies_ORCombined(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.eu",
		otRID,
		`{"type":"eq","field":"region","value":"EU"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	))
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.owned",
		otRID,
		`{"type":"eq","field":"owner","value":"alice"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:alice", Roles: []string{"eu-reader"}}
	q, err := engine.Compile(ctx, user, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q == nil {
		t.Fatalf("expected non-nil query")
	}
	bq, ok := q.(*query.DisjunctionQuery)
	if !ok {
		t.Fatalf("expected *query.DisjunctionQuery, got %T", q)
	}
	if len(bq.Disjuncts) != 2 {
		t.Fatalf("expected 2 disjuncts, got %d", len(bq.Disjuncts))
	}
}

func TestEngine_Compile_GroupScope(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.marketing",
		otRID,
		`{"type":"eq","field":"department","value":"marketing"}`,
		AppliesTo{Groups: []string{"marketing"}},
	))
	gl := &stubGroupLookup{groups: map[string][]string{
		"user:alice": {"marketing", "ops"},
	}}
	engine := New(store, gl)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:alice", Roles: []string{"editor"}}
	q, err := engine.Compile(ctx, user, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q == nil {
		t.Fatalf("expected non-nil query via group membership")
	}

	// Bob is not in "marketing" group — policy should not apply.
	bob := &auth.User{ID: "user:bob", Roles: []string{"editor"}}
	q2, err := engine.Compile(ctx, bob, otRID)
	if err != nil {
		t.Fatalf("Compile bob: %v", err)
	}
	if q2 != nil {
		t.Fatalf("expected nil query for bob, got %T", q2)
	}
}

func TestEngine_Compile_UserScope(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.alice",
		otRID,
		`{"type":"eq","field":"owner","value":"alice"}`,
		AppliesTo{Users: []string{"user:alice"}},
	))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:alice"}
	q, err := engine.Compile(ctx, user, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q == nil {
		t.Fatalf("expected non-nil query for user-scoped policy")
	}
}

func TestEngine_Compile_AdminBypass(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.restrictive",
		otRID,
		`{"type":"eq","field":"region","value":"EU"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	admin := &auth.User{ID: "user:root", Roles: []string{auth.RoleAdmin}}
	q, err := engine.Compile(ctx, admin, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q != nil {
		t.Fatalf("admin must bypass row policies; got %T", q)
	}
}

func TestEngine_Compile_NilUser_ReturnsNil(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.r",
		otRID,
		`{"type":"eq","field":"region","value":"EU"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	q, err := engine.Compile(ctx, nil, otRID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if q != nil {
		t.Fatalf("expected nil query for nil user, got %T", q)
	}
}

func TestEngine_Compile_InvalidPredicate_ReturnsError(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	// Unsupported "bogus" clause type forces where.ConvertToBleveQuery to error.
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.bad",
		otRID,
		`{"type":"bogus","field":"x","value":"y"}`,
		AppliesTo{Roles: []string{"r"}},
	))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "u", Roles: []string{"r"}}
	_, err := engine.Compile(ctx, user, otRID)
	if err == nil {
		t.Fatalf("expected error compiling invalid predicate")
	}
}

func TestEngine_SetPolicy_InvalidatesCache(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	// Initially no policies → nil query.
	user := &auth.User{ID: "user:alice", Roles: []string{"eu-reader"}}
	q, _ := engine.Compile(ctx, user, otRID)
	if q != nil {
		t.Fatalf("expected nil initially")
	}
	// Add a policy and refresh.
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.eu",
		otRID,
		`{"type":"eq","field":"region","value":"EU"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	))
	_ = engine.Reload(ctx)
	q, _ = engine.Compile(ctx, user, otRID)
	if q == nil {
		t.Fatalf("expected non-nil query after Reload")
	}
}

func TestEngine_Compile_ScopedToObjectType(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	otA := "ri.ontology.main.object-type.Customer"
	otB := "ri.ontology.main.object-type.Order"
	_ = store.Create(ctx, newTestPolicy(t, "ri.rls.main.row-policy.eu",
		otA,
		`{"type":"eq","field":"region","value":"EU"}`,
		AppliesTo{Roles: []string{"eu-reader"}},
	))
	engine := New(store, nil)
	_ = engine.Reload(ctx)

	user := &auth.User{ID: "user:alice", Roles: []string{"eu-reader"}}
	qA, _ := engine.Compile(ctx, user, otA)
	if qA == nil {
		t.Fatalf("policy on Customer should apply")
	}
	qB, _ := engine.Compile(ctx, user, otB)
	if qB != nil {
		t.Fatalf("policy on Customer must NOT leak to Order, got %T", qB)
	}
}
