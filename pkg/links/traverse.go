package links

import (
	"context"
	"errors"
	"fmt"
)

// Hop names one step in a multi-hop link traversal.
type Hop struct {
	LinkTypeRID string
	Direction   Direction
}

// PermissionFilter is consulted after each hop to drop PKs the caller is not
// authorized to see. hopIndex is 0-based; outputObjectTypeRID identifies the
// ObjectType the pk belongs to *after* the hop has resolved. Return true to
// keep the pk in the working set, false to drop it.
type PermissionFilter func(ctx context.Context, hopIndex int, outputObjectTypeRID string, pk string) bool

// TraverseOptions tune a multi-hop traversal.
type TraverseOptions struct {
	// MaxHops > 0 caps the number of hops accepted. len(hops) above the cap
	// returns ErrTooManyHops without performing any resolution. 0 disables.
	MaxHops int
	// Permission, when non-nil, prunes PKs that the caller cannot see after
	// each hop. The drop count is reported per hop in TraverseAudit.Denied.
	Permission PermissionFilter
	// DisableCycleGuard turns off the visited-set pruning. Default behaviour
	// seeds the visited set with startPKs (namespaced by startObjectTypeRID)
	// and prunes any (objectTypeRID, pk) that re-appears in a later hop.
	DisableCycleGuard bool
}

// TraverseAudit reports per-hop statistics. All slices have one entry per hop
// (including hops that were short-circuited because the working set went
// empty), so len(.Inputs) == len(hops) is always true on a successful return.
type TraverseAudit struct {
	Inputs  []int // PKs entering the hop (post previous-hop pruning)
	Outputs []int // PKs returned by ResolveLinked, deduplicated
	Pruned  []int // PKs dropped by the cycle guard after this hop
	Denied  []int // PKs dropped by the Permission filter after this hop
}

// ErrTooManyHops is returned when len(hops) exceeds TraverseOptions.MaxHops.
var ErrTooManyHops = errors.New("links: too many hops")

// TraverseHops walks a sequence of link hops starting from startPKs.
//
// After every hop the working set is deduplicated, the cycle guard removes
// PKs whose (destinationObjectType, pk) pair has been emitted before (the
// initial set is seeded under startObjectTypeRID), and the optional
// Permission filter prunes inaccessible PKs. The destination ObjectType for
// each hop is derived from the link's Source/TargetObjectType plus the
// requested Direction.
//
// Empty startPKs or empty hops short-circuit to (unique(startPKs), zero
// audit, nil). ctx cancellation between hops returns the ctx error. Errors
// from ResolveLinked are wrapped with the failing hop index.
func (r *Resolver) TraverseHops(
	ctx context.Context,
	startObjectTypeRID string,
	startPKs []string,
	hops []Hop,
	opts TraverseOptions,
) ([]string, TraverseAudit, error) {
	audit := TraverseAudit{}
	if len(startPKs) == 0 || len(hops) == 0 {
		return uniqueStrings(startPKs), audit, nil
	}
	if opts.MaxHops > 0 && len(hops) > opts.MaxHops {
		return nil, audit, fmt.Errorf("%w: requested %d hops > MaxHops %d", ErrTooManyHops, len(hops), opts.MaxHops)
	}

	visited := make(map[string]struct{})
	if !opts.DisableCycleGuard {
		for _, pk := range startPKs {
			visited[visitedKey(startObjectTypeRID, pk)] = struct{}{}
		}
	}

	current := uniqueStrings(startPKs)
	for i, h := range hops {
		if err := ctx.Err(); err != nil {
			return nil, audit, err
		}

		lt, err := r.getLinkType(ctx, h.LinkTypeRID)
		if err != nil {
			return nil, audit, fmt.Errorf("hop %d: get link type: %w", i, err)
		}
		outOT := lt.TargetObjectType
		if h.Direction == DirectionReverse {
			outOT = lt.SourceObjectType
		}

		audit.Inputs = append(audit.Inputs, len(current))

		next, err := r.dispatch(ctx, lt, current, h.Direction)
		if err != nil {
			return nil, audit, fmt.Errorf("hop %d: %w", i, err)
		}
		next = uniqueStrings(next)
		audit.Outputs = append(audit.Outputs, len(next))

		pruned := 0
		if !opts.DisableCycleGuard {
			kept := next[:0]
			for _, pk := range next {
				key := visitedKey(outOT, pk)
				if _, ok := visited[key]; ok {
					pruned++
					continue
				}
				visited[key] = struct{}{}
				kept = append(kept, pk)
			}
			next = kept
		}
		audit.Pruned = append(audit.Pruned, pruned)

		denied := 0
		if opts.Permission != nil {
			kept := make([]string, 0, len(next))
			for _, pk := range next {
				if opts.Permission(ctx, i, outOT, pk) {
					kept = append(kept, pk)
					continue
				}
				denied++
			}
			next = kept
		}
		audit.Denied = append(audit.Denied, denied)

		current = next
		if len(current) == 0 {
			for j := i + 1; j < len(hops); j++ {
				audit.Inputs = append(audit.Inputs, 0)
				audit.Outputs = append(audit.Outputs, 0)
				audit.Pruned = append(audit.Pruned, 0)
				audit.Denied = append(audit.Denied, 0)
			}
			return nil, audit, nil
		}
	}
	return current, audit, nil
}

func visitedKey(objectTypeRID, pk string) string {
	return objectTypeRID + "|" + pk
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
