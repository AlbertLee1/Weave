package gdpr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_GDPR_RejectsAmbiguousJSONBody continues the P2A-30x
// ambiguous-JSON hardening series (rounds 1, 15-20) into pkg/gdpr
// — high-stakes right-to-erasure / data-portability surfaces. Two
// POST endpoints still decoded via
// `json.NewDecoder(r.Body).Decode(&req)`:
//
//   - POST /api/admin/gdpr/erase   (Erase) — right-to-be-forgotten
//   - POST /api/admin/gdpr/export  (Export) — data portability ZIP
//
// Smuggling vector on /erase is particularly severe: a body like
// `{"userId":"user:alice"}{"userId":"user:bob"}` triggers an
// asynchronous erasure of `user:alice`'s data while an audit
// pipeline re-parsing the raw bytes sees `user:bob`. When the
// erasure later destroys alice's records, the audit trail
// misattributes the action — a compliance dead-end.
//
// On /export the same vector lets an attacker download alice's
// data while the audit log says they exported bob's — a GDPR
// audit-failure scenario that would let one user impersonate
// another's data-portability request.
//
// Fix mirrors rounds 15-20: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason. The /export handler already had
// an `r.ContentLength != 0` empty-body guard that we preserve
// because the endpoint also accepts a `?userId=` query parameter
// fallback.
func TestBDD_GDPR_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("Erase rejects concatenated JSON without enqueueing an erasure job", func(t *testing.T) {
		jobStore := NewMemoryJobStore()
		h := newTestHandler(t, jobStore, simpleEraser(jobStore))

		body := `{"userId":"user:alice"}{"userId":"user:bob"}`
		rec := doJSON(h, http.MethodPost, "/api/admin/gdpr/erase", body, "user:admin")

		assertGDPRSingleJSONRejection(t, rec)

		// Non-mutation snapshot: NO job should have been enqueued.
		// MemoryJobStore exposes its map only inside the package,
		// which is fine for this test file (same package).
		jobStore.mu.Lock()
		n := len(jobStore.jobs)
		jobStore.mu.Unlock()
		if n != 0 {
			t.Errorf("ambiguous body must not enqueue any erasure job; got %d jobs", n)
		}
	})

	t.Run("Erase with well-formed body still enqueues (regression guard)", func(t *testing.T) {
		jobStore := NewMemoryJobStore()
		h := newTestHandler(t, jobStore, simpleEraser(jobStore))

		rec := doJSON(h, http.MethodPost, "/api/admin/gdpr/erase",
			`{"userId":"user:bob"}`, "user:admin")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("happy Erase: status=%d, want 202; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Export rejects concatenated JSON", func(t *testing.T) {
		jobStore := NewMemoryJobStore()
		h := newTestHandler(t, jobStore, simpleEraser(jobStore))
		h.SetExporter(NewExporter())

		body := `{"userId":"user:alice"}{"userId":"user:bob"}`
		rec := doJSON(h, http.MethodPost, "/api/admin/gdpr/export", body, "user:admin")

		assertGDPRSingleJSONRejection(t, rec)
	})

	t.Run("Export keeps the query-param fallback when no body supplied (regression guard)", func(t *testing.T) {
		// curl-style: no body, userId only in query string. Must
		// continue to work because round-21 only tightens the body
		// decode — the empty-body guard in the handler keeps the
		// query-param fallback alive. httputil.ReadJSON returns
		// io.EOF on an empty body which the existing
		// `r.ContentLength != 0` wrapper has always avoided calling
		// the decoder for.
		jobStore := NewMemoryJobStore()
		h := newTestHandler(t, jobStore, simpleEraser(jobStore))
		h.SetExporter(NewExporter())

		rec := doRequest(h, http.MethodPost, "/api/admin/gdpr/export?userId=user:bob", nil, "user:admin")
		if rec.Code != http.StatusOK {
			t.Fatalf("query-param Export: status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})
}

func assertGDPRSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
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
