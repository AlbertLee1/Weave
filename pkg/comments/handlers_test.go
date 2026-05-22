package comments

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newTestRouter(store Store, user *auth.User) http.Handler {
	return newTestRouterWithMentions(store, user, nil, nil)
}

func newTestRouterWithMentions(
	store Store,
	user *auth.User,
	dir MentionUserDirectory,
	notifier MentionNotifier,
) http.Handler {
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
	h := NewHandler(store)
	if dir != nil {
		h.SetMentionUserDirectory(dir)
	}
	if notifier != nil {
		h.SetMentionNotifier(notifier)
	}
	h.RegisterRoutes(r)
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

const targetRID = "ri.ontology.main.object.t1"

func TestHandler_CreateRequiresAuth(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil) // anonymous
	body := mustEncode(t, map[string]any{"targetRid": targetRID, "body": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/comments", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anon create: want 401, got %d", w.Code)
	}
}

func TestHandler_CreateUnavailableWhenStoreNil(t *testing.T) {
	r := newTestRouter(nil, &auth.User{ID: "user:alice"})
	body := mustEncode(t, map[string]any{"targetRid": targetRID, "body": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/comments", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil store: want 500, got %d", w.Code)
	}
}

func TestHandler_FullCRUDFlow(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}

	// Alice posts a top-level comment.
	withAlice := newTestRouter(store, alice)
	createBody := mustEncode(t, map[string]any{
		"targetRid": targetRID,
		"body":      "first",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/comments", bytes.NewReader(createBody))
	w := httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("alice POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var top Comment
	if err := json.Unmarshal(w.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if top.Author != alice.ID || top.Body != "first" || top.ID == "" {
		t.Fatalf("create returned wrong shape: %+v", top)
	}

	// Empty body → 400.
	req = httptest.NewRequest(http.MethodPost, "/api/v2/comments",
		bytes.NewReader(mustEncode(t, map[string]any{"targetRid": targetRID, "body": ""})))
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty body: want 400, got %d", w.Code)
	}

	// Bad target RID → 400.
	req = httptest.NewRequest(http.MethodPost, "/api/v2/comments",
		bytes.NewReader(mustEncode(t, map[string]any{"targetRid": "not-a-rid", "body": "x"})))
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad target: want 400, got %d", w.Code)
	}

	// Bob replies to alice's comment.
	withBob := newTestRouter(store, bob)
	replyBody := mustEncode(t, map[string]any{
		"targetRid": targetRID,
		"body":      "reply",
		"parentId":  top.ID,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/comments", bytes.NewReader(replyBody))
	w = httptest.NewRecorder()
	withBob.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bob reply: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var reply Comment
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	if reply.ParentID != top.ID || reply.Author != bob.ID {
		t.Fatalf("reply shape wrong: %+v", reply)
	}

	// Bob cannot reply to a non-existent parent → 400.
	req = httptest.NewRequest(http.MethodPost, "/api/v2/comments",
		bytes.NewReader(mustEncode(t, map[string]any{
			"targetRid": targetRID, "body": "x", "parentId": "nope"})))
	w = httptest.NewRecorder()
	withBob.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad parent: want 400, got %d", w.Code)
	}

	// LIST scoped to target.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/comments?targetRid="+targetRID, nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", w.Code)
	}
	var listResp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 2 || len(listResp.Comments) != 2 {
		t.Fatalf("list: want 2 total, got %d (rows=%d)", listResp.Total, len(listResp.Comments))
	}

	// LIST without targetRid → 400.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/comments", nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing targetRid: want 400, got %d", w.Code)
	}

	// LIST scoped to parent.
	req = httptest.NewRequest(http.MethodGet,
		"/api/v2/comments?targetRid="+targetRID+"&parentId="+top.ID, nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list-parent: want 200, got %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp.Total != 1 || listResp.Comments[0].ID != reply.ID {
		t.Fatalf("list-parent wrong: %+v", listResp)
	}

	// GET single.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/comments/"+top.ID, nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d", w.Code)
	}

	// GET unknown → 404.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/comments/nope", nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get-missing: want 404, got %d", w.Code)
	}

	// EDIT by author.
	editBody := mustEncode(t, map[string]any{"body": "first (edited)"})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/comments/"+top.ID, bytes.NewReader(editBody))
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("edit-author: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var edited Comment
	_ = json.Unmarshal(w.Body.Bytes(), &edited)
	if edited.Body != "first (edited)" {
		t.Fatalf("edit didn't persist: %q", edited.Body)
	}

	// EDIT by non-author → 403.
	req = httptest.NewRequest(http.MethodPut, "/api/v2/comments/"+top.ID, bytes.NewReader(editBody))
	w = httptest.NewRecorder()
	withBob.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("edit-non-author: want 403, got %d", w.Code)
	}

	// DELETE by non-author → 403.
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/comments/"+top.ID, nil)
	w = httptest.NewRecorder()
	withBob.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("delete-non-author: want 403, got %d", w.Code)
	}

	// DELETE by author → 204, then GET returns tombstone (body empty + deletedAt set).
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/comments/"+top.ID, nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v2/comments/"+top.ID, nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get-tombstone: want 200, got %d", w.Code)
	}
	var tomb Comment
	_ = json.Unmarshal(w.Body.Bytes(), &tomb)
	if tomb.Body != "" || tomb.DeletedAt == nil {
		t.Fatalf("tombstone not redacted: %+v", tomb)
	}

	// Re-DELETE by author → 404.
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/comments/"+top.ID, nil)
	w = httptest.NewRecorder()
	withAlice.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("re-delete: want 404, got %d", w.Code)
	}
}

func TestBDD_HandlerRejectsAmbiguousJSONBodies_RSI003(t *testing.T) {
	t.Run("create rejects a valid comment followed by another JSON value", func(t *testing.T) {
		store := NewMemoryStore()
		user := &auth.User{ID: "user:alice"}
		r := newTestRouter(store, user)

		first := string(mustEncode(t, map[string]any{
			"targetRid": targetRID,
			"body":      "first",
		}))
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v2/comments",
			strings.NewReader(first+`{"smuggled":true}`),
		)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertRSI003BadRequest(t, w)
		rows, total, err := store.List(context.Background(), ListQuery{TargetRID: targetRID, Limit: 10})
		if err != nil {
			t.Fatalf("store.List: %v", err)
		}
		if total != 0 || len(rows) != 0 {
			t.Fatalf("ambiguous create persisted comments: total=%d rows=%d", total, len(rows))
		}
	})

	t.Run("update rejects a valid edit followed by another JSON value", func(t *testing.T) {
		store := NewMemoryStore()
		user := &auth.User{ID: "user:alice"}
		row := &Comment{
			ID:        "comment-1",
			TargetRID: targetRID,
			Body:      "original",
			Author:    user.ID,
		}
		if err := store.Create(context.Background(), row); err != nil {
			t.Fatalf("seed comment: %v", err)
		}
		r := newTestRouter(store, user)

		first := string(mustEncode(t, map[string]any{"body": "mutated"}))
		req := httptest.NewRequest(
			http.MethodPut,
			"/api/v2/comments/"+row.ID,
			strings.NewReader(first+`{"smuggled":true}`),
		)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertRSI003BadRequest(t, w)
		got, err := store.Get(context.Background(), row.ID)
		if err != nil {
			t.Fatalf("store.Get: %v", err)
		}
		if got.Body != "original" {
			t.Fatalf("ambiguous update mutated body to %q", got.Body)
		}
	})
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

func TestHandler_CreateFiresMentionNotifications(t *testing.T) {
	store := NewMemoryStore()
	dir := &stubDirectory{
		byEmail: map[string]MentionUser{
			"bob@example.com": {ID: "user:bob@example.com", Email: "bob@example.com", Name: "Bob"},
		},
	}
	notif := &stubNotifier{}
	r := newTestRouterWithMentions(store, &auth.User{ID: "user:alice@example.com"}, dir, notif)
	body := mustEncode(t, map[string]any{
		"targetRid": targetRID,
		"body":      "ping @bob@example.com please review",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/comments", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	got := notif.snapshot()
	if len(got) != 1 || got[0].RecipientID != "user:bob@example.com" {
		t.Fatalf("expected one notification for bob, got %+v", got)
	}
	if got[0].AuthorID != "user:alice@example.com" {
		t.Fatalf("notification author wrong: %+v", got[0])
	}
}

func TestHandler_SearchMentions_AuthRequired(t *testing.T) {
	r := newTestRouterWithMentions(nil, nil, &stubDirectory{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/mentions/search?q=alice", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anon search: want 401, got %d", w.Code)
	}
}

func TestHandler_SearchMentions_UnavailableWithoutDirectory(t *testing.T) {
	r := newTestRouterWithMentions(nil, &auth.User{ID: "user:alice"}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/mentions/search?q=alice", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("no directory: want 500, got %d", w.Code)
	}
}

func TestHandler_SearchMentions_EmptyQueryReturnsEmpty(t *testing.T) {
	dir := &stubDirectory{
		search: []MentionUser{
			{ID: "user:alice@example.com", Email: "alice@example.com", Name: "Alice"},
		},
	}
	r := newTestRouterWithMentions(nil, &auth.User{ID: "user:alice"}, dir, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/mentions/search?q=", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty q: want 200, got %d", w.Code)
	}
	var resp MentionSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) != 0 {
		t.Fatalf("empty q should return empty users, got %+v", resp.Users)
	}
}

func TestHandler_SearchMentions_ReturnsUsers(t *testing.T) {
	dir := &stubDirectory{
		search: []MentionUser{
			{ID: "user:alice@example.com", Email: "alice@example.com", Name: "Alice"},
			{ID: "user:bob@example.com", Email: "bob@example.com", Name: "Bob"},
		},
	}
	r := newTestRouterWithMentions(nil, &auth.User{ID: "user:carol"}, dir, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/mentions/search?q=al&limit=5", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search: want 200, got %d", w.Code)
	}
	var resp MentionSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Fatalf("want 2 users, got %d", len(resp.Users))
	}
	if resp.Users[0].Email != "alice@example.com" {
		t.Fatalf("unexpected first user: %+v", resp.Users[0])
	}
}
