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
	"github.com/liyang/weave/pkg/oss/where"
)

// setupRegexSearchRouter wires an OSS handler in front of a real Bleve index
// so search?regex=... can be tested end-to-end.
func setupRegexSearchRouter(t *testing.T) chi.Router {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "userId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("user", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	docs := map[string]map[string]interface{}{
		"u1": {"userId": "u1", "name": "alice"},
		"u2": {"userId": "u2", "name": "bob"},
		"u3": {"userId": "u3", "name": "alfred"},
	}
	for id, d := range docs {
		if err := mgr.IndexDocument("user", id, d); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.user",
		OntologyRID: testOntologyRID,
		APIName:     "user",
		PrimaryKey:  "userId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	svc := oss.NewService(repo, mgr, &mockLinkResolver{results: make(map[string][]string)})
	h := oss.NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func TestSearchObjects_RegexQueryParam_PrefixMatch(t *testing.T) {
	r := setupRegexSearchRouter(t)

	body := `{"select":["userId","name"]}`
	req := httptest.NewRequest("POST", searchURL("regex=name:al.*"), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := map[string]bool{}
	for _, row := range resp.Data {
		got[row["userId"].(string)] = true
	}
	if !got["u1"] || !got["u3"] || got["u2"] {
		t.Fatalf("want {u1, u3} only, got %v", got)
	}
}

func TestSearchObjects_RegexBodyClause(t *testing.T) {
	r := setupRegexSearchRouter(t)

	body := `{"where":{"type":"regex","field":"name","value":"b.*"},"select":["userId"]}`
	req := httptest.NewRequest("POST", searchURL(""), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Data) != 1 || resp.Data[0]["userId"] != "u2" {
		t.Fatalf("want [u2], got %v", resp.Data)
	}
}

func TestSearchObjects_RegexQueryParam_OverridesBody(t *testing.T) {
	// body.where says match "b.*" but query-string says "al.*" — the override wins.
	r := setupRegexSearchRouter(t)

	body := `{"where":{"type":"regex","field":"name","value":"b.*"},"select":["userId"]}`
	req := httptest.NewRequest("POST", searchURL("regex=name:al.*"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	got := map[string]bool{}
	for _, row := range resp.Data {
		got[row["userId"].(string)] = true
	}
	if !got["u1"] || !got["u3"] || got["u2"] {
		t.Fatalf("query-string should win, want {u1, u3}; got %v", got)
	}
}

func TestSearchObjects_RegexQueryParam_MalformedReturns400(t *testing.T) {
	r := setupRegexSearchRouter(t)

	body := `{"select":["userId"]}`
	cases := []string{
		"al.*",       // no colon
		":al.*",      // empty field
		"name:",      // empty pattern
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			req := httptest.NewRequest("POST", searchURL("regex="+raw), strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
			}
			var apiErr struct {
				ErrorName string `json:"errorName"`
			}
			_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
			if apiErr.ErrorName != "InvalidRegex" {
				t.Fatalf("errorName = %q, want InvalidRegex (body=%s)", apiErr.ErrorName, rr.Body.String())
			}
		})
	}
}

func TestSearchObjects_RegexInvalidPatternReturns400(t *testing.T) {
	// An unbalanced parenthesis is rejected at convert time and surfaces as
	// SearchObjectsFailed (400) by the handler.
	r := setupRegexSearchRouter(t)

	body := `{"where":{"type":"regex","field":"name","value":"^(unbalanced"},"select":["userId"]}`
	req := httptest.NewRequest("POST", searchURL(""), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "regex pattern invalid") {
		t.Fatalf("body should mention 'regex pattern invalid'; got %s", rr.Body.String())
	}
}

func TestSearchObjects_RegexTimeoutCancelledContext(t *testing.T) {
	// Pre-cancel the request context with DeadlineExceeded so the bleve search
	// returns immediately. The service-layer wrapping should mention the
	// regex timeout in the surfaced error, proving the cancellation path is
	// wired regardless of how fast bleve completes against tiny indexes.
	r := setupRegexSearchRouter(t)

	body := `{"where":{"type":"regex","field":"name","value":"al.*"},"select":["userId"]}`
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	req := httptest.NewRequest("POST", searchURL(""), strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	body2 := rr.Body.String()
	if !strings.Contains(body2, "regex search exceeded") || !strings.Contains(body2, where.RegexQueryTimeout.String()) {
		t.Fatalf("body should mention regex timeout %s; got %s", where.RegexQueryTimeout, body2)
	}
}

func TestSearchObjects_RegexPatternColonsTolerated(t *testing.T) {
	// A pattern containing a colon should split on the FIRST colon only so
	// patterns like `^foo:bar$` survive the round-trip.
	r := setupRegexSearchRouter(t)

	// Index a doc whose name contains a literal colon.
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	props := []index.Property{
		{APIName: "userId", BaseType: "string", IsSearchable: true},
		{APIName: "tag", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("tagged", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("tagged", "t1", map[string]interface{}{"userId": "t1", "tag": "ns:value"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.tagged",
		OntologyRID: testOntologyRID,
		APIName:     "tagged",
		PrimaryKey:  "userId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	svc := oss.NewService(repo, mgr, &mockLinkResolver{results: make(map[string][]string)})
	h := oss.NewHandler(svc)
	r = chi.NewRouter()
	h.RegisterRoutes(r)

	body := `{"select":["userId","tag"]}`
	url := "/api/v2/ontologies/" + testOntologyRID + "/objects/tagged/search?regex=tag:ns:.*"
	req := httptest.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Data) != 1 || resp.Data[0]["userId"] != "t1" {
		t.Fatalf("want [t1], got %v", resp.Data)
	}
}
