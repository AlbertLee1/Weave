package strategies

import "regexp"

// RegexConfig configures the REGEX strategy: substrings matching Pattern are
// rewritten via Replacement using Go's regexp.ReplaceAllString semantics ($0
// for the full match, $1..$N for capture groups, $$ for a literal dollar).
//
// Pattern is held as a pre-compiled *regexp.Regexp so admins pay parser cost
// once at policy-load time, not per row. A nil Pattern is treated fail-closed
// — the input is returned unchanged rather than triggering a panic.
type RegexConfig struct {
	Pattern     *regexp.Regexp
	Replacement string
}

// Regex applies cfg.Pattern.ReplaceAllString(value, cfg.Replacement) after
// stringifying value through the same toString helper the other strategies
// use. Returns the stringified input unchanged when Pattern is nil.
func Regex(value interface{}, cfg RegexConfig) string {
	s := toString(value)
	if cfg.Pattern == nil {
		return s
	}
	return cfg.Pattern.ReplaceAllString(s, cfg.Replacement)
}
