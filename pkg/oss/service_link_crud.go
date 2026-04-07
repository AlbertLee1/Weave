package oss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/oms"
)

// ErrLinkTypeNotFound is returned when the caller references a LinkType that
// does not exist in the ontology. Handlers map this to 404.
var ErrLinkTypeNotFound = errors.New("link type not found")

// ErrUnsupportedCardinality is returned when the caller tries to mutate a
// link edge for a LinkType that is NOT declared as MANY_TO_MANY. Current
// scope: only M2M edges are mutable via the link CRUD API; N:1 / 1:N links
// are backed by FK columns and must be mutated through object updates.
var ErrUnsupportedCardinality = errors.New("link CRUD supports MANY_TO_MANY cardinality only")

// CreateLink inserts or updates a single many-to-many link edge between two
// objects. The underlying PG repository is UPSERT-based (idempotent on
// (link_type_rid, source_pk, target_pk)), so calling CreateLink twice with
// identical arguments is a no-op. Properties are JSON-marshaled before being
// persisted; a nil map produces a NULL edge_properties column.
func (s *ServiceImpl) CreateLink(ctx context.Context, req CreateLinkRequest) error {
	lt, err := s.resolveM2MLinkType(ctx, req.OntologyRID, req.LinkTypeAPIName)
	if err != nil {
		return err
	}

	var raw json.RawMessage
	if req.Properties != nil {
		b, merr := json.Marshal(req.Properties)
		if merr != nil {
			return fmt.Errorf("marshal edge properties: %w", merr)
		}
		raw = b
	}

	return s.omsRepo.UpsertLinkEdge(ctx, &oms.LinkEdge{
		LinkTypeRID:    lt.RID,
		SourceObjectPK: req.SourcePK,
		TargetObjectPK: req.TargetPK,
		EdgeProperties: raw,
	})
}

// DeleteLink removes a single many-to-many link edge. Idempotent: deleting a
// non-existent edge returns nil.
func (s *ServiceImpl) DeleteLink(ctx context.Context, req DeleteLinkRequest) error {
	lt, err := s.resolveM2MLinkType(ctx, req.OntologyRID, req.LinkTypeAPIName)
	if err != nil {
		return err
	}
	return s.omsRepo.DeleteLinkEdge(ctx, lt.RID, req.SourcePK, req.TargetPK)
}

// resolveM2MLinkType looks up a LinkType by ontology+API name and validates
// that it is MANY_TO_MANY. Returns ErrLinkTypeNotFound if the link type does
// not exist, or ErrUnsupportedCardinality if it is backed by an FK column.
func (s *ServiceImpl) resolveM2MLinkType(ctx context.Context, ontologyRID, apiName string) (*oms.LinkType, error) {
	lt, err := s.omsRepo.GetLinkTypeByAPIName(ctx, ontologyRID, apiName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrLinkTypeNotFound, apiName)
	}
	if lt == nil {
		return nil, fmt.Errorf("%w: %s", ErrLinkTypeNotFound, apiName)
	}
	if lt.Cardinality != "MANY_TO_MANY" {
		return nil, fmt.Errorf("%w: %s has cardinality %q", ErrUnsupportedCardinality, apiName, lt.Cardinality)
	}
	return lt, nil
}
