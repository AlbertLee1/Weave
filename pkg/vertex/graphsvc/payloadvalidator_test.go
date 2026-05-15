package graphsvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// stubReferenceLookup is the smallest oms-shaped lookup PayloadValidator
// needs for its 422 reference checks. The two maps mirror what callers
// would otherwise hit Postgres for; missing keys return ErrNotFound so the
// validator can map to PayloadValidationError(StatusUnknown*Reference).
type stubReferenceLookup struct {
	objectTypes map[string]*oms.ObjectType
	linkTypes   map[string]*oms.LinkType
}

func (s *stubReferenceLookup) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	if ot, ok := s.objectTypes[rid]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (s *stubReferenceLookup) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	if lt, ok := s.linkTypes[rid]; ok {
		return lt, nil
	}
	return nil, oms.ErrNotFound
}

func newValidator(t *testing.T, refs graphsvc.ReferenceLookup) *graphsvc.PayloadValidator {
	t.Helper()
	v, err := graphsvc.NewPayloadValidator(refs)
	if err != nil {
		t.Fatalf("NewPayloadValidator: %v", err)
	}
	return v
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestPayloadValidator_Given_PayloadMissingLayers_When_Validate_Then_400LayersRequired
// Mirrors BDD #1: payload without `layers` → 400 + clear "layers field required".
func TestPayloadValidator_Given_PayloadMissingLayers_When_Validate_Then_400LayersRequired(t *testing.T) {
	v := newValidator(t, &stubReferenceLookup{})
	err := v.Validate(context.Background(), raw(t, map[string]any{"edges": []any{}}))
	var pve *graphsvc.PayloadValidationError
	if !errors.As(err, &pve) {
		t.Fatalf("err = %v, want *PayloadValidationError", err)
	}
	if pve.Status != 400 {
		t.Errorf("Status = %d, want 400", pve.Status)
	}
	if pve.Code != graphsvc.PayloadCodeSchema {
		t.Errorf("Code = %q, want %q", pve.Code, graphsvc.PayloadCodeSchema)
	}
	// The user-facing reason must surface the missing field by name so
	// the BDD acceptance "layers field required" is satisfied verbatim.
	if !strings.Contains(strings.ToLower(pve.Reason), "layers") {
		t.Errorf("Reason = %q, want it to mention `layers`", pve.Reason)
	}
}

// TestPayloadValidator_Given_LayerObjectTypeRidMissingFromOMS_When_Validate_Then_422ObjectTypeNotFound
// BDD #2: layer.objectTypeRid pointing at unknown ObjectType → 422 + "objectType not found".
func TestPayloadValidator_Given_LayerObjectTypeRidMissingFromOMS_When_Validate_Then_422ObjectTypeNotFound(t *testing.T) {
	v := newValidator(t, &stubReferenceLookup{}) // empty stub → every RID misses
	payload := raw(t, map[string]any{
		"layers": []any{
			map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.ghost"},
		},
	})
	err := v.Validate(context.Background(), payload)
	var pve *graphsvc.PayloadValidationError
	if !errors.As(err, &pve) {
		t.Fatalf("err = %v, want *PayloadValidationError", err)
	}
	if pve.Status != 422 {
		t.Errorf("Status = %d, want 422", pve.Status)
	}
	if pve.Code != graphsvc.PayloadCodeUnknownObjectType {
		t.Errorf("Code = %q, want %q", pve.Code, graphsvc.PayloadCodeUnknownObjectType)
	}
	if !strings.Contains(strings.ToLower(pve.Reason), "objecttype not found") {
		t.Errorf("Reason = %q, want it to contain `objectType not found`", pve.Reason)
	}
	if !strings.Contains(pve.Reason, "ri.ontology.main.object-type.ghost") {
		t.Errorf("Reason = %q, want it to mention the offending RID", pve.Reason)
	}
}

// TestPayloadValidator_Given_EdgeLinkTypeRidMissingFromOMS_When_Validate_Then_422LinkTypeNotFound
// BDD #3: edge.linkTypeRid pointing at unknown LinkType → 422.
func TestPayloadValidator_Given_EdgeLinkTypeRidMissingFromOMS_When_Validate_Then_422LinkTypeNotFound(t *testing.T) {
	refs := &stubReferenceLookup{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ontology.main.object-type.airport": {RID: "ri.ontology.main.object-type.airport"},
		},
	}
	v := newValidator(t, refs)
	payload := raw(t, map[string]any{
		"layers": []any{
			map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.airport"},
		},
		"edges": []any{
			map[string]any{"id": "E1", "linkTypeRid": "ri.ontology.main.link-type.ghost"},
		},
	})
	err := v.Validate(context.Background(), payload)
	var pve *graphsvc.PayloadValidationError
	if !errors.As(err, &pve) {
		t.Fatalf("err = %v, want *PayloadValidationError", err)
	}
	if pve.Status != 422 {
		t.Errorf("Status = %d, want 422", pve.Status)
	}
	if pve.Code != graphsvc.PayloadCodeUnknownLinkType {
		t.Errorf("Code = %q, want %q", pve.Code, graphsvc.PayloadCodeUnknownLinkType)
	}
	if !strings.Contains(strings.ToLower(pve.Reason), "linktype not found") {
		t.Errorf("Reason = %q, want it to contain `linkType not found`", pve.Reason)
	}
}

// TestPayloadValidator_Given_PositionWithStringCoord_When_Validate_Then_400Schema
// BDD #4: positions value with non-numeric coord → 400.
func TestPayloadValidator_Given_PositionWithStringCoord_When_Validate_Then_400Schema(t *testing.T) {
	v := newValidator(t, &stubReferenceLookup{})
	payload := raw(t, map[string]any{
		"layers":    []any{},
		"positions": map[string]any{"n1": map[string]any{"x": "left", "y": 0}},
	})
	err := v.Validate(context.Background(), payload)
	var pve *graphsvc.PayloadValidationError
	if !errors.As(err, &pve) {
		t.Fatalf("err = %v, want *PayloadValidationError", err)
	}
	if pve.Status != 400 {
		t.Errorf("Status = %d, want 400 for non-numeric coord", pve.Status)
	}
	if pve.Code != graphsvc.PayloadCodeSchema {
		t.Errorf("Code = %q, want %q", pve.Code, graphsvc.PayloadCodeSchema)
	}
	if !strings.Contains(pve.Field, "positions") {
		t.Errorf("Field = %q, want it to contain `positions`", pve.Field)
	}
}

// TestPayloadValidator_Given_ValidPayload_When_Validate_Then_NoError
func TestPayloadValidator_Given_ValidPayload_When_Validate_Then_NoError(t *testing.T) {
	refs := &stubReferenceLookup{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ontology.main.object-type.airport": {RID: "ri.ontology.main.object-type.airport"},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.ontology.main.link-type.flights": {RID: "ri.ontology.main.link-type.flights"},
		},
	}
	v := newValidator(t, refs)
	payload := raw(t, map[string]any{
		"layers": []any{map[string]any{"id": "L1", "objectTypeRid": "ri.ontology.main.object-type.airport"}},
		"edges":  []any{map[string]any{"id": "E1", "linkTypeRid": "ri.ontology.main.link-type.flights"}},
		"positions": map[string]any{
			"n1": map[string]any{"x": 1.5, "y": -2.0, "pinned": true},
		},
	})
	if err := v.Validate(context.Background(), payload); err != nil {
		t.Errorf("Validate(valid payload) = %v, want nil", err)
	}
}

// TestPayloadValidator_Given_NilReferenceLookup_When_LayerHasObjectTypeRid_Then_NoReferenceCheck
// Degraded-mode boots wire a nil ReferenceLookup. Schema still runs; reference
// resolution is skipped so the route stays usable for SDK probes.
func TestPayloadValidator_Given_NilReferenceLookup_When_LayerHasObjectTypeRid_Then_NoReferenceCheck(t *testing.T) {
	v := newValidator(t, nil)
	payload := raw(t, map[string]any{
		"layers": []any{map[string]any{"id": "L1", "objectTypeRid": "ri.whatever"}},
	})
	if err := v.Validate(context.Background(), payload); err != nil {
		t.Errorf("Validate with nil refs = %v, want nil (refs disabled)", err)
	}
}

// TestPayloadValidator_Given_EmptyPayload_When_Validate_Then_AcceptedAsNoop
// Vertex callers may POST with an empty body to lazy-init a graph; do not
// reject — handler-level checks already cover "missing payload".
func TestPayloadValidator_Given_EmptyPayload_When_Validate_Then_AcceptedAsNoop(t *testing.T) {
	v := newValidator(t, &stubReferenceLookup{})
	if err := v.Validate(context.Background(), nil); err != nil {
		t.Errorf("Validate(nil) = %v, want nil", err)
	}
	if err := v.Validate(context.Background(), json.RawMessage{}); err != nil {
		t.Errorf("Validate(empty) = %v, want nil", err)
	}
}
