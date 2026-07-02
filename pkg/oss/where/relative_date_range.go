package where

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// convertRelativeDateRange handles the Foundry SearchJsonQueryV2
// "relativeDateRange" operator (RelativeDateRangeQuery):
//
//	{"type":"relativeDateRange","field":"<dateProp>",
//	 "relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
//	 "relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
//	 "timeZoneId":"Etc/UTC"}
//
// Semantics (per the official RelativeDateRangeQuery model): both bounds
// are resolved against query execution time and truncated to the start of
// the bound's timeUnit period in the supplied tz database zone — e.g.
// {value:-7, timeUnit:DAY} is midnight seven days ago, {value:1,
// timeUnit:MONTH} is the start of NEXT month (offset first, then
// truncate). relativeStartTime is INCLUSIVE and relativeEndTime is
// EXCLUSIVE; negative values reach into the past. timeZoneId is required
// and at least one bound must be present.
//
// The resolved absolute instants become a Bleve date-range query — the
// same query family the gt/gte/lt/lte operators use for date properties —
// so index-side type dispatch stays identical.
func convertRelativeDateRange(clause *WhereClause, opts *ConvertOptions) (query.Query, error) {
	if strings.TrimSpace(clause.Field) == "" {
		return nil, fmt.Errorf("relativeDateRange requires a field")
	}
	if strings.TrimSpace(clause.TimeZoneID) == "" {
		return nil, fmt.Errorf("relativeDateRange requires a timeZoneId (tz database zone, e.g. \"Etc/UTC\")")
	}
	loc, err := time.LoadLocation(clause.TimeZoneID)
	if err != nil {
		return nil, fmt.Errorf("relativeDateRange timeZoneId %q is not a valid tz database zone: %w", clause.TimeZoneID, err)
	}

	startBound, err := parseRelativeBound("relativeStartTime", clause.RelativeStartTime)
	if err != nil {
		return nil, err
	}
	endBound, err := parseRelativeBound("relativeEndTime", clause.RelativeEndTime)
	if err != nil {
		return nil, err
	}
	if startBound == nil && endBound == nil {
		return nil, fmt.Errorf("relativeDateRange requires at least one of relativeStartTime / relativeEndTime")
	}

	now := time.Now()
	if opts != nil && opts.Now != nil {
		now = opts.Now()
	}

	var startStr, endStr string
	if startBound != nil {
		startStr = resolveRelativePoint(now, loc, *startBound).Format(time.RFC3339Nano)
	}
	if endBound != nil {
		endStr = resolveRelativePoint(now, loc, *endBound).Format(time.RFC3339Nano)
	}

	// Foundry contract: lower bound inclusive, upper bound exclusive.
	startInclusive, endInclusive := true, false
	q := bleve.NewDateRangeInclusiveStringQuery(startStr, endStr, &startInclusive, &endInclusive)
	q.SetField(clause.Field)
	return q, nil
}

// parseRelativeBound decodes an optional RelativePointInTime bound. A
// missing key / JSON null yields (nil, nil); a present bound must carry a
// supported timeUnit and — when it names a discriminator at all — the
// "relativePoint" type.
func parseRelativeBound(key string, raw json.RawMessage) (*RelativePointInTime, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var bound RelativePointInTime
	if err := json.Unmarshal(raw, &bound); err != nil {
		return nil, fmt.Errorf("relativeDateRange %s must be a relativePoint object with integer value and timeUnit: %w", key, err)
	}
	if bound.Type != "" && bound.Type != "relativePoint" {
		return nil, fmt.Errorf("relativeDateRange %s type must be \"relativePoint\", got %q", key, bound.Type)
	}
	switch bound.TimeUnit {
	case "DAY", "WEEK", "MONTH", "YEAR":
		return &bound, nil
	default:
		return nil, fmt.Errorf("relativeDateRange %s timeUnit must be one of DAY/WEEK/MONTH/YEAR, got %q", key, bound.TimeUnit)
	}
}

// resolveRelativePoint applies the signed offset to "now" in the bound's
// unit and truncates the result to the start of that unit's period in
// loc: DAY → local midnight, WEEK → Monday midnight (ISO week start),
// MONTH → first of the month, YEAR → January 1st. Offset-then-truncate is
// what makes {value:1, timeUnit:MONTH} mean "start of next month".
func resolveRelativePoint(now time.Time, loc *time.Location, bound RelativePointInTime) time.Time {
	local := now.In(loc)
	switch bound.TimeUnit {
	case "WEEK":
		t := local.AddDate(0, 0, 7*bound.Value)
		daysSinceMonday := (int(t.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
		t = t.AddDate(0, 0, -daysSinceMonday)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	case "MONTH":
		// Compute the target month arithmetically instead of AddDate so a
		// short target month cannot overflow into the one after it
		// (Jan 31 + 1 month must land in February, not March).
		totalMonths := local.Year()*12 + int(local.Month()) - 1 + bound.Value
		return time.Date(totalMonths/12, time.Month(totalMonths%12+1), 1, 0, 0, 0, 0, loc)
	case "YEAR":
		return time.Date(local.Year()+bound.Value, time.January, 1, 0, 0, 0, 0, loc)
	default: // "DAY" — parseRelativeBound already rejected everything else
		t := local.AddDate(0, 0, bound.Value)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
}
