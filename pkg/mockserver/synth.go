package mockserver

import "strings"

// synthesizer walks a JSON-Schema fragment and returns a sample value of
// the declared shape. It is constructed once per request so the visited
// $ref set is per-call (cheap) and never leaks across requests.
//
// Resolution rules:
//
//   - example wins for primitives (string / integer / number / boolean).
//     The OpenAPI spec uses `example` for hand-authored sample values and
//     a mock that serves the example back is the most useful behaviour
//     for SDK consumers reading their schema.
//   - enum (when no example) → first value, the canonical "default".
//   - $ref to components/schemas → resolved against the schema map; cycle
//     detection breaks the second visit by returning nil so the response
//     is finite even for self-referential models (Node.child -> Node).
//   - allOf merges the per-branch object property maps in declared order.
//   - oneOf / anyOf picks the first branch.
type synthesizer struct {
	schemas   map[string]any
	responses map[string]any
	visiting  map[string]int
}

func newSynthesizer(schemas, responses map[string]any) *synthesizer {
	return &synthesizer{
		schemas:   schemas,
		responses: responses,
		visiting:  map[string]int{},
	}
}

func (s *synthesizer) synthesize(schema map[string]any) any {
	if schema == nil {
		return nil
	}

	if ref, ok := schema["$ref"].(string); ok {
		return s.resolveRef(ref)
	}

	if alts, ok := schema["allOf"].([]any); ok {
		out := map[string]any{}
		for _, alt := range alts {
			if branch, ok := alt.(map[string]any); ok {
				if v, ok := s.synthesize(branch).(map[string]any); ok {
					for k, val := range v {
						out[k] = val
					}
				}
			}
		}
		return out
	}
	if alts, ok := schema["oneOf"].([]any); ok && len(alts) > 0 {
		if first, ok := alts[0].(map[string]any); ok {
			return s.synthesize(first)
		}
	}
	if alts, ok := schema["anyOf"].([]any); ok && len(alts) > 0 {
		if first, ok := alts[0].(map[string]any); ok {
			return s.synthesize(first)
		}
	}

	if ex, ok := schema["example"]; ok {
		return ex
	}

	if enumVals, ok := schema["enum"].([]any); ok && len(enumVals) > 0 {
		return enumVals[0]
	}

	switch typeOf(schema) {
	case "string":
		if def, ok := schema["default"].(string); ok {
			return def
		}
		// `format: date-time` etc → still default to empty string. Tests
		// that need a specific timestamp set `example` explicitly.
		return ""
	case "integer":
		if def, ok := schema["default"]; ok {
			return def
		}
		return int64(0)
	case "number":
		if def, ok := schema["default"]; ok {
			return def
		}
		return float64(0)
	case "boolean":
		if def, ok := schema["default"]; ok {
			return def
		}
		return false
	case "array":
		items, _ := schema["items"].(map[string]any)
		if items == nil {
			return []any{}
		}
		return []any{s.synthesize(items)}
	case "object", "":
		return s.synthesizeObject(schema)
	}
	return nil
}

func (s *synthesizer) synthesizeObject(schema map[string]any) any {
	out := map[string]any{}
	props, _ := schema["properties"].(map[string]any)
	for name, raw := range props {
		sub, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out[name] = s.synthesize(sub)
	}
	if len(out) == 0 {
		// `additionalProperties: <schema>` carries a sample shape too;
		// emit an empty map so callers can shape-check without crashing.
		if _, ok := schema["additionalProperties"]; ok {
			return map[string]any{}
		}
	}
	return out
}

// resolveRef walks `#/components/schemas/<Name>` (the only $ref shape
// emitted by our spec). Cycle detection: a ref visited twice in the same
// synthesise call returns nil so the response stays finite.
func (s *synthesizer) resolveRef(ref string) any {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	name := ref[len(prefix):]
	if s.visiting[name] >= 1 {
		return nil
	}
	target, ok := s.schemas[name].(map[string]any)
	if !ok {
		return nil
	}
	s.visiting[name]++
	defer func() { s.visiting[name]-- }()
	return s.synthesize(target)
}

func typeOf(schema map[string]any) string {
	if t, ok := schema["type"].(string); ok {
		return t
	}
	return ""
}
