// Package contract implements Pact-style consumer-driven contract tests for
// Weave's HTTP API. SDKs (Python, TS, Go, frontend) author pact JSON files
// describing their expected request/response interactions; the provider
// (cmd/server) replays each pact against a real chi router via VerifyPact and
// fails fast on any drift.
//
// The wire format is a small subset of the canonical Pact spec — enough to
// cover request/response shape verification with type / regex / presence
// matchers — and is deliberately CGO-free + zero new dependencies.
package contract

import (
	"encoding/json"
	"fmt"
	"os"
)

// Pact is the top-level contract artifact authored by an SDK consumer and
// verified by the provider.
type Pact struct {
	Consumer     Participant       `json:"consumer"`
	Provider     Participant       `json:"provider"`
	Interactions []Interaction     `json:"interactions"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Participant identifies one side of a contract — either the SDK that writes
// the pact (consumer) or the HTTP server that replays it (provider).
type Participant struct {
	Name string `json:"name"`
}

// Interaction is a single request/response pair the consumer asserts the
// provider supports.
type Interaction struct {
	Description   string   `json:"description"`
	ProviderState string   `json:"providerState,omitempty"`
	Request       Request  `json:"request"`
	Response      Response `json:"response"`
}

// Request describes how the consumer invokes the provider. Body MAY be omitted
// for GET/DELETE; Query and Headers are optional convenience fields.
type Request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// Response describes the response shape the consumer expects. Body holds the
// canonical example; Matchers overrides per-path comparison rules; Strict
// flips actual-may-have-extra-keys to actual-MUST-NOT-have-extra-keys.
type Response struct {
	Status   int                    `json:"status"`
	Headers  map[string]string      `json:"headers,omitempty"`
	Body     json.RawMessage        `json:"body,omitempty"`
	Matchers map[string]MatcherRule `json:"matchers,omitempty"`
	Strict   bool                   `json:"strict,omitempty"`
}

// MatcherRule overrides default exact-match comparison at a single body path.
//
// Match values:
//   - "exact"    — deep equality (the default; equivalent to omitting the rule)
//   - "type"     — actual must be the same JSON type as Value (string/number/integer/boolean/array/object/null)
//   - "regex"    — actual must be a string matching the regex in Value
//   - "presence" — key must exist (any value, including null)
//   - "ignore"   — skip comparison at this path entirely
type MatcherRule struct {
	Match string      `json:"match"`
	Value interface{} `json:"value,omitempty"`
}

// LoadPact reads and parses a pact file from disk.
func LoadPact(path string) (*Pact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("contract: read pact %q: %w", path, err)
	}
	return LoadPactBytes(data)
}

// LoadPactBytes parses a pact from a byte slice and validates the minimum
// schema required for verification.
func LoadPactBytes(data []byte) (*Pact, error) {
	var p Pact
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("contract: parse pact: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks that the pact carries the minimum fields the verifier needs.
func (p *Pact) Validate() error {
	for i, in := range p.Interactions {
		if in.Request.Method == "" {
			return fmt.Errorf("contract: interaction[%d] %q: request.method is required", i, in.Description)
		}
		if in.Request.Path == "" {
			return fmt.Errorf("contract: interaction[%d] %q: request.path is required", i, in.Description)
		}
		if in.Response.Status == 0 {
			return fmt.Errorf("contract: interaction[%d] %q: response.status is required", i, in.Description)
		}
	}
	return nil
}
