package oss

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
)

// mockIngestPublisher records Publish calls so tests can assert on the
// batch shape without needing a real NATS connection.
type mockIngestPublisher struct {
	batches []*funnel.EditBatch
}

func (m *mockIngestPublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	m.batches = append(m.batches, batch)
	return uint64(len(m.batches)), nil
}

func newIngestRouter(pub IngestPublisher) chi.Router {
	h := NewStreamIngestHandler(pub)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", h.ServeHTTP)
	return r
}

// TestStreamIngestHappyPath is the US-061 red-first acceptance test.
// It verifies:
//   - POST with a valid batch of edits returns 200 with batchId + editCount
//   - Each edit is tagged source="ingest"
//   - The publisher receives one batch with the correct OntologyAPIName
func TestStreamIngestHappyPath(t *testing.T) {
	pub := &mockIngestPublisher{}
	r := newIngestRouter(pub)

	body := `{
		"edits": [
			{"type":"CREATE","objectType":"Order","primaryKey":"order-1","properties":{"total":100}},
			{"type":"CREATE","objectType":"Order","primaryKey":"order-2","properties":{"total":200}},
			{"type":"MODIFY","objectType":"Order","primaryKey":"order-3","properties":{"total":300}}
		]
	}`

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp StreamIngestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.BatchID == "" {
		t.Fatal("expected non-empty batchId")
	}
	if resp.EditCount != 3 {
		t.Fatalf("editCount = %d, want 3", resp.EditCount)
	}

	// Verify publisher received exactly one batch.
	if len(pub.batches) != 1 {
		t.Fatalf("publisher received %d batches, want 1", len(pub.batches))
	}
	batch := pub.batches[0]
	if batch.OntologyAPIName != "northwind" {
		t.Fatalf("OntologyAPIName = %q, want %q", batch.OntologyAPIName, "northwind")
	}
	if len(batch.Edits) != 3 {
		t.Fatalf("batch edits = %d, want 3", len(batch.Edits))
	}
	for i, e := range batch.Edits {
		if e.Source != funnel.EditSourceIngest {
			t.Errorf("edit[%d].Source = %q, want %q", i, e.Source, funnel.EditSourceIngest)
		}
	}
}

// TestStreamIngestEmptyEdits verifies that an empty edits array returns 400.
func TestStreamIngestEmptyEdits(t *testing.T) {
	pub := &mockIngestPublisher{}
	r := newIngestRouter(pub)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(`{"edits":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

// TestStreamIngestExceedsMaxEdits verifies that more than 1000 edits returns 400.
func TestStreamIngestExceedsMaxEdits(t *testing.T) {
	pub := &mockIngestPublisher{}
	r := newIngestRouter(pub)

	edits := make([]funnel.Edit, 1001)
	for i := range edits {
		edits[i] = funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Order", PrimaryKey: "pk"}
	}
	bodyBytes, _ := json.Marshal(map[string]interface{}{"edits": edits})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

// TestStreamIngestInvalidJSON verifies that malformed JSON returns 400.
func TestStreamIngestInvalidJSON(t *testing.T) {
	pub := &mockIngestPublisher{}
	r := newIngestRouter(pub)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

// mockIngestPolicyChecker is a test double for IngestPolicyChecker.
type mockIngestPolicyChecker struct {
	allowed bool
	err     error
}

func (m *mockIngestPolicyChecker) AllowedForIngest(_ context.Context, _, _ string) (bool, error) {
	return m.allowed, m.err
}

// newIngestRouterWithChecker creates a chi router with a StreamIngestHandler
// that has a policy checker wired in.
func newIngestRouterWithChecker(pub IngestPublisher, checker IngestPolicyChecker) chi.Router {
	h := NewStreamIngestHandler(pub)
	h.SetPolicyChecker(checker)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", h.ServeHTTP)
	return r
}

// TestStreamIngestPolicy verifies US-062: policy engine enforcement on ingest.
func TestStreamIngestPolicy(t *testing.T) {
	validBody := `{"edits":[{"type":"CREATE","objectType":"Order","primaryKey":"pk-1","properties":{"total":100}}]}`

	t.Run("denied by policy returns 403 IngestNotAllowed", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		checker := &mockIngestPolicyChecker{allowed: false}
		r := newIngestRouterWithChecker(pub, checker)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/streams/Order/ingest",
			strings.NewReader(validBody))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.WithUser(req.Context(), &auth.User{
			ID:    "user-1",
			Roles: []string{auth.RoleIngestWriter},
		}))

		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusForbidden, rr.Body.String())
		}
		// Verify error name is IngestNotAllowed
		var errResp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if name, _ := errResp["errorName"].(string); name != "IngestNotAllowed" {
			t.Fatalf("errorName = %q, want %q", name, "IngestNotAllowed")
		}
		// Publisher must NOT have been called
		if len(pub.batches) != 0 {
			t.Fatalf("publisher received %d batches, want 0", len(pub.batches))
		}
	})

	t.Run("allowed by policy returns 200", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		checker := &mockIngestPolicyChecker{allowed: true}
		r := newIngestRouterWithChecker(pub, checker)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/streams/Order/ingest",
			strings.NewReader(validBody))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.WithUser(req.Context(), &auth.User{
			ID:    "user-1",
			Roles: []string{auth.RoleIngestWriter},
		}))

		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
		}
		if len(pub.batches) != 1 {
			t.Fatalf("publisher received %d batches, want 1", len(pub.batches))
		}
	})

	t.Run("policy checker error returns 403", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		checker := &mockIngestPolicyChecker{allowed: false, err: errors.New("engine failure")}
		r := newIngestRouterWithChecker(pub, checker)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/streams/Order/ingest",
			strings.NewReader(validBody))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.WithUser(req.Context(), &auth.User{
			ID:    "user-1",
			Roles: []string{auth.RoleIngestWriter},
		}))

		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusForbidden, rr.Body.String())
		}
	})

	t.Run("nil checker allows ingest (backwards compat)", func(t *testing.T) {
		// When no policy checker is set, the handler should still work
		pub := &mockIngestPublisher{}
		r := newIngestRouter(pub)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/streams/Order/ingest",
			strings.NewReader(validBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
		}
	})
}

// TestStreamIngestRateLimiter verifies US-063: token bucket rate limiting
// per ontology with 429 + Retry-After on exhaustion.
func TestStreamIngestRateLimiter(t *testing.T) {
	validBody := `{"edits":[{"type":"CREATE","objectType":"Order","primaryKey":"pk-1","properties":{"total":100}}]}`

	t.Run("returns 429 with Retry-After when rate limit exhausted", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		h := NewStreamIngestHandler(pub)
		// Set a very low rate limit: 1 request per second, burst of 1
		h.SetRateLimiter(NewPerOntologyRateLimiter(1, 1))

		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", h.ServeHTTP)

		// First request should succeed (consumes the 1-token burst)
		req1 := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/streams/Order/ingest",
			strings.NewReader(validBody))
		req1.Header.Set("Content-Type", "application/json")
		rr1 := httptest.NewRecorder()
		r.ServeHTTP(rr1, req1)

		if rr1.Code != http.StatusOK {
			t.Fatalf("first request: status = %d, want %d; body = %s", rr1.Code, http.StatusOK, rr1.Body.String())
		}

		// Second request immediately should be rate-limited
		req2 := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/streams/Order/ingest",
			strings.NewReader(validBody))
		req2.Header.Set("Content-Type", "application/json")
		rr2 := httptest.NewRecorder()
		r.ServeHTTP(rr2, req2)

		if rr2.Code != http.StatusTooManyRequests {
			t.Fatalf("second request: status = %d, want %d; body = %s", rr2.Code, http.StatusTooManyRequests, rr2.Body.String())
		}

		// Must have Retry-After header
		retryAfter := rr2.Header().Get("Retry-After")
		if retryAfter == "" {
			t.Fatal("missing Retry-After header on 429 response")
		}

		// Must have RESOURCE_EXHAUSTED error code
		var errResp map[string]interface{}
		if err := json.Unmarshal(rr2.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if code, _ := errResp["errorCode"].(string); code != "RESOURCE_EXHAUSTED" {
			t.Fatalf("errorCode = %q, want %q", code, "RESOURCE_EXHAUSTED")
		}
		if name, _ := errResp["errorName"].(string); name != "IngestRateLimitExceeded" {
			t.Fatalf("errorName = %q, want %q", name, "IngestRateLimitExceeded")
		}

		// Publisher should only have received one batch (from the first request)
		if len(pub.batches) != 1 {
			t.Fatalf("publisher received %d batches, want 1", len(pub.batches))
		}
	})

	t.Run("per-ontology isolation: different ontologies have independent limits", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		h := NewStreamIngestHandler(pub)
		h.SetRateLimiter(NewPerOntologyRateLimiter(1, 1))

		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", h.ServeHTTP)

		// Exhaust ontology "alpha"
		req1 := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/alpha/streams/Order/ingest",
			strings.NewReader(validBody))
		req1.Header.Set("Content-Type", "application/json")
		rr1 := httptest.NewRecorder()
		r.ServeHTTP(rr1, req1)
		if rr1.Code != http.StatusOK {
			t.Fatalf("alpha first: status = %d, want %d", rr1.Code, http.StatusOK)
		}

		// ontology "beta" should still be allowed
		req2 := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/beta/streams/Order/ingest",
			strings.NewReader(validBody))
		req2.Header.Set("Content-Type", "application/json")
		rr2 := httptest.NewRecorder()
		r.ServeHTTP(rr2, req2)
		if rr2.Code != http.StatusOK {
			t.Fatalf("beta first: status = %d, want %d", rr2.Code, http.StatusOK)
		}

		// "alpha" should be rate-limited
		req3 := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/alpha/streams/Order/ingest",
			strings.NewReader(validBody))
		req3.Header.Set("Content-Type", "application/json")
		rr3 := httptest.NewRecorder()
		r.ServeHTTP(rr3, req3)
		if rr3.Code != http.StatusTooManyRequests {
			t.Fatalf("alpha second: status = %d, want %d", rr3.Code, http.StatusTooManyRequests)
		}
	})

	t.Run("nil rate limiter allows all requests (backwards compat)", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		h := NewStreamIngestHandler(pub)
		// No rate limiter set

		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", h.ServeHTTP)

		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/northwind/streams/Order/ingest",
				strings.NewReader(validBody))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("request %d: status = %d, want %d", i, rr.Code, http.StatusOK)
			}
		}
		if len(pub.batches) != 5 {
			t.Fatalf("publisher received %d batches, want 5", len(pub.batches))
		}
	})
}

// TestStreamIngestExact1000 verifies that exactly 1000 edits is accepted.
func TestStreamIngestExact1000(t *testing.T) {
	pub := &mockIngestPublisher{}
	r := newIngestRouter(pub)

	edits := make([]funnel.Edit, 1000)
	for i := range edits {
		edits[i] = funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Order", PrimaryKey: "pk"}
	}
	bodyBytes, _ := json.Marshal(map[string]interface{}{"edits": edits})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if len(pub.batches) != 1 {
		t.Fatalf("publisher received %d batches, want 1", len(pub.batches))
	}
	if len(pub.batches[0].Edits) != 1000 {
		t.Fatalf("batch edits = %d, want 1000", len(pub.batches[0].Edits))
	}
}
