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
)

// IsKnownStrategy reports whether s is a recognised MaskStrategy.
func IsKnownStrategy(s MaskStrategy) bool {
	switch s {
	case MaskStrategyRedact, MaskStrategyHash, MaskStrategyNull, MaskStrategyPartial:
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
	default:
		return strategies.Name(s)
	}
}

// ApplyStrategyTransforms is the MaskStrategy analogue of ApplyTransforms.
// Mutates props in place, rewriting each key listed in transforms with the
// corresponding strategy. Keys not present in props are ignored; keys present
// in props but not in transforms are untouched.
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
