package where

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// ConvertToBleveQuery translates a Palantir V2 WhereClause into a Bleve query.
func ConvertToBleveQuery(clause *WhereClause) (query.Query, error) {
	if clause == nil {
		return nil, fmt.Errorf("where clause is nil")
	}

	switch clause.Type {
	case "eq":
		return convertEq(clause)
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
	case "containsAllTerms":
		return convertContainsAllTerms(clause)
	case "containsAnyTerm":
		return convertContainsAnyTerm(clause)
	case "containsAllTermsInOrder":
		return convertContainsAllTermsInOrder(clause)
	case "startsWith":
		return convertStartsWith(clause)
	case "wildcard":
		return convertWildcard(clause)
	case "and":
		return convertAnd(clause)
	case "or":
		return convertOr(clause)
	case "not":
		return convertNot(clause)
	case "withinBoundingBox":
		return convertWithinBoundingBox(clause)
	case "intersectsBoundingBox":
		return convertWithinBoundingBox(clause) // same as within for point data
	case "withinPolygon":
		return nil, fmt.Errorf("withinPolygon not yet supported")
	case "intersectsPolygon":
		return nil, fmt.Errorf("intersectsPolygon not yet supported")
	case "withinDistanceOf":
		return convertWithinDistanceOf(clause)
	default:
		return nil, fmt.Errorf("unsupported where clause type: %q", clause.Type)
	}
}

// convertEq handles the "eq" operator.
// For strings: TermQuery. For numbers: NumericRangeQuery with min==max. For booleans: BoolFieldQuery.
func convertEq(clause *WhereClause) (query.Query, error) {
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

// convertContainsAllTerms handles the "containsAllTerms" operator.
// Splits the value into terms and requires ALL to match (BooleanQuery with Must for each).
func convertContainsAllTerms(clause *WhereClause) (query.Query, error) {
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
		bq.AddMust(mq)
	}
	return bq, nil
}

// convertContainsAnyTerm handles the "containsAnyTerm" operator.
// Uses a MatchQuery which ORs terms by default.
func convertContainsAnyTerm(clause *WhereClause) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("containsAnyTerm value must be a string: %w", err)
	}

	q := bleve.NewMatchQuery(strVal)
	q.SetField(clause.Field)
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

// convertAnd handles the "and" logical operator.
// Each sub-clause becomes a Must in a BooleanQuery.
func convertAnd(clause *WhereClause) (query.Query, error) {
	var subClauses []WhereClause
	if err := json.Unmarshal(clause.Value, &subClauses); err != nil {
		return nil, fmt.Errorf("and value must be an array of where clauses: %w", err)
	}

	bq := bleve.NewBooleanQuery()
	for i := range subClauses {
		sub, err := ConvertToBleveQuery(&subClauses[i])
		if err != nil {
			return nil, fmt.Errorf("and sub-clause %d: %w", i, err)
		}
		bq.AddMust(sub)
	}
	return bq, nil
}

// convertOr handles the "or" logical operator.
// Each sub-clause becomes a Should in a BooleanQuery with MinShould=1.
func convertOr(clause *WhereClause) (query.Query, error) {
	var subClauses []WhereClause
	if err := json.Unmarshal(clause.Value, &subClauses); err != nil {
		return nil, fmt.Errorf("or value must be an array of where clauses: %w", err)
	}

	if len(subClauses) == 0 {
		return bleve.NewMatchNoneQuery(), nil
	}

	bq := bleve.NewBooleanQuery()
	for i := range subClauses {
		sub, err := ConvertToBleveQuery(&subClauses[i])
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

// convertNot handles the "not" logical operator.
// Supports both Palantir V2 array format: {"type":"not","value":[{"type":"eq",...}]}
// and single object format: {"type":"not","value":{"type":"eq",...}}
func convertNot(clause *WhereClause) (query.Query, error) {
	// Try array format first (Palantir V2)
	var subClauses []WhereClause
	if err := json.Unmarshal(clause.Value, &subClauses); err == nil && len(subClauses) > 0 {
		sub, err := ConvertToBleveQuery(&subClauses[0])
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

	sub, err := ConvertToBleveQuery(&subClause)
	if err != nil {
		return nil, fmt.Errorf("not sub-clause: %w", err)
	}

	bq := bleve.NewBooleanQuery()
	bq.AddMust(bleve.NewMatchAllQuery())
	bq.AddMustNot(sub)
	return bq, nil
}
