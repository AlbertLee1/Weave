package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// setupDatetimeSearchTest builds an OSS service around an "event" object
// type that carries a timestamp property (occurredAt) and an analyzed text
// property (description) so the Foundry interval / relativeDateRange
// filter operators can be exercised end to end through the chi router.
//
// Timestamps are seeded relative to the real wall clock because HTTP
// callers cannot inject a test clock; the BDD windows are days wide so the
// assertions stay deterministic no matter when the suite runs.
func setupDatetimeSearchTest(t *testing.T) http.Handler {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "eventId", BaseType: "string", IsSearchable: true},
		{APIName: "description", BaseType: "string", IsSearchable: true},
		{APIName: "occurredAt", BaseType: "timestamp", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("event", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	now := time.Now().UTC()
	docs := []struct {
		id   string
		desc string
		ts   time.Time
	}{
		{"evt-recent", "database migration completed successfully", now.Add(-48 * time.Hour)},
		{"evt-old", "database backup rotated", now.Add(-30 * 24 * time.Hour)},
		{"evt-future", "scheduled maintenance window", now.Add(9 * 24 * time.Hour)},
	}
	for _, d := range docs {
		doc := map[string]interface{}{
			"eventId":     d.id,
			"description": d.desc,
			"occurredAt":  d.ts.Format(time.RFC3339),
		}
		if err := mgr.IndexDocument("event", d.id, doc); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.id, err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.event",
		OntologyRID: testOntologyRID,
		APIName:     "event",
		DisplayName: "Event",
		PrimaryKey:  "eventId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	svc := oss.NewService(repo, mgr, &mockLinkResolver{results: map[string][]string{}})
	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func datetimeSearch(t *testing.T, router http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/event/search",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func datetimePrimaryKeys(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var page struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	pks := make([]string, 0, len(page.Data))
	for _, obj := range page.Data {
		pk, _ := obj["__primaryKey"].(string)
		pks = append(pks, pk)
	}
	sort.Strings(pks)
	return pks
}

func assertInvalidWhereClause(t *testing.T, rec *httptest.ResponseRecorder, reasonSub string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.ErrorCode != "INVALID_ARGUMENT" {
		t.Errorf("errorCode = %q, want INVALID_ARGUMENT", env.ErrorCode)
	}
	if env.ErrorName != "InvalidWhereClause" {
		t.Errorf("errorName = %q, want InvalidWhereClause", env.ErrorName)
	}
	if !strings.Contains(env.Parameters["reason"], reasonSub) {
		t.Errorf("parameters.reason = %q, want it to mention %q", env.Parameters["reason"], reasonSub)
	}
}

// TestBDD_SearchObjects_RelativeDateRange_FoundryParity covers the Foundry
// SearchJsonQueryV2 parity gap on POST .../objects/{objectType}/search:
// the `relativeDateRange` filter type (RelativeDateRangeQuery).
//
// Foundry contract: bounds are RelativePointInTime objects resolved
// against query execution time and truncated to the start of the bound's
// timeUnit in the REQUIRED timeZoneId; relativeStartTime is inclusive,
// relativeEndTime exclusive; negative values reach into the past.
//
// Acceptance criteria (Given → When → Then):
//
//	Given events at now-2d (evt-recent), now-30d (evt-old), now+9d
//	      (evt-future)
//	When  the search filters occurredAt to the last 7 days
//	      (relativeStartTime -7 DAY, relativeEndTime +1 DAY, Etc/UTC)
//	Then  HTTP 200 with exactly evt-recent
//
//	When  the window is the past year up to tomorrow
//	Then  evt-old joins evt-recent, evt-future stays excluded
//
//	When  timeZoneId is missing / the bound timeUnit is invalid
//	Then  HTTP 400 InvalidWhereClause with an explanatory reason
func TestBDD_SearchObjects_RelativeDateRange_FoundryParity(t *testing.T) {
	router := setupDatetimeSearchTest(t)

	t.Run("last 7 days window returns only the recent event", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"relativeDateRange","field":"occurredAt",
			"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"},
			"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
			"timeZoneId":"Etc/UTC"}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := datetimePrimaryKeys(t, rec)
		if len(got) != 1 || got[0] != "evt-recent" {
			t.Errorf("primary keys = %v, want [evt-recent]", got)
		}
	})

	t.Run("year-wide window includes the old event but not the future one", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"relativeDateRange","field":"occurredAt",
			"relativeStartTime":{"type":"relativePoint","value":-1,"timeUnit":"YEAR"},
			"relativeEndTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
			"timeZoneId":"Etc/UTC"}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := datetimePrimaryKeys(t, rec)
		want := []string{"evt-old", "evt-recent"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("primary keys = %v, want %v", got, want)
		}
	})

	t.Run("future-only window returns the future event", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"relativeDateRange","field":"occurredAt",
			"relativeStartTime":{"type":"relativePoint","value":1,"timeUnit":"DAY"},
			"timeZoneId":"Etc/UTC"}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := datetimePrimaryKeys(t, rec)
		if len(got) != 1 || got[0] != "evt-future" {
			t.Errorf("primary keys = %v, want [evt-future]", got)
		}
	})

	t.Run("missing timeZoneId returns 400 InvalidWhereClause", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"relativeDateRange","field":"occurredAt",
			"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"DAY"}}}`)
		assertInvalidWhereClause(t, rec, "timeZoneId")
	})

	t.Run("invalid timeUnit returns 400 InvalidWhereClause", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"relativeDateRange","field":"occurredAt",
			"relativeStartTime":{"type":"relativePoint","value":-7,"timeUnit":"FORTNIGHT"},
			"timeZoneId":"Etc/UTC"}}`)
		assertInvalidWhereClause(t, rec, "timeUnit")
	})
}

// TestBDD_SearchObjects_Interval_FoundryParity covers the Foundry
// SearchJsonQueryV2 parity gap for the `interval` filter type
// (IntervalQuery): a sub-rule tree evaluated against the ANALYZED form of
// text fields — per the official foundry-platform-python model docs this
// operator is a text-intervals query, not a time filter.
//
// Acceptance criteria (Given → When → Then):
//
//	Given evt-recent "database migration completed successfully",
//	      evt-old "database backup rotated",
//	      evt-future "scheduled maintenance window"
//	When  searching with rule {match "database migration" ordered}
//	Then  HTTP 200 with exactly evt-recent (evt-old lacks "migration")
//
//	When  the rule is {fuzzy term "databse"} (typo, default fuzziness 2)
//	Then  both database events match
//
//	When  the rule type is unknown
//	Then  HTTP 400 InvalidWhereClause names the unsupported rule type
func TestBDD_SearchObjects_Interval_FoundryParity(t *testing.T) {
	router := setupDatetimeSearchTest(t)

	t.Run("match rule requires all terms of the query", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"interval","field":"description",
			"rule":{"type":"match","query":"database migration","ordered":true}}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := datetimePrimaryKeys(t, rec)
		if len(got) != 1 || got[0] != "evt-recent" {
			t.Errorf("primary keys = %v, want [evt-recent]", got)
		}
	})

	t.Run("anyOf rule unions sub-rules", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"interval","field":"description",
			"rule":{"type":"anyOf","rules":[
				{"type":"match","query":"backup","ordered":false},
				{"type":"match","query":"maintenance","ordered":false}]}}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := datetimePrimaryKeys(t, rec)
		want := []string{"evt-future", "evt-old"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("primary keys = %v, want %v", got, want)
		}
	})

	t.Run("fuzzy rule tolerates a typo with default fuzziness", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"interval","field":"description",
			"rule":{"type":"fuzzy","term":"databse"}}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := datetimePrimaryKeys(t, rec)
		want := []string{"evt-old", "evt-recent"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("primary keys = %v, want %v", got, want)
		}
	})

	t.Run("unknown rule type returns 400 InvalidWhereClause", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"interval","field":"description",
			"rule":{"type":"regexp","query":"data.*"}}}`)
		assertInvalidWhereClause(t, rec, "unsupported interval rule type")
	})

	t.Run("missing rule returns 400 InvalidWhereClause", func(t *testing.T) {
		rec := datetimeSearch(t, router, `{"select":["eventId"],"where":{
			"type":"interval","field":"description"}}`)
		assertInvalidWhereClause(t, rec, "rule")
	})
}
