package oms

import (
	"fmt"
	"strings"
)

// CompositeKeySeparator is the delimiter the URL path / canonical-string
// representation uses to join composite-key components. Single-PK ObjectTypes
// treat the URL segment as opaque (the separator may legitimately appear
// inside the value), so this character is only consulted when the ObjectType
// declares more than one key property.
const CompositeKeySeparator = ":"

// ParseCompositeKey decodes the URL-segment representation of a primary key
// against an ObjectType expecting `expected` key properties.
//
//	expected == 1 → returns []string{raw} unchanged. The legacy single-PK
//	                value may itself contain ':' (e.g. RIDs), so no splitting
//	                happens. This preserves backward compatibility with every
//	                pre-US-211 client.
//	expected >  1 → splits on CompositeKeySeparator and requires exactly
//	                `expected` non-empty parts; anything else is an error.
//	expected <= 0 → an error (no key declared).
func ParseCompositeKey(raw string, expected int) ([]string, error) {
	if expected <= 0 {
		return nil, fmt.Errorf("object type has no primary key declared")
	}
	if expected == 1 {
		if raw == "" {
			return nil, fmt.Errorf("primaryKey value is empty")
		}
		return []string{raw}, nil
	}
	parts := strings.Split(raw, CompositeKeySeparator)
	if len(parts) != expected {
		return nil, fmt.Errorf("composite primary key requires %d parts separated by %q, got %d",
			expected, CompositeKeySeparator, len(parts))
	}
	for i, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("composite primary key part %d is empty", i)
		}
	}
	return parts, nil
}

// JoinCompositeKey produces the canonical URL-segment / Bleve-doc-ID form
// of a composite key. For a single-element slice it returns the lone value
// unchanged so single-PK objects round-trip identically to the pre-US-211
// behaviour.
func JoinCompositeKey(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, CompositeKeySeparator)
}
