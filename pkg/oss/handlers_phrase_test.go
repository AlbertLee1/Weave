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

// setupPhraseSearchRouter wires an OSS handler in front of a real Bleve
// index populated with short descriptive sentences so the `phrase` operator
// can be exercised end-to-end.
func setupPhraseSearchRouter(t *testing.T) chi.Router {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "articleId", BaseType: "string", IsSearchable: true},
		{APIName: "description", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("article", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	docs := map[string]map[string]interface{}{
		"a1": {"articleId": "a1", "description": "the quick fox jumps over the lazy dog"},
		"a2": {"articleId": "a2", "description": "the quick brown fox jumps over the lazy dog"},
		"a3": {"articleId": "a3", "description": "the cat sleeps on the mat"},
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

func phraseSearchURL() string {
	return "/api/v2/ontologies/" + testOntologyRID + "/objects/article/search"
}

func TestSearchObjects_Phrase_StructuredValue_StrictAdjacency(t *testing.T) {
	r := setupPhraseSearchRouter(t)

	body := `{"where":{"type":"phrase","field":"description","value":{"phrase":"quick fox","slop":0}},"select":["articleId","description"]}`
	req := httptest.NewRequest("POST", phraseSearchURL(), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	ids := collectIDs(t, rr.Body.Bytes())
	if len(ids) != 1 || ids[0] != "a1" {
		t.Fatalf("got %v, want [a1]", ids)
	}
}

func TestSearchObjects_Phrase_StructuredValue_Slop1(t *testing.T) {
	r := setupPhraseSearchRouter(t)

	body := `{"where":{"type":"phrase","field":"description","value":{"phrase":"quick fox","slop":1}},"select":["articleId","description"]}`
	req := httptest.NewRequest("POST", phraseSearchURL(), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	ids := collectIDs(t, rr.Body.Bytes())
	if len(ids) != 2 {
		t.Fatalf("got %v, want 2 results (a1, a2)", ids)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["a1"] || !got["a2"] {
		t.Fatalf("got %v, want both a1 and a2", ids)
	}
}

func TestSearchObjects_Phrase_LuceneStringForm(t *testing.T) {
	r := setupPhraseSearchRouter(t)

	// Lucene-style '"quick fox"~1' passed as the clause value (a JSON string).
	body := `{"where":{"type":"phrase","field":"description","value":"\"quick fox\"~1"},"select":["articleId","description"]}`
	req := httptest.NewRequest("POST", phraseSearchURL(), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	ids := collectIDs(t, rr.Body.Bytes())
	if len(ids) != 2 {
		t.Fatalf("got %v, want 2 results", ids)
	}
}

func TestSearchObjects_Phrase_InvalidSlop(t *testing.T) {
	r := setupPhraseSearchRouter(t)

	body := `{"where":{"type":"phrase","field":"description","value":{"phrase":"quick fox","slop":9999}},"select":["articleId"]}`
	req := httptest.NewRequest("POST", phraseSearchURL(), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected non-200 for out-of-range slop, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// collectIDs extracts the articleId/userId primary key values from a search
// response body (shaped like {"data": [{...}], "nextPageToken": ...}).
func collectIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, string(body))
	}
	var ids []string
	for _, row := range resp.Data {
		if v, ok := row["articleId"].(string); ok {
			ids = append(ids, v)
			continue
		}
		if v, ok := row["userId"].(string); ok {
			ids = append(ids, v)
		}
	}
	return ids
}
