package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// setupFacetSearchRouter builds an OSS router backed by a real Bleve index
// populated with a small article corpus that has known object-type / owner
// distributions so the US-236 facet contract can be exercised end-to-end.
func setupFacetSearchRouter(t *testing.T) chi.Router {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	// owner + category use the not_analyzed analyzer so facet buckets
	// surface the raw term ("alice") rather than the English-stemmed form
	// ("alic"), matching realistic facet-field modelling.
	props := []index.Property{
		{APIName: "articleId", BaseType: "string", IsSearchable: true},
		{APIName: "title", BaseType: "string", IsSearchable: true},
		{APIName: "owner", BaseType: "string", IsSearchable: true, Analyzer: "not_analyzed"},
		{APIName: "category", BaseType: "string", IsSearchable: true, Analyzer: "not_analyzed"},
	}
	if _, err := mgr.EnsureIndex("article", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	docs := map[string]map[string]interface{}{
		"a1": {"articleId": "a1", "title": "alpha", "owner": "alice", "category": "news"},
		"a2": {"articleId": "a2", "title": "beta", "owner": "alice", "category": "news"},
		"a3": {"articleId": "a3", "title": "gamma", "owner": "alice", "category": "blog"},
		"a4": {"articleId": "a4", "title": "delta", "owner": "bob", "category": "news"},
		"a5": {"articleId": "a5", "title": "epsilon", "owner": "carol", "category": "blog"},
	}
	for id, d := range docs {
		if err := mgr.IndexDocument("article", id, d); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.article",
		OntologyRID: testOntologyRID,
		APIName:     "article",
		PrimaryKey:  "articleId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	svc := oss.NewService(repo, mgr, &mockLinkResolver{results: make(map[string][]string)})
	h := oss.NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func facetSearchURL(rawQuery string) string {
	u := "/api/v2/ontologies/" + testOntologyRID + "/objects/article/search"
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return u
}

// facetResponse mirrors the wire shape of an ObjectPage with facets so tests
// can read `facets[field]` without bothering with WireObject's custom
// (un)marshaller.
type facetResponse struct {
	Data   []map[string]interface{}    `json:"data"`
	Facets map[string][]facetBucketRow `json:"facets"`
}

type facetBucketRow struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

func decodeFacetResponse(t *testing.T, body []byte) facetResponse {
	t.Helper()
	var resp facetResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v; raw=%s", err, string(body))
	}
	return resp
}

func TestSearchObjects_FacetsQueryParam_SingleField(t *testing.T) {
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId","owner"]}`
	req := httptest.NewRequest("POST", facetSearchURL("facets=owner"), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeFacetResponse(t, rr.Body.Bytes())
	buckets, ok := resp.Facets["owner"]
	if !ok {
		t.Fatalf("expected facets.owner to be present; facets=%v", resp.Facets)
	}
	counts := map[string]int{}
	for _, b := range buckets {
		counts[b.Value] = b.Count
	}
	if counts["alice"] != 3 {
		t.Fatalf("expected alice=3, got %d (buckets=%v)", counts["alice"], buckets)
	}
	if counts["bob"] != 1 {
		t.Fatalf("expected bob=1, got %d", counts["bob"])
	}
	if counts["carol"] != 1 {
		t.Fatalf("expected carol=1, got %d", counts["carol"])
	}
}

func TestSearchObjects_FacetsQueryParam_MultipleFields(t *testing.T) {
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"]}`
	req := httptest.NewRequest("POST", facetSearchURL("facets=owner,category"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeFacetResponse(t, rr.Body.Bytes())
	if _, ok := resp.Facets["owner"]; !ok {
		t.Fatalf("expected owner facet, got %v", resp.Facets)
	}
	catBuckets, ok := resp.Facets["category"]
	if !ok {
		t.Fatalf("expected category facet, got %v", resp.Facets)
	}
	counts := map[string]int{}
	for _, b := range catBuckets {
		counts[b.Value] = b.Count
	}
	if counts["news"] != 3 {
		t.Fatalf("expected category.news=3, got %d (buckets=%v)", counts["news"], catBuckets)
	}
	if counts["blog"] != 2 {
		t.Fatalf("expected category.blog=2, got %d", counts["blog"])
	}
}

func TestSearchObjects_FacetsBody_Accepted(t *testing.T) {
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"],"facets":["owner"]}`
	req := httptest.NewRequest("POST", facetSearchURL(""), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeFacetResponse(t, rr.Body.Bytes())
	if _, ok := resp.Facets["owner"]; !ok {
		t.Fatalf("expected body-specified owner facet, got %v", resp.Facets)
	}
}

func TestSearchObjects_FacetsQueryParam_OverridesBody(t *testing.T) {
	// The query param should replace the body entirely so URL-only
	// invocations stay first-class.
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"],"facets":["owner"]}`
	req := httptest.NewRequest("POST", facetSearchURL("facets=category"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeFacetResponse(t, rr.Body.Bytes())
	if _, ok := resp.Facets["owner"]; ok {
		t.Fatalf("expected owner facet to be dropped when overridden; facets=%v", resp.Facets)
	}
	if _, ok := resp.Facets["category"]; !ok {
		t.Fatalf("expected category facet from query param; facets=%v", resp.Facets)
	}
}

func TestSearchObjects_FacetsOmitted_NoKey(t *testing.T) {
	// With neither body nor query-param facets, the response must remain
	// byte-identical to the pre-US-236 shape — no `facets` key.
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"]}`
	req := httptest.NewRequest("POST", facetSearchURL(""), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw["facets"]; ok {
		t.Fatalf("expected no `facets` key when unrequested; body=%s", rr.Body.String())
	}
}

func TestSearchObjects_FacetsQueryParam_EmptyReturns400(t *testing.T) {
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"]}`
	// Each raw value is already URL-encoded so httptest.NewRequest can
	// parse the request line; handler-side parsing sees the decoded form.
	cases := map[string]string{
		"empty":           "",
		"single_comma":    ",",
		"double_comma":    ",,",
		"whitespace_only": "%20%20%20",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("POST", facetSearchURL("facets="+raw), strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for facets=%q, got %d body=%s", raw, rr.Code, rr.Body.String())
			}
			var apiErr struct {
				ErrorCode string `json:"errorCode"`
				ErrorName string `json:"errorName"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if apiErr.ErrorCode != "INVALID_ARGUMENT" {
				t.Fatalf("expected errorCode=INVALID_ARGUMENT, got %s", apiErr.ErrorCode)
			}
			if apiErr.ErrorName != "InvalidFacets" {
				t.Fatalf("expected errorName=InvalidFacets, got %s", apiErr.ErrorName)
			}
		})
	}
}

func TestSearchObjects_FacetsQueryParam_UnknownField_EmptyBucket(t *testing.T) {
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"]}`
	req := httptest.NewRequest("POST", facetSearchURL("facets=doesNotExist"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeFacetResponse(t, rr.Body.Bytes())
	buckets, ok := resp.Facets["doesNotExist"]
	if !ok {
		t.Fatalf("expected doesNotExist key to be present with empty buckets; facets=%v", resp.Facets)
	}
	if len(buckets) != 0 {
		t.Fatalf("expected empty bucket list for unknown field, got %v", buckets)
	}
}

func TestSearchObjects_FacetsQueryParam_Deduplicates(t *testing.T) {
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"]}`
	req := httptest.NewRequest("POST", facetSearchURL("facets=owner,owner,owner"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeFacetResponse(t, rr.Body.Bytes())
	if _, ok := resp.Facets["owner"]; !ok {
		t.Fatalf("expected owner facet, got %v", resp.Facets)
	}
	if len(resp.Facets) != 1 {
		t.Fatalf("expected exactly one facet key after dedup, got %d (%v)", len(resp.Facets), resp.Facets)
	}
}
