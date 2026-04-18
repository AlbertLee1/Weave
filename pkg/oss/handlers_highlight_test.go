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

// setupHighlightSearchRouter wires an OSS handler in front of a real Bleve
// index populated with descriptive text so the US-235 `_highlights` contract
// can be exercised end-to-end.
func setupHighlightSearchRouter(t *testing.T) chi.Router {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "articleId", BaseType: "string", IsSearchable: true},
		{APIName: "title", BaseType: "string", IsSearchable: true},
		{APIName: "body", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("article", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	docs := map[string]map[string]interface{}{
		"a1": {
			"articleId": "a1",
			"title":     "the quick brown fox",
			"body":      "the quick brown fox jumps over the lazy dog",
		},
		"a2": {
			"articleId": "a2",
			"title":     "elephants in zoos",
			"body":      "the fox roamed across the meadow looking for a rabbit",
		},
		"a3": {
			"articleId": "a3",
			"title":     "kittens",
			"body":      "kittens drink milk quietly",
		},
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

func highlightSearchURL(rawQuery string) string {
	u := "/api/v2/ontologies/" + testOntologyRID + "/objects/article/search"
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return u
}

// decodeHighlightResponse extracts the decoded `data` rows from a JSON search
// response so tests can index into the `_highlights` map without bothering
// with WireObject's custom (un)marshaller.
func decodeHighlightResponse(t *testing.T, body []byte) []map[string]interface{} {
	t.Helper()
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v; raw=%s", err, string(body))
	}
	return resp.Data
}

func TestSearchObjects_Highlight_BodyEnabled_WrapsWithMark(t *testing.T) {
	r := setupHighlightSearchRouter(t)

	body := `{"where":{"type":"eq","field":"body","value":"fox"},"select":["articleId","body"],"highlight":{"fields":["body"]}}`
	req := httptest.NewRequest("POST", highlightSearchURL(""), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows := decodeHighlightResponse(t, rr.Body.Bytes())
	if len(rows) == 0 {
		t.Fatalf("expected at least one row, got %d", len(rows))
	}
	foundMark := false
	for _, row := range rows {
		hl, ok := row["_highlights"].(map[string]interface{})
		if !ok {
			t.Fatalf("row missing _highlights map: %v", row)
		}
		bodySnippets, ok := hl["body"].([]interface{})
		if !ok || len(bodySnippets) == 0 {
			t.Fatalf("expected body snippets, got %v", hl)
		}
		for _, s := range bodySnippets {
			snippet, _ := s.(string)
			if strings.Contains(snippet, "<mark>fox</mark>") {
				foundMark = true
			}
		}
	}
	if !foundMark {
		t.Fatalf("expected at least one <mark>fox</mark> snippet in responses: %s", rr.Body.String())
	}
}

func TestSearchObjects_Highlight_OmitsFieldWhenDisabled(t *testing.T) {
	// Without the body flag AND without the query param, responses must
	// remain byte-identical to the pre-US-235 shape — no `_highlights` key.
	r := setupHighlightSearchRouter(t)

	body := `{"where":{"type":"eq","field":"body","value":"fox"},"select":["articleId","body"]}`
	req := httptest.NewRequest("POST", highlightSearchURL(""), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows := decodeHighlightResponse(t, rr.Body.Bytes())
	for _, row := range rows {
		if _, ok := row["_highlights"]; ok {
			t.Fatalf("highlights disabled, but got `_highlights` key: %v", row)
		}
	}
}

func TestSearchObjects_Highlight_QueryParamTrue_EnablesAllFields(t *testing.T) {
	r := setupHighlightSearchRouter(t)

	body := `{"where":{"type":"eq","field":"body","value":"fox"},"select":["articleId","body","title"]}`
	req := httptest.NewRequest("POST", highlightSearchURL("highlight=true"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows := decodeHighlightResponse(t, rr.Body.Bytes())
	// At least one returned row should carry highlights on `body` because
	// that's the field the match targeted.
	found := false
	for _, row := range rows {
		hl, ok := row["_highlights"].(map[string]interface{})
		if !ok {
			continue
		}
		if arr, ok := hl["body"].([]interface{}); ok && len(arr) > 0 {
			for _, s := range arr {
				if strings.Contains(s.(string), "<mark>") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected at least one <mark>-wrapped body snippet, got %s", rr.Body.String())
	}
}

func TestSearchObjects_Highlight_QueryParamFields_RestrictsToList(t *testing.T) {
	// `?highlight=body` should highlight only the body field even when the
	// query also hits another searchable field (title).
	r := setupHighlightSearchRouter(t)

	body := `{"where":{"type":"or","value":[{"type":"eq","field":"body","value":"fox"},{"type":"eq","field":"title","value":"fox"}]},"select":["articleId","body","title"]}`
	req := httptest.NewRequest("POST", highlightSearchURL("highlight=body"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows := decodeHighlightResponse(t, rr.Body.Bytes())
	if len(rows) == 0 {
		t.Fatalf("expected rows, got 0")
	}
	for _, row := range rows {
		hl, ok := row["_highlights"].(map[string]interface{})
		if !ok {
			continue
		}
		if _, exists := hl["title"]; exists {
			t.Fatalf("title should be excluded from `?highlight=body`, got %v", hl)
		}
	}
}

func TestSearchObjects_Highlight_QueryParamFalse_DisablesBodyOpt(t *testing.T) {
	// Query-string wins: even when the body asks for highlights, an
	// explicit `?highlight=false` must suppress the `_highlights` key.
	r := setupHighlightSearchRouter(t)

	body := `{"where":{"type":"eq","field":"body","value":"fox"},"select":["articleId","body"],"highlight":{"fields":["body"]}}`
	req := httptest.NewRequest("POST", highlightSearchURL("highlight=false"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows := decodeHighlightResponse(t, rr.Body.Bytes())
	for _, row := range rows {
		if _, ok := row["_highlights"]; ok {
			t.Fatalf("`?highlight=false` should suppress _highlights, got %v", row)
		}
	}
}

func TestSearchObjects_Highlight_InvalidQueryParam_Returns400(t *testing.T) {
	r := setupHighlightSearchRouter(t)

	body := `{"where":{"type":"eq","field":"body","value":"fox"},"select":["articleId"]}`
	req := httptest.NewRequest("POST", highlightSearchURL("highlight=,,,"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "InvalidHighlight" {
		t.Fatalf("errorName=%q, want InvalidHighlight (body=%s)", apiErr.ErrorName, rr.Body.String())
	}
}

func TestSearchObjects_Highlight_NoMatchingField_OmitsKeyPerRow(t *testing.T) {
	// Rows that have no matching terms on the highlighted field should
	// still come back — just without a `_highlights` entry. This proves
	// the feature is strictly additive: non-matches never inject an
	// empty or null highlight map.
	r := setupHighlightSearchRouter(t)

	body := `{"where":{"type":"eq","field":"body","value":"fox"},"select":["articleId","body","title"],"highlight":{"fields":["title"]}}`
	req := httptest.NewRequest("POST", highlightSearchURL(""), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows := decodeHighlightResponse(t, rr.Body.Bytes())
	for _, row := range rows {
		if hl, ok := row["_highlights"].(map[string]interface{}); ok {
			if v, present := hl["title"]; present {
				arr, _ := v.([]interface{})
				if len(arr) == 0 {
					t.Fatalf("highlight map should omit empty field entries, got %v", hl)
				}
			}
		}
	}
}

func TestSearchObjects_Highlight_WireRoundTrip(t *testing.T) {
	// A WireObject with Highlights must round-trip through
	// Marshal/Unmarshal, so downstream SDKs can parse responses using the
	// same type we emit.
	wo := &oss.WireObject{
		RID:        "ri.phonograph2-objects.main.object.a1",
		PrimaryKey: "a1",
		APIName:    "article",
		Properties: map[string]interface{}{"articleId": "a1"},
		Highlights: map[string][]string{
			"body": {"the quick brown <mark>fox</mark> jumps"},
		},
	}
	data, err := json.Marshal(wo)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"_highlights"`) {
		t.Fatalf("expected `_highlights` key in JSON, got %s", string(data))
	}

	var decoded oss.WireObject
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Highlights) != 1 || len(decoded.Highlights["body"]) != 1 {
		t.Fatalf("Highlights lost in round-trip: %+v", decoded.Highlights)
	}
	if decoded.Highlights["body"][0] != wo.Highlights["body"][0] {
		t.Fatalf("snippet changed: got %q want %q",
			decoded.Highlights["body"][0], wo.Highlights["body"][0])
	}
}
