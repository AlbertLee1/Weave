// Package funcregistry hosts the Vertex-side Function registry surfaces
// (VTX-048): metadata + I/O signature lookup, semver-range version
// resolution, and the registration-time parameter-type allowlist used to
// reject Function rows that try to declare unsupported types like
// "aggregation" or "notification".
//
// The package is HTTP-thin: pure-Go validators and resolvers live in
// paramtypes.go / semver_range.go so they can be reused by future
// runtime / SDK surfaces without dragging chi or the OMS repository in.
package funcregistry

import (
	"fmt"
	"strings"

	"github.com/liyang/weave/pkg/oms"
)

// allowedParameterTypes is the canonical set of strings a Function
// parameter or return `type` field may carry at registration time.
// Mirrors Palantir Foundry's "primitive scalars / Optional / Collection"
// subset: primitive scalars + array (the Collection wrapper). Optional
// is expressed by setting required=false on a param, not by a wrapper
// type, so it does not need an entry here.
var allowedParameterTypes = map[string]struct{}{
	"string":    {},
	"integer":   {},
	"short":     {},
	"long":      {},
	"float":     {},
	"double":    {},
	"boolean":   {},
	"byte":      {},
	"date":      {},
	"timestamp": {},
	"decimal":   {},
	"array":     {}, // Collection
}

// IsAllowedParameterType reports whether t may appear as a Function
// parameter or return type at registration time. The empty string is
// treated as "untyped" and accepted — the type system already permits
// signatures that elide the per-param type, and the runtime validator
// (oms.ValidateAndCoerceFunctionParams) short-circuits in that case.
func IsAllowedParameterType(t string) bool {
	if t == "" {
		return true
	}
	_, ok := allowedParameterTypes[t]
	return ok
}

// ParamTypeError is the typed error returned by ValidateParameterTypes
// when a parameter or return type falls outside the allowlist. The
// HTTP handler maps this to a 400 with a structured body so SDK callers
// can pinpoint the offending field without scraping the message.
//
// Parameter is the param name on a param-level rejection; for return
// type rejections it is empty and IsReturn is true so callers can
// disambiguate the two cases without nullable comparisons.
type ParamTypeError struct {
	Parameter string
	Type      string
	IsReturn  bool
}

// Error implements the error interface. The wording deliberately names
// "registration" so log lines and UI toasts make the rejection point
// explicit — this validator runs at write time, not execute time.
func (e *ParamTypeError) Error() string {
	if e.IsReturn {
		return fmt.Sprintf("return type %q is not allowed at registration (only primitive scalars and array Collections are supported)", e.Type)
	}
	return fmt.Sprintf("parameter %q has unsupported type %q at registration (only primitive scalars and array Collections are supported)", e.Parameter, e.Type)
}

// ValidateParameterTypes walks the parsed signature and returns the
// first *ParamTypeError it encounters. An empty / no-contract signature
// passes — every parameter is implicitly untyped. Params are checked in
// declaration order; the return type is checked last so a param-level
// rejection is surfaced before a return-level one (deterministic for
// tests and easier to fix incrementally for SDK callers).
func ValidateParameterTypes(sig oms.ParsedFunctionSignature) error {
	for _, p := range sig.Params {
		t := strings.TrimSpace(p.Type)
		if !IsAllowedParameterType(t) {
			return &ParamTypeError{Parameter: p.Name, Type: p.Type}
		}
	}
	if sig.Returns != nil {
		t := strings.TrimSpace(sig.Returns.Type)
		if !IsAllowedParameterType(t) {
			return &ParamTypeError{Type: sig.Returns.Type, IsReturn: true}
		}
	}
	return nil
}
