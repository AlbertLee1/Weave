package oss

import (
	"strings"

	"github.com/liyang/weave/pkg/apierror"
)

// OrderBy is the Foundry SearchOrderByV2 wire shape shared by
// SearchObjectsRequestV2.orderBy and LoadObjectSetRequestV2.orderBy:
//
//	{"orderType": "fields"|"relevance" (optional, default "fields"),
//	 "fields": [{"field": "<prop>", "direction": "asc"|"desc"}]}
type OrderBy struct {
	// OrderType selects the ordering family. "fields" (or omitted) sorts by
	// the Fields list; "relevance" sorts by full-text match score. Any other
	// value is rejected with InvalidOrderBy instead of being silently
	// coerced.
	OrderType string         `json:"orderType,omitempty"`
	Fields    []OrderByField `json:"fields,omitempty"`
}

// OrderByField specifies a single field ordering. Direction defaults to
// "asc" when omitted, per the Foundry SearchOrderingV2 contract.
type OrderByField struct {
	Field     string `json:"field"`
	Direction string `json:"direction,omitempty"`
}

// SearchOrderByV2 orderType values. Matched case-insensitively so
// hand-written clients sending "FIELDS"/"Relevance" still resolve; Foundry's
// canonical wire form is lowercase.
const (
	orderTypeFields    = "fields"
	orderTypeRelevance = "relevance"
)

// scoreSortField is Bleve's reserved sort key for match score; the leading
// '-' sorts best-match first, which is what "relevance" ordering means.
const scoreSortField = "-_score"

// IsRelevance reports whether the caller asked for score ordering. Safe on
// a nil receiver so handlers can call it straight off an optional body
// field.
func (o *OrderBy) IsRelevance() bool {
	return o != nil && strings.EqualFold(strings.TrimSpace(o.OrderType), orderTypeRelevance)
}

// BleveSortOrder validates the Foundry V2 orderBy body and lowers it to a
// Bleve SortBy slice ("field" ascending, "-field" descending, "-_score" for
// relevance). Returns (nil, nil) when no ordering was expressed — a nil
// receiver, or orderType "fields" with an empty fields list — so callers can
// fall back to their legacy defaults. Invalid input (unknown orderType,
// empty field name, direction other than asc/desc, relevance combined with
// fields) returns a 400 InvalidOrderBy instead of silently returning
// unsorted data: silent misordering is exactly the bug class this contract
// exists to prevent.
func (o *OrderBy) BleveSortOrder() ([]string, *apierror.APIError) {
	if o == nil {
		return nil, nil
	}

	orderType := strings.ToLower(strings.TrimSpace(o.OrderType))
	switch orderType {
	case orderTypeRelevance:
		if len(o.Fields) > 0 {
			return nil, apierror.NewInvalidParameter("InvalidOrderBy", map[string]string{
				"reason": `orderBy with orderType "relevance" must not specify fields; relevance sorts by full-text match score`,
			})
		}
		return []string{scoreSortField}, nil
	case "", orderTypeFields:
		// fall through to field-list handling below
	default:
		return nil, apierror.NewInvalidParameter("InvalidOrderBy", map[string]string{
			"reason":    `orderBy.orderType must be "fields" or "relevance"`,
			"orderType": o.OrderType,
		})
	}

	if len(o.Fields) == 0 {
		return nil, nil
	}
	sortOrder := make([]string, 0, len(o.Fields))
	for _, f := range o.Fields {
		field := strings.TrimSpace(f.Field)
		if field == "" {
			return nil, apierror.NewInvalidParameter("InvalidOrderBy", map[string]string{
				"reason": "orderBy.fields[].field is required and must be a property apiName",
			})
		}
		switch strings.ToLower(strings.TrimSpace(f.Direction)) {
		case "", "asc":
			sortOrder = append(sortOrder, field)
		case "desc":
			sortOrder = append(sortOrder, "-"+field)
		default:
			return nil, apierror.NewInvalidParameter("InvalidOrderBy", map[string]string{
				"reason":    `orderBy.fields[].direction must be "asc" or "desc" (default "asc")`,
				"field":     f.Field,
				"direction": f.Direction,
			})
		}
	}
	return sortOrder, nil
}
