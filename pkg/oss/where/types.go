package where

import "encoding/json"

// WhereClause represents a Palantir V2 Where filter clause.
type WhereClause struct {
	Type  string          `json:"type"`
	Field string          `json:"field,omitempty"`
	Value json.RawMessage `json:"value"`
}

// FuzzyConfig configures fuzzy matching for text search queries.
type FuzzyConfig struct {
	MaxEdits int `json:"maxEdits,omitempty"` // 0, 1 or 2 (Levenshtein edit distance); default 1 when struct is non-nil and MaxEdits is 0
}

// MaxFuzziness is the upper bound on fuzzy matching edit distance. Values
// outside [0, MaxFuzziness] are rejected at the HTTP layer.
const MaxFuzziness = 2

// ConvertOptions holds optional settings for where clause conversion.
type ConvertOptions struct {
	Fuzzy *FuzzyConfig
}
