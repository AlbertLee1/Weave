package where

import (
	"encoding/json"
	"time"
)

// WhereClause represents a Palantir V2 Where filter clause.
//
// Most operators carry their payload in Value, but two Foundry
// SearchJsonQueryV2 operators put their payload in dedicated top-level
// keys (matching the official wire shape exactly):
//
//   - "interval" (IntervalQuery) uses Rule — a sub-rule tree evaluated
//     against the analyzed form of text fields.
//   - "relativeDateRange" (RelativeDateRangeQuery) uses
//     RelativeStartTime / RelativeEndTime / TimeZoneID.
type WhereClause struct {
	Type  string          `json:"type"`
	Field string          `json:"field,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`

	// Rule is the IntervalQueryRule union for type=="interval". Kept raw
	// because the union recurses (allOf/anyOf nest further rules).
	Rule json.RawMessage `json:"rule,omitempty"`

	// RelativeStartTime / RelativeEndTime are RelativePointInTime bounds
	// for type=="relativeDateRange". The lower bound is inclusive, the
	// upper bound exclusive; negative values reach into the past.
	RelativeStartTime json.RawMessage `json:"relativeStartTime,omitempty"`
	RelativeEndTime   json.RawMessage `json:"relativeEndTime,omitempty"`

	// TimeZoneID is the REQUIRED tz database zone (e.g. "Etc/UTC",
	// "America/New_York") used to truncate relativeDateRange bounds to
	// the start of their timeUnit period.
	TimeZoneID string `json:"timeZoneId,omitempty"`
}

// RelativePointInTime is the Foundry RelativeDateRangeBound wire shape:
// a point in time specified relative to query execution time.
type RelativePointInTime struct {
	// Type is the union discriminator; Foundry serializes "relativePoint".
	// Tolerated empty for callers that omit it.
	Type string `json:"type,omitempty"`
	// Value is the signed offset: negative into the past, positive into
	// the future, zero for the current period.
	Value int `json:"value"`
	// TimeUnit is one of DAY / WEEK / MONTH / YEAR (RelativeTimeUnit).
	TimeUnit string `json:"timeUnit"`
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

	// Now overrides the wall clock used to resolve relativeDateRange
	// bounds. Nil means time.Now. Tests inject a fixed clock so the
	// resulting Bleve date ranges are deterministic.
	Now func() time.Time
}
