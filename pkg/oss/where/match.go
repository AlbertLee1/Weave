package where

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// MatchClause evaluates a WhereClause tree in-memory against a single row
// (a property map, typically from a funnel.BroadcastEvent). It is the
// complement to ConvertToBleveQuery — used by paths like SSE subscribe
// where we need to filter a live event stream without running every event
// through a Bleve index.
//
// Evaluation is intentionally conservative: unsupported operators or
// malformed values return false so the caller drops the event rather than
// leaking a row that was never meant to match. A nil clause matches every
// row so an ObjectSet without a filter hop streams unconditionally.
//
// Supported operators (covering the US-056 Browser realtime filter set):
//
//	eq, in, gt, gte, lt, lte, isNull, contains, startsWith, and, or, not.
//
// Unsupported operators (wildcard / containsAllTerms / geo / ...) fall back
// to false. Future stories that need them should extend this switch rather
// than inventing a second evaluator.
func MatchClause(clause *WhereClause, row map[string]interface{}) bool {
	if clause == nil {
		return true
	}
	if row == nil {
		row = map[string]interface{}{}
	}

	switch clause.Type {
	case "eq":
		return matchEq(clause, row)
	case "in":
		return matchIn(clause, row)
	case "gt":
		return matchRange(clause, row, false, true, false, false)
	case "gte":
		return matchRange(clause, row, true, true, false, false)
	case "lt":
		return matchRange(clause, row, false, false, false, true)
	case "lte":
		return matchRange(clause, row, false, false, true, true)
	case "isNull":
		return matchIsNull(clause, row)
	case "contains":
		return matchContains(clause, row)
	case "containsAnyTerm":
		return matchContainsAnyTerm(clause, row)
	case "startsWith":
		return matchStartsWith(clause, row)
	case "containsAllTermsInOrderPrefixLastTerm":
		return matchContainsAllTermsInOrderPrefixLastTerm(clause, row)
	case "and":
		return matchAnd(clause, row)
	case "or":
		return matchOr(clause, row)
	case "not":
		return matchNot(clause, row)
	case "withinPolygon", "intersectsPolygon":
		return matchPolygon(clause, row)
	case "doesNotIntersectPolygon":
		return !matchPolygon(clause, row)
	case "doesNotIntersectBoundingBox":
		return !matchBoundingBox(clause, row)
	default:
		return false
	}
}

// ValidateMatchClauseSupported returns an error when a where tree contains an
// operator the in-memory matcher would otherwise treat as false. Callers that
// expose user-authored filters should use this to avoid silently emptying a
// result set when they cannot delegate to Bleve.
func ValidateMatchClauseSupported(clause *WhereClause) error {
	if clause == nil {
		return nil
	}
	switch clause.Type {
	case "eq", "in", "gt", "gte", "lt", "lte", "isNull", "contains", "containsAnyTerm", "startsWith", "containsAllTermsInOrderPrefixLastTerm", "withinPolygon", "intersectsPolygon", "doesNotIntersectPolygon", "doesNotIntersectBoundingBox":
		return nil
	case "and", "or":
		var subs []WhereClause
		if err := json.Unmarshal(clause.Value, &subs); err != nil {
			return fmt.Errorf("invalid %s where value: %w", clause.Type, err)
		}
		for i := range subs {
			if err := ValidateMatchClauseSupported(&subs[i]); err != nil {
				return err
			}
		}
		return nil
	case "not":
		var subs []WhereClause
		if err := json.Unmarshal(clause.Value, &subs); err == nil && len(subs) > 0 {
			return ValidateMatchClauseSupported(&subs[0])
		}
		var sub WhereClause
		if err := json.Unmarshal(clause.Value, &sub); err != nil {
			return fmt.Errorf("invalid not where value: %w", err)
		}
		return ValidateMatchClauseSupported(&sub)
	default:
		return fmt.Errorf("unsupported where operator %q for in-memory matching", clause.Type)
	}
}

func matchEq(clause *WhereClause, row map[string]interface{}) bool {
	raw, ok := row[clause.Field]
	if !ok {
		return false
	}
	return matchEqValue(clause.Value, raw)
}

// matchEqValue compares one scalar JSON candidate against a row value with
// eq semantics. Shared by "eq" and "in" so a candidate list element matches
// exactly when the equivalent eq clause would.
func matchEqValue(value json.RawMessage, raw interface{}) bool {
	// Number
	var numVal float64
	if err := json.Unmarshal(value, &numVal); err == nil {
		if rowNum, ok := coerceNumber(raw); ok {
			return rowNum == numVal
		}
		return false
	}

	// Bool — ordering matters: JSON numbers don't unmarshal into bool so we
	// can always try bool before string, but we must try bool BEFORE string
	// because `true` and `false` are valid JSON literals that are NOT valid
	// JSON strings.
	var boolVal bool
	if err := json.Unmarshal(value, &boolVal); err == nil {
		if rowBool, ok := raw.(bool); ok {
			return rowBool == boolVal
		}
		return false
	}

	// String
	var strVal string
	if err := json.Unmarshal(value, &strVal); err == nil {
		if rowStr, ok := raw.(string); ok {
			return rowStr == strVal
		}
		return false
	}

	return false
}

// matchIn evaluates the Foundry "in" operator in-memory: the row value
// must equal ANY candidate in the array (per-element semantics identical
// to eq via matchEqValue). An empty candidate list matches nothing, and —
// per the conservative MatchClause contract — malformed values (non-array
// / null) also match nothing rather than leaking events.
func matchIn(clause *WhereClause, row map[string]interface{}) bool {
	raw, ok := row[clause.Field]
	if !ok {
		return false
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(clause.Value, &elems); err != nil {
		return false
	}
	for _, elem := range elems {
		if matchEqValue(elem, raw) {
			return true
		}
	}
	return false
}

func matchRange(clause *WhereClause, row map[string]interface{}, minInclusive, hasMin, maxInclusive, hasMax bool) bool {
	raw, ok := row[clause.Field]
	if !ok {
		return false
	}

	var numVal float64
	if err := json.Unmarshal(clause.Value, &numVal); err == nil {
		rowNum, ok := coerceNumber(raw)
		if !ok {
			return false
		}
		if hasMin {
			if minInclusive {
				if rowNum < numVal {
					return false
				}
			} else {
				if rowNum <= numVal {
					return false
				}
			}
		}
		if hasMax {
			if maxInclusive {
				if rowNum > numVal {
					return false
				}
			} else {
				if rowNum >= numVal {
					return false
				}
			}
		}
		return true
	}

	// String (treated as lexicographic comparison — good enough for ISO dates).
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err == nil {
		rowStr, ok := raw.(string)
		if !ok {
			return false
		}
		if hasMin {
			if minInclusive {
				if rowStr < strVal {
					return false
				}
			} else {
				if rowStr <= strVal {
					return false
				}
			}
		}
		if hasMax {
			if maxInclusive {
				if rowStr > strVal {
					return false
				}
			} else {
				if rowStr >= strVal {
					return false
				}
			}
		}
		return true
	}

	return false
}

func matchIsNull(clause *WhereClause, row map[string]interface{}) bool {
	var want bool
	if err := json.Unmarshal(clause.Value, &want); err != nil {
		return false
	}
	raw, present := row[clause.Field]
	isNull := !present || raw == nil
	return isNull == want
}

func matchContains(clause *WhereClause, row map[string]interface{}) bool {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return false
	}
	rowVal, ok := row[clause.Field].(string)
	if !ok {
		return false
	}
	return strings.Contains(strings.ToLower(rowVal), strings.ToLower(strVal))
}

func matchContainsAnyTerm(clause *WhereClause, row map[string]interface{}) bool {
	query, ok := stringOrStringArrayValue(clause.Value)
	if !ok {
		return false
	}
	queryTerms := tokenSet(query)
	if len(queryTerms) == 0 {
		return false
	}
	rowValues, ok := rowTextValues(row[clause.Field])
	if !ok {
		return false
	}
	for _, value := range rowValues {
		for _, token := range tokenizeText(value) {
			if queryTerms[token] {
				return true
			}
		}
	}
	return false
}

func tokenSet(text string) map[string]bool {
	tokens := tokenizeText(text)
	if len(tokens) == 0 {
		return nil
	}
	set := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		set[token] = true
	}
	return set
}

func tokenizeText(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

func rowTextValues(raw interface{}) ([]string, bool) {
	switch v := raw.(type) {
	case string:
		return []string{v}, true
	case []string:
		return v, true
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				continue
			}
			values = append(values, str)
		}
		return values, len(values) > 0
	default:
		return nil, false
	}
}

func stringOrStringArrayValue(value json.RawMessage) (string, bool) {
	var strVal string
	if err := json.Unmarshal(value, &strVal); err == nil {
		return strVal, true
	}
	var arrVal []string
	if err := json.Unmarshal(value, &arrVal); err == nil {
		return strings.Join(arrVal, " "), true
	}
	return "", false
}

func matchStartsWith(clause *WhereClause, row map[string]interface{}) bool {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return false
	}
	rowVal, ok := row[clause.Field].(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.ToLower(rowVal), strings.ToLower(strVal))
}

func matchAnd(clause *WhereClause, row map[string]interface{}) bool {
	var subs []WhereClause
	if err := json.Unmarshal(clause.Value, &subs); err != nil {
		return false
	}
	for i := range subs {
		if !MatchClause(&subs[i], row) {
			return false
		}
	}
	return true
}

func matchOr(clause *WhereClause, row map[string]interface{}) bool {
	var subs []WhereClause
	if err := json.Unmarshal(clause.Value, &subs); err != nil {
		return false
	}
	if len(subs) == 0 {
		return false
	}
	for i := range subs {
		if MatchClause(&subs[i], row) {
			return true
		}
	}
	return false
}

func matchNot(clause *WhereClause, row map[string]interface{}) bool {
	// Palantir V2 accepts both single-object and single-element-array forms.
	var subs []WhereClause
	if err := json.Unmarshal(clause.Value, &subs); err == nil && len(subs) > 0 {
		return !MatchClause(&subs[0], row)
	}
	var sub WhereClause
	if err := json.Unmarshal(clause.Value, &sub); err != nil {
		return false
	}
	return !MatchClause(&sub, row)
}

// extractGeoPoint extracts latitude and longitude from a row's location field.
// Supports map with "latitude"/"longitude" keys.
func extractGeoPoint(row map[string]interface{}, field string) (lat, lon float64, ok bool) {
	raw, exists := row[field]
	if !exists {
		return 0, 0, false
	}
	locMap, isMap := raw.(map[string]interface{})
	if !isMap {
		return 0, 0, false
	}
	latVal, latOk := coerceNumber(locMap["latitude"])
	lonVal, lonOk := coerceNumber(locMap["longitude"])
	if !latOk || !lonOk {
		return 0, 0, false
	}
	return latVal, lonVal, true
}

// matchPolygon evaluates withinPolygon / intersectsPolygon in-memory using ray casting.
// For point data, "within" and "intersects" are equivalent.
func matchPolygon(clause *WhereClause, row map[string]interface{}) bool {
	var pq PolygonQuery
	if err := json.Unmarshal(clause.Value, &pq); err != nil {
		return false
	}
	if len(pq.Polygon) == 0 || len(pq.Polygon[0]) < 3 {
		return false
	}

	lat, lon, ok := extractGeoPoint(row, clause.Field)
	if !ok {
		return false
	}

	// GeoJSON polygon coordinates are [lon, lat]; PointInPolygon expects (x=lon, y=lat).
	return PointInPolygon(lon, lat, pq.Polygon[0])
}

// matchBoundingBox evaluates intersectsBoundingBox / withinBoundingBox in-memory.
func matchBoundingBox(clause *WhereClause, row map[string]interface{}) bool {
	var bb BoundingBox
	if err := json.Unmarshal(clause.Value, &bb); err != nil {
		return false
	}

	lat, lon, ok := extractGeoPoint(row, clause.Field)
	if !ok {
		return false
	}

	return lat >= bb.BottomRight.Latitude && lat <= bb.TopLeft.Latitude &&
		lon >= bb.TopLeft.Longitude && lon <= bb.BottomRight.Longitude
}

// matchContainsAllTermsInOrderPrefixLastTerm evaluates the autocomplete
// operator in-memory. All terms must appear adjacent and in order, with
// the last term treated as a prefix match.
func matchContainsAllTermsInOrderPrefixLastTerm(clause *WhereClause, row map[string]interface{}) bool {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return false
	}

	terms := SplitTerms(strVal)
	if len(terms) == 0 {
		return false
	}

	rowVal, ok := row[clause.Field].(string)
	if !ok {
		return false
	}

	// Tokenize row value the same way as Bleve's standard analyzer.
	rowTokens := strings.Fields(strings.ToLower(rowVal))
	numTerms := len(terms)

	lowerTerms := make([]string, numTerms)
	for i, t := range terms {
		lowerTerms[i] = strings.ToLower(t)
	}

	// Slide a window of size numTerms across rowTokens.
	for start := 0; start <= len(rowTokens)-numTerms; start++ {
		match := true
		for i := 0; i < numTerms-1; i++ {
			if rowTokens[start+i] != lowerTerms[i] {
				match = false
				break
			}
		}
		if match && strings.HasPrefix(rowTokens[start+numTerms-1], lowerTerms[numTerms-1]) {
			return true
		}
	}

	return false
}

// coerceNumber tolerates the handful of Go numeric shapes that can land in a
// property map after JSON decoding or map[string]interface{} hand-construction
// in tests / funnel payloads. Bool is explicitly NOT treated as a number.
func coerceNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}
