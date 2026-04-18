package cellsec

import (
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/masking"
)

func TestCellMask_Validate(t *testing.T) {
	cases := []struct {
		name string
		m    *CellMask
		want error
	}{
		{
			name: "valid",
			m: &CellMask{
				RID:             "ri.cellsec.main.cell-mask.1",
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PrimaryKey:      "c-100",
				PropertyAPIName: "ssn",
				MaskRule:        masking.MaskRuleHash,
			},
			want: nil,
		},
		{
			name: "nil receiver",
			m:    nil,
			want: ErrObjectTypeRIDRequired,
		},
		{
			name: "missing objectTypeRid",
			m: &CellMask{
				PrimaryKey:      "c-100",
				PropertyAPIName: "ssn",
				MaskRule:        masking.MaskRuleHash,
			},
			want: ErrObjectTypeRIDRequired,
		},
		{
			name: "missing primaryKey",
			m: &CellMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PropertyAPIName: "ssn",
				MaskRule:        masking.MaskRuleHash,
			},
			want: ErrPrimaryKeyRequired,
		},
		{
			name: "missing propertyApiName",
			m: &CellMask{
				ObjectTypeRID: "ri.ontology.main.object-type.Customer",
				PrimaryKey:    "c-100",
				MaskRule:      masking.MaskRuleHash,
			},
			want: ErrPropertyRequired,
		},
		{
			name: "missing maskRule",
			m: &CellMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PrimaryKey:      "c-100",
				PropertyAPIName: "ssn",
			},
			want: ErrMaskRuleRequired,
		},
		{
			name: "unknown maskRule",
			m: &CellMask{
				ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
				PrimaryKey:      "c-100",
				PropertyAPIName: "ssn",
				MaskRule:        masking.MaskRule("shrug"),
			},
			want: ErrUnknownMaskRule,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.m.Validate()
			if !errors.Is(got, tc.want) {
				t.Fatalf("Validate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAppliesTo_IsApplicable_Reused(t *testing.T) {
	// Sanity: cellsec reuses masking.AppliesTo so the semantics are already
	// validated in pkg/masking. This smoke test just verifies the struct
	// reuse compiles and runs through CellMask.
	a := masking.AppliesTo{Roles: []string{"finance"}}
	fin := &auth.User{ID: "u:fin", Roles: []string{"finance"}}
	ops := &auth.User{ID: "u:ops", Roles: []string{"ops"}}
	if !a.IsApplicable(fin, nil) {
		t.Fatalf("finance should match")
	}
	if a.IsApplicable(ops, nil) {
		t.Fatalf("ops should not match")
	}
}
