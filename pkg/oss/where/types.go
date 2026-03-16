package where

import "encoding/json"

// WhereClause represents a Palantir V2 Where filter clause.
type WhereClause struct {
	Type  string          `json:"type"`
	Field string          `json:"field,omitempty"`
	Value json.RawMessage `json:"value"`
}
