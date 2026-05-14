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
