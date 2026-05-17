// Package strategies implements the four canonical mask strategies wired by
// US-433: HASH (sha256-prefixed hex), REDACT (literal "***"), NULL (collapse
// to nil), and PARTIAL (preserve first/last two characters, mask everything
// in between).
//
// The package is intentionally narrow: it owns the *value transforms* only.
// Strategy taxonomy types and call-site dispatch (CellMask / ColumnMask
// engine plumbing) live in the parent pkg/masking package and delegate here
// so a single source of truth produces every masked byte on the wire.
package strategies

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Name is the canonical wire-format identifier for a strategy.
type Name string

const (
	NameHash    Name = "HASH"
	NameRedact  Name = "REDACT"
	NameNull    Name = "NULL"
	NamePartial Name = "PARTIAL"
	NameFPE     Name = "FPE"
	NameRegex   Name = "REGEX"
)

// RedactReplacement is the constant string emitted by the REDACT strategy.
const RedactReplacement = "***"

// PartialKeepEdge is the number of leading and trailing characters Partial
// preserves. Strings shorter than 2*PartialKeepEdge are fully masked so no
// source byte leaks.
const PartialKeepEdge = 2

// Hash returns sha256:<hex> of the stringified value. Non-string primitives
// are stringified via fmt.Sprint so admins can hash numeric ids and bool
// flags without coercion at the call site.
func Hash(value interface{}) string {
	s := toString(value)
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Redact returns the literal RedactReplacement constant. Shape-stable across
// every value type — callers reading the masked column always see "***".
func Redact() string {
	return RedactReplacement
}

// Null returns nil regardless of input. The cell on the wire becomes JSON
// null, which downstream consumers MUST treat as "value withheld" and not
// "field absent".
func Null() interface{} {
	return nil
}

// Partial preserves the first PartialKeepEdge and last PartialKeepEdge
// characters of the stringified value, replacing everything in between with
// asterisks. Strings whose length is <= 2*PartialKeepEdge are fully masked
// so no edge-byte leaks.
func Partial(value interface{}) interface{} {
	s := toString(value)
	if s == "" {
		return ""
	}
	n := len(s)
	if n <= 2*PartialKeepEdge {
		return strings.Repeat("*", n)
	}
	return s[:PartialKeepEdge] + strings.Repeat("*", n-2*PartialKeepEdge) + s[n-PartialKeepEdge:]
}

// Apply dispatches value through the named strategy. Unknown names pass the
// value through unchanged so a stale cached policy (e.g. a strategy added
// in a newer release that hasn't propagated to this binary) never panics in
// production. nil inputs short-circuit to nil for HASH / REDACT / PARTIAL;
// NULL collapses any input to nil regardless.
//
// FPE and REGEX are NOT dispatchable through Apply — they require per-mask
// configuration (key/tweak/radix or pattern/replacement). Callers must route
// those through ApplyWithConfig. To preserve the back-compat contract, Apply
// passes FPE/REGEX inputs through unchanged rather than panicking; this
// matches the "unknown name" fall-through and prevents a runtime regression
// against any caller still on the legacy two-arg signature.
func Apply(name Name, value interface{}) interface{} {
	if name == NameNull {
		return Null()
	}
	if value == nil {
		return nil
	}
	switch name {
	case NameHash:
		return Hash(value)
	case NameRedact:
		return Redact()
	case NamePartial:
		return Partial(value)
	default:
		return value
	}
}

// ApplyConfig bundles the per-strategy configuration used by ApplyWithConfig.
// HASH / REDACT / NULL / PARTIAL ignore the field entirely; FPE / REGEX
// require the matching sub-config. Splitting the config into nested structs
// (rather than a flat map) keeps "valid config for strategy X" type-checkable
// at the call site.
type ApplyConfig struct {
	FPE   FPEConfig
	Regex RegexConfig
}

// ApplyWithConfig is the config-aware US-489 extension of Apply. It returns
// (value, error) so callers can fail closed on misconfigured FPE keys / bad
// alphabets without conflating them with "strategy not applicable, pass
// through". Strategies that don't need config are forwarded to Apply so the
// transform table on the wire stays a single source of truth.
//
// nil inputs short-circuit to nil for every strategy except NULL (which
// collapses to nil regardless). Unknown names pass through with a nil error,
// matching Apply's stale-policy semantics.
func ApplyWithConfig(name Name, value interface{}, cfg ApplyConfig) (interface{}, error) {
	if name == NameNull {
		return Null(), nil
	}
	if value == nil {
		return nil, nil
	}
	switch name {
	case NameFPE:
		s := toString(value)
		out, err := FPE(s, cfg.FPE)
		if err != nil {
			return value, err
		}
		return out, nil
	case NameRegex:
		return Regex(value, cfg.Regex), nil
	default:
		// Delegate the four legacy strategies + the unknown fall-through to
		// the config-free Apply so the legacy contract stays byte-identical.
		return Apply(name, value), nil
	}
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
