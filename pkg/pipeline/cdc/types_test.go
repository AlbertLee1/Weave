package cdc_test

import (
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/pipeline/cdc"
)

func TestTableMappingValidate_Empty(t *testing.T) {
	cases := []struct {
		name string
		m    cdc.TableMapping
		want string
	}{
		{
			name: "missing schema",
			m: cdc.TableMapping{
				Table:             "orders",
				OntologyAPIName:   "northwind",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id"},
			},
			want: "Schema",
		},
		{
			name: "missing table",
			m: cdc.TableMapping{
				Schema:            "public",
				OntologyAPIName:   "northwind",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id"},
			},
			want: "Table",
		},
		{
			name: "missing ontology",
			m: cdc.TableMapping{
				Schema:            "public",
				Table:             "orders",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id"},
			},
			want: "OntologyAPIName",
		},
		{
			name: "missing object type",
			m: cdc.TableMapping{
				Schema:            "public",
				Table:             "orders",
				OntologyAPIName:   "northwind",
				PrimaryKeyColumns: []string{"id"},
			},
			want: "ObjectType",
		},
		{
			name: "missing primary key",
			m: cdc.TableMapping{
				Schema:          "public",
				Table:           "orders",
				OntologyAPIName: "northwind",
				ObjectType:      "Order",
			},
			want: "PrimaryKeyColumns",
		},
		{
			name: "blank pk column",
			m: cdc.TableMapping{
				Schema:            "public",
				Table:             "orders",
				OntologyAPIName:   "northwind",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id", " "},
			},
			want: "PrimaryKeyColumns[1]",
		},
		{
			name: "blank property target",
			m: cdc.TableMapping{
				Schema:            "public",
				Table:             "orders",
				OntologyAPIName:   "northwind",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id"},
				PropertyColumns:   map[string]string{"name": " "},
			},
			want: "PropertyColumns[\"name\"]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q missing substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestTableMappingValidate_OK(t *testing.T) {
	m := cdc.TableMapping{
		Schema:            "public",
		Table:             "orders",
		OntologyAPIName:   "northwind",
		ObjectType:        "Order",
		PrimaryKeyColumns: []string{"id"},
		PropertyColumns:   map[string]string{"customer_id": "customerId"},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := m.Key(); got != "public.orders" {
		t.Fatalf("Key()=%q want public.orders", got)
	}
}

func TestConfigValidate_DuplicateKey(t *testing.T) {
	c := &cdc.Config{
		Tables: []cdc.TableMapping{
			{
				Schema:            "public",
				Table:             "orders",
				OntologyAPIName:   "northwind",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id"},
			},
			{
				Schema:            "public",
				Table:             "orders",
				OntologyAPIName:   "northwind",
				ObjectType:        "OrderRetro",
				PrimaryKeyColumns: []string{"id"},
			},
		},
	}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestConfigLookup(t *testing.T) {
	c := &cdc.Config{
		Tables: []cdc.TableMapping{
			{
				Schema:            "public",
				Table:             "orders",
				OntologyAPIName:   "northwind",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id"},
			},
		},
	}
	if _, ok := c.Lookup("public", "orders"); !ok {
		t.Fatalf("expected lookup hit")
	}
	if _, ok := c.Lookup("public", "missing"); ok {
		t.Fatalf("expected lookup miss")
	}
	var nilCfg *cdc.Config
	if _, ok := nilCfg.Lookup("public", "orders"); ok {
		t.Fatalf("nil Config lookup should be miss")
	}
}

func TestPrimaryKeyFor_SingleAndComposite(t *testing.T) {
	single := cdc.TableMapping{PrimaryKeyColumns: []string{"id"}}
	pk, err := single.PrimaryKeyFor(map[string]string{"id": "10248"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pk != "10248" {
		t.Fatalf("single pk=%q want 10248", pk)
	}

	composite := cdc.TableMapping{PrimaryKeyColumns: []string{"order_id", "product_id"}}
	pk, err = composite.PrimaryKeyFor(map[string]string{"order_id": "10248", "product_id": "11"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pk != "10248:11" {
		t.Fatalf("composite pk=%q want 10248:11", pk)
	}

	if _, err := composite.PrimaryKeyFor(map[string]string{"order_id": "10248"}); err == nil {
		t.Fatalf("expected missing-column error")
	}
	empty := cdc.TableMapping{}
	if _, err := empty.PrimaryKeyFor(nil); err == nil {
		t.Fatalf("expected error for empty PK columns")
	}
}
