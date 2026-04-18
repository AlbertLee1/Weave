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

// setupFuzzySearchRouter wires an OSS handler in front of a real Bleve index
// so search?fuzziness=... can be tested end-to-end.
func setupFuzzySearchRouter(t *testing.T) (chi.Router, *index.Manager) {
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
		"u1": {"userId": "u1", "name": "kafka"},
		"u2": {"userId": "u2", "name": "spark"},
		"u3": {"userId": "u3", "name": "flink"},
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
	return r, mgr
}

// searchURL builds the /search URL with the given ontology+type and optional
// raw query string (no leading ?).
func searchURL(rawQuery string) string {
	u := "/api/v2/ontologies/" + testOntologyRID + "/objects/user/search"
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return u
}

func TestSearchObjects_FuzzinessQueryParam_One(t *testing.T) {
	r, _ := setupFuzzySearchRouter(t)

	body := `{"where":{"type":"eq","field":"name","value":"Kafca"},"select":["userId","name"]}`
	req := httptest.NewRequest("POST", searchURL("fuzziness=1"), strings.NewReader(body)).
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
	if len(resp.Data) != 1 {
		t.Fatalf("want 1 row, got %d (%v)", len(resp.Data), resp.Data)
	}
	if resp.Data[0]["userId"] != "u1" {
		t.Fatalf("want u1, got %v", resp.Data[0]["userId"])
	}
}

func TestSearchObjects_FuzzinessQueryParam_Zero_DisablesFuzzy(t *testing.T) {
	// fuzziness=0 MUST disable fuzzy matching even if the body says otherwise.
	r, _ := setupFuzzySearchRouter(t)

	body := `{"where":{"type":"eq","field":"name","value":"Kafca"},"fuzzy":{"maxEdits":1},"select":["userId"]}`
	req := httptest.NewRequest("POST", searchURL("fuzziness=0"), strings.NewReader(body))
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
	if len(resp.Data) != 0 {
		t.Fatalf("fuzziness=0 must disable fuzzy; got %d rows", len(resp.Data))
	}
}

func TestSearchObjects_FuzzinessQueryParam_Two(t *testing.T) {
	r, _ := setupFuzzySearchRouter(t)

	// "kaffca" → "kafka" is two edits; maxEdits=2 should match.
	body := `{"where":{"type":"fuzzy","field":"name","value":"kaffca"},"select":["userId"]}`
	req := httptest.NewRequest("POST", searchURL("fuzziness=2"), strings.NewReader(body))
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
	if len(resp.Data) != 1 || resp.Data[0]["userId"] != "u1" {
		t.Fatalf("want [u1], got %v", resp.Data)
	}
}

func TestSearchObjects_FuzzinessQueryParam_OutOfRange(t *testing.T) {
	r, _ := setupFuzzySearchRouter(t)

	body := `{"where":{"type":"eq","field":"name","value":"Kafca"},"select":["userId"]}`
	cases := []string{"3", "-1", "9999", "abc"}
	for _, raw := range cases {
		t.Run("fuzziness="+raw, func(t *testing.T) {
			req := httptest.NewRequest("POST", searchURL("fuzziness="+raw), strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
			}
			var apiErr struct {
				ErrorName string            `json:"errorName"`
				Params    map[string]string `json:"parameters"`
			}
			_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
			if apiErr.ErrorName != "InvalidFuzziness" {
				t.Fatalf("errorName = %q, want InvalidFuzziness (body=%s)", apiErr.ErrorName, rr.Body.String())
			}
		})
	}
}

func TestSearchObjects_FuzzyBodyField_OutOfRange(t *testing.T) {
	// fuzzy.maxEdits in the body is also bounded.
	r, _ := setupFuzzySearchRouter(t)

	body := `{"where":{"type":"eq","field":"name","value":"Kafca"},"fuzzy":{"maxEdits":5},"select":["userId"]}`
	req := httptest.NewRequest("POST", searchURL(""), strings.NewReader(body))
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
	if apiErr.ErrorName != "InvalidFuzziness" {
		t.Fatalf("errorName = %q, want InvalidFuzziness", apiErr.ErrorName)
	}
}

func TestSearchObjects_FuzzinessQueryParam_OverridesBody(t *testing.T) {
	// Body says maxEdits=2 but query-string says fuzziness=1; "kaffca" → "kafka"
	// is 2 edits so the query-string override must cause 0 hits.
	r, _ := setupFuzzySearchRouter(t)

	body := `{"where":{"type":"fuzzy","field":"name","value":"kaffca"},"fuzzy":{"maxEdits":2},"select":["userId"]}`
	req := httptest.NewRequest("POST", searchURL("fuzziness=1"), strings.NewReader(body))
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
	if len(resp.Data) != 0 {
		t.Fatalf("query-string override should win; got %d rows", len(resp.Data))
	}
}
