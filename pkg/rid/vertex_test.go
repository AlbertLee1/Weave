package rid

import (
	"strings"
	"testing"
)

func TestNewVertexRIDs_AreParseable_AndCarryVertexRealm(t *testing.T) {
	cases := []struct {
		name string
		got  string
		rt   string
	}{
		{"graph", NewVertexGraphRID(), "graph"},
		{"case-study", NewVertexCaseStudyRID(), "case-study"},
		{"scenario", NewVertexScenarioRID(), "scenario"},
		{"template", NewVertexTemplateRID(), "template"},
		{"share-link", NewVertexShareLinkRID(), "share-link"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.HasPrefix(c.got, "ri.vertex.main."+c.rt+".") {
				t.Fatalf("%s minter produced %q", c.name, c.got)
			}
			parsed, err := Parse(c.got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.got, err)
			}
			if parsed.Service != "vertex" || parsed.Realm != "main" || parsed.ResourceType != c.rt {
				t.Errorf("parsed = %+v", parsed)
			}
		})
	}
}

func TestValidateVertexRID_AcceptsKnownTypes(t *testing.T) {
	for _, mint := range []func() string{NewVertexGraphRID, NewVertexCaseStudyRID, NewVertexScenarioRID, NewVertexTemplateRID, NewVertexShareLinkRID} {
		r := mint()
		if _, err := ValidateVertexRID(r); err != nil {
			t.Errorf("ValidateVertexRID(%q): %v", r, err)
		}
	}
}

func TestValidateVertexRID_RejectsForeignService(t *testing.T) {
	bad := New("ontology", "main", "ontology")
	if _, err := ValidateVertexRID(bad); err == nil || !strings.Contains(err.Error(), "service=vertex") {
		t.Fatalf("expected service mismatch error; got %v", err)
	}
}

func TestValidateVertexRID_RejectsUnknownResourceType(t *testing.T) {
	bad := New("vertex", "main", "made-up")
	if _, err := ValidateVertexRID(bad); err == nil || !strings.Contains(err.Error(), "unknown resource type") {
		t.Fatalf("expected unknown-resource-type error; got %v", err)
	}
}

func TestValidateVertexRID_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-rid",
		"ri.vertex.main.graph.notauuid",
	}
	for _, in := range cases {
		if _, err := ValidateVertexRID(in); err == nil {
			t.Errorf("expected error on %q", in)
		}
	}
}
