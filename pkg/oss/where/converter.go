package where

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// ConvertToBleveQueryWithOpts translates a WhereClause into a Bleve query with
// optional settings such as fuzzy matching.
func ConvertToBleveQueryWithOpts(clause *WhereClause, opts *ConvertOptions) (query.Query, error) {
	if clause == nil {
		return nil, fmt.Errorf("where clause is nil")
	}
	if opts == nil {
		opts = &ConvertOptions{}
	}

	fuzz := resolveFuzziness(opts)

	switch clause.Type {
	case "eq":
		return convertEqFuzzy(clause, fuzz)
	case "gt":
		return convertRange(clause, false, true, false, false)
	case "gte":
		return convertRange(clause, true, true, false, false)
	case "lt":
		return convertRange(clause, false, false, false, true)
	case "lte":
		return convertRange(clause, false, false, true, true)
	case "isNull":
		return convertIsNull(clause)
	case "contains":
		return convertContains(clause)
	case "fuzzy":
		return convertFuzzy(clause, fuzz)
	case "phrase":
		return convertPhraseSlop(clause)
	case "regex":
		return convertRegex(clause)
	case "containsAllTerms":
		return convertContainsAllTermsFuzzy(clause, fuzz)
	case "containsAnyTerm":
		return convertContainsAnyTermFuzzy(clause, fuzz)
	case "containsAllTermsInOrder":
		return convertContainsAllTermsInOrder(clause)
	case "containsAllTermsInOrderPrefixLastTerm":
		return convertContainsAllTermsInOrderPrefixLastTerm(clause)
	case "startsWith":
		return convertStartsWith(clause)
	case "wildcard":
		return convertWildcard(clause)
	case "and":
		return convertAndWithOpts(clause, opts)
	case "or":
		return convertOrWithOpts(clause, opts)
	case "not":
		return convertNotWithOpts(clause, opts)
	case "withinBoundingBox":
		return convertWithinBoundingBox(clause)
	case "intersectsBoundingBox":
		return convertWithinBoundingBox(clause)
	case "withinPolygon":
		return convertWithinPolygon(clause)
	case "intersectsPolygon":
		return convertIntersectsPolygon(clause)
	case "doesNotIntersectPolygon":
		return convertDoesNotIntersectPolygon(clause)
	case "doesNotIntersectBoundingBox":
		return convertDoesNotIntersectBoundingBox(clause)
	case "withinDistanceOf":
		return convertWithinDistanceOf(clause)
	default:
		return nil, fmt.Errorf("unsupported where clause type: %q", clause.Type)
	}
}

// ConvertToBleveQuery translates a Palantir V2 WhereClause into a Bleve query.
func ConvertToBleveQuery(clause *WhereClause) (query.Query, error) {
	return ConvertToBleveQueryWithOpts(clause, nil)
}

// resolveFuzziness returns the effective fuzziness from ConvertOptions.
// Returns 0 when fuzzy is disabled; defaults to 1 when FuzzyConfig is present but MaxEdits is 0.
func resolveFuzziness(opts *ConvertOptions) int {
	if opts == nil || opts.Fuzzy == nil {
		return 0
	}
	if opts.Fuzzy.MaxEdits <= 0 {
		return 1 // default
	}
	return opts.Fuzzy.MaxEdits
}

// convertEq handles the "eq" operator (non-fuzzy, for backward compatibility).
func convertEq(clause *WhereClause) (query.Query, error) {
	return convertEqFuzzy(clause, 0)
}

// convertEqFuzzy handles the "eq" operator with optional fuzzy matching.
// For strings: MatchQuery with optional fuzziness. For numbers/booleans: unchanged.
func convertEqFuzzy(clause *WhereClause, fuzz int) (query.Query, error) {
	// Try number first.
	var numVal float64
	if err := json.Unmarshal(clause.Value, &numVal); err == nil {
		inclusive := true
		q := bleve.NewNumericRangeInclusiveQuery(&numVal, &numVal, &inclusive, &inclusive)
		q.SetField(clause.Field)
		return q, nil
	}

	// Try boolean.
	var boolVal bool
	if err := json.Unmarshal(clause.Value, &boolVal); err == nil {
		q := bleve.NewBoolFieldQuery(boolVal)
		q.SetField(clause.Field)
		return q, nil
	}

	// Try string: use MatchQuery which applies the same text analysis (lowercasing)
	// as indexing, so "Brazil" matches the indexed token "brazil".
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err == nil {
		q := bleve.NewMatchQuery(strVal)
		q.SetField(clause.Field)
		if fuzz > 0 {
			q.SetFuzziness(fuzz)
		}
		return q, nil
	}

	return nil, fmt.Errorf("unsupported value type for eq")
}

// convertRange handles gt, gte, lt, lte operators.
// It first tries to unmarshal the value as a number, then as a string (date).
func convertRange(clause *WhereClause, minInclusive, hasMin, maxInclusive, hasMax bool) (query.Query, error) {
	// Try number.
	var numVal float64
	if err := json.Unmarshal(clause.Value, &numVal); err == nil {
		var minPtr, maxPtr *float64
		var minIncPtr, maxIncPtr *bool
		if hasMin {
			minPtr = &numVal
			minIncPtr = &minInclusive
		}
		if hasMax {
			maxPtr = &numVal
			maxIncPtr = &maxInclusive
		}
		q := bleve.NewNumericRangeInclusiveQuery(minPtr, maxPtr, minIncPtr, maxIncPtr)
		q.SetField(clause.Field)
		return q, nil
	}

	// Try string (treat as date).
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err == nil {
		var start, end string
		if hasMin {
			start = strVal
		}
		if hasMax {
			end = strVal
		}
		q := bleve.NewDateRangeInclusiveStringQuery(start, end, &minInclusive, &maxInclusive)
		q.SetField(clause.Field)
		return q, nil
	}

	return nil, fmt.Errorf("unsupported value type for range query")
}

// convertIsNull handles the "isNull" operator.
// If value is true: field must NOT exist (MustNot WildcardQuery("*")).
// If value is false: field MUST exist (WildcardQuery("*")).
func convertIsNull(clause *WhereClause) (query.Query, error) {
	var isNull bool
	if err := json.Unmarshal(clause.Value, &isNull); err != nil {
		return nil, fmt.Errorf("isNull value must be a boolean: %w", err)
	}

	wq := bleve.NewWildcardQuery("*")
	wq.SetField(clause.Field)

	if isNull {
		// Field must NOT exist.
		bq := bleve.NewBooleanQuery()
		bq.AddMust(bleve.NewMatchAllQuery())
		bq.AddMustNot(wq)
		return bq, nil
	}

	// Field MUST exist.
	return wq, nil
}

// convertContains handles the "contains" operator using a TermQuery.
func convertContains(clause *WhereClause) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("contains value must be a string: %w", err)
	}

	q := bleve.NewTermQuery(strVal)
	q.SetField(clause.Field)
	return q, nil
}

// convertFuzzy handles the "fuzzy" operator using a Bleve FuzzyQuery.
// Unlike MatchQuery.SetFuzziness (which tokenises+analyses the input),
// FuzzyQuery treats the value as a single pre-analysed term and matches it
// against the index with Levenshtein edit distance — the canonical tool for
// "the indexed token is Kafka, the query is Kafca" lookups.
//
// Fuzziness precedence: when the caller provides no FuzzyConfig (fuzz==0) the
// operator still needs a sensible default, so it falls back to 1 — matching the
// resolveFuzziness "fuzzy-is-set-but-maxEdits-omitted" default.
func convertFuzzy(clause *WhereClause, fuzz int) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("fuzzy value must be a string: %w", err)
	}
	if strings.TrimSpace(strVal) == "" {
		return nil, fmt.Errorf("fuzzy value must be a non-empty string")
	}

	effective := fuzz
	if effective <= 0 {
		effective = 1
	}

	q := bleve.NewFuzzyQuery(strings.ToLower(strVal))
	q.SetField(clause.Field)
	q.SetFuzziness(effective)
	return q, nil
}

// convertPhraseSlop handles the "phrase" operator. Value shapes:
//   - {"phrase": "quick fox", "slop": 2}
//   - "\"quick fox\"~2" (Lucene-style, slop optional)
//
// slop=0 means strict adjacency (same as bleve's PhraseQuery); higher values
// tolerate position gaps / reordering up to the slop budget. Slop is clamped
// to [0, MaxPhraseSlop] — larger values are rejected to keep the path-walk
// bounded.
func convertPhraseSlop(clause *WhereClause) (query.Query, error) {
	val, err := ParsePhraseSlopValue(clause.Value)
	if err != nil {
		return nil, fmt.Errorf("phrase: %w", err)
	}
	if val.Slop < 0 || val.Slop > MaxPhraseSlop {
		return nil, fmt.Errorf("phrase slop must be in [0, %d], got %d", MaxPhraseSlop, val.Slop)
	}

	terms := SplitTerms(val.Phrase)
	if len(terms) == 0 {
		return bleve.NewMatchNoneQuery(), nil
	}

	// Lowercase to match the default text analyser's indexed form — same
	// treatment the fuzzy / prefix operators apply so mixed-case input doesn't
	// silently miss. Callers who need case-sensitive matching can pre-index
	// the field with a keyword analyser; the operator itself stays consistent
	// with the rest of the where package.
	lowered := make([]string, len(terms))
	for i, t := range terms {
		lowered[i] = strings.ToLower(t)
	}

	if val.Slop == 0 && len(lowered) > 1 {
		// slop=0 is strict adjacency — delegate to bleve's MatchPhraseQuery so
		// callers get the optimised path; our custom searcher is only needed
		// when slop > 0.
		q := bleve.NewMatchPhraseQuery(val.Phrase)
		q.SetField(clause.Field)
		return q, nil
	}

	q := NewPhraseSlopQuery(lowered, val.Slop, clause.Field)
	return q, nil
}

// convertContainsAllTerms handles the "containsAllTerms" operator (non-fuzzy).
func convertContainsAllTerms(clause *WhereClause) (query.Query, error) {
	return convertContainsAllTermsFuzzy(clause, 0)
}

// convertContainsAllTermsFuzzy handles "containsAllTerms" with optional fuzzy matching.
// Splits the value into terms and requires ALL to match (BooleanQuery with Must for each).
func convertContainsAllTermsFuzzy(clause *WhereClause, fuzz int) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("containsAllTerms value must be a string: %w", err)
	}

	terms := SplitTerms(strVal)
	if len(terms) == 0 {
		return bleve.NewMatchNoneQuery(), nil
	}

	bq := bleve.NewBooleanQuery()
	for _, term := range terms {
		mq := bleve.NewMatchQuery(term)
		mq.SetField(clause.Field)
		if fuzz > 0 {
			mq.SetFuzziness(fuzz)
		}
		bq.AddMust(mq)
	}
	return bq, nil
}

// convertContainsAnyTerm handles the "containsAnyTerm" operator (non-fuzzy).
func convertContainsAnyTerm(clause *WhereClause) (query.Query, error) {
	return convertContainsAnyTermFuzzy(clause, 0)
}

// convertContainsAnyTermFuzzy handles "containsAnyTerm" with optional fuzzy matching.
// Uses a MatchQuery which ORs terms by default.
//
// DOG-004: accept either the canonical string form or an array of terms.
// Earlier frontend builds serialised the value as an array (one element
// per whitespace token), which produced
// `containsAnyTerm value must be a string` errors that surfaced in the
// browser as `INVALID_ARGUMENT: SearchObjectsFailed`. We now coerce the
// array to a space-joined string so the Bleve MatchQuery analyser
// tokenises it the same way regardless of the wire shape — older clients
// keep working while we converge the contract on the typed string form.
func convertContainsAnyTermFuzzy(clause *WhereClause, fuzz int) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		var arrVal []string
		if arrErr := json.Unmarshal(clause.Value, &arrVal); arrErr != nil {
			return nil, fmt.Errorf("containsAnyTerm value must be a string or array of strings: %w", err)
		}
		strVal = strings.Join(arrVal, " ")
	}

	q := bleve.NewMatchQuery(strVal)
	q.SetField(clause.Field)
	if fuzz > 0 {
		q.SetFuzziness(fuzz)
	}
	return q, nil
}

// convertContainsAllTermsInOrder handles the "containsAllTermsInOrder" operator.
// Uses a MatchPhraseQuery.
func convertContainsAllTermsInOrder(clause *WhereClause) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("containsAllTermsInOrder value must be a string: %w", err)
	}

	q := bleve.NewMatchPhraseQuery(strVal)
	q.SetField(clause.Field)
	return q, nil
}

// convertContainsAllTermsInOrderPrefixLastTerm handles the autocomplete operator.
// First N-1 terms must appear in order (phrase match), last term is a prefix match.
// Single-term input uses PrefixQuery only.
func convertContainsAllTermsInOrderPrefixLastTerm(clause *WhereClause) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("containsAllTermsInOrderPrefixLastTerm value must be a string: %w", err)
	}

	terms := SplitTerms(strVal)
	if len(terms) == 0 {
		return bleve.NewMatchNoneQuery(), nil
	}

	// Lowercase all terms to match Bleve's standard analyzer behavior.
	lowerTerms := make([]string, len(terms))
	for i, t := range terms {
		lowerTerms[i] = strings.ToLower(t)
	}

	if len(lowerTerms) == 1 {
		q := bleve.NewPrefixQuery(lowerTerms[0])
		q.SetField(clause.Field)
		return q, nil
	}

	return NewPhrasePrefixQuery(lowerTerms, clause.Field), nil
}

// convertStartsWith handles the "startsWith" operator using a PrefixQuery.
func convertStartsWith(clause *WhereClause) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("startsWith value must be a string: %w", err)
	}

	q := bleve.NewPrefixQuery(strings.ToLower(strVal))
	q.SetField(clause.Field)
	return q, nil
}

// convertWildcard handles the "wildcard" operator using a WildcardQuery.
func convertWildcard(clause *WhereClause) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("wildcard value must be a string: %w", err)
	}
	q := bleve.NewWildcardQuery(strVal)
	q.SetField(clause.Field)
	return q, nil
}

// convertAnd handles the "and" logical operator (non-opts).
func convertAnd(clause *WhereClause) (query.Query, error) {
	return convertAndWithOpts(clause, nil)
}

// convertAndWithOpts handles "and" with options threading.
func convertAndWithOpts(clause *WhereClause, opts *ConvertOptions) (query.Query, error) {
	var subClauses []WhereClause
	if err := json.Unmarshal(clause.Value, &subClauses); err != nil {
		return nil, fmt.Errorf("and value must be an array of where clauses: %w", err)
	}

	bq := bleve.NewBooleanQuery()
	for i := range subClauses {
		sub, err := ConvertToBleveQueryWithOpts(&subClauses[i], opts)
		if err != nil {
			return nil, fmt.Errorf("and sub-clause %d: %w", i, err)
		}
		bq.AddMust(sub)
	}
	return bq, nil
}

// convertOr handles the "or" logical operator (non-opts).
func convertOr(clause *WhereClause) (query.Query, error) {
	return convertOrWithOpts(clause, nil)
}

// convertOrWithOpts handles "or" with options threading.
func convertOrWithOpts(clause *WhereClause, opts *ConvertOptions) (query.Query, error) {
	var subClauses []WhereClause
	if err := json.Unmarshal(clause.Value, &subClauses); err != nil {
		return nil, fmt.Errorf("or value must be an array of where clauses: %w", err)
	}

	if len(subClauses) == 0 {
		return bleve.NewMatchNoneQuery(), nil
	}

	bq := bleve.NewBooleanQuery()
	for i := range subClauses {
		sub, err := ConvertToBleveQueryWithOpts(&subClauses[i], opts)
		if err != nil {
			return nil, fmt.Errorf("or sub-clause %d: %w", i, err)
		}
		bq.AddShould(sub)
	}
	bq.SetMinShould(1)
	return bq, nil
}

// GeoPoint for geospatial queries.
type GeoPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// BoundingBox for geospatial bounding box queries.
type BoundingBox struct {
	TopLeft     GeoPoint `json:"topLeft"`
	BottomRight GeoPoint `json:"bottomRight"`
}

// DistanceQuery for geospatial distance queries.
type DistanceQuery struct {
	Center   GeoPoint `json:"center"`
	Distance string   `json:"distance"` // e.g., "10km"
}

// convertWithinBoundingBox handles the "withinBoundingBox" and "intersectsBoundingBox" operators.
func convertWithinBoundingBox(clause *WhereClause) (query.Query, error) {
	var bb BoundingBox
	if err := json.Unmarshal(clause.Value, &bb); err != nil {
		return nil, fmt.Errorf("withinBoundingBox value: %w", err)
	}
	q := bleve.NewGeoBoundingBoxQuery(
		bb.TopLeft.Longitude, bb.TopLeft.Latitude,
		bb.BottomRight.Longitude, bb.BottomRight.Latitude,
	)
	q.SetField(clause.Field)
	return q, nil
}

// convertWithinDistanceOf handles the "withinDistanceOf" operator.
func convertWithinDistanceOf(clause *WhereClause) (query.Query, error) {
	var dq DistanceQuery
	if err := json.Unmarshal(clause.Value, &dq); err != nil {
		return nil, fmt.Errorf("withinDistanceOf value: %w", err)
	}
	q := bleve.NewGeoDistanceQuery(dq.Center.Longitude, dq.Center.Latitude, dq.Distance)
	q.SetField(clause.Field)
	return q, nil
}

// PolygonQuery for geospatial polygon queries.
// Polygon is a GeoJSON Polygon coordinates array: [ring][point][lon,lat].
type PolygonQuery struct {
	Polygon [][][]float64 `json:"polygon"`
}

// parsePolygonQuery unmarshals a WhereClause value into a PolygonQuery.
func parsePolygonQuery(clause *WhereClause) (*PolygonQuery, error) {
	var pq PolygonQuery
	if err := json.Unmarshal(clause.Value, &pq); err != nil {
		return nil, fmt.Errorf("polygon value: %w", err)
	}
	if len(pq.Polygon) == 0 || len(pq.Polygon[0]) < 3 {
		return nil, fmt.Errorf("polygon must have at least one ring with 3+ points")
	}
	return &pq, nil
}

// polygonToGeoShapeCoords converts GeoJSON Polygon coordinates [ring][point][lon,lat]
// to the [][][][]float64 format expected by bleve.NewGeoShapeQuery.
func polygonToGeoShapeCoords(polygon [][][]float64) [][][][]float64 {
	return [][][][]float64{polygon}
}

// convertWithinPolygon handles the "withinPolygon" operator using Bleve GeoShapeQuery.
func convertWithinPolygon(clause *WhereClause) (query.Query, error) {
	pq, err := parsePolygonQuery(clause)
	if err != nil {
		return nil, err
	}
	coords := polygonToGeoShapeCoords(pq.Polygon)
	q, err := bleve.NewGeoShapeQuery(coords, "polygon", "within")
	if err != nil {
		return nil, fmt.Errorf("withinPolygon: %w", err)
	}
	q.SetField(clause.Field)
	return q, nil
}

// convertIntersectsPolygon handles the "intersectsPolygon" operator using Bleve GeoShapeQuery.
func convertIntersectsPolygon(clause *WhereClause) (query.Query, error) {
	pq, err := parsePolygonQuery(clause)
	if err != nil {
		return nil, err
	}
	coords := polygonToGeoShapeCoords(pq.Polygon)
	q, err := bleve.NewGeoShapeQuery(coords, "polygon", "intersects")
	if err != nil {
		return nil, fmt.Errorf("intersectsPolygon: %w", err)
	}
	q.SetField(clause.Field)
	return q, nil
}

// convertDoesNotIntersectPolygon handles the "doesNotIntersectPolygon" operator.
// It negates the intersectsPolygon query.
func convertDoesNotIntersectPolygon(clause *WhereClause) (query.Query, error) {
	inner, err := convertIntersectsPolygon(clause)
	if err != nil {
		return nil, err
	}
	bq := bleve.NewBooleanQuery()
	bq.AddMust(bleve.NewMatchAllQuery())
	bq.AddMustNot(inner)
	return bq, nil
}

// convertDoesNotIntersectBoundingBox handles the "doesNotIntersectBoundingBox" operator.
// Uses GeoShapeQuery with a rectangular polygon so it works on both geopoint and geoshape fields.
func convertDoesNotIntersectBoundingBox(clause *WhereClause) (query.Query, error) {
	var bb BoundingBox
	if err := json.Unmarshal(clause.Value, &bb); err != nil {
		return nil, fmt.Errorf("doesNotIntersectBoundingBox value: %w", err)
	}
	// Convert bounding box to a rectangular polygon (counter-clockwise)
	rectPolygon := [][][][]float64{{
		{
			{bb.TopLeft.Longitude, bb.BottomRight.Latitude},
			{bb.BottomRight.Longitude, bb.BottomRight.Latitude},
			{bb.BottomRight.Longitude, bb.TopLeft.Latitude},
			{bb.TopLeft.Longitude, bb.TopLeft.Latitude},
			{bb.TopLeft.Longitude, bb.BottomRight.Latitude},
		},
	}}
	inner, err := bleve.NewGeoShapeQuery(rectPolygon, "polygon", "intersects")
	if err != nil {
		return nil, fmt.Errorf("doesNotIntersectBoundingBox: %w", err)
	}
	inner.SetField(clause.Field)
	bq := bleve.NewBooleanQuery()
	bq.AddMust(bleve.NewMatchAllQuery())
	bq.AddMustNot(inner)
	return bq, nil
}

// convertNot handles the "not" logical operator (non-opts).
func convertNot(clause *WhereClause) (query.Query, error) {
	return convertNotWithOpts(clause, nil)
}

// convertNotWithOpts handles "not" with options threading.
// Supports both Palantir V2 array format and single object format.
func convertNotWithOpts(clause *WhereClause, opts *ConvertOptions) (query.Query, error) {
	// Try array format first (Palantir V2)
	var subClauses []WhereClause
	if err := json.Unmarshal(clause.Value, &subClauses); err == nil && len(subClauses) > 0 {
		sub, err := ConvertToBleveQueryWithOpts(&subClauses[0], opts)
		if err != nil {
			return nil, fmt.Errorf("not sub-clause: %w", err)
		}
		bq := bleve.NewBooleanQuery()
		bq.AddMust(bleve.NewMatchAllQuery())
		bq.AddMustNot(sub)
		return bq, nil
	}

	// Fall back to single object format
	var subClause WhereClause
	if err := json.Unmarshal(clause.Value, &subClause); err != nil {
		return nil, fmt.Errorf("not value must be a where clause or array: %w", err)
	}

	sub, err := ConvertToBleveQueryWithOpts(&subClause, opts)
	if err != nil {
		return nil, fmt.Errorf("not sub-clause: %w", err)
	}

	bq := bleve.NewBooleanQuery()
	bq.AddMust(bleve.NewMatchAllQuery())
	bq.AddMustNot(sub)
	return bq, nil
}
