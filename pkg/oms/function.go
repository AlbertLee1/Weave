package oms

import (
	"encoding/json"
	"fmt"
)

// Function runtime values pinned by the registry. The migration carries the
// matching CHECK constraint at the DB layer; Validate() repeats the check at
// write time so the API surface returns a typed 400 instead of a generic
// insert failure.
const (
	FunctionRuntimeGoja = "goja"
	FunctionRuntimeHTTP = "http"
)

// NormalisedRuntime returns the runtime field with the empty default
// substituted to "goja". Callers that dispatch on runtime should always read
// through this helper so the absent-runtime case routes the same way the DB
// default does.
func (f Function) NormalisedRuntime() string {
	if f.Runtime == "" {
		return FunctionRuntimeGoja
	}
	return f.Runtime
}

// Validate checks the required shape of a Function row. Repository writes and
// admin handlers run this before persisting so a misconfigured row never
// reaches the executor.
func (f Function) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("function requires name")
	}
	if f.SourceCode == "" {
		return fmt.Errorf("function requires sourceCode")
	}
	switch f.NormalisedRuntime() {
	case FunctionRuntimeGoja, FunctionRuntimeHTTP:
	default:
		return fmt.Errorf("function runtime %q is not supported (expected goja or http)", f.Runtime)
	}
	if err := ValidateFunctionSignature(f.Signature); err != nil {
		return fmt.Errorf("function signature: %w", err)
	}
	return nil
}

// functionSignatureSchema mirrors the JSON shape declared in US-215:
//
//	{
//	  "params": [{"name","type","required","default"}, ...],
//	  "returns": {"type"}
//	}
//
// We keep the wire shape as json.RawMessage on the model so callers can store
// arbitrary forward-compatible additions; ValidateFunctionSignature enforces
// only the invariants the runtime validator depends on.
type functionSignatureSchema struct {
	Params  []functionSignatureParam `json:"params"`
	Returns *functionSignatureReturn `json:"returns"`
}

type functionSignatureParam struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Default  json.RawMessage `json:"default,omitempty"`
}

type functionSignatureReturn struct {
	Type string `json:"type"`
}

// ValidateFunctionSignature accepts an empty / null signature (no contract
// declared) and otherwise enforces the params-array + returns-object shape
// the runtime validator depends on. Each param must carry a name; if returns
// is present it must declare a type.
func ValidateFunctionSignature(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	trimmed := trimASCIISpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "{}" {
		return nil
	}

	// First pass: catch JSON shape errors with full fidelity (e.g. params
	// being an object instead of an array). We decode into a peek struct
	// that surfaces params/returns as RawMessage so the strong typing
	// happens in the second pass below and we can produce specific errors.
	var peek struct {
		Params  json.RawMessage `json:"params"`
		Returns json.RawMessage `json:"returns"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	if len(peek.Params) > 0 {
		paramsTrimmed := trimASCIISpace(peek.Params)
		if len(paramsTrimmed) == 0 || paramsTrimmed[0] != '[' {
			return fmt.Errorf("params must be an array")
		}
	}
	if len(peek.Returns) > 0 {
		returnsTrimmed := trimASCIISpace(peek.Returns)
		if len(returnsTrimmed) == 0 || returnsTrimmed[0] != '{' {
			return fmt.Errorf("returns must be an object")
		}
	}

	var sig functionSignatureSchema
	if err := json.Unmarshal(raw, &sig); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}
	for i, p := range sig.Params {
		if p.Name == "" {
			return fmt.Errorf("params[%d]: name is required", i)
		}
	}
	if sig.Returns != nil && sig.Returns.Type == "" {
		return fmt.Errorf("returns.type is required")
	}
	return nil
}

// normaliseSignatureForWrite returns the JSONB payload to persist for the
// given signature. An empty / null signature is stored as the canonical empty
// object so the column's NOT NULL DEFAULT '{}' invariant holds at every
// write site. Callers MUST pass through this helper rather than handing
// raw nil to the driver — pgx encodes nil JSON as the string "null", which
// the JSONB column happily accepts but breaks the "empty signature ⇒ {}"
// round-trip the read path relies on.
func normaliseSignatureForWrite(raw json.RawMessage) []byte {
	trimmed := trimASCIISpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return []byte("{}")
	}
	return trimmed
}

// signatureFromBytes decodes a JSONB column into the wire shape the API
// returns. Stored empty objects collapse back to nil so JSON marshalling
// honours the omitempty tag — clients see the absence of a signature
// rather than a confusing literal "{}" with no params/returns.
func signatureFromBytes(raw []byte) json.RawMessage {
	trimmed := trimASCIISpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "{}" || string(trimmed) == "null" {
		return nil
	}
	out := make(json.RawMessage, len(trimmed))
	copy(out, trimmed)
	return out
}

func trimASCIISpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end {
		switch b[start] {
		case ' ', '\t', '\n', '\r':
			start++
			continue
		}
		break
	}
	for end > start {
		switch b[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
			continue
		}
		break
	}
	return b[start:end]
}
