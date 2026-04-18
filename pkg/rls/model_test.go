package rls

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func TestRowPolicy_Validate(t *testing.T) {
	validPredicate := json.RawMessage(`{"type":"eq","field":"region","value":"EU"}`)

	tests := []struct {
		name    string
		p       *RowPolicy
		wantErr error
	}{
		{
			name: "valid",
			p: &RowPolicy{
				RID:           "ri.rls.main.row-policy.eu-only",
				ObjectTypeRID: "ri.ontology.main.object-type.Customer",
				Predicate:     validPredicate,
				AppliesTo:     AppliesTo{Roles: []string{"eu-reader"}},
			},
			wantErr: nil,
		},
		{
			name: "missing object_type_rid",
			p: &RowPolicy{
				RID:       "ri.rls.main.row-policy.bad",
				Predicate: validPredicate,
				AppliesTo: AppliesTo{Roles: []string{"r"}},
			},
			wantErr: ErrObjectTypeRIDRequired,
		},
		{
			name: "missing predicate",
			p: &RowPolicy{
				RID:           "ri.rls.main.row-policy.bad",
				ObjectTypeRID: "ri.ontology.main.object-type.Customer",
				AppliesTo:     AppliesTo{Roles: []string{"r"}},
			},
			wantErr: ErrPredicateRequired,
		},
		{
			name: "empty appliesTo is allowed (applies to nobody)",
			p: &RowPolicy{
				RID:           "ri.rls.main.row-policy.none",
				ObjectTypeRID: "ri.ontology.main.object-type.Customer",
				Predicate:     validPredicate,
				AppliesTo:     AppliesTo{},
			},
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAppliesTo_IsApplicable(t *testing.T) {
	user := &auth.User{
		ID:    "user:alice@ex.com",
		Email: "alice@ex.com",
		Roles: []string{"editor", "eu-reader"},
	}
	userGroups := []string{"marketing", "eu"}

	tests := []struct {
		name  string
		scope AppliesTo
		want  bool
	}{
		{"empty - no match", AppliesTo{}, false},
		{"role match", AppliesTo{Roles: []string{"eu-reader"}}, true},
		{"role no match", AppliesTo{Roles: []string{"admin"}}, false},
		{"group match", AppliesTo{Groups: []string{"eu"}}, true},
		{"group no match", AppliesTo{Groups: []string{"legal"}}, false},
		{"user id match", AppliesTo{Users: []string{"user:alice@ex.com"}}, true},
		{"user id no match", AppliesTo{Users: []string{"user:bob@ex.com"}}, false},
		{"email match via users", AppliesTo{Users: []string{"alice@ex.com"}}, true},
		{"union: role no match + group match", AppliesTo{Roles: []string{"admin"}, Groups: []string{"eu"}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.scope.IsApplicable(user, userGroups)
			if got != tc.want {
				t.Fatalf("IsApplicable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAppliesTo_IsApplicable_NilUser(t *testing.T) {
	scope := AppliesTo{Roles: []string{"admin"}, Users: []string{"x"}, Groups: []string{"g"}}
	if scope.IsApplicable(nil, nil) {
		t.Fatalf("nil user must never match a scope")
	}
}
