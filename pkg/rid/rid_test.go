package rid

import (
	"regexp"
	"strings"
	"testing"
)

var ridPattern = regexp.MustCompile(
	`^ri\.[a-z-]+\.[a-z-]+\.[a-z-]+\.[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

func TestNewRID_Format(t *testing.T) {
	rid := New("ontology", "main", "object-type")
	if !ridPattern.MatchString(rid) {
		t.Fatalf("RID %q does not match expected format ri.{service}.{realm}.{type}.{uuid}", rid)
	}
}

func TestNewObjectTypeRID(t *testing.T) {
	rid := NewObjectTypeRID()
	if !strings.HasPrefix(rid, "ri.ontology.main.object-type.") {
		t.Fatalf("expected prefix ri.ontology.main.object-type., got %q", rid)
	}
}

func TestNewPropertyRID(t *testing.T) {
	rid := NewPropertyRID()
	if !strings.HasPrefix(rid, "ri.ontology.main.property.") {
		t.Fatalf("expected prefix ri.ontology.main.property., got %q", rid)
	}
}

func TestNewLinkTypeRID(t *testing.T) {
	rid := NewLinkTypeRID()
	if !strings.HasPrefix(rid, "ri.ontology.main.link-type.") {
		t.Fatalf("expected prefix ri.ontology.main.link-type., got %q", rid)
	}
}

func TestNewObjectRID(t *testing.T) {
	rid := NewObjectRID()
	if !strings.HasPrefix(rid, "ri.ontology.main.object.") {
		t.Fatalf("expected prefix ri.ontology.main.object., got %q", rid)
	}
}

func TestNewActionTypeRID(t *testing.T) {
	rid := NewActionTypeRID()
	if !strings.HasPrefix(rid, "ri.ontology.main.action-type.") {
		t.Fatalf("expected prefix ri.ontology.main.action-type., got %q", rid)
	}
}

func TestParseRID(t *testing.T) {
	input := "ri.ontology.main.object-type.550e8400-e29b-41d4-a716-446655440000"
	r, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Service != "ontology" {
		t.Errorf("Service = %q, want %q", r.Service, "ontology")
	}
	if r.Realm != "main" {
		t.Errorf("Realm = %q, want %q", r.Realm, "main")
	}
	if r.ResourceType != "object-type" {
		t.Errorf("ResourceType = %q, want %q", r.ResourceType, "object-type")
	}
	if r.ID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("ID = %q, want %q", r.ID, "550e8400-e29b-41d4-a716-446655440000")
	}
}

func TestParseRID_Invalid(t *testing.T) {
	cases := []string{"invalid", "ri.a.b"}
	for _, c := range cases {
		_, err := Parse(c)
		if err == nil {
			t.Errorf("Parse(%q) expected error, got nil", c)
		}
	}
}
