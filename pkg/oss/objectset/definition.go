package objectset

import (
	"encoding/json"
	"fmt"
)

// Definition represents a declarative ObjectSet definition.
// ObjectSets are composable and lazy-evaluated.
type Definition struct {
	Type       string          `json:"type"`
	ObjectType string          `json:"objectType,omitempty"`  // for "base"
	ObjectSet  *Definition     `json:"objectSet,omitempty"`   // for "filter", "searchAround", "nearestNeighbors", "withProperties"
	ObjectSets []*Definition   `json:"objectSets,omitempty"`  // for "union", "intersect", "subtract"
	Where      json.RawMessage `json:"where,omitempty"`       // for "filter" — raw JSON of WhereClause
	Link       string          `json:"link,omitempty"`        // for "searchAround" — link type API name
	// Direction is optional on "searchAround". "" / "forward" walks the link in
	// its declared source -> target direction; "reverse" walks target -> source.
	Direction string `json:"direction,omitempty"`
	Reference string `json:"reference,omitempty"` // for "reference" — stored objectSet ID

	// For "nearestNeighbors"
	PropertyIdentifier  *PropertyIdentifier `json:"propertyIdentifier,omitempty"`
	NumNeighbors        *int                `json:"numNeighbors,omitempty"`
	SimilarityThreshold *float64            `json:"similarityThreshold,omitempty"`
	Query               *NNQuery            `json:"query,omitempty"`

	// For "withProperties"
	Properties []string `json:"properties,omitempty"`
}

// PropertyIdentifier identifies a property for nearestNeighbors.
type PropertyIdentifier struct {
	Property struct {
		APIName string `json:"apiName"`
	} `json:"property"`
}

// NNQuery represents a nearest-neighbors query (vector or text).
type NNQuery struct {
	Vector *VectorQuery `json:"vector,omitempty"`
	Text   *TextQuery   `json:"text,omitempty"`
}

// VectorQuery provides a raw vector for similarity search.
type VectorQuery struct {
	Value []float64 `json:"value"`
}

// TextQuery provides a text string for similarity search.
type TextQuery struct {
	Value string `json:"value"`
}

// Validate checks that the definition has the required fields for its type.
func (d *Definition) Validate() error {
	switch d.Type {
	case "base":
		if d.ObjectType == "" {
			return fmt.Errorf("base objectSet requires objectType")
		}
	case "filter":
		if d.ObjectSet == nil {
			return fmt.Errorf("filter objectSet requires objectSet")
		}
		if len(d.Where) == 0 {
			return fmt.Errorf("filter objectSet requires where clause")
		}
	case "union", "intersect", "subtract":
		if len(d.ObjectSets) < 2 {
			return fmt.Errorf("%s objectSet requires at least 2 objectSets", d.Type)
		}
	case "searchAround":
		if d.ObjectSet == nil {
			return fmt.Errorf("searchAround objectSet requires objectSet")
		}
		if d.Link == "" {
			return fmt.Errorf("searchAround objectSet requires link")
		}
	case "reference":
		if d.Reference == "" {
			return fmt.Errorf("reference objectSet requires reference")
		}
	case "nearestNeighbors":
		if d.ObjectSet == nil {
			return fmt.Errorf("nearestNeighbors requires objectSet")
		}
		if d.PropertyIdentifier == nil {
			return fmt.Errorf("nearestNeighbors requires propertyIdentifier")
		}
	case "withProperties":
		if d.ObjectSet == nil {
			return fmt.Errorf("withProperties requires objectSet")
		}
	default:
		return fmt.Errorf("unknown objectSet type: %q", d.Type)
	}
	return nil
}

// ParseDefinition parses a JSON byte slice into a Definition.
func ParseDefinition(data []byte) (*Definition, error) {
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse objectSet definition: %w", err)
	}
	return &def, nil
}
