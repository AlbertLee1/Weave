package types

import (
	"errors"
	"fmt"
	"sort"
)

// ErrInterfaceConflict is the sentinel returned when MergeInterfaceProperties
// detects a property whose ObjectType-declared shape disagrees with the
// Interface-declared shape (e.g. different BaseType, mismatched array-ness, or
// a structural sub-type divergence). Conflicts are aggregated; the returned
// error wraps this sentinel.
var ErrInterfaceConflict = errors.New("interface/object property merge conflict")

// PropertyDef describes a single property as seen by either an Interface or an
// ObjectType: the canonical name, its DataType, and whether the property is
// nullable. The struct is intentionally minimal so the merge can be exercised
// in unit tests without dragging in the full pkg/oms Property surface.
type PropertyDef struct {
	APIName  string
	DataType DataType
	Nullable bool
}

// MergedProperty pairs a property name with its winning DataType plus a
// provenance marker. "interface-only" means the ObjectType inherits from the
// interface; "object-only" means the property is local; "merged" means both
// declared it compatibly.
type MergedProperty struct {
	PropertyDef
	Source string // "interface-only" | "object-only" | "merged"
}

// MergeInterfaceProperties merges the property sets contributed by an
// Interface and an ObjectType into a single resolved view. Compatibility rules:
//
//  1. Same APIName + structurally-equal DataType + same Nullable → merged.
//  2. Same APIName but different shape → conflict.
//  3. Either side alone → carried through with appropriate Source.
//
// The returned slice is sorted by APIName for deterministic snapshots. If any
// conflicts were detected the returned error wraps ErrInterfaceConflict and
// names each offending property.
func MergeInterfaceProperties(iface, object []PropertyDef) ([]MergedProperty, error) {
	byName := make(map[string]*MergedProperty, len(iface)+len(object))
	for _, p := range iface {
		byName[p.APIName] = &MergedProperty{PropertyDef: p, Source: "interface-only"}
	}
	var conflicts []string
	for _, p := range object {
		if existing, hit := byName[p.APIName]; hit {
			if dataTypeEqual(existing.DataType, p.DataType) && existing.Nullable == p.Nullable {
				existing.Source = "merged"
				continue
			}
			conflicts = append(conflicts, p.APIName)
			continue
		}
		byName[p.APIName] = &MergedProperty{PropertyDef: p, Source: "object-only"}
	}

	out := make([]MergedProperty, 0, len(byName))
	for _, m := range byName {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].APIName < out[j].APIName })

	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return out, fmt.Errorf("%w: %v", ErrInterfaceConflict, conflicts)
	}
	return out, nil
}

// dataTypeEqual is a structural equality test on DataType that ignores Go-map
// iteration order in the Fields case and handles nested SubType / Variants.
func dataTypeEqual(a, b DataType) bool {
	if a.Type != b.Type {
		return false
	}
	if !ptrEqual(a.Precision, b.Precision) || !ptrEqual(a.Scale, b.Scale) {
		return false
	}
	if (a.SubType == nil) != (b.SubType == nil) {
		return false
	}
	if a.SubType != nil && !dataTypeEqual(*a.SubType, *b.SubType) {
		return false
	}
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for k, av := range a.Fields {
		bv, ok := b.Fields[k]
		if !ok || !dataTypeEqual(av, bv) {
			return false
		}
	}
	if len(a.Variants) != len(b.Variants) {
		return false
	}
	for i := range a.Variants {
		if !dataTypeEqual(a.Variants[i], b.Variants[i]) {
			return false
		}
	}
	return true
}

func ptrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
