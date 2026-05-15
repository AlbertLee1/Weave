package graphsvc

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSingleOntology_AcceptsLayersInSameOntology(t *testing.T) {
	err := ValidateSingleOntology(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers: []Layer{
			{ObjectType: "Airport", OntologyRID: "ri.weave.main.ontology.aviation"},
			{ObjectType: "Flight", OntologyRID: "ri.weave.main.ontology.aviation"},
		},
	})
	if err != nil {
		t.Errorf("expected nil; got %v", err)
	}
}

func TestValidateSingleOntology_AcceptsLayerWithEmptyOntology_AsInherited(t *testing.T) {
	err := ValidateSingleOntology(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers:      []Layer{{ObjectType: "Airport"}},
	})
	if err != nil {
		t.Errorf("expected nil; got %v", err)
	}
}

func TestValidateSingleOntology_RejectsCrossOntologyLayer(t *testing.T) {
	err := ValidateSingleOntology(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers: []Layer{
			{ObjectType: "Airport", OntologyRID: "ri.weave.main.ontology.aviation"},
			{ObjectType: "Hospital", OntologyRID: "ri.weave.main.ontology.medical"},
		},
	})
	if err == nil || !errors.Is(err, ErrCrossOntologyLayers) {
		t.Fatalf("expected ErrCrossOntologyLayers; got %v", err)
	}
	if !strings.Contains(err.Error(), "Hospital") {
		t.Errorf("expected layer context in error; got %v", err)
	}
}

func TestValidateSingleOntology_RejectsMissingOntologyRid(t *testing.T) {
	err := ValidateSingleOntology(GraphPayload{Layers: []Layer{{ObjectType: "X"}}})
	if err == nil {
		t.Fatal("expected error on missing OntologyRID")
	}
}

// VTX-058: layer.extendedLabels[] schema — accept the three documented kinds
// (property / timeSeries / measure), reject anything else with sentinel
// ErrUnknownLabelKind so handlers can map to HTTP 422.

func TestValidateExtendedLabels_Given_AllThreeKinds_When_Validate_Then_OK(t *testing.T) {
	err := ValidateExtendedLabels(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers: []Layer{
			{
				ObjectType: "Airport",
				ExtendedLabels: []ExtendedLabel{
					{Kind: ExtendedLabelKindProperty, Property: "onTimePct"},
					{Kind: ExtendedLabelKindTimeSeries, Property: "throughput"},
					{Kind: ExtendedLabelKindMeasure, MeasureRid: "ri.functions.measure.total-alerts"},
				},
			},
		},
	})
	if err != nil {
		t.Errorf("expected nil; got %v", err)
	}
}

func TestValidateExtendedLabels_Given_NoLabels_When_Validate_Then_OK(t *testing.T) {
	err := ValidateExtendedLabels(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers:      []Layer{{ObjectType: "Airport"}},
	})
	if err != nil {
		t.Errorf("expected nil on layer with no extendedLabels; got %v", err)
	}
}

func TestValidateExtendedLabels_Given_EmptyLabelsSlice_When_Validate_Then_OK(t *testing.T) {
	err := ValidateExtendedLabels(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers:      []Layer{{ObjectType: "Airport", ExtendedLabels: []ExtendedLabel{}}},
	})
	if err != nil {
		t.Errorf("expected nil on empty extendedLabels slice; got %v", err)
	}
}

func TestValidateExtendedLabels_Given_UnknownKind_When_Validate_Then_ErrUnknownLabelKind(t *testing.T) {
	err := ValidateExtendedLabels(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers: []Layer{
			{ObjectType: "Airport", ExtendedLabels: []ExtendedLabel{
				{Kind: "badge"}, // not in allowed set
			}},
		},
	})
	if err == nil || !errors.Is(err, ErrUnknownLabelKind) {
		t.Fatalf("expected ErrUnknownLabelKind; got %v", err)
	}
	if !strings.Contains(err.Error(), "badge") {
		t.Errorf("expected kind %q in error context; got %v", "badge", err)
	}
	if !strings.Contains(err.Error(), "Airport") {
		t.Errorf("expected layer ObjectType in error context; got %v", err)
	}
}

func TestValidateExtendedLabels_Given_BlankKind_When_Validate_Then_ErrUnknownLabelKind(t *testing.T) {
	// A missing/empty discriminator should fail closed (empty string is not
	// in the allowed set).
	err := ValidateExtendedLabels(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers: []Layer{
			{ObjectType: "Airport", ExtendedLabels: []ExtendedLabel{{Kind: ""}}},
		},
	})
	if err == nil || !errors.Is(err, ErrUnknownLabelKind) {
		t.Fatalf("expected ErrUnknownLabelKind on blank kind; got %v", err)
	}
}

func TestValidateExtendedLabels_Given_MixedKinds_When_OneUnknown_Then_ErrUnknownLabelKindIdentifiesIndex(t *testing.T) {
	err := ValidateExtendedLabels(GraphPayload{
		OntologyRID: "ri.weave.main.ontology.aviation",
		Layers: []Layer{
			{ObjectType: "Airport", ExtendedLabels: []ExtendedLabel{
				{Kind: ExtendedLabelKindProperty, Property: "onTimePct"},
				{Kind: "histogram"}, // index 1 is bad
			}},
		},
	})
	if err == nil || !errors.Is(err, ErrUnknownLabelKind) {
		t.Fatalf("expected ErrUnknownLabelKind; got %v", err)
	}
	if !strings.Contains(err.Error(), "extendedLabels[1]") {
		t.Errorf("expected extendedLabels index #1 in error context; got %v", err)
	}
}

func TestValidExtendedLabelKinds_IsClosedSetOfThree(t *testing.T) {
	want := map[string]bool{
		ExtendedLabelKindProperty:   true,
		ExtendedLabelKindTimeSeries: true,
		ExtendedLabelKindMeasure:    true,
	}
	if len(ValidExtendedLabelKinds) != 3 {
		t.Fatalf("ValidExtendedLabelKinds size = %d, want 3", len(ValidExtendedLabelKinds))
	}
	for _, k := range ValidExtendedLabelKinds {
		if !want[k] {
			t.Errorf("unexpected kind %q in ValidExtendedLabelKinds", k)
		}
	}
}
