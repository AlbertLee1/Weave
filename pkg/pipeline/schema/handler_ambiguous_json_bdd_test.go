package schema

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_InferSchema_RejectsAmbiguousJSONBody is the round-22
// sibling to the pkg/pipeline ambiguous-JSON rejection BDD. The
// InferSchema handler uses an explicit 8 MiB MaxInferenceRequestBytes
// cap (httputil.ReadJSON's 1 MiB default would reject legitimate
// inline samples), so we couldn't simply swap to httputil.ReadJSON.
// Instead the handler keeps the per-call cap AND adds an inline
// `dec.Decode(&extra) != io.EOF` check that mirrors httputil's
// "single JSON value" rejection.
//
// Smuggling vector: a body like
// `{"format":"csv","sample":"safe,1\n"}{"format":"json","sample":"smuggled"}`
// would let the handler infer a CSV schema while audit pipelines
// re-parsing the raw bytes see a json-format request — schema
// audit drift.
func TestBDD_InferSchema_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("InferSchema rejects concatenated JSON", func(t *testing.T) {
		body := `{"format":"csv","sample":"id,name\n1,alice\n"}{"format":"json","sample":"[]"}`
		req := withAuth(httptest.NewRequest(http.MethodPost,
			"/api/v2/pipelines/schema/infer", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		newRouter().ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
		}
		var env struct {
			ErrorName  string            `json:"errorName"`
			Parameters map[string]string `json:"parameters"`
		}
		_ = json.NewDecoder(w.Body).Decode(&env)
		if env.ErrorName != "InvalidRequestBody" {
			t.Errorf("errorName: got %q, want InvalidRequestBody", env.ErrorName)
		}
		if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
			t.Errorf("reason should mention 'single JSON value', got %q", env.Parameters["reason"])
		}
	})

	t.Run("InferSchema with well-formed body still infers (regression guard)", func(t *testing.T) {
		body, _ := json.Marshal(InferRequest{
			Format: "csv",
			Sample: "id,name\n1,alice\n2,bob\n",
		})
		req := withAuth(httptest.NewRequest(http.MethodPost,
			"/api/v2/pipelines/schema/infer", strings.NewReader(string(body))))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		newRouter().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("happy infer: status=%d body=%s", w.Code, w.Body.String())
		}
	})
}
