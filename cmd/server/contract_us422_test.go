package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// US-422 OpenAPI quality contract gate.
//
// TestContract_AllRoutesDocumented (in contract_test.go) catches "route added
// but no spec entry". These quality gates catch the next failure mode: a spec
// entry that exists in name only — empty operation block, no responses, no
// summary, no tags, or a requestBody without a content schema. Together they
// turn the OpenAPI document into a strong contract instead of a presence
// checklist.

// extractSpecOperationDetails returns one entry per declared operation,
// keyed by (METHOD, path) so failure messages can name the offender directly.
func extractSpecOperationDetails(t *testing.T, doc map[string]any) map[specOperationKey]map[string]any {
	t.Helper()
	out := map[specOperationKey]map[string]any{}
	pathsRaw, ok := doc["paths"]
	if !ok {
		return out
	}
	paths, ok := pathsRaw.(map[string]any)
	if !ok {
		t.Fatalf("paths: expected map, got %T", pathsRaw)
	}
	verbs := map[string]string{
		"get":     "GET",
		"post":    "POST",
		"put":     "PUT",
		"delete":  "DELETE",
		"patch":   "PATCH",
		"head":    "HEAD",
		"options": "OPTIONS",
	}
	for path, item := range paths {
		pathItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for verb, methodName := range verbs {
			raw, has := pathItem[verb]
			if !has {
				continue
			}
			op, ok := raw.(map[string]any)
			if !ok {
				// Verb declared but value is not a map — record as empty so
				// the quality tests can flag it.
				op = map[string]any{}
			}
			out[specOperationKey{Method: methodName, Path: path}] = op
		}
	}
	return out
}

func nonEmptyMap(v any) bool {
	m, ok := v.(map[string]any)
	return ok && len(m) > 0
}

func nonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}

func nonEmptyList(v any) bool {
	l, ok := v.([]any)
	return ok && len(l) > 0
}

// TestContract_AllOperationsHaveResponses asserts that every documented
// operation declares at least one response. An operation with no responses
// fails clients that try to render expected status codes (the SDKs and the
// Swagger UI both depend on the responses block being non-empty).
func TestContract_AllOperationsHaveResponses(t *testing.T) {
	ops := extractSpecOperationDetails(t, loadCanonicalSpec(t))
	var bad []specOperationKey
	for key, op := range ops {
		if !nonEmptyMap(op["responses"]) {
			bad = append(bad, key)
		}
	}
	reportContractFailure(t, bad,
		"openapi.yaml has operations missing a non-empty `responses` block:",
		"Add at least one HTTP status entry under the `responses` key for each operation.")
}

// TestContract_AllOperationsHaveSummary asserts that every documented
// operation has a meaningful `summary` (or `description`). The Swagger UI
// renders the summary as the operation title; an empty title makes the
// generated docs unusable.
func TestContract_AllOperationsHaveSummary(t *testing.T) {
	ops := extractSpecOperationDetails(t, loadCanonicalSpec(t))
	var bad []specOperationKey
	for key, op := range ops {
		if !nonEmptyString(op["summary"]) && !nonEmptyString(op["description"]) {
			bad = append(bad, key)
		}
	}
	reportContractFailure(t, bad,
		"openapi.yaml has operations missing a `summary` or `description`:",
		"Add a one-line `summary:` (preferred) or full `description:` for each operation.")
}

// TestContract_AllOperationsHaveTags asserts that every documented operation
// is grouped under at least one tag. Without tags the Swagger UI dumps every
// operation in a single flat list, which is unmanageable for ~200 endpoints.
func TestContract_AllOperationsHaveTags(t *testing.T) {
	ops := extractSpecOperationDetails(t, loadCanonicalSpec(t))
	var bad []specOperationKey
	for key, op := range ops {
		if !nonEmptyList(op["tags"]) {
			bad = append(bad, key)
		}
	}
	reportContractFailure(t, bad,
		"openapi.yaml has operations missing a `tags:` entry:",
		"Add at least one tag (declared in the top-level `tags:` block) per operation.")
}

// TestContract_RequestBodyHasContentSchema asserts that any operation
// declaring a `requestBody` also defines its content/schema. A bare
// requestBody (or one with empty `content:`) tells SDK generators nothing
// about the wire shape and silently breaks generated client code.
func TestContract_RequestBodyHasContentSchema(t *testing.T) {
	ops := extractSpecOperationDetails(t, loadCanonicalSpec(t))
	var bad []specOperationKey
	for key, op := range ops {
		raw, has := op["requestBody"]
		if !has {
			continue
		}
		body, ok := raw.(map[string]any)
		if !ok {
			bad = append(bad, key)
			continue
		}
		if _, isRef := body["$ref"]; isRef {
			continue
		}
		if !nonEmptyMap(body["content"]) {
			bad = append(bad, key)
		}
	}
	reportContractFailure(t, bad,
		"openapi.yaml has operations whose `requestBody` lacks a non-empty `content:` map:",
		"Define `content.<media-type>.schema` for each request body so SDK generators emit typed parameters.")
}

func reportContractFailure(t *testing.T, bad []specOperationKey, header, footer string) {
	t.Helper()
	if len(bad) == 0 {
		return
	}
	sort.Slice(bad, func(i, j int) bool {
		if bad[i].Path != bad[j].Path {
			return bad[i].Path < bad[j].Path
		}
		return bad[i].Method < bad[j].Method
	})
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for _, k := range bad {
		fmt.Fprintf(&b, "  %s %s\n", k.Method, k.Path)
	}
	b.WriteString("\n")
	b.WriteString(footer)
	t.Fatal(b.String())
}
