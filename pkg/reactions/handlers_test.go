package reactions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newTestRouter(store Store, user *auth.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			if user != nil {
				ctx = auth.WithUser(ctx, user)
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	NewHandler(store).RegisterRoutes(r)
	return r
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestHandler_CreateAggregateDelete(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, alice)

	target := "ri.weave.main.object.42"

	// CREATE 👍
	body := mustEncode(t, map[string]any{"targetRid": target, "emoji": "👍"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/reactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created Reaction
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.UserID != alice.ID || created.TargetRID != target || created.Emoji != "👍" || created.ID == "" {
		t.Fatalf("create returned wrong shape: %+v", created)
	}

	// Idempotent re-CREATE
	req = httptest.NewRequest(http.MethodPost, "/api/v2/reactions", bytes.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("re-POST: want 201, got %d", w.Code)
	}
	var second Reaction
	_ = json.Unmarshal(w.Body.Bytes(), &second)
	if second.ID != created.ID {
		t.Fatalf("idempotent re-POST: id changed (%s vs %s)", second.ID, created.ID)
	}

	// AGGREGATE — alice sees her 👍 with mine=true
	q := url.Values{"targetRid": []string{target}}
	req = httptest.NewRequest(http.MethodGet, "/api/v2/reactions?"+q.Encode(), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var summary Summary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.TargetRID != target || len(summary.Emojis) != 1 {
		t.Fatalf("summary unexpected: %+v", summary)
	}
	if summary.Emojis[0] != (EmojiCount{Emoji: "👍", Count: 1, Mine: true}) {
		t.Fatalf("summary bucket: %+v", summary.Emojis[0])
	}

	// DELETE
	delQ := url.Values{"targetRid": []string{target}, "emoji": []string{"👍"}}
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/reactions?"+delQ.Encode(), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d (%s)", w.Code, w.Body.String())
	}

	// Re-DELETE → 404
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/reactions?"+delQ.Encode(), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("re-DELETE: want 404, got %d", w.Code)
	}

	// AGGREGATE after delete: empty list, non-nil emojis
	req = httptest.NewRequest(http.MethodGet, "/api/v2/reactions?"+q.Encode(), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if string(raw["emojis"]) != "[]" {
		t.Fatalf("post-delete emojis should marshal as [] not null, got %s", string(raw["emojis"]))
	}
}

func TestHandler_AggregateMineFlag(t *testing.T) {
	store := NewMemoryStore()
	target := "ri.weave.main.object.42"

	// alice and bob both react with 👍; bob also reacts with 🚀.
	for _, p := range []struct {
		user, emoji string
	}{
		{"user:alice", "👍"},
		{"user:bob", "👍"},
		{"user:bob", "🚀"},
	} {
		body := mustEncode(t, map[string]any{"targetRid": target, "emoji": p.emoji})
		r := newTestRouter(store, &auth.User{ID: p.user})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/reactions", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("setup POST %s/%s: %d", p.user, p.emoji, w.Code)
		}
	}

	// Alice's view: 👍 mine=true, 🚀 mine=false.
	r := newTestRouter(store, &auth.User{ID: "user:alice"})
	req := httptest.NewRequest(http.MethodGet, "/api/v2/reactions?targetRid="+target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var summary Summary
	_ = json.Unmarshal(w.Body.Bytes(), &summary)
	if len(summary.Emojis) != 2 {
		t.Fatalf("alice expected 2 buckets, got %+v", summary)
	}
	for _, b := range summary.Emojis {
		switch b.Emoji {
		case "👍":
			if b.Count != 2 || !b.Mine {
				t.Fatalf("alice 👍 bucket wrong: %+v", b)
			}
		case "🚀":
			if b.Count != 1 || b.Mine {
				t.Fatalf("alice 🚀 bucket wrong: %+v", b)
			}
		default:
			t.Fatalf("unexpected bucket: %+v", b)
		}
	}

	// Bob's view: both mine=true.
	r = newTestRouter(store, &auth.User{ID: "user:bob"})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/reactions?targetRid="+target, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &summary)
	for _, b := range summary.Emojis {
		if !b.Mine {
			t.Fatalf("bob bucket %+v should be mine=true", b)
		}
	}
}

func TestHandler_InvalidInputs(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, &auth.User{ID: "user:alice"})

	cases := []struct {
		name string
		body any
		want string
	}{
		{"missing target", map[string]any{"emoji": "👍"}, "InvalidReactionTarget"},
		{"bad target", map[string]any{"targetRid": "nope", "emoji": "👍"}, "InvalidReactionTarget"},
		{"missing emoji", map[string]any{"targetRid": "ri.weave.main.object.42"}, "InvalidReactionEmoji"},
		{"empty emoji", map[string]any{"targetRid": "ri.weave.main.object.42", "emoji": ""}, "InvalidReactionEmoji"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := mustEncode(t, tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v2/reactions", bytes.NewReader(body))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
			}
			var env map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &env)
			if env["errorName"] != tc.want {
				t.Fatalf("errorName: got %v want %v", env["errorName"], tc.want)
			}
		})
	}

	// Bad delete query.
	req := httptest.NewRequest(http.MethodDelete, "/api/v2/reactions?targetRid=nope&emoji=👍", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DELETE bad target: want 400, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/reactions?targetRid=ri.weave.main.object.42&emoji=", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DELETE missing emoji: want 400, got %d", w.Code)
	}

	// GET aggregate with missing/bad target.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/reactions", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET no target: want 400, got %d", w.Code)
	}
}

func TestBDD_HandlerRejectsAmbiguousJSONBodies_RSI003(t *testing.T) {
	store := NewMemoryStore()
	user := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, user)
	target := "ri.weave.main.object.42"

	first := string(mustEncode(t, map[string]any{"targetRid": target, "emoji": "👍"}))
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v2/reactions",
		strings.NewReader(first+`{"smuggled":true}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertRSI003BadRequest(t, w)
	buckets, err := store.AggregateForTarget(context.Background(), user.ID, target)
	if err != nil {
		t.Fatalf("store.AggregateForTarget: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("ambiguous create persisted reactions: %+v", buckets)
	}
}

func assertRSI003BadRequest(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "InvalidRequestBody") {
		t.Fatalf("expected InvalidRequestBody in response body: %s", w.Body.String())
	}
}

func TestHandler_DegradedModeNoStore(t *testing.T) {
	r := newTestRouter(nil, &auth.User{ID: "user:alice"})
	cases := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/v2/reactions?targetRid=ri.weave.main.object.42", nil},
		{http.MethodPost, "/api/v2/reactions", []byte(`{"targetRid":"ri.weave.main.object.42","emoji":"👍"}`)},
		{http.MethodDelete, "/api/v2/reactions?targetRid=ri.weave.main.object.42&emoji=👍", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var req *http.Request
			if tc.body == nil {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("%s %s: want 500 ReactionsUnavailable, got %d (%s)", tc.method, tc.path, w.Code, w.Body.String())
			}
			var env map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &env)
			if env["errorName"] != "ReactionsUnavailable" {
				t.Fatalf("errorName mismatch: %v", env["errorName"])
			}
		})
	}
}

func TestHandler_RequiresAuth(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/reactions?targetRid=ri.weave.main.object.42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anon GET: want 401, got %d", w.Code)
	}
}
