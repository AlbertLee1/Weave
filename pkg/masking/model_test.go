package masking

import (
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func TestColumnMask_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mask    *ColumnMask
		wantErr error
	}{
		{
			name: "valid hash",
			mask: &ColumnMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PropertyAPIName: "ssn",
				MaskRule:        MaskRuleHash,
			},
			wantErr: nil,
		},
		{
			name: "valid redact",
			mask: &ColumnMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PropertyAPIName: "ssn",
				MaskRule:        MaskRuleRedact,
			},
			wantErr: nil,
		},
		{
			name: "valid partial",
			mask: &ColumnMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PropertyAPIName: "ssn",
				MaskRule:        MaskRulePartial,
			},
			wantErr: nil,
		},
		{
			name: "missing ObjectTypeRID",
			mask: &ColumnMask{
				PropertyAPIName: "ssn",
				MaskRule:        MaskRuleHash,
			},
			wantErr: ErrObjectTypeRIDRequired,
		},
		{
			name: "missing property",
			mask: &ColumnMask{
				ObjectTypeRID: "ri.ontology.main.object-type.Customer",
				MaskRule:      MaskRuleHash,
			},
			wantErr: ErrPropertyRequired,
		},
		{
			name: "missing rule",
			mask: &ColumnMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PropertyAPIName: "ssn",
			},
			wantErr: ErrMaskRuleRequired,
		},
		{
			name: "unknown rule",
			mask: &ColumnMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PropertyAPIName: "ssn",
				MaskRule:        "bogus",
			},
			wantErr: ErrUnknownMaskRule,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mask.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate(): err=%v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAppliesTo_IsApplicable(t *testing.T) {
	user := &auth.User{ID: "user:alice@ex.com", Email: "alice@ex.com", Roles: []string{"editor"}}
	tests := []struct {
		name       string
		applies    AppliesTo
		userGroups []string
		want       bool
	}{
		{
			name:    "empty never applies",
			applies: AppliesTo{},
			want:    false,
		},
		{
			name:    "role match",
			applies: AppliesTo{Roles: []string{"editor"}},
			want:    true,
		},
		{
			name:    "role miss",
			applies: AppliesTo{Roles: []string{"admin"}},
			want:    false,
		},
		{
			name:       "group match",
			applies:    AppliesTo{Groups: []string{"finance"}},
			userGroups: []string{"finance", "ops"},
			want:       true,
		},
		{
			name:       "group miss",
			applies:    AppliesTo{Groups: []string{"finance"}},
			userGroups: []string{"ops"},
			want:       false,
		},
		{
			name:    "user id match",
			applies: AppliesTo{Users: []string{"user:alice@ex.com"}},
			want:    true,
		},
		{
			name:    "email match",
			applies: AppliesTo{Users: []string{"alice@ex.com"}},
			want:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.applies.IsApplicable(user, tc.userGroups)
			if got != tc.want {
				t.Fatalf("IsApplicable()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestAppliesTo_IsApplicable_NilUser(t *testing.T) {
	a := AppliesTo{Roles: []string{"editor"}}
	if a.IsApplicable(nil, nil) {
		t.Fatalf("nil user must not match any AppliesTo")
	}
}
