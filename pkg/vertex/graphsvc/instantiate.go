package graphsvc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Instantiate substitutes parameter values into a template payload at the
// JSON paths listed in parameterizedFields, and (when the payload carries a
// "searchAroundFnRid" string) returns the planned search-around calls in
// payload.searchAroundCalls for the client to execute.
//
// Path syntax mirrors the one the Vertex UI stores on save-as-template:
// dot-separated keys with optional bracketed array indices, e.g.
// "layers[0].filter.objectRid".
//
// Parameter binding uses the leaf segment of each path (e.g. "objectRid") as
// the key into params. A missing key leaves the field untouched — this lets
// templates carry placeholder defaults that survive a no-arg instantiate. A
// malformed path or one that overshoots the payload structure surfaces as a
// non-nil error so callers can reject the request with 400.
//
// The search-around shortcut activates when params contains "objectRids" as a
// JSON array of strings AND the payload root has a "searchAroundFnRid" string
// field. The output gains a top-level "searchAroundCalls" array of
// {functionRid, objectRid, params} entries — same shape as the existing TS
// helper at web/src/features/vertex/templates/parameterizedSearchAround.ts.
func Instantiate(payload json.RawMessage, parameterizedFields []string, params map[string]json.RawMessage) (json.RawMessage, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	var root any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	for _, field := range parameterizedFields {
		segs, err := splitPath(field)
		if err != nil {
			return nil, &InvalidTemplateFieldError{Field: field, Reason: err.Error()}
		}
		if len(segs) == 0 {
			continue
		}
		leaf := segs[len(segs)-1].key
		raw, ok := params[leaf]
		if !ok {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, &InvalidTemplateFieldError{Field: field, Reason: fmt.Sprintf("decode param %q: %v", leaf, err)}
		}
		if err := setAtPath(root, segs, value); err != nil {
			return nil, &InvalidTemplateFieldError{Field: field, Reason: err.Error()}
		}
	}

	if rootObj, ok := root.(map[string]any); ok {
		if calls, ok := planSearchAround(rootObj, params); ok {
			rootObj["searchAroundCalls"] = calls
		}
	}

	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode instantiated payload: %w", err)
	}
	return out, nil
}

// InvalidTemplateFieldError signals a path-resolution problem. Handlers should
// translate it to 400 with the field + reason surfaced in the body.
type InvalidTemplateFieldError struct {
	Field  string
	Reason string
}

func (e *InvalidTemplateFieldError) Error() string {
	return fmt.Sprintf("instantiate: bad parameterized field %q: %s", e.Field, e.Reason)
}

// pathSegment is one step in a dotted path; index ≥ 0 means an array index
// was attached (e.g. layers[0]).
type pathSegment struct {
	key   string
	index int  // -1 when not indexed
	array bool // true when bracketed index supplied
}

var segPattern = regexp.MustCompile(`^([^\[\]]+)(\[(\d+)\])?$`)

func splitPath(p string) ([]pathSegment, error) {
	if p == "" {
		return nil, fmt.Errorf("empty path")
	}
	parts := strings.Split(p, ".")
	out := make([]pathSegment, 0, len(parts))
	for _, part := range parts {
		m := segPattern.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("segment %q has invalid syntax", part)
		}
		seg := pathSegment{key: m[1], index: -1}
		if m[2] != "" {
			idx, err := strconv.Atoi(m[3])
			if err != nil {
				return nil, fmt.Errorf("segment %q has invalid index", part)
			}
			seg.array = true
			seg.index = idx
		}
		out = append(out, seg)
	}
	return out, nil
}

// setAtPath descends root following segs and assigns value at the leaf. The
// path must already exist; we deliberately do NOT auto-create missing
// containers because templates are snapshots — substituting into a phantom
// structure would silently hide author mistakes.
func setAtPath(root any, segs []pathSegment, value any) error {
	if len(segs) == 0 {
		return fmt.Errorf("empty path")
	}
	cur := root
	for i, seg := range segs {
		obj, ok := cur.(map[string]any)
		if !ok {
			return fmt.Errorf("segment %q: parent is not an object", seg.key)
		}
		child, exists := obj[seg.key]
		if !exists {
			return fmt.Errorf("segment %q: key not found", seg.key)
		}
		if seg.array {
			arr, ok := child.([]any)
			if !ok {
				return fmt.Errorf("segment %q: not an array", seg.key)
			}
			if seg.index < 0 || seg.index >= len(arr) {
				return fmt.Errorf("segment %q: index %d out of range (len=%d)", seg.key, seg.index, len(arr))
			}
			if i == len(segs)-1 {
				arr[seg.index] = value
				return nil
			}
			cur = arr[seg.index]
			continue
		}
		if i == len(segs)-1 {
			obj[seg.key] = value
			return nil
		}
		cur = child
	}
	return nil
}

// planSearchAround inspects the instantiated payload for a top-level
// "searchAroundFnRid" string + an "objectRids" parameter (string array). When
// both are present it returns a deduped list of {functionRid, objectRid,
// params:{}} call descriptors. Matches the TS planner so the client can
// execute the plan straight from the instantiate response.
func planSearchAround(root map[string]any, params map[string]json.RawMessage) ([]map[string]any, bool) {
	fnRaw, ok := root["searchAroundFnRid"].(string)
	if !ok || strings.TrimSpace(fnRaw) == "" {
		return nil, false
	}
	rawIDs, ok := params["objectRids"]
	if !ok {
		return nil, false
	}
	var ids []string
	if err := json.Unmarshal(rawIDs, &ids); err != nil || len(ids) == 0 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, map[string]any{
			"functionRid": fnRaw,
			"objectRid":   id,
			"params":      map[string]any{},
		})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
