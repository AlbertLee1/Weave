package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_Pipeline_RejectsAmbiguousJSONBody continues the P2A-30x
// ambiguous-JSON hardening series (rounds 1, 15-21) into
// pkg/pipeline. Two POST endpoints still decoded via
// `json.NewDecoder(r.Body).Decode(&req)`:
//
//   - POST /api/v2/pipelines       (CreatePipeline)
//   - PUT  /api/v2/pipelines/{id}  (UpdatePipeline)
//
// Smuggling vector: a body like
// `{"name":"safe","enabled":false}{"name":"smuggled","enabled":true}`
// creates a disabled pipeline while audit pipelines re-parsing the
// raw bytes see a different (enabled) configuration than what
// actually landed in storage. For ETL pipelines that move customer
// data, this is a real audit-failure scenario.
//
// Fix mirrors rounds 15-21: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason. httputil is already imported.
func TestBDD_Pipeline_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("CreatePipeline rejects concatenated JSON without persisting any pipeline", func(t *testing.T) {
		h, store := newHandlerWithStore()
		r := newRouter(h)

		// {"name":"safe",...,"enabled":false}{"name":"smuggled","enabled":true}
		body := `{"name":"safe","inputs":[{"name":"a","type":"objectset","config":{"objectType":"X"}}],"outputs":[{"name":"o","type":"jdbc","input":"a","config":{"table":"t"}}],"enabled":false}{"name":"smuggled","enabled":true}`
		req := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines", strings.NewReader(body))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertPipelineSingleJSONRejection(t, w)

		// Non-mutation snapshot: no pipeline persisted.
		got, _ := store.ListPipelines(context.Background(), "user:alice")
		if len(got) != 0 {
			t.Errorf("ambiguous body must not persist any pipeline; got %d", len(got))
		}
	})

	t.Run("CreatePipeline with well-formed body still succeeds (regression guard)", func(t *testing.T) {
		h, _ := newHandlerWithStore()
		r := newRouter(h)
		body := samplePipelineBody(t, "")
		req := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines", strings.NewReader(body))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("happy Create: status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("UpdatePipeline rejects concatenated JSON without persisting the change", func(t *testing.T) {
		h, _ := newHandlerWithStore()
		r := newRouter(h)

		// Seed a pipeline so we have an id.
		seedBody := samplePipelineBody(t, "")
		seedReq := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines", strings.NewReader(seedBody))
		seedReq = withAuthContext(seedReq, "user:alice")
		seedReq.Header.Set("Content-Type", "application/json")
		seedRec := httptest.NewRecorder()
		r.ServeHTTP(seedRec, seedReq)
		if seedRec.Code != http.StatusCreated {
			t.Fatalf("seed: status=%d body=%s", seedRec.Code, seedRec.Body.String())
		}
		var seeded Pipeline
		_ = json.Unmarshal(seedRec.Body.Bytes(), &seeded)
		originalName := seeded.Name

		// PUT with concatenated body — first decode renames; trailing
		// would smuggle an enabled flip.
		body := `{"name":"Renamed-Safe"}{"enabled":true}`
		req := httptest.NewRequest(http.MethodPut, "/api/v2/pipelines/"+seeded.ID, bytes.NewReader([]byte(body)))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertPipelineSingleJSONRejection(t, w)

		// Non-mutation snapshot: name stayed at original.
		after, err := h.store.GetPipeline(context.Background(), seeded.ID)
		if err != nil {
			t.Fatalf("GetPipeline after rejected PUT: %v", err)
		}
		if after.Name != originalName {
			t.Errorf("ambiguous PUT mutated name: got %q want %q", after.Name, originalName)
		}
	})
}

func assertPipelineSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorName != "InvalidRequestBody" {
		t.Errorf("errorName: got %q, want InvalidRequestBody", env.ErrorName)
	}
	if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
		t.Errorf("reason should mention 'single JSON value', got %q", env.Parameters["reason"])
	}
}
