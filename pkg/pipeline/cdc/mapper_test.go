package cdc_test

import (
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/pipeline/cdc"
)

func newOrdersMapping() cdc.TableMapping {
	return cdc.TableMapping{
		Schema:            "public",
		Table:             "orders",
		OntologyAPIName:   "northwind",
		ObjectType:        "Order",
		PrimaryKeyColumns: []string{"id"},
		PropertyColumns: map[string]string{
			"customer_id": "customerId",
			"total":       "total",
			"shipped_at":  "shippedAt",
		},
	}
}

func TestEventToEdit_Insert(t *testing.T) {
	ev := &cdc.ChangeEvent{
		Op:     cdc.ChangeOpInsert,
		Schema: "public",
		Table:  "orders",
		After: map[string]string{
			"id":          "10248",
			"customer_id": "ALFKI",
			"total":       "440.00",
			"shipped_at":  "2026-04-28",
			"private":     "ignored",
		},
		CommitTime: time.Now(),
	}
	edit, err := cdc.EventToEdit(ev, newOrdersMapping())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edit.Type != funnel.EditTypeCreate {
		t.Fatalf("Type=%s want CREATE", edit.Type)
	}
	if edit.ObjectType != "Order" || edit.PrimaryKey != "10248" {
		t.Fatalf("ObjectType/PK got %q/%q", edit.ObjectType, edit.PrimaryKey)
	}
	if edit.Source != funnel.EditSourceIngest {
		t.Fatalf("Source=%s want ingest", edit.Source)
	}
	if got := edit.Properties["customerId"]; got != "ALFKI" {
		t.Fatalf("customerId=%v want ALFKI", got)
	}
	if got := edit.Properties["total"]; got != "440.00" {
		t.Fatalf("total=%v want 440.00", got)
	}
	if _, ok := edit.Properties["private"]; ok {
		t.Fatalf("expected unmapped column to be dropped")
	}
}

func TestEventToEdit_UpdateDropsNullByDefault(t *testing.T) {
	mapping := newOrdersMapping()
	ev := &cdc.ChangeEvent{
		Op:     cdc.ChangeOpUpdate,
		Schema: "public",
		Table:  "orders",
		Before: map[string]string{"id": "10248"},
		After: map[string]string{
			"id":          "10248",
			"customer_id": "ALFKI",
			"shipped_at":  nullValue(),
		},
	}
	edit, err := cdc.EventToEdit(ev, mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edit.Type != funnel.EditTypeModify {
		t.Fatalf("Type=%s want MODIFY", edit.Type)
	}
	if _, ok := edit.Properties["shippedAt"]; ok {
		t.Fatalf("expected nullable shippedAt to be dropped")
	}
	if edit.Properties["customerId"] != "ALFKI" {
		t.Fatalf("customerId not propagated: %#v", edit.Properties)
	}
}

func TestEventToEdit_UpdateIncludeNulls(t *testing.T) {
	mapping := newOrdersMapping()
	mapping.IncludeNullProperties = true
	ev := &cdc.ChangeEvent{
		Op:     cdc.ChangeOpUpdate,
		Schema: "public",
		Table:  "orders",
		After: map[string]string{
			"id":         "10248",
			"shipped_at": nullValue(),
		},
	}
	edit, err := cdc.EventToEdit(ev, mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := edit.Properties["shippedAt"]
	if !ok {
		t.Fatalf("expected shippedAt to be present")
	}
	if v != nil {
		t.Fatalf("shippedAt=%v want nil", v)
	}
}

func TestEventToEdit_Delete(t *testing.T) {
	mapping := newOrdersMapping()
	ev := &cdc.ChangeEvent{
		Op:     cdc.ChangeOpDelete,
		Schema: "public",
		Table:  "orders",
		Before: map[string]string{"id": "10248"},
	}
	edit, err := cdc.EventToEdit(ev, mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edit.Type != funnel.EditTypeDelete {
		t.Fatalf("Type=%s want DELETE", edit.Type)
	}
	if edit.PrimaryKey != "10248" {
		t.Fatalf("PrimaryKey=%q want 10248", edit.PrimaryKey)
	}
	if edit.Properties != nil {
		t.Fatalf("DELETE edit should not carry properties: %#v", edit.Properties)
	}
}

func TestEventToEdit_DeleteFallsBackToAfter(t *testing.T) {
	mapping := newOrdersMapping()
	ev := &cdc.ChangeEvent{
		Op:     cdc.ChangeOpDelete,
		Schema: "public",
		Table:  "orders",
		After:  map[string]string{"id": "10248"},
	}
	edit, err := cdc.EventToEdit(ev, mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edit.PrimaryKey != "10248" {
		t.Fatalf("PrimaryKey=%q want 10248", edit.PrimaryKey)
	}
}

func TestEventToEdit_Errors(t *testing.T) {
	mapping := newOrdersMapping()
	cases := []struct {
		name string
		ev   *cdc.ChangeEvent
		want string
	}{
		{
			name: "nil event",
			ev:   nil,
			want: "ChangeEvent is nil",
		},
		{
			name: "unknown op",
			ev:   &cdc.ChangeEvent{Op: "TRUNCATE"},
			want: "unknown ChangeOp",
		},
		{
			name: "insert without after",
			ev:   &cdc.ChangeEvent{Op: cdc.ChangeOpInsert, Schema: "public", Table: "orders"},
			want: "INSERT event has no After tuple",
		},
		{
			name: "delete without tuple",
			ev:   &cdc.ChangeEvent{Op: cdc.ChangeOpDelete, Schema: "public", Table: "orders"},
			want: "DELETE event has no Before/After tuple",
		},
		{
			name: "missing pk column",
			ev: &cdc.ChangeEvent{
				Op:    cdc.ChangeOpInsert,
				After: map[string]string{"customer_id": "ALFKI"},
			},
			want: "primary-key column \"id\" missing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cdc.EventToEdit(tc.ev, mapping)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q missing substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestIsNullValue(t *testing.T) {
	if !cdc.IsNullValue(nullValue()) {
		t.Fatalf("expected sentinel to register as null")
	}
	if cdc.IsNullValue("hello") {
		t.Fatalf("non-sentinel should not register as null")
	}
}

// nullValue returns the sentinel the decoder/mapper use for SQL NULL
// inside the flat string tuple maps. Re-derived here to keep tests
// independent of the unexported constant in the production package.
func nullValue() string {
	return "\x00\x00CDC_NULL\x00\x00"
}
