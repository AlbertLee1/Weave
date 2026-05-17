package oss

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fakeIndexReadiness is a recording stub for the DOG-003 fail-fast guard
// on the stream ingest handler. ready=true mimics a bootstrapped index;
// ready=false mimics the dogfood scenario where the funnel consumer would
// silently drop every edit with "index not found".
type fakeIndexReadiness struct {
	ready bool
	calls int
}

func (f *fakeIndexReadiness) IndexReady(ontologyAPIName, objectType string) bool {
	f.calls++
	return f.ready
}

func newIngestRouterWithReadiness(pub IngestPublisher, checker IndexReadinessChecker) chi.Router {
	h := NewStreamIngestHandler(pub)
	h.SetIndexReadinessChecker(checker)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", h.ServeHTTP)
	return r
}

// TestBDD_StreamIngest_FailsFastWhenIndexMissing locks in DOG-003
// bdd_acceptance #3: when the target ObjectType has no Bleve index, the
// ingest endpoint must reject the batch with a non-2xx error rather than
// returning a fake editCount success that the funnel consumer would then
// silently drop.
func TestBDD_StreamIngest_FailsFastWhenIndexMissing(t *testing.T) {
	pub := &mockIngestPublisher{}
	checker := &fakeIndexReadiness{ready: false}
	r := newIngestRouterWithReadiness(pub, checker)

	body := `{"edits": [
		{"type":"CREATE","objectType":"AI_News","primaryKey":"news-1","properties":{"title":"x"}}
	]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ainews/streams/AI_News/ingest",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 IndexNotReady, got %d: %s", rr.Code, rr.Body.String())
	}
	if checker.calls != 1 {
		t.Fatalf("expected IndexReady probed once, got %d", checker.calls)
	}
	if len(pub.batches) != 0 {
		t.Fatalf("publisher must NOT receive a batch when the index is missing, got %d", len(pub.batches))
	}

	// Sanity check: the error body carries the IndexNotReady marker so
	// operators can distinguish this surface from generic publish failures.
	var body409 map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body409); err != nil {
		t.Fatalf("unmarshal 409 body: %v", err)
	}
	if body409["errorName"] != "IndexNotReady" {
		t.Errorf("expected errorName=IndexNotReady, got %v", body409["errorName"])
	}
}

// TestBDD_StreamIngest_PassesWhenIndexReady covers the green path: with a
// bootstrapped index the readiness probe is consulted, returns true, and
// the publisher receives the batch as before.
func TestBDD_StreamIngest_PassesWhenIndexReady(t *testing.T) {
	pub := &mockIngestPublisher{}
	checker := &fakeIndexReadiness{ready: true}
	r := newIngestRouterWithReadiness(pub, checker)

	body := `{"edits": [
		{"type":"CREATE","objectType":"AI_News","primaryKey":"news-1","properties":{"title":"x"}}
	]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ainews/streams/AI_News/ingest",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if checker.calls != 1 {
		t.Fatalf("expected IndexReady probed once, got %d", checker.calls)
	}
	if len(pub.batches) != 1 {
		t.Fatalf("expected 1 batch published, got %d", len(pub.batches))
	}
}
