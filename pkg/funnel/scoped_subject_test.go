package funnel

import "testing"

// US-044: NATS subjects must be scoped per ontology so the funnel consumer can
// route edits to the correct per-ontology Bleve index. Subject format:
//
//	edits.{ontologyApiName}.{objectType}
func TestBuildSubject_ScopedByOntology(t *testing.T) {
	got := BuildSubject("northwind", "Employee")
	want := "edits.northwind.Employee"
	if got != want {
		t.Fatalf("BuildSubject(northwind, Employee) = %q, want %q", got, want)
	}
}

func TestBuildSubject_TwoOntologiesAreDistinct(t *testing.T) {
	a := BuildSubject("northwind", "Order")
	b := BuildSubject("chinook", "Order")
	if a == b {
		t.Fatalf("expected scoped subjects to differ for distinct ontologies, got %q == %q", a, b)
	}
}

func TestParseSubject_ExtractsOntologyAndObjectType(t *testing.T) {
	ontology, objectType, err := ParseSubject("edits.northwind.Employee")
	if err != nil {
		t.Fatalf("ParseSubject: unexpected error %v", err)
	}
	if ontology != "northwind" || objectType != "Employee" {
		t.Fatalf("ParseSubject = (%q, %q), want (northwind, Employee)", ontology, objectType)
	}
}

func TestParseSubject_RejectsLegacyTwoTokenSubject(t *testing.T) {
	if _, _, err := ParseSubject("edits.Employee"); err == nil {
		t.Fatalf("expected ParseSubject to reject legacy two-token subject %q", "edits.Employee")
	}
}
