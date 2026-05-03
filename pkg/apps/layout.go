// Package apps implements the Weave Workshop-lite App Editor (US-391+):
// per-user / per-org Apps with versioned layout history and a JSON DSL
// describing the page tree.
//
// This file holds the Layout DSL types and validator. The layout schema
// is intentionally tiny so the wire shape can be evolved without a
// schema change — new component types or property keys land as opaque
// `props` payloads while the row/col grid stays validated.
package apps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Layout DSL node types. The PRD wire shape is:
//
//	{type: "row", children: [{type: "col", width: 6, child: {type: "component", componentType: "table", ...}}]}
//
// row → bag of column children spanning a 12-column grid.
// col → grid cell with an integer 1..12 width and a single child (row, col, or component).
// component → leaf, identified by componentType, with an opaque props bag.
const (
	NodeRow       = "row"
	NodeCol       = "col"
	NodeComponent = "component"
)

// MaxLayoutDepth bounds how deeply a layout may nest. Pathological
// clients could otherwise post a million-deep tree and blow the Go
// stack via the recursive validator.
const MaxLayoutDepth = 32

// MaxColumns is the grid width — total widths of a row's direct col
// children must not exceed this. Mirrors Bootstrap / antd grid
// convention so the front-end can render without a translation table.
const MaxColumns = 12

// LayoutError is the structured validation failure surfaced to API
// callers. Path is dotted segments ("children[0].child.componentType")
// so a SPA editor can highlight the offending node directly.
type LayoutError struct {
	Path string
	Msg  string
}

func (e *LayoutError) Error() string {
	if e.Path == "" {
		return e.Msg
	}
	return e.Path + ": " + e.Msg
}

// ValidateLayout parses raw and verifies it conforms to the Layout DSL.
// Empty / nil input is rejected — every App must have a root node, even
// if it's an empty single-component placeholder.
func ValidateLayout(raw json.RawMessage) error {
	if len(bytes.TrimSpace([]byte(raw))) == 0 {
		return &LayoutError{Path: "", Msg: "layout must not be empty"}
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return &LayoutError{Path: "", Msg: "invalid JSON: " + err.Error()}
	}
	// Strict decode: reject trailing tokens after the root object so
	// callers that paste two layouts can't get the second one silently
	// dropped.
	if dec.More() {
		return &LayoutError{Path: "", Msg: "unexpected trailing data after layout"}
	}
	return validateNode(root, "", 0)
}

// validateNode recursively validates a single Layout node. path is the
// dotted JSON path from root to the current node (used in error paths);
// depth is the current nesting depth (incremented on row/col descent).
func validateNode(v any, path string, depth int) error {
	if depth > MaxLayoutDepth {
		return &LayoutError{Path: path, Msg: fmt.Sprintf("layout depth exceeds %d", MaxLayoutDepth)}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return &LayoutError{Path: path, Msg: "node must be a JSON object"}
	}
	rawType, hasType := obj["type"]
	if !hasType {
		return &LayoutError{Path: path, Msg: `missing required "type" field`}
	}
	nodeType, ok := rawType.(string)
	if !ok {
		return &LayoutError{Path: pathJoin(path, "type"), Msg: `"type" must be a string`}
	}
	switch nodeType {
	case NodeRow:
		return validateRow(obj, path, depth)
	case NodeCol:
		return validateCol(obj, path, depth)
	case NodeComponent:
		return validateComponent(obj, path)
	case "":
		return &LayoutError{Path: pathJoin(path, "type"), Msg: `"type" must not be empty`}
	default:
		return &LayoutError{Path: pathJoin(path, "type"),
			Msg: fmt.Sprintf("unknown node type %q (allowed: row, col, component)", nodeType)}
	}
}

func validateRow(obj map[string]any, path string, depth int) error {
	rawChildren, hasChildren := obj["children"]
	if !hasChildren {
		return &LayoutError{Path: path, Msg: `"row" requires a "children" array`}
	}
	children, ok := rawChildren.([]any)
	if !ok {
		return &LayoutError{Path: pathJoin(path, "children"), Msg: `"children" must be an array`}
	}
	if len(children) == 0 {
		return &LayoutError{Path: pathJoin(path, "children"), Msg: `"children" must not be empty`}
	}
	totalWidth := 0
	for i, c := range children {
		childPath := pathIndex(pathJoin(path, "children"), i)
		childObj, ok := c.(map[string]any)
		if !ok {
			return &LayoutError{Path: childPath, Msg: "row child must be a JSON object"}
		}
		// Row's direct children must be cols (PRD wire shape):
		// components and nested rows live one level deeper inside cols.
		if t, _ := childObj["type"].(string); t != NodeCol {
			return &LayoutError{Path: pathJoin(childPath, "type"),
				Msg: fmt.Sprintf("row children must be %q nodes, got %q", NodeCol, t)}
		}
		if err := validateNode(childObj, childPath, depth+1); err != nil {
			return err
		}
		w, _ := readColumnWidth(childObj)
		totalWidth += w
	}
	if totalWidth > MaxColumns {
		return &LayoutError{Path: pathJoin(path, "children"),
			Msg: fmt.Sprintf("col width sum %d exceeds grid width %d", totalWidth, MaxColumns)}
	}
	return nil
}

func validateCol(obj map[string]any, path string, depth int) error {
	width, ok := readColumnWidth(obj)
	if !ok {
		return &LayoutError{Path: pathJoin(path, "width"),
			Msg: fmt.Sprintf(`"width" must be an integer in [1, %d]`, MaxColumns)}
	}
	if width < 1 || width > MaxColumns {
		return &LayoutError{Path: pathJoin(path, "width"),
			Msg: fmt.Sprintf(`"width" must be in [1, %d]`, MaxColumns)}
	}
	rawChild, hasChild := obj["child"]
	if !hasChild {
		return &LayoutError{Path: path, Msg: `"col" requires a "child" node`}
	}
	return validateNode(rawChild, pathJoin(path, "child"), depth+1)
}

func validateComponent(obj map[string]any, path string) error {
	rawCT, has := obj["componentType"]
	if !has {
		return &LayoutError{Path: path, Msg: `"component" requires a "componentType" string`}
	}
	ct, ok := rawCT.(string)
	if !ok {
		return &LayoutError{Path: pathJoin(path, "componentType"), Msg: `"componentType" must be a string`}
	}
	if ct == "" {
		return &LayoutError{Path: pathJoin(path, "componentType"), Msg: `"componentType" must not be empty`}
	}
	return nil
}

// readColumnWidth extracts a col node's width as an int. The decoder
// uses json.Number so we can tell 6 from 6.5 and reject the latter.
// Returns (width, ok).
func readColumnWidth(obj map[string]any) (int, bool) {
	rawW, has := obj["width"]
	if !has {
		return 0, false
	}
	num, ok := rawW.(json.Number)
	if !ok {
		return 0, false
	}
	// Reject non-integer widths (6.5 is not a valid grid span).
	if _, err := num.Int64(); err != nil {
		return 0, false
	}
	v, err := num.Int64()
	if err != nil {
		return 0, false
	}
	if v < 0 || v > 1<<31-1 {
		return 0, false
	}
	return int(v), true
}

func pathJoin(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func pathIndex(parent string, i int) string {
	return fmt.Sprintf("%s[%d]", parent, i)
}

// ErrInvalidLayout is the sentinel returned when a layout fails
// validation at the store boundary. Callers can errors.Is it to map
// validation failures to 400 InvalidAppLayout without unwrapping
// a *LayoutError.
var ErrInvalidLayout = errors.New("apps: invalid layout")
