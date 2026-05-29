package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_NotificationsUnreadCount covers the round-66 Foundry-
// parity gap. The Foundry navbar polls an unread-count badge
// every N seconds; today the SPA has to GET
// /api/v2/notifications?unread=true and `length` the response
// just to render a single integer. With 100 users × 100
// notifications each, this means 10K rows serialized + parsed +
// GC'd per minute for a badge that only needs an int. A
// dedicated COUNT(*) endpoint backed by the existing
// idx_notifications_user_unread partial index returns the answer
// without loading any rows.
//
// Wire shape:
//
//	GET /api/v2/notifications/unread-count
//	  200 + {count: N}  → number of unread rows belonging to the
//	                      authenticated user
//
// Scenarios:
//   - Empty inbox returns count=0 (empty array surfaces as 0).
//   - 3 unread + 2 read for the user returns count=3.
//   - Notifications belonging to OTHER users do NOT leak into
//     the caller's count (user-scope isolation).
//   - Count endpoint never returns a list — body must NOT carry
//     a `data` key (perf invariant).
//   - The Repository.CountNotifications method is called with
//     unreadOnly=true (the underlying SQL must use the partial
//     index path, not COUNT(*) over the whole table).
func TestBDD_NotificationsUnreadCount(t *testing.T) {
	const userID = "dev-user"
	const otherUser = "u-other"

	newServer := func(t *testing.T, ns []oms.Notification) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.notifications = append(repo.notifications, ns...)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/notifications/unread-count", handler.GetNotificationsUnreadCount)
		return r, repo
	}

	doGet := func(t *testing.T, r *chi.Mux) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v2/notifications/unread-count", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Empty inbox returns count=0", func(t *testing.T) {
		r, _ := newServer(t, nil)
		rec := doGet(t, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		count, ok := body["count"].(float64)
		if !ok {
			t.Fatalf("count missing/wrong type: %v (full=%v)", body["count"], body)
		}
		if int(count) != 0 {
			t.Errorf("count=%v, want 0", count)
		}
	})

	t.Run("3 unread + 2 read for the user returns count=3", func(t *testing.T) {
		now := time.Now().UTC()
		ns := []oms.Notification{
			{ID: "n1", UserID: userID, Title: "u1", Body: "b1", Type: "mention", Read: false, CreatedAt: now},
			{ID: "n2", UserID: userID, Title: "u2", Body: "b2", Type: "mention", Read: false, CreatedAt: now},
			{ID: "n3", UserID: userID, Title: "u3", Body: "b3", Type: "watch", Read: false, CreatedAt: now},
			{ID: "n4", UserID: userID, Title: "u4", Body: "b4", Type: "mention", Read: true, CreatedAt: now},
			{ID: "n5", UserID: userID, Title: "u5", Body: "b5", Type: "watch", Read: true, CreatedAt: now},
		}
		r, repo := newServer(t, ns)
		rec := doGet(t, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if int(body["count"].(float64)) != 3 {
			t.Errorf("count=%v, want 3", body["count"])
		}
		// The Repository method MUST be called with unreadOnly=true
		// so the SQL uses the partial-index path. Asserting the
		// call shape prevents a future "just call ListNotifications
		// and len() it" regression.
		if repo.countNotificationsCalls != 1 {
			t.Errorf("CountNotifications calls=%d, want 1",
				repo.countNotificationsCalls)
		}
		if !repo.countNotificationsLastUnreadOnly {
			t.Errorf("CountNotifications was called with unreadOnly=false; want true (index path)")
		}
	})

	t.Run("Other users' notifications do not leak into caller's count", func(t *testing.T) {
		now := time.Now().UTC()
		ns := []oms.Notification{
			// caller has 1 unread
			{ID: "n1", UserID: userID, Title: "u1", Body: "b1", Read: false, CreatedAt: now},
			// other user has 5 unread — must NOT count
			{ID: "n2", UserID: otherUser, Title: "u2", Body: "b2", Read: false, CreatedAt: now},
			{ID: "n3", UserID: otherUser, Title: "u3", Body: "b3", Read: false, CreatedAt: now},
			{ID: "n4", UserID: otherUser, Title: "u4", Body: "b4", Read: false, CreatedAt: now},
			{ID: "n5", UserID: otherUser, Title: "u5", Body: "b5", Read: false, CreatedAt: now},
			{ID: "n6", UserID: otherUser, Title: "u6", Body: "b6", Read: false, CreatedAt: now},
		}
		r, _ := newServer(t, ns)
		rec := doGet(t, r)
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if int(body["count"].(float64)) != 1 {
			t.Errorf("count=%v, want 1 (other user's 5 unread must not leak)", body["count"])
		}
	})

	t.Run("Response body carries count only, no data array", func(t *testing.T) {
		// Perf invariant: the count endpoint must NEVER return rows.
		// Foundry's navbar polls every few seconds; serializing even
		// a few KB of row data per poll defeats the point of the
		// endpoint.
		now := time.Now().UTC()
		ns := []oms.Notification{
			{ID: "n1", UserID: userID, Title: "u1", Body: "b1", Read: false, CreatedAt: now},
		}
		r, _ := newServer(t, ns)
		rec := doGet(t, r)
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if _, hasData := body["data"]; hasData {
			t.Errorf("response carries data key — must be count-only; body=%v", body)
		}
	})
}
