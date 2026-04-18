package oms

import (
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/types"
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

// ParsedFunctionSignature is the typed view of a function's wire signature.
// Returned by ParseFunctionSignature so callers (the runtime validator, future
// SDK generators, doc tooling) can read params + returns directly without
// re-implementing the JSON shape walk.
type ParsedFunctionSignature struct {
	Params  []FunctionParam
	Returns *FunctionReturn
}

// FunctionParam is one entry of ParsedFunctionSignature.Params.
type FunctionParam struct {
	Name     string
	Type     string
	Required bool
	Default  json.RawMessage
}

// FunctionReturn is the parsed `returns` clause of a signature.
type FunctionReturn struct {
	Type string
}

// HasContract reports whether the signature declares any params or returns.
// An empty contract means "no contract declared" and the runtime validator
// short-circuits — every input is accepted.
func (s ParsedFunctionSignature) HasContract() bool {
	return len(s.Params) > 0 || s.Returns != nil
}

// ParseFunctionSignature decodes the wire signature into the typed view used
// by the runtime validator. An empty / null / "{}" raw message returns the
// zero-value ParsedFunctionSignature with HasContract()==false. Callers MUST
// have already passed the raw bytes through ValidateFunctionSignature; this
// helper assumes the shape is well-formed and returns a generic decode error
// otherwise.
func ParseFunctionSignature(raw json.RawMessage) (ParsedFunctionSignature, error) {
	trimmed := trimASCIISpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "{}" {
		return ParsedFunctionSignature{}, nil
	}
	var sig functionSignatureSchema
	if err := json.Unmarshal(trimmed, &sig); err != nil {
		return ParsedFunctionSignature{}, fmt.Errorf("invalid signature: %w", err)
	}
	out := ParsedFunctionSignature{}
	if len(sig.Params) > 0 {
		out.Params = make([]FunctionParam, len(sig.Params))
		for i, p := range sig.Params {
			out.Params[i] = FunctionParam{
				Name:     p.Name,
				Type:     p.Type,
				Required: p.Required,
				Default:  p.Default,
			}
		}
	}
	if sig.Returns != nil {
		out.Returns = &FunctionReturn{Type: sig.Returns.Type}
	}
	return out, nil
}

// FunctionParamError is the typed error returned by ValidateAndCoerceFunctionParams
// so HTTP handlers can surface a 400 with a structured `parameter`+`reason`
// payload without grepping the message string. Code is one of:
//   - "missing_required" — the param is required and no value was supplied
//   - "type_mismatch"    — the supplied value does not match the declared type
//   - "unknown_parameter"— the input map carries a name not declared in the signature
//   - "default_invalid"  — a declared default value cannot be decoded as JSON
type FunctionParamError struct {
	Parameter string
	Code      string
	Reason    string
}

// Error implements the error interface. The message stays human-readable for
// log output; structured handlers should reach for Parameter / Code directly.
func (e *FunctionParamError) Error() string {
	if e.Parameter == "" {
		return e.Reason
	}
	return fmt.Sprintf("parameter %q: %s", e.Parameter, e.Reason)
}

// ValidateAndCoerceFunctionParams enforces the signature contract against the
// caller-supplied params map. Behaviour:
//   - Empty / no-contract signature: returns the input map unchanged (every
//     input accepted, including unknown keys — an undeclared signature opts
//     out of validation).
//   - For each declared param: if missing AND required → missing_required.
//     If missing with a declared default → the default is decoded and placed
//     into the result map. If missing optional with no default → the key stays
//     absent in the output.
//   - For each supplied param: if a type is declared, the value must satisfy
//     types.Validate against {Type: BaseType(param.Type)}; otherwise it passes
//     through as-is. Optional params accept JSON null without complaint.
//   - Any input key NOT declared in the signature → unknown_parameter.
//
// The returned map is a fresh allocation; the caller may mutate it without
// affecting the input.
func ValidateAndCoerceFunctionParams(sig ParsedFunctionSignature, input map[string]interface{}) (map[string]interface{}, error) {
	if !sig.HasContract() {
		out := make(map[string]interface{}, len(input))
		for k, v := range input {
			out[k] = v
		}
		return out, nil
	}

	declared := make(map[string]FunctionParam, len(sig.Params))
	for _, p := range sig.Params {
		declared[p.Name] = p
	}

	for name := range input {
		if _, ok := declared[name]; !ok {
			return nil, &FunctionParamError{
				Parameter: name,
				Code:      "unknown_parameter",
				Reason:    fmt.Sprintf("parameter %q is not declared in the function signature", name),
			}
		}
	}

	out := make(map[string]interface{}, len(sig.Params))
	for _, p := range sig.Params {
		val, present := input[p.Name]

		if !present || val == nil {
			if len(p.Default) > 0 {
				var dv interface{}
				if err := json.Unmarshal(p.Default, &dv); err != nil {
					return nil, &FunctionParamError{
						Parameter: p.Name,
						Code:      "default_invalid",
						Reason:    fmt.Sprintf("default value is not valid JSON: %v", err),
					}
				}
				out[p.Name] = dv
				continue
			}
			if p.Required {
				return nil, &FunctionParamError{
					Parameter: p.Name,
					Code:      "missing_required",
					Reason:    fmt.Sprintf("required parameter %q is missing", p.Name),
				}
			}
			continue
		}

		if p.Type != "" {
			dt := types.DataType{Type: types.BaseType(p.Type)}
			if err := types.Validate(val, dt, !p.Required); err != nil {
				return nil, &FunctionParamError{
					Parameter: p.Name,
					Code:      "type_mismatch",
					Reason:    err.Error(),
				}
			}
		}
		out[p.Name] = val
	}

	return out, nil
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
