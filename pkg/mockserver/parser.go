// Package mockserver builds a stand-alone HTTP mock from an OpenAPI 3.x
// specification. It is the offline-test surface mirrored into the SDKs:
// SDK consumers point their generated client at the mock binary and get
// deterministic, schema-shaped responses without standing up the full
// Weave stack.
//
// The package has two halves:
//
//   - parser.go  walks an OpenAPI YAML/JSON document into a flat list of
//     (Method, Path, Status, Schema) operations, resolving $ref pointers
//     into components.schemas / components.responses.
//
//   - server.go  builds a chi.Router from those operations, synthesises a
//     sample response body per operation at boot, and dispatches incoming
//     requests via chi's templated path matcher. Per-route overrides take
//     precedence so test authors can pin specific status codes / payloads.
package mockserver

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Operation is a single (method, path, status, schema) tuple distilled
// from an OpenAPI document. The Schema map is the *resolved* response
// body schema (with $refs left in place — the synthesizer resolves them
// against Spec.Schemas at synth time so cycle detection is per-call).
type Operation struct {
	Method      string
	Path        string
	OperationID string
	Status      int
	ContentType string
	Schema      map[string]any
}

// Spec is the parsed form of an OpenAPI document.
type Spec struct {
	Operations []Operation
	Schemas    map[string]any // raw components.schemas map
	Responses  map[string]any // raw components.responses map
}

// ParseSpec parses an OpenAPI 3.x document (YAML or JSON; YAML is the
// superset we accept) into a flat list of mock-able operations.
func ParseSpec(data []byte) (*Spec, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi yaml: %w", err)
	}
	if doc == nil {
		return nil, errors.New("openapi document is empty")
	}

	pathsAny, ok := doc["paths"]
	if !ok {
		return nil, errors.New("openapi: missing paths section")
	}
	paths, ok := pathsAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("openapi: paths is %T, expected map", pathsAny)
	}

	schemas := lookupComponents(doc, "schemas")
	responses := lookupComponents(doc, "responses")

	verbs := []string{"get", "post", "put", "delete", "patch", "head", "options"}

	// Sort path keys for deterministic output (stable test assertions and
	// stable boot-time logs).
	pathKeys := make([]string, 0, len(paths))
	for p := range paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)

	var ops []Operation
	for _, path := range pathKeys {
		item, ok := paths[path].(map[string]any)
		if !ok {
			continue
		}
		for _, verb := range verbs {
			rawOp, ok := item[verb].(map[string]any)
			if !ok {
				continue
			}
			op, err := buildOperation(verb, path, rawOp, responses)
			if err != nil {
				return nil, fmt.Errorf("openapi: %s %s: %w", verb, path, err)
			}
			ops = append(ops, op)
		}
	}

	return &Spec{
		Operations: ops,
		Schemas:    schemas,
		Responses:  responses,
	}, nil
}

// lookupComponents reaches into doc.components.<key> and returns the
// nested map (or an empty map if absent / malformed). Callers depend on
// non-nil so synthesizer lookups never need a separate nil-check.
func lookupComponents(doc map[string]any, key string) map[string]any {
	cmp, ok := doc["components"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	m, ok := cmp[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// buildOperation extracts a single Operation from the raw OpenAPI
// operation map. It picks the lowest 2xx response status declared (200
// beats 201 beats 204) so the typical mock returns the most "filled-in"
// shape; callers can pin a specific status via Override.
//
// When no 2xx is declared at all, the operation falls through to the
// numerically-lowest status declared with 0 as the sentinel for "no
// declared response" — the dispatcher returns 200 + empty body in that
// case.
func buildOperation(verb, path string, raw, components map[string]any) (Operation, error) {
	upper := upperVerb(verb)
	op := Operation{Method: upper, Path: path}
	if id, ok := raw["operationId"].(string); ok {
		op.OperationID = id
	}

	respMap, _ := raw["responses"].(map[string]any)
	if respMap == nil {
		return op, nil
	}

	status, body, ct := pickResponseStatus(respMap, components)
	op.Status = status
	op.Schema = body
	op.ContentType = ct
	return op, nil
}

func upperVerb(v string) string {
	switch v {
	case "get":
		return http.MethodGet
	case "post":
		return http.MethodPost
	case "put":
		return http.MethodPut
	case "delete":
		return http.MethodDelete
	case "patch":
		return http.MethodPatch
	case "head":
		return http.MethodHead
	case "options":
		return http.MethodOptions
	}
	return v
}

// pickResponseStatus iterates the responses map and returns the lowest
// 2xx (200 < 201 < 204 < 299) along with the application/json schema for
// that status. Falls back to the lowest declared status overall if no
// 2xx exists. When no `default` is involved we never auto-select 4xx/5xx
// — those are explicit error shapes a mock should not emit by default.
func pickResponseStatus(responses, components map[string]any) (int, map[string]any, string) {
	type cand struct {
		status int
		raw    any
	}
	var twoxx []cand
	var others []cand
	for k, v := range responses {
		if k == "default" {
			continue
		}
		n, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		c := cand{status: n, raw: v}
		if n >= 200 && n < 300 {
			twoxx = append(twoxx, c)
		} else {
			others = append(others, c)
		}
	}
	pool := twoxx
	if len(pool) == 0 {
		pool = others
	}
	if len(pool) == 0 {
		return 0, nil, ""
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].status < pool[j].status })
	chosen := pool[0]
	body, ct := extractResponseBody(chosen.raw, components)
	return chosen.status, body, ct
}

// extractResponseBody resolves a single response object — either inline
// or via $ref to components.responses — to its application/json schema.
// Returns (nil, "") when no JSON content is declared (e.g. 204).
func extractResponseBody(raw any, components map[string]any) (map[string]any, string) {
	respObj := resolveResponseRef(raw, components)
	if respObj == nil {
		return nil, ""
	}
	contentMap, ok := respObj["content"].(map[string]any)
	if !ok {
		return nil, ""
	}
	// Prefer application/json; fall back to whatever's first declared
	// (sorted) so a spec that only ships text/plain still serves something.
	if json, ok := contentMap["application/json"].(map[string]any); ok {
		schema, _ := json["schema"].(map[string]any)
		return schema, "application/json"
	}
	keys := make([]string, 0, len(contentMap))
	for k := range contentMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry, ok := contentMap[k].(map[string]any)
		if !ok {
			continue
		}
		schema, _ := entry["schema"].(map[string]any)
		return schema, k
	}
	return nil, ""
}

// resolveResponseRef follows a single level of $ref (components.responses
// only — schemas are resolved later by the synthesizer). Returns the raw
// response object on success or nil if the ref dangles.
func resolveResponseRef(raw any, components map[string]any) map[string]any {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	if ref, ok := m["$ref"].(string); ok {
		const prefix = "#/components/responses/"
		if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
			name := ref[len(prefix):]
			if target, ok := components[name].(map[string]any); ok {
				return target
			}
		}
		return nil
	}
	return m
}
