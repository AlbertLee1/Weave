package where

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
)

// --- "relativeDateRange" operator tests (Foundry SearchJsonQueryV2 parity) ---
//
// Foundry RelativeDateRangeQuery contract:
//
//	{"type":"relativeDateRange","field":"<prop>",
//	 "relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
//	 "relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
//	 "timeZoneId":"Etc/UTC"}
//
// Bounds are resolved relative to query execution time, truncated to the
// start of the bound's timeUnit period in the supplied tz database zone
// (e.g. {value:1, timeUnit:MONTH} means "the start of next month").
// relativeStartTime is INCLUSIVE, relativeEndTime is EXCLUSIVE. Negative
// values point into the past, positive into the future. timeZoneId is
// REQUIRED; at least one bound is required.

// fixedNow is a Wednesday (2026-06-17) mid-month / mid-year so every
// truncation unit (DAY / WEEK / MONTH / YEAR) has a distinct boundary.
var fixedNow = time.Date(2026, time.June, 17, 15, 4, 5, 0, time.UTC)

func fixedNowOpts() *ConvertOptions {
	return &ConvertOptions{Now: func() time.Time { return fixedNow }}
}

// setupDateTestIndex creates a Bleve index with an "occurredAt" datetime
// field and documents at absolute instants chosen around fixedNow.
func setupDateTestIndex(t *testing.T) bleve.Index {
	t.Helper()

	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("occurredAt", bleve.NewDateTimeFieldMapping())
	docMapping.AddFieldMappingsAt("name", bleve.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "dates"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id string
		ts string
	}{
		{"d-old", "2026-06-01T12:00:00Z"},        // 16 days before fixedNow
		{"d-8days", "2026-06-09T12:00:00Z"},      // 8 days before fixedNow
		{"d-start-edge", "2026-06-10T00:00:00Z"}, // exactly midnight UTC 7 days ago
		{"d-recent", "2026-06-15T12:00:00Z"},     // 2 days before fixedNow
		{"d-tomorrow", "2026-06-18T00:00:00Z"},   // exactly midnight UTC tomorrow
		{"d-june20", "2026-06-20T12:00:00Z"},     // 3 days after fixedNow
		{"d-nextmonth", "2026-07-02T12:00:00Z"},  // next calendar month
	}
	for _, d := range docs {
		if err := idx.Index(d.id, map[string]interface{}{"occurredAt": d.ts, "name": d.id}); err != nil {
			t.Fatalf("index doc %s: %v", d.id, err)
		}
	}
	return idx
}

func relativeDateRangeClause(t *testing.T, payload string) *WhereClause {
	t.Helper()
	var clause WhereClause
	if err := json.Unmarshal([]byte(payload), &clause); err != nil {
		t.Fatalf("unmarshal clause: %v", err)
	}
	return &clause
}

func TestRelativeDateRange_ResolvesBoundsAgainstInjectedClock(t *testing.T) {
	idx := setupDateTestIndex(t)

	tests := []struct {
		name    string
		payload string
		want    []string
	}{
		{
			name: "last 7 days window: start inclusive at midnight -7d, end exclusive at midnight +1d",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
				"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
				"timeZoneId":"Etc/UTC"}`,
			// d-8days (06-09) < 06-10T00:00 start → out.
			// d-start-edge sits exactly on the inclusive start → in.
			// d-tomorrow sits exactly on the exclusive end → out.
			want: []string{"d-recent", "d-start-edge"},
		},
		{
			name: "start-only bound leaves the range open-ended",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
				"timeZoneId":"Etc/UTC"}`,
			want: []string{"d-june20", "d-nextmonth", "d-recent", "d-start-edge", "d-tomorrow"},
		},
		{
			name: "end-only bound truncates to the start of next month",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"MONTH"},
				"timeZoneId":"Etc/UTC"}`,
			// end = 2026-07-01T00:00Z (start of NEXT month, not now+1 month).
			want: []string{"d-8days", "d-june20", "d-old", "d-recent", "d-start-edge", "d-tomorrow"},
		},
		{
			name: "WEEK truncates to the ISO week start (Monday midnight)",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":0,"timeUnit":"WEEK"},
				"timeZoneId":"Etc/UTC"}`,
			// fixedNow is Wednesday 06-17 → week start Monday 06-15T00:00Z.
			want: []string{"d-june20", "d-nextmonth", "d-recent", "d-tomorrow"},
		},
		{
			name: "YEAR truncates to January 1st",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":0,"timeUnit":"YEAR"},
				"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"YEAR"},
				"timeZoneId":"Etc/UTC"}`,
			want: []string{"d-8days", "d-june20", "d-nextmonth", "d-old", "d-recent", "d-start-edge", "d-tomorrow"},
		},
		{
			name: "future-only window matches nothing seeded in the past",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":1,"timeUnit":"YEAR"},
				"timeZoneId":"Etc/UTC"}`,
			want: []string{},
		},
		{
			name: "timeZoneId shifts the midnight boundary (America/New_York is UTC-4 in June)",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
				"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
				"timeZoneId":"America/New_York"}`,
			// Start = 2026-06-10T00:00-04:00 = 2026-06-10T04:00Z, so the
			// 00:00Z edge doc falls OUT of the window; end = 06-18T04:00Z
			// pulls the 06-18T00:00Z doc IN.
			want: []string{"d-recent", "d-tomorrow"},
		},
		{
			name: "bound type discriminator relativePoint is accepted when present or omitted",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"value":-7,"timeUnit":"DAY"},
				"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
				"timeZoneId":"Etc/UTC"}`,
			want: []string{"d-recent", "d-start-edge"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clause := relativeDateRangeClause(t, tc.payload)
			ids := searchWithWhereOpts(t, idx, clause, fixedNowOpts())
			assertIDs(t, ids, tc.want)
		})
	}
}

func TestRelativeDateRange_DefaultsToWallClockWhenNoClockInjected(t *testing.T) {
	// Without an injected clock the converter must fall back to time.Now:
	// index one doc 2 days in the past and one 400 days in the past, then a
	// "last 7 days" window must return only the recent one. Bounds are days
	// wide, so this stays deterministic regardless of when the test runs.
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("occurredAt", bleve.NewDateTimeFieldMapping())
	indexMapping.DefaultMapping = docMapping

	idx, err := bleve.New(filepath.Join(t.TempDir(), "wallclock"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	now := time.Now().UTC()
	docs := map[string]string{
		"recent":  now.Add(-48 * time.Hour).Format(time.RFC3339),
		"ancient": now.Add(-400 * 24 * time.Hour).Format(time.RFC3339),
	}
	for id, ts := range docs {
		if err := idx.Index(id, map[string]interface{}{"occurredAt": ts}); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}

	clause := relativeDateRangeClause(t, `{"type":"relativeDateRange","field":"occurredAt",
		"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
		"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
		"timeZoneId":"Etc/UTC"}`)
	assertIDs(t, searchWithWhere(t, idx, clause), []string{"recent"})
}

func TestRelativeDateRange_InvalidPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantSub string
	}{
		{
			name: "missing timeZoneId rejected",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"}}`,
			wantSub: "timeZoneId",
		},
		{
			name: "invalid timeZoneId rejected",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
				"timeZoneId":"Mars/Olympus_Mons"}`,
			wantSub: "timeZoneId",
		},
		{
			name: "missing both bounds rejected",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"timeZoneId":"Etc/UTC"}`,
			wantSub: "relativeStartTime",
		},
		{
			name: "invalid timeUnit rejected",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"HOUR"},
				"timeZoneId":"Etc/UTC"}`,
			wantSub: "timeUnit",
		},
		{
			name: "non-integer bound value rejected",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeStartTime":{"type":"relativePoint","value":"yesterday","timeUnit":"DAY"},
				"timeZoneId":"Etc/UTC"}`,
			wantSub: "relativeStartTime",
		},
		{
			name: "wrong bound discriminator rejected",
			payload: `{"type":"relativeDateRange","field":"occurredAt",
				"relativeEndTime":{"type":"absolutePoint","value":1,"timeUnit":"DAY"},
				"timeZoneId":"Etc/UTC"}`,
			wantSub: "relativePoint",
		},
		{
			name: "missing field rejected",
			payload: `{"type":"relativeDateRange",
				"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
				"timeZoneId":"Etc/UTC"}`,
			wantSub: "field",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clause := relativeDateRangeClause(t, tc.payload)
			_, err := ConvertToBleveQueryWithOpts(clause, fixedNowOpts())
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidWhereClause) {
				t.Errorf("error %v must wrap ErrInvalidWhereClause so the handler returns 400", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q must mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}
