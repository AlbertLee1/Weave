// Package graphsvc owns Vertex System Graph validation rules.
//
// VTX-118 codifies the v1 limitation: a System Graph may only reference
// object types from a single ontology. Cross-ontology graphs are
// rejected at the API boundary with 422 (handlers translate the
// returned ErrCrossOntologyLayers).
package graphsvc

import (
	"errors"
	"fmt"
)

// ErrCrossOntologyLayers is returned by ValidateSingleOntology when a
// payload's layers reference more than one ontology. Handlers map this
// to HTTP 422.
var ErrCrossOntologyLayers = errors.New("vertex graph layers must all live in a single ontology (v1 limitation)")

// GraphPayload is the minimal shape ValidateSingleOntology inspects.
// Full System Graph schemas (positions, styles, filters, etc.) layer
// on top of this without affecting the cross-ontology check.
type GraphPayload struct {
	OntologyRID string  `json:"ontologyRid"`
	Layers      []Layer `json:"layers"`
}

// Layer is one drawn layer in a System Graph; OntologyRID identifies
// where the layer's ObjectType lives.
type Layer struct {
	ObjectType  string `json:"objectType"`
	OntologyRID string `json:"ontologyRid"`
}

// ValidateSingleOntology returns nil iff every layer references the
// same ontology as the graph's top-level OntologyRID. A layer with an
// empty OntologyRID is treated as "inherit from the graph" and accepted.
//
// Returning a wrapped sentinel (ErrCrossOntologyLayers) lets handlers
// use errors.Is to map to 422 without string-matching.
func ValidateSingleOntology(p GraphPayload) error {
	if p.OntologyRID == "" {
		return fmt.Errorf("vertex graph: top-level ontologyRid is required")
	}
	for i, l := range p.Layers {
		if l.OntologyRID == "" || l.OntologyRID == p.OntologyRID {
			continue
		}
		return fmt.Errorf("%w: layer #%d (%s) references %s but graph is in %s",
			ErrCrossOntologyLayers, i, l.ObjectType, l.OntologyRID, p.OntologyRID)
	}
	return nil
}
