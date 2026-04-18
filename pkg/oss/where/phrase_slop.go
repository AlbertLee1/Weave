package where

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PhraseSlopValue is the structured value shape for the "phrase" where-clause
// operator. Callers can send the structured form directly, or pass a Lucene-
// style string `"<phrase>"~<slop>` (double or single quoted) which
// ParsePhraseSlopString will decode.
type PhraseSlopValue struct {
	Phrase string `json:"phrase"`
	Slop   int    `json:"slop,omitempty"`
}

// ParsePhraseSlopValue extracts a PhraseSlopValue from a WhereClause value.
// Two shapes are supported:
//   - JSON object {"phrase": "quick fox", "slop": 2}
//   - JSON string "quick fox"~2 (Lucene-style slop suffix; slop optional).
//     Quotes may be single or double; a bare string (no quotes) is treated
//     as the phrase with slop=0 so callers can always use the operator.
//
// Returned slop is clamped to [0, MaxPhraseSlop] by the caller (validated at
// the converter entry).
func ParsePhraseSlopValue(raw json.RawMessage) (PhraseSlopValue, error) {
	var zero PhraseSlopValue

	var obj PhraseSlopValue
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Phrase != "" {
		return obj, nil
	}

	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return zero, fmt.Errorf("phrase value must be an object or string: %w", err)
	}
	return ParsePhraseSlopString(str)
}

// ParsePhraseSlopString decodes a Lucene-style `"<phrase>"~<slop>` expression.
// The ~<slop> suffix is optional (defaulting to 0). Surrounding single or
// double quotes on the phrase are stripped; unquoted bare strings are also
// accepted so callers can always use the operator symmetrically.
func ParsePhraseSlopString(in string) (PhraseSlopValue, error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return PhraseSlopValue{}, fmt.Errorf("phrase value must be a non-empty string")
	}

	slop := 0
	if i := strings.LastIndex(s, "~"); i >= 0 && isSlopTail(s[i+1:]) {
		n, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return PhraseSlopValue{}, fmt.Errorf("invalid phrase slop %q: %w", s[i+1:], err)
		}
		if n < 0 {
			return PhraseSlopValue{}, fmt.Errorf("phrase slop must be non-negative, got %d", n)
		}
		slop = n
		s = strings.TrimSpace(s[:i])
	}

	phrase := stripPhraseQuotes(s)
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return PhraseSlopValue{}, fmt.Errorf("phrase value must contain at least one term")
	}

	return PhraseSlopValue{Phrase: phrase, Slop: slop}, nil
}

// isSlopTail returns true when the trailing segment is a non-empty integer
// literal — guards against phrases that legitimately contain '~' characters.
func isSlopTail(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func stripPhraseQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
