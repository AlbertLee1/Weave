package aip

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_AIP_RejectsAmbiguousJSONBody extends the P2A-30x
// ambiguous-JSON hardening series (rounds 1, 15, 16, 17) into
// pkg/aip — the Foundry-aligned AIP chat / thread surface. Four
// POST endpoints decoded via `json.NewDecoder(r.Body).Decode(&req)`
// which accepts only the first JSON value and silently drops
// trailing bytes:
//
//   - POST /api/v2/aip/threads              (CreateThread)
//   - PUT  /api/v2/aip/threads/{id}         (UpdateThread)
//   - POST /api/v2/aip/threads/{id}/messages (SendMessage)
//   - POST /api/v2/aip/threads/{id}/fork    (ForkThread)
//
// AIP is the SDK surface the SPA / OSDK use to drive LLM
// conversations. Smuggling a trailing object that flips
// `temperature` or `model` past the audit log lets an attacker
// confuse downstream observability about which prompt actually
// landed.
//
// Fix mirrors rounds 15-17: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason.
func TestBDD_AIP_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("CreateThread rejects concatenated JSON", func(t *testing.T) {
		h, _ := newTestHandler()
		r := newRouter(h)

		body := `{"provider":"mock","title":"safe"}{"provider":"mock","title":"smuggled"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads", strings.NewReader(body))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertAIPSingleJSONRejection(t, w)
	})

	t.Run("CreateThread accepts well-formed body (regression guard)", func(t *testing.T) {
		h, _ := newTestHandler()
		r := newRouter(h)

		body := `{"provider":"mock","title":"happy"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads", strings.NewReader(body))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("happy CreateThread: status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("UpdateThread rejects concatenated JSON", func(t *testing.T) {
		h, _ := newTestHandler()
		r := newRouter(h)
		// Seed a thread first.
		seedReq := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads",
			strings.NewReader(`{"provider":"mock","title":"original"}`))
		seedReq = withAuthContext(seedReq, "user:alice")
		seedRec := httptest.NewRecorder()
		r.ServeHTTP(seedRec, seedReq)
		if seedRec.Code != http.StatusCreated {
			t.Fatalf("seed: status=%d body=%s", seedRec.Code, seedRec.Body.String())
		}
		var seeded Thread
		_ = json.Unmarshal(seedRec.Body.Bytes(), &seeded)

		// {"title":"renamed-safe"}{"model":"smuggled-model"}
		body := `{"title":"renamed-safe"}{"model":"smuggled-model"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v2/aip/threads/"+seeded.ID,
			strings.NewReader(body))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertAIPSingleJSONRejection(t, w)
	})

	t.Run("SendMessage rejects concatenated JSON", func(t *testing.T) {
		h, _ := newTestHandler()
		r := newRouter(h)
		// Seed a thread to receive the message.
		seedReq := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads",
			strings.NewReader(`{"provider":"mock","title":"chat"}`))
		seedReq = withAuthContext(seedReq, "user:alice")
		seedRec := httptest.NewRecorder()
		r.ServeHTTP(seedRec, seedReq)
		var seeded Thread
		_ = json.Unmarshal(seedRec.Body.Bytes(), &seeded)

		// {"content":"safe-prompt"}{"content":"smuggled-prompt","temperature":1.5}
		body := `{"content":"safe-prompt"}{"content":"smuggled-prompt","temperature":1.5}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/aip/threads/"+seeded.ID+"/messages",
			strings.NewReader(body))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertAIPSingleJSONRejection(t, w)
	})

	t.Run("ForkThread rejects concatenated JSON", func(t *testing.T) {
		h, _ := newTestHandler()
		r := newRouter(h)
		// Seed a thread + one message so we have a messageId to fork at.
		seedReq := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads",
			strings.NewReader(`{"provider":"mock","title":"origin"}`))
		seedReq = withAuthContext(seedReq, "user:alice")
		seedRec := httptest.NewRecorder()
		r.ServeHTTP(seedRec, seedReq)
		var seeded Thread
		_ = json.Unmarshal(seedRec.Body.Bytes(), &seeded)
		msgReq := httptest.NewRequest(http.MethodPost,
			"/api/v2/aip/threads/"+seeded.ID+"/messages",
			strings.NewReader(`{"content":"hi"}`))
		msgReq = withAuthContext(msgReq, "user:alice")
		msgRec := httptest.NewRecorder()
		r.ServeHTTP(msgRec, msgReq)
		// We don't need to capture the message id — the fork
		// endpoint will reject the smuggled body before checking it.

		// {"messageId":1}{"messageId":2,"title":"smuggled"}
		body := `{"messageId":1}{"messageId":2,"title":"smuggled"}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/aip/threads/"+seeded.ID+"/fork",
			strings.NewReader(body))
		req = withAuthContext(req, "user:alice")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertAIPSingleJSONRejection(t, w)
	})
}

func assertAIPSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
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
