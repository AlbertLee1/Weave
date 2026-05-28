package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// PRD-V2 Gap-D4 round 46: MCP completion/complete protocol method.
//
// The MCP spec defines completion/complete so AI clients can request
// autocomplete suggestions for prompt arguments and resource template
// variables as the user types. Without this method, clients fall
// straight to a typed-text-only UX.
//
// Wire shape (MCP spec):
//
//   request: {
//     "method": "completion/complete",
//     "params": {
//       "ref": {
//         "type": "ref/prompt"   | "ref/resource",
//         "name": "<prompt name>" | "uri": "<resource template URI>"
//       },
//       "argument": {
//         "name":  "<arg or variable name>",
//         "value": "<user-typed prefix>"
//       }
//     }
//   }
//
//   response: {
//     "completion": {
//       "values":  [..up to 100 strings..],
//       "total":   <int>,
//       "hasMore": <bool>
//     }
//   }
//
// Round 46 ships the protocol handler with an extensible provider
// hook (CompletionSource interface). Production wires the default
// provider that yields ontology-derived completions:
//
//   - prompt argument "objectType"  → matching ObjectType apiNames
//   - prompt argument "actionType"  → matching ActionType apiNames
//
// Any unrecognized (ref, argument) pair returns an empty completion
// set, which is valid per the spec. Future rounds can extend the
// provider with more sources (LinkType names, ValueType names,
// arbitrary registered enums, etc.) without touching the handler.

// maxCompletionValues caps the response per the MCP spec (the spec
// says implementations SHOULD return at most 100 values).
const maxCompletionValues = 100

// CompletionRef identifies what the user is completing — either a
// prompt argument or a resource template variable.
type CompletionRef struct {
	Type string `json:"type"`           // "ref/prompt" or "ref/resource"
	Name string `json:"name,omitempty"` // populated for ref/prompt
	URI  string `json:"uri,omitempty"`  // populated for ref/resource
}

// CompletionArgument carries the argument the user is completing and
// the prefix typed so far.
type CompletionArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CompletionParams is the wire shape of completion/complete params.
type CompletionParams struct {
	Ref      CompletionRef      `json:"ref"`
	Argument CompletionArgument `json:"argument"`
}

// CompletionValues is the inner shape of the completion response.
type CompletionValues struct {
	Values  []string `json:"values"`
	Total   int      `json:"total"`
	HasMore bool     `json:"hasMore"`
}

// CompletionSource is the pluggable hook a Server consults to
// resolve a (ref, argument) pair into completion candidates. A nil
// source is the same as a provider that always yields the empty
// set — the protocol handler still works and returns valid empty
// responses, just without ontology-derived completions.
type CompletionSource interface {
	// Complete returns the matching candidates for the given ref +
	// argument prefix. Implementations should:
	//   - filter by prefix on the argument's Value
	//   - sort deterministically (callers expect stable order across
	//     calls so the UI doesn't jump)
	//   - return at most ``limit`` results; the handler enforces the
	//     spec-mandated 100-value cap on top.
	Complete(ctx context.Context, ref CompletionRef, arg CompletionArgument, limit int) ([]string, error)
}

// SetCompletionSource wires the ontology-aware completion provider.
// Pass nil to disable provider-backed completions (the handler still
// answers completion/complete but always with an empty set).
func (s *Server) SetCompletionSource(src CompletionSource) {
	s.completionSource = src
}

// handleCompletionComplete implements the MCP completion/complete
// method. Returns -32602 InvalidParams on malformed wire shapes
// (missing ref.type, unknown ref.type, missing argument.name).
// Returns -32603 InternalError if the provider call fails — the
// envelope still satisfies the JSON-RPC contract so clients can
// surface the error to the user.
func (s *Server) handleCompletionComplete(ctx context.Context, req *Request) *Response {
	var params CompletionParams
	if err := unmarshalParams(req, &params); err != nil {
		return NewErrorResponse(req.ID, CodeInvalidParams,
			"completion/complete: invalid params: "+err.Error(), nil)
	}
	if params.Ref.Type == "" {
		return NewErrorResponse(req.ID, CodeInvalidParams,
			"completion/complete: ref.type is required", nil)
	}
	switch params.Ref.Type {
	case "ref/prompt":
		if params.Ref.Name == "" {
			return NewErrorResponse(req.ID, CodeInvalidParams,
				`completion/complete: ref.name is required when ref.type="ref/prompt"`, nil)
		}
	case "ref/resource":
		if params.Ref.URI == "" {
			return NewErrorResponse(req.ID, CodeInvalidParams,
				`completion/complete: ref.uri is required when ref.type="ref/resource"`, nil)
		}
	default:
		return NewErrorResponse(req.ID, CodeInvalidParams,
			fmt.Sprintf(`completion/complete: unsupported ref.type %q`, params.Ref.Type), nil)
	}
	if params.Argument.Name == "" {
		return NewErrorResponse(req.ID, CodeInvalidParams,
			"completion/complete: argument.name is required", nil)
	}

	var values []string
	if s.completionSource != nil {
		out, err := s.completionSource.Complete(ctx, params.Ref, params.Argument, maxCompletionValues)
		if err != nil {
			return NewErrorResponse(req.ID, CodeInternalError,
				"completion/complete: provider error: "+err.Error(), nil)
		}
		values = out
	}
	// Enforce the 100-value cap defensively even when the provider
	// returned more. hasMore is true iff we truncated. total is the
	// pre-truncation count so the client can show "X / Y total".
	total := len(values)
	hasMore := false
	if len(values) > maxCompletionValues {
		values = values[:maxCompletionValues]
		hasMore = true
	}
	return NewSuccessResponse(req.ID, map[string]any{
		"completion": CompletionValues{
			Values:  append([]string{}, values...),
			Total:   total,
			HasMore: hasMore,
		},
	})
}

// unmarshalParams pulls the params field off a Request into the
// given destination. Returns a clear error when params is empty or
// the JSON is malformed. Kept local because the existing
// pkg/mcp helpers expect map[string]any.
func unmarshalParams(req *Request, dst any) error {
	if len(req.Params) == 0 {
		return fmt.Errorf("params is required")
	}
	return json.Unmarshal(req.Params, dst)
}

// PrefixFilter is a small helper a CompletionSource implementation
// can use to filter + sort + dedupe a candidate list by the user's
// typed prefix (case-insensitive). Exposed because every realistic
// source pulls the same matcher shape.
func PrefixFilter(candidates []string, prefix string, limit int) []string {
	low := strings.ToLower(prefix)
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if low != "" && !strings.HasPrefix(strings.ToLower(c), low) {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
