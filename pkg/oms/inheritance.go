package oms

import (
	"context"
	"errors"
	"fmt"
)

// ErrInheritanceCycle is returned by the inheritance resolver when an
// ObjectType's `extends_rid` chain loops back on itself (A→B→A or longer).
// In single-parent inheritance this is the only "diamond"-style failure
// possible; the same sentinel is what admin handlers surface as a 400.
var ErrInheritanceCycle = errors.New("object type inheritance forms a cycle")

// MaxInheritanceDepth bounds chain walks defensively. The handler-layer
// cycle guard catches loops first; this is a backstop for malformed data
// where the chain is acyclic but unreasonably deep.
const MaxInheritanceDepth = 32

// inheritanceReader is the narrow Repository subset required to walk an
// ObjectType inheritance chain. The full Repository satisfies it; tests
// can swap in a smaller fake.
type inheritanceReader interface {
	GetObjectType(ctx context.Context, rid string) (*ObjectType, error)
	ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error)
	ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error)
}

// ResolvedObjectType is the merged view of an ObjectType combined with the
// transitive contributions of its `extends_rid` chain. Direct fields on the
// child win over inherited ones; child-declared properties / outgoing links
// override matching api-name entries from any parent.
type ResolvedObjectType struct {
	ObjectType
	// Properties is the merged property list keyed-by-apiName-then-overridden.
	// Order is parent-first within each level so older ancestors appear at
	// the head of the slice.
	Properties []Property
	// OutgoingLinkTypes carries the merged outgoing links via the same rule.
	OutgoingLinkTypes []LinkType
	// ExtendsChain lists ancestor RIDs from immediate parent to root, useful
	// for clients that want to walk the lineage without re-querying.
	ExtendsChain []string
}

// ResolveInheritedObjectType walks an ObjectType's `extends_rid` chain and
// returns a merged view. Properties and outgoing links from ancestors are
// included; child-declared entries (matching api_name) override the
// inherited ones. Cycle in the chain returns ErrInheritanceCycle.
func ResolveInheritedObjectType(ctx context.Context, repo inheritanceReader, ot *ObjectType) (*ResolvedObjectType, error) {
	if ot == nil {
		return nil, errors.New("ResolveInheritedObjectType: nil ObjectType")
	}

	chain, err := walkInheritanceChain(ctx, repo, ot)
	if err != nil {
		return nil, err
	}

	mergedProps := map[string]Property{}
	mergedLinks := map[string]LinkType{}
	propOrder := []string{}
	linkOrder := []string{}

	// Merge oldest ancestor first so later (more derived) entries overwrite.
	for i := len(chain) - 1; i >= 0; i-- {
		anc := chain[i]
		props := anc.Properties
		if props == nil {
			loaded, err := repo.ListProperties(ctx, anc.RID)
			if err != nil {
				return nil, fmt.Errorf("load properties for %s: %w", anc.RID, err)
			}
			props = loaded
		}
		for _, p := range props {
			if _, seen := mergedProps[p.APIName]; !seen {
				propOrder = append(propOrder, p.APIName)
			}
			mergedProps[p.APIName] = p
		}

		links, err := repo.ListOutgoingLinkTypes(ctx, anc.RID)
		if err != nil {
			return nil, fmt.Errorf("load outgoing links for %s: %w", anc.RID, err)
		}
		for _, lt := range links {
			if _, seen := mergedLinks[lt.APIName]; !seen {
				linkOrder = append(linkOrder, lt.APIName)
			}
			mergedLinks[lt.APIName] = lt
		}
	}

	resolved := &ResolvedObjectType{ObjectType: *ot}
	resolved.Properties = make([]Property, 0, len(propOrder))
	for _, name := range propOrder {
		resolved.Properties = append(resolved.Properties, mergedProps[name])
	}
	resolved.OutgoingLinkTypes = make([]LinkType, 0, len(linkOrder))
	for _, name := range linkOrder {
		resolved.OutgoingLinkTypes = append(resolved.OutgoingLinkTypes, mergedLinks[name])
	}
	if len(chain) > 1 {
		resolved.ExtendsChain = make([]string, 0, len(chain)-1)
		for _, anc := range chain[1:] {
			resolved.ExtendsChain = append(resolved.ExtendsChain, anc.RID)
		}
	}
	return resolved, nil
}

// walkInheritanceChain returns the inheritance chain from `ot` (index 0) up
// to its root ancestor. Cycles return ErrInheritanceCycle; the chain is
// also capped at MaxInheritanceDepth as a defensive backstop.
func walkInheritanceChain(ctx context.Context, repo inheritanceReader, ot *ObjectType) ([]*ObjectType, error) {
	chain := []*ObjectType{ot}
	visited := map[string]bool{ot.RID: true}
	current := ot
	for current.ExtendsRID != "" {
		if len(chain) > MaxInheritanceDepth {
			return nil, fmt.Errorf("inheritance chain exceeds depth %d", MaxInheritanceDepth)
		}
		if visited[current.ExtendsRID] {
			return nil, ErrInheritanceCycle
		}
		parent, err := repo.GetObjectType(ctx, current.ExtendsRID)
		if err != nil {
			return nil, fmt.Errorf("load parent %s: %w", current.ExtendsRID, err)
		}
		chain = append(chain, parent)
		visited[parent.RID] = true
		current = parent
	}
	return chain, nil
}

// ValidateInheritanceCandidate checks that setting `ot.ExtendsRID` to
// `parentRID` would not introduce a cycle. The caller has already populated
// `ot.RID` (so it can be added to the visited set) and is responsible for
// loading the candidate parent if extra validation (e.g. same-ontology) is
// required. Returns ErrInheritanceCycle on loop, nil otherwise.
func ValidateInheritanceCandidate(ctx context.Context, repo inheritanceReader, otRID, parentRID string) error {
	if parentRID == "" {
		return nil
	}
	if otRID == parentRID {
		return ErrInheritanceCycle
	}
	visited := map[string]bool{otRID: true}
	currentRID := parentRID
	for depth := 0; depth < MaxInheritanceDepth; depth++ {
		if visited[currentRID] {
			return ErrInheritanceCycle
		}
		visited[currentRID] = true
		parent, err := repo.GetObjectType(ctx, currentRID)
		if err != nil {
			return fmt.Errorf("load parent %s: %w", currentRID, err)
		}
		if parent.ExtendsRID == "" {
			return nil
		}
		currentRID = parent.ExtendsRID
	}
	return fmt.Errorf("inheritance chain exceeds depth %d", MaxInheritanceDepth)
}
