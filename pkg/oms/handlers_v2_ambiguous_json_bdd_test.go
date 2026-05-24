package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_OMS_V2_RejectsAmbiguousJSONBody extends the P2A-30x
// ambiguous-JSON hardening series (rounds 1, 15) into pkg/oms.
// Three POST endpoints — GetObjectTypesByRidBatchV2,
// GetActionTypesByRidBatchV2, LoadMetadataV2 — used
// `json.NewDecoder(r.Body).Decode(&req)` which accepts only the
// first JSON value and silently drops trailing bytes. All three are
// read-only batch lookups, but the smuggling vector still exists:
// an audit pipeline / WAF re-parsing the raw bytes sees a different
// RID list (or metadata subset) than what the handler actually
// looked up, breaking observability of what was queried.
//
// Fix mirrors round 15: swap to httputil.ReadJSON which enforces
// dec.Decode(&extra) == io.EOF and returns 400 InvalidRequestBody
// with a "single JSON value" reason.
func TestBDD_OMS_V2_RejectsAmbiguousJSONBody(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"

	t.Run("GetObjectTypesByRidBatchV2 rejects concatenated JSON", func(t *testing.T) {
		repo := &mockRepo{
			ontologies:  []oms.Ontology{{RID: ontRID, APIName: "test"}},
			objectTypes: []oms.ObjectType{{RID: "ri.ontology.main.object-type.ot1", OntologyRID: ontRID, APIName: "Employee", Status: "ACTIVE"}},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch", handler.GetObjectTypesByRidBatchV2)

		// {"rids":["safe"]}{"rids":["smuggled"]} — first decodes to ["safe"];
		// a re-parsing audit pipeline sees a lookup of "smuggled" too.
		body := `{"rids":["safe"]}{"rids":["smuggled"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/test/objectTypes/getByRidBatch",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertOMSSingleJSONValueRejection(t, w)
	})

	t.Run("GetActionTypesByRidBatchV2 rejects concatenated JSON", func(t *testing.T) {
		repo := &mockRepo{
			ontologies:  []oms.Ontology{{RID: ontRID, APIName: "test"}},
			actionTypes: []oms.ActionType{{RID: "ri.ontology.main.action-type.a1", OntologyRID: ontRID, APIName: "createX", Status: "ACTIVE"}},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch", handler.GetActionTypesByRidBatchV2)

		body := `{"rids":["safe"]}{"rids":["smuggled"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/test/actionTypes/getByRidBatch",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertOMSSingleJSONValueRejection(t, w)
	})

	t.Run("LoadMetadataV2 rejects concatenated JSON", func(t *testing.T) {
		repo := &mockRepo{
			ontologies: []oms.Ontology{{RID: ontRID, APIName: "test"}},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

		// {"objectTypes":{}}{"linkTypes":{}} — first decode pulls only
		// objectTypes; audit pipeline sees a request for both.
		body := `{"objectTypes":{}}{"linkTypes":{}}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/test/metadata",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertOMSSingleJSONValueRejection(t, w)
	})

	t.Run("All three endpoints still accept well-formed bodies (regression guard)", func(t *testing.T) {
		repo := &mockRepo{
			ontologies:  []oms.Ontology{{RID: ontRID, APIName: "test"}},
			objectTypes: []oms.ObjectType{{RID: "ri.ontology.main.object-type.ot1", OntologyRID: ontRID, APIName: "Employee", Status: "ACTIVE"}},
			actionTypes: []oms.ActionType{{RID: "ri.ontology.main.action-type.a1", OntologyRID: ontRID, APIName: "createX", Status: "ACTIVE"}},
		}
		handler := oms.NewOMSHandler(repo)

		// objectTypes/getByRidBatch
		{
			r := chi.NewRouter()
			r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch", handler.GetObjectTypesByRidBatchV2)
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/test/objectTypes/getByRidBatch",
				strings.NewReader(`{"rids":["ri.ontology.main.object-type.ot1"]}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("objectTypes happy: status=%d, want 200; body=%s", w.Code, w.Body.String())
			}
		}
		// actionTypes/getByRidBatch
		{
			r := chi.NewRouter()
			r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch", handler.GetActionTypesByRidBatchV2)
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/test/actionTypes/getByRidBatch",
				strings.NewReader(`{"rids":["ri.ontology.main.action-type.a1"]}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("actionTypes happy: status=%d, want 200; body=%s", w.Code, w.Body.String())
			}
		}
		// LoadMetadataV2
		{
			r := chi.NewRouter()
			r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/test/metadata",
				strings.NewReader(`{"objectTypes":{}}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("LoadMetadataV2 happy: status=%d, want 200; body=%s", w.Code, w.Body.String())
			}
		}
	})
}

func assertOMSSingleJSONValueRejection(t *testing.T, rec *httptest.ResponseRecorder) {
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
