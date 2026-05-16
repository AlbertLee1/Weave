package masking

import (
	"strings"

	"github.com/liyang/weave/pkg/masking/strategies"
)

// MaskStrategy is the US-376 wire-format strategy taxonomy
// (REDACT | HASH | NULL | PARTIAL). It overlaps with the legacy MaskRule
// vocabulary (hash | redact | partial) but adds an explicit NULL strategy that
// rewrites the cell to JSON null and uses the canonical Foundry-style
// uppercase identifiers so CEL-driven masks remain self-describing on the
// wire.
//
// MaskRule stays as the backwards-compat surface so US-257 / US-258 callers
// keep working unchanged. NewStrategyFromRule / RuleFromStrategy provide the
// crosswalk for the engine fall-through path: when a CellMask author specifies
// only the legacy mask_rule, the engine maps it onto a strategy at evaluation
// time so a single dispatch covers both surfaces.
type MaskStrategy string

const (
	MaskStrategyRedact  MaskStrategy = "REDACT"
	MaskStrategyHash    MaskStrategy = "HASH"
	MaskStrategyNull    MaskStrategy = "NULL"
	MaskStrategyPartial MaskStrategy = "PARTIAL"
	// MaskStrategyFPE applies NIST SP 800-38G FF1 format-preserving
	// encryption. Requires per-mask config (AES key, tweak, radix or
	// alphabet) — not addressable through ApplyMaskStrategy; route through
	// ApplyMaskStrategyWithConfig / ApplyStrategyTransformsWithConfig.
	MaskStrategyFPE MaskStrategy = "FPE"
	// MaskStrategyRegex applies a regex.ReplaceAllString rewrite. Requires
	// per-mask config (compiled pattern + replacement).
	MaskStrategyRegex MaskStrategy = "REGEX"
)

// IsKnownStrategy reports whether s is a recognised MaskStrategy.
func IsKnownStrategy(s MaskStrategy) bool {
	switch s {
	case MaskStrategyRedact, MaskStrategyHash, MaskStrategyNull, MaskStrategyPartial,
		MaskStrategyFPE, MaskStrategyRegex:
		return true
	default:
		return false
	}
}

// NormalizeStrategy upper-cases s and trims whitespace; admins frequently
// post lower-case strategies in tests / curl. An unrecognised value returns
// the empty MaskStrategy so callers can branch on IsKnownStrategy after the
// canonicalisation pass.
func NormalizeStrategy(s MaskStrategy) MaskStrategy {
	out := MaskStrategy(strings.ToUpper(strings.TrimSpace(string(s))))
	if !IsKnownStrategy(out) {
		return ""
	}
	return out
}

// StrategyFromRule maps a legacy MaskRule onto the new MaskStrategy taxonomy.
// Unknown rules return the empty strategy so engine code can fall through
// without an explicit nil check.
func StrategyFromRule(r MaskRule) MaskStrategy {
	switch r {
	case MaskRuleHash:
		return MaskStrategyHash
	case MaskRuleRedact:
		return MaskStrategyRedact
	case MaskRulePartial:
		return MaskStrategyPartial
	default:
		return ""
	}
}

// RuleFromStrategy is the inverse mapping. NULL has no MaskRule equivalent
// so it returns the empty rule; callers should prefer ApplyMaskStrategy when
// they may encounter a NULL-strategy mask.
func RuleFromStrategy(s MaskStrategy) MaskRule {
	switch s {
	case MaskStrategyHash:
		return MaskRuleHash
	case MaskStrategyRedact:
		return MaskRuleRedact
	case MaskStrategyPartial:
		return MaskRulePartial
	default:
		return ""
	}
}

// ApplyMaskStrategy rewrites value according to s. Unknown strategies pass
// the value through unchanged so a stale cached policy never panics in
// production. NULL collapses any non-nil input to nil. The actual transform
// implementations live in pkg/masking/strategies (US-433) — this function is
// the strategy-taxonomy → algorithm dispatch shim.
func ApplyMaskStrategy(s MaskStrategy, value interface{}) interface{} {
	return strategies.Apply(strategyToName(s), value)
}

func strategyToName(s MaskStrategy) strategies.Name {
	switch s {
	case MaskStrategyHash:
		return strategies.NameHash
	case MaskStrategyRedact:
		return strategies.NameRedact
	case MaskStrategyNull:
		return strategies.NameNull
	case MaskStrategyPartial:
		return strategies.NamePartial
	case MaskStrategyFPE:
		return strategies.NameFPE
	case MaskStrategyRegex:
		return strategies.NameRegex
	default:
		return strategies.Name(s)
	}
}

// ApplyStrategyTransforms is the MaskStrategy analogue of ApplyTransforms.
// Mutates props in place, rewriting each key listed in transforms with the
// corresponding strategy. Keys not present in props are ignored; keys present
// in props but not in transforms are untouched.
//
// Strategies that need per-mask configuration (FPE / REGEX) cannot be applied
// via this entry point — there is nowhere to thread the config in — so they
// pass through unchanged. Callers wiring FPE / REGEX must route through
// ApplyStrategyTransformsWithConfig.
func ApplyStrategyTransforms(props map[string]interface{}, transforms map[string]MaskStrategy) {
	if len(props) == 0 || len(transforms) == 0 {
		return
	}
	for k, s := range transforms {
		if _, ok := props[k]; !ok {
			continue
		}
		props[k] = ApplyMaskStrategy(s, props[k])
	}
}

// StrategyApplication carries a strategy together with the per-mask
// configuration it needs (FPE key/tweak/radix, REGEX pattern/replacement).
// HASH / REDACT / NULL / PARTIAL ignore Config and may pass the zero value.
type StrategyApplication struct {
	Strategy MaskStrategy
	Config   strategies.ApplyConfig
}

// ApplyMaskStrategyWithConfig is the config-aware US-489 sibling of
// ApplyMaskStrategy. Returns (value, error) so callers can fail closed on
// misconfigured FPE keys / bad regex pattern shapes without conflating the
// failure with "strategy not applicable, pass through". HASH / REDACT / NULL
// / PARTIAL ignore cfg and return a nil error.
func ApplyMaskStrategyWithConfig(s MaskStrategy, value interface{}, cfg strategies.ApplyConfig) (interface{}, error) {
	return strategies.ApplyWithConfig(strategyToName(s), value, cfg)
}

// ApplyStrategyTransformsWithConfig is the config-aware US-489 sibling of
// ApplyStrategyTransforms. Mutates props in place, rewriting each key listed
// in transforms with the matching strategy + per-mask config. Errors are
// aggregated by key and returned to the caller — successfully applied keys
// are NOT rolled back, matching the existing in-place semantics; callers
// reading the error can decide whether to short-circuit the response or
// surface a per-row warning.
func ApplyStrategyTransformsWithConfig(props map[string]interface{}, transforms map[string]StrategyApplication) map[string]error {
	if len(props) == 0 || len(transforms) == 0 {
		return nil
	}
	var errs map[string]error
	for k, app := range transforms {
		if _, ok := props[k]; !ok {
			continue
		}
		out, err := ApplyMaskStrategyWithConfig(app.Strategy, props[k], app.Config)
		if err != nil {
			if errs == nil {
				errs = make(map[string]error)
			}
			errs[k] = err
			// Fail closed: replace the value with nil so a misconfigured
			// FPE / REGEX policy never leaks the clear value just because
			// the cipher errored. Callers that want a different fallback
			// (e.g. redact) can inspect the error map and rewrite.
			props[k] = nil
			continue
		}
		props[k] = out
	}
	return errs
}
