package objectset

import (
	"encoding/json"
	"fmt"
	"time"
)

// TimeRangeHint is the optional caller-declared time window an ObjectSet
// query is asking about (US-485). The executor uses it to short-circuit
// the irrelevant tier when the window falls wholly inside the hot
// (Bleve) or cold (Parquet) partition. A nil hint disables routing and
// the executor falls back to the legacy "always merge" behaviour.
//
// Both bounds are pointers so either side may be open-ended:
//   - From == nil ⇒ "since the beginning of time" (cold-side open)
//   - To   == nil ⇒ "until now"                  (hot-side open)
//
// The executor reads the window as inclusive on both ends, matching the
// way the cold-tier router's `before` cutoff is interpreted by
// materialize.TierRouter.
type TimeRangeHint struct {
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`
}

// Definition represents a declarative ObjectSet definition.
// ObjectSets are composable and lazy-evaluated.
type Definition struct {
	Type       string          `json:"type"`
	ObjectType string          `json:"objectType,omitempty"` // for "base", "static", "asType"
	ObjectSet  *Definition     `json:"objectSet,omitempty"`  // for "filter", "searchAround", "nearestNeighbors", "withProperties", "asType", "asBaseObjectTypes", "interfaceLinkSearchAround"
	ObjectSets []*Definition   `json:"objectSets,omitempty"` // for "union", "intersect", "subtract"
	Where      json.RawMessage `json:"where,omitempty"`      // for "filter" — raw JSON of WhereClause
	Link       string          `json:"link,omitempty"`       // for "searchAround" — link type API name
	// Direction is optional on "searchAround". "" / "forward" walks the link in
	// its declared source -> target direction; "reverse" walks target -> source.
	Direction string `json:"direction,omitempty"`
	Reference string `json:"reference,omitempty"` // for "reference" — stored objectSet ID

	// For "nearestNeighbors"
	PropertyIdentifier *PropertyIdentifier `json:"propertyIdentifier,omitempty"`
	// PropertyIdentifiers (Gap-Q4) enumerates multiple vector columns
	// to run KNN against in parallel. The executor dispatches one
	// store call per column and fuses the per-column matches by
	// keeping the minimum distance per primary key, so a PK that is
	// "close" on any single column floats to the top. Mutually
	// exclusive with PropertyIdentifier (singular) — Validate rejects
	// setting both. A list with a single entry is allowed and behaves
	// identically to the singular form.
	PropertyIdentifiers []PropertyIdentifier `json:"propertyIdentifiers,omitempty"`
	// FusionStrategy (Gap-Q4 follow-up, round 50) selects how
	// multi-column NN matches are combined. Allowed values:
	//   - ""    — defaults to "min" (backwards-compat with round 49)
	//   - "min" — keep minimum distance per primary key, sort ascending
	//   - "rrf" — Reciprocal Rank Fusion (Cormack et al., k=60):
	//             score(pk) = sum_c 1 / (60 + rank_c(pk))
	//             ranks start at 1, absent PKs contribute 0,
	//             sort by score descending.
	// Ignored on single-column queries (no fusion needed). Unknown
	// values are rejected at Validate().
	FusionStrategy      string   `json:"fusionStrategy,omitempty"`
	NumNeighbors        *int     `json:"numNeighbors,omitempty"`
	SimilarityThreshold *float64 `json:"similarityThreshold,omitempty"`
	Query               *NNQuery `json:"query,omitempty"`

	// For "withProperties"
	Properties []string `json:"properties,omitempty"`
	// DerivedProperties enumerates per-object metrics the executor should
	// compute by traversing one link hop from each base object. The resulting
	// values are attached to Result.DerivedValues keyed by base primary key
	// and by derived property name.
	DerivedProperties []DerivedPropertyDef `json:"derivedProperties,omitempty"`

	// For "static" — explicit list of primary keys of ObjectType.
	PrimaryKeys []string `json:"primaryKeys,omitempty"`

	// For "interfaceBase" — the interface API name whose implementing object
	// types define the base set.
	InterfaceType string `json:"interfaceType,omitempty"`

	// For "interfaceLinkSearchAround" — the interface link type API name to
	// walk from the inner objectSet.
	InterfaceLink string `json:"interfaceLink,omitempty"`

	// For "methodInput" — name of the function method input parameter whose
	// bound ObjectSet should be used at execution time.
	Input string `json:"input,omitempty"`

	// For "searchAround" (US-226) — Path enumerates multiple link hops to walk
	// in order from the inner objectSet. When Path is set the legacy Link /
	// Direction fields must be unset; an empty Path falls back to the legacy
	// single-hop behaviour driven by Link.
	Path []PathStep `json:"path,omitempty"`

	// For "sample" (US-225) — Size is the target sample count and Seed is the
	// optional deterministic PRNG seed. Both are pointers so an unset Seed is
	// distinguishable from the zero seed and a missing Size fails validation.
	Size *int   `json:"size,omitempty"`
	Seed *int64 `json:"seed,omitempty"`

	// TimeRange (US-485) declares the wall-clock window the request is
	// scoped to. The executor uses it on "base" objectSets to route
	// hot/cold tiers — a hot-only window skips the cold tier round-trip
	// and a cold-only window skips the Bleve query. nil is the
	// backwards-compatible "always merge both tiers" path.
	TimeRange *TimeRangeHint `json:"timeRange,omitempty"`
}

// PathStep declares one hop in a multi-segment searchAround traversal
// (US-226). Link is the link type API name to walk; Direction defaults to
// "forward". ExpectedObjectType, when non-empty, is checked against the
// link resolver's target type for this hop — a mismatch is a hard error so
// cross-hop paths fail loudly instead of walking the wrong index.
type PathStep struct {
	Link               string `json:"link"`
	Direction          string `json:"direction,omitempty"`
	ExpectedObjectType string `json:"expectedObjectType,omitempty"`
}

// DerivedPropertyDef declares a single per-object computation the executor
// evaluates for each object in a withProperties base set. Name is the output
// key (surfaced to API callers as a regular property). For link-hop metrics
// (count / sum / avg / min / max) Link + Direction + Field describe the
// traversal. For metric="formula" (US-201) Formula holds the JS expression
// evaluated by pkg/types/formula against the base object's properties, and
// Link / Direction / Field are ignored.
type DerivedPropertyDef struct {
	Name      string `json:"name"`
	Link      string `json:"link,omitempty"`
	Direction string `json:"direction,omitempty"`
	Metric    string `json:"metric,omitempty"`
	Field     string `json:"field,omitempty"`
	Formula   string `json:"formula,omitempty"`
}

// IsFormula reports whether the derived property should be evaluated as a
// JS formula against the base object. Callers accept either explicit
// metric="formula" or a non-empty Formula with an unset metric.
func (d DerivedPropertyDef) IsFormula() bool {
	return d.Metric == "formula" || (d.Metric == "" && d.Formula != "")
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
		if len(d.Path) > 0 {
			if d.Link != "" {
				return fmt.Errorf("searchAround objectSet cannot set both link and path")
			}
			if len(d.Path) > MaxSearchAroundHops {
				return fmt.Errorf("searchAround path has %d hops, exceeds the %d-hop limit for chained searchAround", len(d.Path), MaxSearchAroundHops)
			}
			for i, step := range d.Path {
				if step.Link == "" {
					return fmt.Errorf("searchAround path[%d] requires link", i)
				}
			}
		} else if d.Link == "" {
			return fmt.Errorf("searchAround objectSet requires link or path")
		}
	case "reference":
		if d.Reference == "" {
			return fmt.Errorf("reference objectSet requires reference")
		}
	case "nearestNeighbors":
		if d.ObjectSet == nil {
			return fmt.Errorf("nearestNeighbors requires objectSet")
		}
		hasSingular := d.PropertyIdentifier != nil
		hasPlural := len(d.PropertyIdentifiers) > 0
		if hasSingular && hasPlural {
			return fmt.Errorf("nearestNeighbors: propertyIdentifier and propertyIdentifiers are mutually exclusive")
		}
		if !hasSingular && !hasPlural {
			return fmt.Errorf("nearestNeighbors requires propertyIdentifier or propertyIdentifiers")
		}
		switch d.FusionStrategy {
		case "", "min", "rrf":
			// accepted
		default:
			return fmt.Errorf("nearestNeighbors: unknown fusionStrategy %q (allowed: \"\" / \"min\" / \"rrf\")", d.FusionStrategy)
		}
		if d.NumNeighbors != nil && *d.NumNeighbors > MaxNearestNeighbors {
			return fmt.Errorf("nearestNeighbors: numNeighbors %d exceeds the %d-neighbor limit", *d.NumNeighbors, MaxNearestNeighbors)
		}
		if d.Query != nil && d.Query.Vector != nil && len(d.Query.Vector.Value) > MaxVectorDimension {
			return fmt.Errorf("nearestNeighbors: query vector has %d dimensions, exceeds the %d-dimension limit", len(d.Query.Vector.Value), MaxVectorDimension)
		}
	case "withProperties":
		if d.ObjectSet == nil {
			return fmt.Errorf("withProperties requires objectSet")
		}
		for i, dp := range d.DerivedProperties {
			if dp.Name == "" {
				return fmt.Errorf("withProperties derivedProperties[%d] requires name", i)
			}
			if dp.IsFormula() {
				if dp.Formula == "" {
					return fmt.Errorf("withProperties derivedProperties[%d] (%q) metric %q requires formula", i, dp.Name, "formula")
				}
				continue
			}
			if dp.Link == "" {
				return fmt.Errorf("withProperties derivedProperties[%d] (%q) requires link", i, dp.Name)
			}
			if dp.Metric == "" {
				return fmt.Errorf("withProperties derivedProperties[%d] (%q) requires metric", i, dp.Name)
			}
			switch dp.Metric {
			case "count":
				// count ignores Field
			case "sum", "avg", "min", "max":
				if dp.Field == "" {
					return fmt.Errorf("withProperties derivedProperties[%d] (%q) metric %q requires field", i, dp.Name, dp.Metric)
				}
			default:
				return fmt.Errorf("withProperties derivedProperties[%d] (%q) unknown metric %q", i, dp.Name, dp.Metric)
			}
		}
	case "static":
		if d.ObjectType == "" {
			return fmt.Errorf("static objectSet requires objectType")
		}
	case "asType":
		if d.ObjectType == "" {
			return fmt.Errorf("asType objectSet requires objectType")
		}
		if d.ObjectSet == nil {
			return fmt.Errorf("asType objectSet requires objectSet")
		}
	case "asBaseObjectTypes":
		if d.ObjectSet == nil {
			return fmt.Errorf("asBaseObjectTypes objectSet requires objectSet")
		}
	case "interfaceBase":
		if d.InterfaceType == "" {
			return fmt.Errorf("interfaceBase objectSet requires interfaceType")
		}
	case "interfaceLinkSearchAround":
		if d.ObjectSet == nil {
			return fmt.Errorf("interfaceLinkSearchAround objectSet requires objectSet")
		}
		if d.InterfaceLink == "" {
			return fmt.Errorf("interfaceLinkSearchAround objectSet requires interfaceLink")
		}
	case "methodInput":
		if d.Input == "" {
			return fmt.Errorf("methodInput objectSet requires input")
		}
	case "sample":
		if d.ObjectSet == nil {
			return fmt.Errorf("sample objectSet requires objectSet")
		}
		if d.Size == nil {
			return fmt.Errorf("sample objectSet requires size")
		}
		if *d.Size <= 0 {
			return fmt.Errorf("sample objectSet requires size > 0, got %d", *d.Size)
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
