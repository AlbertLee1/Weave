package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// interfaceResolverRepo is the narrow oms.Repository subset the
// pgInterfaceResolver needs. Kept local to cmd/server to avoid bloating the
// 50+-method oms.Repository interface (see Codebase Patterns note about
// narrow in-package interfaces beating expanding oms.Repository).
type interfaceResolverRepo interface {
	GetOntology(ctx context.Context, ridOrApiName string) (*oms.Ontology, error)
	GetInterfaceByAPIName(ctx context.Context, ontologyRID, apiName string) (*oms.Interface, error)
	ListInterfaceObjectTypes(ctx context.Context, interfaceRID string) ([]oms.ObjectType, error)
}

// pgInterfaceResolver satisfies pkg/oss/objectset.InterfaceResolver by
// walking the ontology scope stamped on the request context through the
// oms repository and returning the apiNames of the ObjectTypes that
// implement the requested interface. This is what unlocks the
// interfaceBase polymorphic code path for live production requests —
// previously the executor short-circuited with "interface resolver not
// configured" because main.go did not wire a resolver at all (see
// Phase 6 gate follow-up documented in progress.txt).
type pgInterfaceResolver struct {
	repo interfaceResolverRepo
}

func newPGInterfaceResolver(repo interfaceResolverRepo) *pgInterfaceResolver {
	return &pgInterfaceResolver{repo: repo}
}

func (r *pgInterfaceResolver) ResolveInterfaceObjectTypes(ctx context.Context, interfaceAPIName string) ([]string, error) {
	if r == nil || r.repo == nil {
		return nil, errors.New("interface resolver: nil repo")
	}
	scope := index.OntologyScopeFromContext(ctx)
	if scope == "" {
		return nil, errors.New("interface resolver: no ontology scope on context")
	}
	ont, err := r.repo.GetOntology(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("interface resolver: lookup ontology %q: %w", scope, err)
	}
	if ont == nil {
		return nil, fmt.Errorf("interface resolver: ontology %q not found", scope)
	}
	iface, err := r.repo.GetInterfaceByAPIName(ctx, ont.RID, interfaceAPIName)
	if err != nil {
		return nil, fmt.Errorf("interface resolver: lookup interface %q: %w", interfaceAPIName, err)
	}
	if iface == nil {
		return nil, fmt.Errorf("interface resolver: interface %q not found in ontology %q", interfaceAPIName, scope)
	}
	ots, err := r.repo.ListInterfaceObjectTypes(ctx, iface.RID)
	if err != nil {
		return nil, fmt.Errorf("interface resolver: list implementers of %q: %w", interfaceAPIName, err)
	}
	out := make([]string, 0, len(ots))
	for _, ot := range ots {
		out = append(out, ot.APIName)
	}
	return out, nil
}
