//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss"
)

// countingIngestPublisher records how many batches were published.
type countingIngestPublisher struct {
	count int
}

func (p *countingIngestPublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	p.count++
	return uint64(p.count), nil
}

// TestStreamIngest_2000Inserts_And_RateLimited is the US-063 integration
// acceptance test. It verifies:
//  1. A burst of 2000 CREATE edits (split into 2 batches of 1000) can be
//     ingested when the rate limiter has sufficient capacity.
//  2. When the token bucket is exhausted, subsequent requests receive
//     429 RESOURCE_EXHAUSTED with a Retry-After header.
//  3. Per-ontology isolation: rate-limiting one ontology does not affect another.
func TestStreamIngest_2000Inserts_And_RateLimited(t *testing.T) {
	pub := &countingIngestPublisher{}
	handler := oss.NewStreamIngestHandler(pub)
	// Rate limit: 2 requests per second with burst of 2 — tight enough that
	// the third immediate request is blocked, but enough to accept 2 batches.
	handler.SetRateLimiter(oss.NewPerOntologyRateLimiter(2, 2))

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", handler.ServeHTTP)

	// Helper to build a batch of n CREATE edits.
	buildBody := func(n int) string {
		edits := make([]map[string]interface{}, n)
		for i := 0; i < n; i++ {
			edits[i] = map[string]interface{}{
				"type":       "CREATE",
				"objectType": "Order",
				"primaryKey": fmt.Sprintf("order-%04d", i),
				"properties": map[string]interface{}{"total": i * 10},
			}
		}
		b, _ := json.Marshal(map[string]interface{}{"edits": edits})
		return string(b)
	}

	// ---------------------------------------------------------------
	// Phase 1: push 2000 edits as two batches of 1000 (within burst).
	// ---------------------------------------------------------------
	batch1000 := buildBody(1000)

	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(batch1000))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("batch 1: status = %d, want %d; body = %s", rr1.Code, http.StatusOK, rr1.Body.String())
	}
	var resp1 oss.StreamIngestResponse
	if err := json.Unmarshal(rr1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("batch 1: unmarshal: %v", err)
	}
	if resp1.EditCount != 1000 {
		t.Fatalf("batch 1: editCount = %d, want 1000", resp1.EditCount)
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(batch1000))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("batch 2: status = %d, want %d; body = %s", rr2.Code, http.StatusOK, rr2.Body.String())
	}
	var resp2 oss.StreamIngestResponse
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("batch 2: unmarshal: %v", err)
	}
	if resp2.EditCount != 1000 {
		t.Fatalf("batch 2: editCount = %d, want 1000", resp2.EditCount)
	}

	if pub.count != 2 {
		t.Fatalf("publisher received %d batches, want 2", pub.count)
	}

	// ---------------------------------------------------------------
	// Phase 2: burst exhausted — third immediate request → 429.
	// ---------------------------------------------------------------
	smallBody := `{"edits":[{"type":"CREATE","objectType":"Order","primaryKey":"extra-1","properties":{"total":1}}]}`

	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/streams/Order/ingest",
		strings.NewReader(smallBody))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited request: status = %d, want %d; body = %s",
			rr3.Code, http.StatusTooManyRequests, rr3.Body.String())
	}

	retryAfter := rr3.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("rate-limited response missing Retry-After header")
	}

	var errResp map[string]interface{}
	if err := json.Unmarshal(rr3.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if code, _ := errResp["errorCode"].(string); code != "RESOURCE_EXHAUSTED" {
		t.Fatalf("errorCode = %q, want %q", code, "RESOURCE_EXHAUSTED")
	}
	if name, _ := errResp["errorName"].(string); name != "IngestRateLimitExceeded" {
		t.Fatalf("errorName = %q, want %q", name, "IngestRateLimitExceeded")
	}

	// Publisher must NOT have received a third batch.
	if pub.count != 2 {
		t.Fatalf("publisher received %d batches after rate-limit, want 2", pub.count)
	}

	// ---------------------------------------------------------------
	// Phase 3: different ontology is independent — "chinook" should
	// still have its own full burst.
	// ---------------------------------------------------------------
	rr4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/chinook/streams/Track/ingest",
		strings.NewReader(smallBody))
	req4.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rr4, req4)

	if rr4.Code != http.StatusOK {
		t.Fatalf("chinook request: status = %d, want %d; body = %s",
			rr4.Code, http.StatusOK, rr4.Body.String())
	}
	if pub.count != 3 {
		t.Fatalf("publisher received %d batches, want 3 (2 northwind + 1 chinook)", pub.count)
	}
}
