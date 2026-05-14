// VTX-117 — Vertex realm RID minters + resource-type validation.
//
// Vertex resources live under service="vertex", realm="main" with the
// five resource types listed in PRD VTX-117: graph, case-study,
// scenario, template, share-link. New minters mirror the existing
// ontology realm helpers; ValidateVertexRID is a defence-in-depth
// check that surfaces helpful errors when callers pass a non-vertex
// RID into a vertex API.

package rid

import "fmt"

// VertexResourceTypes is the closed set of resource types Vertex
// recognises. Adding a new type here is a deliberate decision — the
// validator rejects unknown types so typos in handler code surface
// immediately rather than landing as opaque "RID not found" errors.
var VertexResourceTypes = map[string]struct{}{
	"graph":       {},
	"case-study":  {},
	"scenario":    {},
	"template":    {},
	"share-link":  {},
	// scenario-run is intentionally absent — Run records borrow the
	// scenario RID as a parent identifier rather than minting their own.
}

// NewVertexGraphRID mints an ri.vertex.main.graph.<uuid>.
func NewVertexGraphRID() string { return New("vertex", "main", "graph") }

// NewVertexCaseStudyRID mints an ri.vertex.main.case-study.<uuid>.
func NewVertexCaseStudyRID() string { return New("vertex", "main", "case-study") }

// NewVertexScenarioRID mints an ri.vertex.main.scenario.<uuid>.
func NewVertexScenarioRID() string { return New("vertex", "main", "scenario") }

// NewVertexTemplateRID mints an ri.vertex.main.template.<uuid>.
func NewVertexTemplateRID() string { return New("vertex", "main", "template") }

// NewVertexShareLinkRID mints an ri.vertex.main.share-link.<uuid>.
func NewVertexShareLinkRID() string { return New("vertex", "main", "share-link") }

// ValidateVertexRID checks that rid is parseable, lives in the vertex
// realm, and uses one of the five known resource types. Returns the
// parsed RID on success so callers do not have to Parse twice.
func ValidateVertexRID(rid string) (*RID, error) {
	parsed, err := Parse(rid)
	if err != nil {
		return nil, err
	}
	if parsed.Service != "vertex" {
		return nil, fmt.Errorf("vertex RID expected service=vertex; got %q in %q", parsed.Service, rid)
	}
	if _, ok := VertexResourceTypes[parsed.ResourceType]; !ok {
		return nil, fmt.Errorf("vertex RID has unknown resource type %q in %q", parsed.ResourceType, rid)
	}
	return parsed, nil
}
