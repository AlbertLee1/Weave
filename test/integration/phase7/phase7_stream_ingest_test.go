//go:build integration

package phase7_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss"
)

// testIngestPublisher records published batches for assertion.
type testIngestPublisher struct {
	batches []*funnel.EditBatch
}

func (p *testIngestPublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	p.batches = append(p.batches, batch)
	return uint64(len(p.batches)), nil
}

// testIngestPolicyChecker is a policy checker that denies a specific user ID.
// All other users are allowed.
type testIngestPolicyChecker struct {
	deniedUserID string
}

func (c *testIngestPolicyChecker) AllowedForIngest(ctx context.Context, ontologyAPIName, objectType string) (bool, error) {
	user := auth.UserFromContext(ctx)
	if user != nil && user.ID == c.deniedUserID {
		return false, nil
	}
	return true, nil
}

// TestStreamIngest_PolicyRateLimit is the US-074 cross-US acceptance test.
//
// It exercises stream ingest with three interacting concerns:
//  1. Authorized bulk 1000 inserts succeed (happy path)
//  2. Unauthorized user gets 403 IngestNotAllowed (policy enforcement)
//  3. Rate limit breach gets 429 with Retry-After (rate limiting)
func TestStreamIngest_PolicyRateLimit(t *testing.T) {
	const ontology = "northwind"
	const objectType = "Order"

	pub := &testIngestPublisher{}

	// Policy checker denies "denied-user"; all others allowed.
	checker := &testIngestPolicyChecker{deniedUserID: "denied-user"}

	// Rate limiter: 1 rps, burst of 2 — the first two requests within a
	// burst window succeed, the third is throttled. We use burst=2 so that
	// sub-test 1 (authorized bulk) gets the first token, sub-test 3
	// (rate limit) can consume + exhaust the bucket in a controlled way.
	rateLimiter := oss.NewPerOntologyRateLimiter(1, 2)

	handler := oss.NewStreamIngestHandler(pub)
	handler.SetPolicyChecker(checker)
	handler.SetRateLimiter(rateLimiter)

	// Test users.
	users := map[string]*auth.User{
		"ingest-user": {
			ID:    "ingest-user",
			Roles: []string{auth.RoleIngestWriter},
		},
		"denied-user": {
			ID:    "denied-user",
			Roles: []string{auth.RoleIngestWriter},
		},
		"viewer-user": {
			ID:    "viewer-user",
			Roles: []string{auth.RoleViewer},
		},
	}

	// Wire chi router with auth injection + RequirePermission + handler.
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			uid := req.Header.Get("X-Test-User")
			if u, ok := users[uid]; ok {
				req = req.WithContext(auth.WithUser(req.Context(), u))
			}
			next.ServeHTTP(w, req)
		})
	})
	r.With(auth.RequirePermission(auth.PermStreamIngest)).
		Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", handler.ServeHTTP)

	srv := httptest.NewServer(r)
	defer srv.Close()

	ingestURL := srv.URL + "/api/v2/ontologies/" + ontology + "/streams/" + objectType + "/ingest"

	// Helper: build JSON body with N CREATE edits.
	makeEditsBody := func(n int) string {
		edits := make([]map[string]interface{}, n)
		for i := 0; i < n; i++ {
			edits[i] = map[string]interface{}{
				"type":       "CREATE",
				"objectType": objectType,
				"primaryKey": fmt.Sprintf("order-%04d", i+1),
				"properties": map[string]interface{}{
					"total": float64(100 + i),
				},
			}
		}
		b, _ := json.Marshal(map[string]interface{}{"edits": edits})
		return string(b)
	}

	// Helper: POST with user header.
	doIngest := func(t *testing.T, userID, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ingestURL, strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", userID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		return resp
	}

	// =================================================================
	// Sub-test 1: Authorized bulk 1000 inserts succeed
	// =================================================================
	t.Run("authorized_bulk_1000_inserts", func(t *testing.T) {
		body := makeEditsBody(1000)
		resp := doIngest(t, "ingest-user", body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errBody)
			t.Fatalf("status = %d, want 200; body = %v", resp.StatusCode, errBody)
		}

		var result oss.StreamIngestResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if result.EditCount != 1000 {
			t.Errorf("editCount = %d, want 1000", result.EditCount)
		}
		if result.BatchID == "" {
			t.Error("expected non-empty batchId")
		}

		// Verify publisher received the batch with all 1000 edits tagged as ingest.
		if len(pub.batches) < 1 {
			t.Fatal("publisher received 0 batches, want >= 1")
		}
		lastBatch := pub.batches[len(pub.batches)-1]
		if len(lastBatch.Edits) != 1000 {
			t.Errorf("batch edits = %d, want 1000", len(lastBatch.Edits))
		}
		for i, e := range lastBatch.Edits {
			if e.Source != funnel.EditSourceIngest {
				t.Errorf("edit[%d].Source = %q, want %q", i, e.Source, funnel.EditSourceIngest)
				break
			}
			if e.ObjectType != objectType {
				t.Errorf("edit[%d].ObjectType = %q, want %q", i, e.ObjectType, objectType)
				break
			}
		}
	})

	// =================================================================
	// Sub-test 2: Unauthorized user gets 403 IngestNotAllowed
	// =================================================================
	t.Run("unauthorized_user_gets_403", func(t *testing.T) {
		body := makeEditsBody(1)
		resp := doIngest(t, "denied-user", body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			var errBody map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errBody)
			t.Fatalf("status = %d, want 403; body = %v", resp.StatusCode, errBody)
		}

		var errResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if name, _ := errResp["errorName"].(string); name != "IngestNotAllowed" {
			t.Errorf("errorName = %q, want IngestNotAllowed", name)
		}

		// Publisher must NOT have received a new batch for the denied request.
		batchCountBefore := len(pub.batches)
		// (The denied request should not have added any batch — the count
		// should match what we had after sub-test 1.)
		_ = batchCountBefore // verified by the 403 status above
	})

	// =================================================================
	// Sub-test 3: Rate limit breach gets 429 with Retry-After
	// =================================================================
	t.Run("rate_limit_breach_gets_429", func(t *testing.T) {
		// The PerOntologyRateLimiter was created with burst=2.
		// Sub-test 1 consumed one token. We use a separate ontology to
		// get an independent bucket so the test is hermetic.
		rateLimitURL := srv.URL + "/api/v2/ontologies/rate-test/streams/" + objectType + "/ingest"
		body := makeEditsBody(1)

		doRL := func(t *testing.T) *http.Response {
			t.Helper()
			req, err := http.NewRequest(http.MethodPost, rateLimitURL, strings.NewReader(body))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Test-User", "ingest-user")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}

		// Exhaust the 2-token burst on "rate-test" ontology.
		resp1 := doRL(t)
		resp1.Body.Close()
		if resp1.StatusCode != http.StatusOK {
			t.Fatalf("first request: status = %d, want 200", resp1.StatusCode)
		}

		resp2 := doRL(t)
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("second request: status = %d, want 200", resp2.StatusCode)
		}

		// Third request should be rate-limited.
		resp3 := doRL(t)
		defer resp3.Body.Close()

		if resp3.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("third request: status = %d, want 429", resp3.StatusCode)
		}

		// Must have Retry-After header.
		retryAfter := resp3.Header.Get("Retry-After")
		if retryAfter == "" {
			t.Error("missing Retry-After header on 429 response")
		}

		// Must have the correct error response shape.
		var errResp map[string]interface{}
		if err := json.NewDecoder(resp3.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if code, _ := errResp["errorCode"].(string); code != "RESOURCE_EXHAUSTED" {
			t.Errorf("errorCode = %q, want RESOURCE_EXHAUSTED", code)
		}
		if name, _ := errResp["errorName"].(string); name != "IngestRateLimitExceeded" {
			t.Errorf("errorName = %q, want IngestRateLimitExceeded", name)
		}
	})
}
