package oms_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-425: publishing or updating a Function flushes every cached result
// keyed on its RID so the next call cannot serve a pre-publish answer.
// Deleting the Function does the same.

func setupFnPublishRouter(repo *mockRepo, c oms.FunctionResultCache) *chi.Mux {
	handler := oms.NewOMSHandler(repo)
	if c != nil {
		handler.SetFunctionResultCache(c)
	}
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions", handler.CreateFunction)
	r.Put("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.UpdateFunction)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.DeleteFunction)
	return r
}

func TestUpdateFunction_FlushesCachedResults(t *testing.T) {
	repo := newPureFixtureRepo(true)
	cache := newRecordingCache()
	router := setupFnPublishRouter(repo, cache)

	fnRID := "ri.ontology.main.function.add"
	cache.Put(fnRID+"@1.0.0#hashA", "v1")
	cache.Put(fnRID+"@1.0.0#hashB", "v2")
	cache.Put("ri.ontology.main.function.other@1.0.0#hashC", "other")

	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/functions/"+fnRID, bytes.NewBufferString(`{"sourceCode":"function main(){return 99}"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	keys := cache.keys()
	if len(keys) != 1 {
		t.Fatalf("expected only the unrelated `other` entry to survive, got %d keys: %v", len(keys), keys)
	}
	if !strings.HasPrefix(keys[0], "ri.ontology.main.function.other@") {
		t.Errorf("expected unrelated entry to survive, got %q", keys[0])
	}
}

func TestDeleteFunction_FlushesCachedResults(t *testing.T) {
	repo := newPureFixtureRepo(true)
	cache := newRecordingCache()
	router := setupFnPublishRouter(repo, cache)

	fnRID := "ri.ontology.main.function.add"
	cache.Put(fnRID+"@1.0.0#hashA", "v1")
	cache.Put("ri.ontology.main.function.other@1.0.0#hashB", "other")

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/northwind/functions/"+fnRID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	keys := cache.keys()
	if len(keys) != 1 || !strings.HasPrefix(keys[0], "ri.ontology.main.function.other@") {
		t.Errorf("expected only unrelated entry to survive, got %v", keys)
	}
}

func TestCreateFunction_AcceptsDependsOn(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:     "ri.ontology.main.ontology.o1",
			APIName: "northwind",
		}},
	}
	router := setupFnPublishRouter(repo, nil)

	body := `{"name":"countOrders","sourceCode":"function main(){return 1}","pure":true,"dependsOn":["Order","Customer"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.functions) != 1 {
		t.Fatalf("expected 1 function persisted, got %d", len(repo.functions))
	}
	got := repo.functions[0].DependsOn
	if len(got) != 2 || got[0] != "Order" || got[1] != "Customer" {
		t.Errorf("DependsOn round-trip mismatch: %v", got)
	}
	if !strings.Contains(w.Body.String(), `"dependsOn":["Order","Customer"]`) {
		t.Errorf("expected response body to echo dependsOn, got: %s", w.Body.String())
	}
}

func TestUpdateFunction_PreservesDependsOnWhenOmitted(t *testing.T) {
	repo := newPureFixtureRepo(true)
	repo.functions[0].DependsOn = []string{"Order"}
	router := setupFnPublishRouter(repo, nil)

	// Update only sourceCode — DependsOn must not be cleared.
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/functions/"+repo.functions[0].RID, bytes.NewBufferString(`{"sourceCode":"function main(){return 1}"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.functions[0].DependsOn) != 1 || repo.functions[0].DependsOn[0] != "Order" {
		t.Errorf("DependsOn should be preserved when omitted, got %v", repo.functions[0].DependsOn)
	}
}

func TestUpdateFunction_DependsOnExplicitlyClearedWhenSentEmpty(t *testing.T) {
	repo := newPureFixtureRepo(true)
	repo.functions[0].DependsOn = []string{"Order"}
	router := setupFnPublishRouter(repo, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/functions/"+repo.functions[0].RID, bytes.NewBufferString(`{"dependsOn":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.functions[0].DependsOn) != 0 {
		t.Errorf("DependsOn should be cleared when [] sent explicitly, got %v", repo.functions[0].DependsOn)
	}
}
