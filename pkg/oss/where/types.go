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
	MaxEdits int `json:"maxEdits,omitempty"` // 1 or 2 (Levenshtein edit distance); default 1 if struct is non-nil
}

// ConvertOptions holds optional settings for where clause conversion.
type ConvertOptions struct {
	Fuzzy *FuzzyConfig
}
