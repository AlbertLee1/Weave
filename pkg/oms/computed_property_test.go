package oms

import (
	"testing"
	"time"
)

func TestComputedAggregation_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		agg     ComputedAggregation
		wantErr bool
	}{
		{"count ok", ComputedAggregation{Type: "count"}, false},
		{"sum needs field", ComputedAggregation{Type: "sum"}, true},
		{"sum with field", ComputedAggregation{Type: "sum", Field: "amount"}, false},
		{"avg with field", ComputedAggregation{Type: "avg", Field: "amount"}, false},
		{"min needs field", ComputedAggregation{Type: "min"}, true},
		{"max with field", ComputedAggregation{Type: "max", Field: "price"}, false},
		{"uppercase count", ComputedAggregation{Type: "COUNT"}, false},
		{"unknown metric", ComputedAggregation{Type: "median", Field: "x"}, true},
		{"empty", ComputedAggregation{}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.agg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestComputedProperty_CacheTTL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cp   ComputedProperty
		want time.Duration
	}{
		{"default when zero", ComputedProperty{}, DefaultComputedPropertyCacheTTL},
		{"explicit 30s", ComputedProperty{CacheTTLSeconds: 30}, 30 * time.Second},
		{"explicit 0 (negative guard)", ComputedProperty{CacheTTLSeconds: -5}, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.cp.CacheTTL(); got != tc.want {
				t.Fatalf("CacheTTL() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestComputedProperty_Validate(t *testing.T) {
	t.Parallel()
	base := ComputedProperty{
		RID:           "ri.ontology.main.computed-property.cp1",
		ObjectTypeRID: "ri.ontology.main.object-type.customer",
		APIName:       "orderCount",
		SourceLinkRID: "ri.ontology.main.link-type.customer-orders",
		Aggregation:   ComputedAggregation{Type: "count"},
	}

	if err := base.Validate(); err != nil {
		t.Fatalf("base valid row rejected: %v", err)
	}

	missing := []ComputedProperty{
		func() ComputedProperty { c := base; c.RID = ""; return c }(),
		func() ComputedProperty { c := base; c.ObjectTypeRID = ""; return c }(),
		func() ComputedProperty { c := base; c.APIName = ""; return c }(),
		func() ComputedProperty { c := base; c.SourceLinkRID = ""; return c }(),
		func() ComputedProperty {
			c := base
			c.Aggregation = ComputedAggregation{Type: "sum"}
			return c
		}(),
	}
	for i, cp := range missing {
		if err := cp.Validate(); err == nil {
			t.Errorf("case %d: expected validation error, got nil", i)
		}
	}
}
