// Package graphsvc owns Vertex System Graph validation rules.
//
// VTX-118 codifies the v1 limitation: a System Graph may only reference
// object types from a single ontology. Cross-ontology graphs are
// rejected at the API boundary with 422 (handlers translate the
// returned ErrCrossOntologyLayers).
//
// VTX-058 layers a second rule on top: each layer.extendedLabels[] entry
// must carry a Kind discriminator drawn from ValidExtendedLabelKinds
// (property / timeSeries / measure). Unknown kinds are rejected at the
// API boundary with 422 (handlers translate ErrUnknownLabelKind).
package graphsvc

import (
	"errors"
	"fmt"
	"strings"
)

// ErrCrossOntologyLayers is returned by ValidateSingleOntology when a
// payload's layers reference more than one ontology. Handlers map this
// to HTTP 422.
var ErrCrossOntologyLayers = errors.New("vertex graph layers must all live in a single ontology (v1 limitation)")

// ErrUnknownLabelKind is returned by ValidateExtendedLabels when a label
// carries a Kind outside ValidExtendedLabelKinds. Handlers map this to
// HTTP 422 (WEAVE_VALIDATION_SCHEMA).
var ErrUnknownLabelKind = errors.New("vertex graph: unknown extendedLabel kind")

// ExtendedLabelKind discriminators. VTX-058 pins the closed set; downstream
// rendering stories own each kind's required fields (VTX-059 Property /
// VTX-060 TimeSeries / VTX-061 Measure).
const (
	ExtendedLabelKindProperty   = "property"
	ExtendedLabelKindTimeSeries = "timeSeries"
	ExtendedLabelKindMeasure    = "measure"
)

// ValidExtendedLabelKinds is the closed set of layer.extendedLabels[].kind
// values accepted by ValidateExtendedLabels.
var ValidExtendedLabelKinds = []string{
	ExtendedLabelKindProperty,
	ExtendedLabelKindTimeSeries,
	ExtendedLabelKindMeasure,
}

// GraphPayload is the minimal shape the validators inspect. Full System
// Graph schemas (positions, styles, filters, etc.) layer on top of this
// without affecting the cross-ontology check or extended-label discriminator.
type GraphPayload struct {
	OntologyRID string  `json:"ontologyRid"`
	Layers      []Layer `json:"layers"`
}

// Layer is one drawn layer in a System Graph; OntologyRID identifies
// where the layer's ObjectType lives. ExtendedLabels carries node-overlay
// label specs (rendered by VTX-059/060/061); validators check only the
// Kind discriminator here, per-kind field requirements belong to the
// rendering stories.
type Layer struct {
	ObjectType     string          `json:"objectType"`
	OntologyRID    string          `json:"ontologyRid"`
	ExtendedLabels []ExtendedLabel `json:"extendedLabels,omitempty"`
}

// ExtendedLabel is a per-layer label spec. Only Kind is required at the
// schema layer; Property / TimeSeriesRid / MeasureRid are kind-specific
// optional carriers populated by the UI and consumed by downstream
// rendering stories.
type ExtendedLabel struct {
	Kind          string `json:"kind"`
	Property      string `json:"property,omitempty"`
	TimeSeriesRid string `json:"timeSeriesRid,omitempty"`
	MeasureRid    string `json:"measureRid,omitempty"`
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

// ValidateExtendedLabels returns nil iff every layer.extendedLabels[].kind
// is drawn from ValidExtendedLabelKinds. A layer with no extendedLabels
// (omitted or empty slice) is accepted — the field is optional.
//
// Returning a wrapped sentinel (ErrUnknownLabelKind) lets handlers use
// errors.Is to map to HTTP 422 without string-matching.
func ValidateExtendedLabels(p GraphPayload) error {
	for li, layer := range p.Layers {
		for ei, label := range layer.ExtendedLabels {
			if !isValidExtendedLabelKind(label.Kind) {
				return fmt.Errorf("%w: layer #%d (%s) extendedLabels[%d].kind=%q (allowed: %s)",
					ErrUnknownLabelKind, li, layer.ObjectType, ei, label.Kind,
					strings.Join(ValidExtendedLabelKinds, ","))
			}
		}
	}
	return nil
}

func isValidExtendedLabelKind(k string) bool {
	for _, v := range ValidExtendedLabelKinds {
		if v == k {
			return true
		}
	}
	return false
}
