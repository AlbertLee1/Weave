package oms

import (
	"encoding/json"
	"testing"
)

// US-210: LinkProperty model + wire-format validation.

func TestLinkProperty_Validate(t *testing.T) {
	t.Parallel()
	base := LinkProperty{
		RID:         "ri.ontology.main.link-property.role",
		LinkTypeRID: "ri.ontology.main.link-type.user-group",
		APIName:     "role",
		BaseType:    "string",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("base valid row rejected: %v", err)
	}

	missing := []LinkProperty{
		func() LinkProperty { c := base; c.RID = ""; return c }(),
		func() LinkProperty { c := base; c.LinkTypeRID = ""; return c }(),
		func() LinkProperty { c := base; c.APIName = ""; return c }(),
		func() LinkProperty { c := base; c.BaseType = ""; return c }(),
	}
	for i, lp := range missing {
		if err := lp.Validate(); err == nil {
			t.Errorf("case %d: expected validation error, got nil", i)
		}
	}
}

func TestLinkProperty_DataTypeJSON_Scalar(t *testing.T) {
	t.Parallel()
	lp := LinkProperty{
		RID: "x", LinkTypeRID: "y", APIName: "z",
		BaseType: "string",
	}
	got := lp.DataTypeJSON()
	if got["type"] != "string" {
		t.Fatalf("want type=string, got %v", got)
	}
}

func TestLinkProperty_DataTypeJSON_Array(t *testing.T) {
	t.Parallel()
	lp := LinkProperty{
		RID: "x", LinkTypeRID: "y", APIName: "z",
		BaseType: "integer",
		IsArray:  true,
	}
	got := lp.DataTypeJSON()
	if got["type"] != "array" {
		t.Fatalf("want type=array, got %v", got)
	}
	sub, _ := got["subType"].(map[string]interface{})
	if sub == nil || sub["type"] != "integer" {
		t.Fatalf("want subType.type=integer, got %v", got["subType"])
	}
}

func TestLinkProperty_DataTypeJSON_WithTypeConfig(t *testing.T) {
	t.Parallel()
	lp := LinkProperty{
		RID: "x", LinkTypeRID: "y", APIName: "z",
		BaseType:   "string",
		TypeConfig: json.RawMessage(`{"enumValues":["admin","member"]}`),
	}
	got := lp.DataTypeJSON()
	values, ok := got["enumValues"].([]interface{})
	if !ok || len(values) != 2 || values[0] != "admin" {
		t.Fatalf("want enumValues=[admin,member], got %v", got)
	}
}

func TestLinkProperty_JSONRoundTrip_OmitsEmptyFields(t *testing.T) {
	t.Parallel()
	lp := LinkProperty{
		RID:         "ri.ontology.main.link-property.role",
		LinkTypeRID: "ri.ontology.main.link-type.user-group",
		APIName:     "role",
		BaseType:    "string",
		IsNullable:  true,
	}
	b, err := json.Marshal(&lp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := decoded["displayName"]; present {
		t.Errorf("displayName should omitempty, but was present: %s", string(b))
	}
	if _, present := decoded["description"]; present {
		t.Errorf("description should omitempty, but was present: %s", string(b))
	}
	if _, present := decoded["typeConfig"]; present {
		t.Errorf("typeConfig should omitempty, but was present: %s", string(b))
	}
	if decoded["isNullable"] != true {
		t.Errorf("isNullable should be true, got %v", decoded["isNullable"])
	}
}
