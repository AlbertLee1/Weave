package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// linkPropagationTraverserRepo is the narrow oms.Repository subset the
// traverser needs to enumerate the propagating-link graph. Kept local to
// cmd/server so pkg/funnel stays free of LinkType/ObjectType lookups —
// same shape as linkPropagationResolverRepo (US-261).
type linkPropagationTraverserRepo interface {
	ListOntologies(ctx context.Context) ([]oms.Ontology, error)
	ListLinkTypes(ctx context.Context, ontologyRID string) ([]oms.LinkType, error)
	GetObjectType(ctx context.Context, rid string) (*oms.ObjectType, error)
}

// linkEdgeTargetLister is the narrow surface over link_edges used by the
// traverser to enumerate downstream PKs. Satisfied by *oms.PGRepository's
// ListEdgeTargets; tests can supply an in-memory fake.
type linkEdgeTargetLister interface {
	ListEdgeTargets(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error)
}

// propagatingLinkBySource is the per-(sourceObjectType-API-name) view of
// the propagating-link graph the traverser hands the funnel consumer
// during BFS. Each entry binds a LinkType RID to its target-side
// ObjectType API name so the consumer can scope the downstream bleve
// fetch + re-index.
type propagatingLinkBySource struct {
	LinkTypeRID             string
	TargetObjectTypeAPIName string
}

// linkPropagationTraverser adapts the OMS repo + LinkEdge store to the
// narrow funnel.LinkPropagationTraverser interface (US-474). The
// adapter caches the propagating-link metadata (which LinkTypes have
// PropagateMarkings=true and which source-ObjectType API name they
// originate from) on a refresh tick so the per-LINK_CREATE BFS is two
// in-memory map lookups + one PG query per hop.
type linkPropagationTraverser struct {
	repo  linkPropagationTraverserRepo
	edges linkEdgeTargetLister

	mu    sync.RWMutex
	bySrc map[string][]propagatingLinkBySource // sourceObjectTypeAPIName -> propagating LinkTypes
}

func newLinkPropagationTraverser(
	repo linkPropagationTraverserRepo,
	edges linkEdgeTargetLister,
) *linkPropagationTraverser {
	return &linkPropagationTraverser{
		repo:  repo,
		edges: edges,
		bySrc: map[string][]propagatingLinkBySource{},
	}
}

// Refresh rebuilds the propagating-link map from the live OMS schema.
// Called once at boot + on a 5-minute tick so a freshly-flipped
// PropagateMarkings flag takes effect without a server restart. Errors
// are surfaced; the caller decides whether to log + degrade or abort.
func (t *linkPropagationTraverser) Refresh(ctx context.Context) error {
	if t == nil || t.repo == nil {
		return nil
	}
	onts, err := t.repo.ListOntologies(ctx)
	if err != nil {
		return fmt.Errorf("list ontologies: %w", err)
	}

	otAPINameCache := map[string]string{}
	resolveAPI := func(rid string) (string, error) {
		if rid == "" {
			return "", nil
		}
		if name, ok := otAPINameCache[rid]; ok {
			return name, nil
		}
		ot, err := t.repo.GetObjectType(ctx, rid)
		if err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				otAPINameCache[rid] = ""
				return "", nil
			}
			return "", err
		}
		otAPINameCache[rid] = ot.APIName
		return ot.APIName, nil
	}

	next := map[string][]propagatingLinkBySource{}
	for _, ont := range onts {
		lts, err := t.repo.ListLinkTypes(ctx, ont.RID)
		if err != nil {
			return fmt.Errorf("list link types for %s: %w", ont.RID, err)
		}
		for _, lt := range lts {
			if !lt.PropagateMarkings {
				continue
			}
			srcAPI, err := resolveAPI(lt.SourceObjectType)
			if err != nil {
				return err
			}
			tgtAPI, err := resolveAPI(lt.TargetObjectType)
			if err != nil {
				return err
			}
			if srcAPI == "" || tgtAPI == "" {
				continue
			}
			next[srcAPI] = append(next[srcAPI], propagatingLinkBySource{
				LinkTypeRID:             lt.RID,
				TargetObjectTypeAPIName: tgtAPI,
			})
		}
	}

	t.mu.Lock()
	t.bySrc = next
	t.mu.Unlock()
	return nil
}

// ListPropagatingOutgoingEdges returns every (linkTypeRID, targetOT,
// targetPK) tuple the consumer should walk forward from
// (sourceObjectTypeAPIName, sourcePKs). Only propagating LinkTypes are
// returned — non-propagating edges are invisible to the walk so a
// propagate=false hop truncates the BFS naturally.
func (t *linkPropagationTraverser) ListPropagatingOutgoingEdges(
	ctx context.Context,
	sourceObjectTypeAPIName string,
	sourcePKs []string,
) ([]funnel.PropagatingOutgoingEdge, error) {
	if t == nil || t.edges == nil || sourceObjectTypeAPIName == "" || len(sourcePKs) == 0 {
		return nil, nil
	}
	t.mu.RLock()
	links := t.bySrc[sourceObjectTypeAPIName]
	t.mu.RUnlock()
	if len(links) == 0 {
		return nil, nil
	}

	var out []funnel.PropagatingOutgoingEdge
	for _, lk := range links {
		targets, err := t.edges.ListEdgeTargets(ctx, lk.LinkTypeRID, sourcePKs)
		if err != nil {
			return nil, fmt.Errorf("list edge targets for %s: %w", lk.LinkTypeRID, err)
		}
		for _, tgtPK := range targets {
			out = append(out, funnel.PropagatingOutgoingEdge{
				LinkTypeRID:             lk.LinkTypeRID,
				TargetObjectTypeAPIName: lk.TargetObjectTypeAPIName,
				TargetPK:                tgtPK,
			})
		}
	}
	return out, nil
}

// runLinkPropagationTraverserRefreshLoop drives Refresh on the
// configured interval until ctx is cancelled. Errors are routed to
// onError so the caller can decide whether to log or surface them.
// Matches the runEditOnlyRefreshLoop shape used by US-472.
func runLinkPropagationTraverserRefreshLoop(
	ctx context.Context,
	t *linkPropagationTraverser,
	interval time.Duration,
	onError func(error),
) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := t.Refresh(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
