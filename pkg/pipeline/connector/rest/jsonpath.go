package rest

import (
	"fmt"
	"strconv"
	"strings"
)

// walkPath traverses a parsed JSON value (the result of
// json.Unmarshal into `any`) along a dotted path and returns the
// terminal value. Keys are interpreted as map[string]any field
// names; integer segments are interpreted as []any indices.
//
// The leading "$." prefix used by JSONPath-style configs is
// stripped, so "$.data.items" and "data.items" are equivalent.
//
// An empty path returns the input unchanged ("the whole payload is
// the records array" sentinel for extractRecords).
//
// A missing intermediate or final key is a hard error so callers can
// distinguish "the path is wrong" from "the path resolves to an
// empty array". For optional paths (e.g. the next-cursor token),
// callers should use walkPathOptional instead.
func walkPath(root any, path string) (any, error) {
	segments := splitPath(path)
	cur := root
	for i, seg := range segments {
		next, ok, err := stepInto(cur, seg)
		if err != nil {
			return nil, fmt.Errorf("rest: jsonPath %q segment[%d]=%q: %w", path, i, seg, err)
		}
		if !ok {
			return nil, fmt.Errorf("rest: jsonPath %q segment[%d]=%q: not found", path, i, seg)
		}
		cur = next
	}
	return cur, nil
}

// walkPathOptional is the missing-key-tolerant sibling of walkPath.
// Returns (nil, nil) when any segment is missing OR when the
// terminal value is JSON null. Used for cursor extraction where the
// absence of a next-cursor field means "end of stream".
func walkPathOptional(root any, path string) (any, error) {
	segments := splitPath(path)
	cur := root
	for i, seg := range segments {
		next, ok, err := stepInto(cur, seg)
		if err != nil {
			return nil, fmt.Errorf("rest: jsonPath %q segment[%d]=%q: %w", path, i, seg, err)
		}
		if !ok {
			return nil, nil
		}
		cur = next
	}
	return cur, nil
}

// splitPath turns a dotted path into per-segment strings. Strips a
// leading "$." (JSONPath compatibility), trims whitespace, and
// returns nil for the empty path so walkPath's terminal "return cur"
// hits on the first iteration.
func splitPath(path string) []string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$") // bare "$" addresses root
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// stepInto descends one path segment from cur. Returns:
//   - (next, true, nil)  — segment resolved
//   - (nil,  false, nil) — segment refers to a missing key / index
//   - (nil,  false, err) — segment is structurally inapplicable
//     (e.g. addressing a key on a primitive, integer segment on a
//     map, …)
//
// Integer-shaped segments take the array-index branch when cur is a
// []any; the same segment falls back to a map lookup when cur is a
// map[string]any (lets paths like "items.0" address arrays AND
// "labels.123" address numeric-keyed maps).
func stepInto(cur any, seg string) (any, bool, error) {
	switch v := cur.(type) {
	case map[string]any:
		next, ok := v[seg]
		return next, ok, nil
	case []any:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return nil, false, fmt.Errorf("array requires integer index, got %q", seg)
		}
		if idx < 0 || idx >= len(v) {
			return nil, false, nil
		}
		return v[idx], true, nil
	case nil:
		// Walking through a JSON null is equivalent to "the value
		// doesn't exist at this point" — let the caller decide
		// whether that's an error (walkPath) or a clean miss
		// (walkPathOptional).
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("cannot descend into %T", cur)
	}
}
