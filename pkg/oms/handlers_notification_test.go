package oms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

type contextKey string

const testUserKey contextKey = "test-user-id"

func setupNotificationRouter(repo *mockRepo, userID string) http.Handler {
	handler := oms.NewOMSHandler(repo)
	handler.SetActorFunc(func(ctx context.Context) string {
		if id, ok := ctx.Value(testUserKey).(string); ok {
			return id
		}
		return userID
	})
	r := chi.NewRouter()
	r.Get("/api/v2/notifications", handler.ListNotifications)
	r.Post("/api/v2/notifications/{notificationId}/read", handler.MarkNotificationRead)
	return r
}

func withTestUser(req *http.Request, userID string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), testUserKey, userID))
}

func TestListNotifications_Success(t *testing.T) {
	now := time.Now()
	repo := &mockRepo{
		notifications: []oms.Notification{
			{ID: "n1", UserID: "alice", Title: "Alert 1", Body: "body1", Type: "info", Read: false, CreatedAt: now},
			{ID: "n2", UserID: "alice", Title: "Alert 2", Body: "body2", Type: "automation", Read: true, CreatedAt: now},
			{ID: "n3", UserID: "bob", Title: "Alert 3", Body: "body3", Type: "info", Read: false, CreatedAt: now},
		},
	}
	router := setupNotificationRouter(repo, "")

	req := httptest.NewRequest("GET", "/api/v2/notifications", nil)
	req = withTestUser(req, "alice")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []oms.Notification `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 notifications for alice, got %d", len(resp.Data))
	}
}

func TestListNotifications_UnreadFilter(t *testing.T) {
	now := time.Now()
	repo := &mockRepo{
		notifications: []oms.Notification{
			{ID: "n1", UserID: "alice", Title: "Alert 1", Body: "body1", Type: "info", Read: false, CreatedAt: now},
			{ID: "n2", UserID: "alice", Title: "Alert 2", Body: "body2", Type: "automation", Read: true, CreatedAt: now},
		},
	}
	router := setupNotificationRouter(repo, "")

	req := httptest.NewRequest("GET", "/api/v2/notifications?unread=true", nil)
	req = withTestUser(req, "alice")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []oms.Notification `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 unread notification, got %d", len(resp.Data))
	}
	if resp.Data[0].ID != "n1" {
		t.Fatalf("expected n1, got %s", resp.Data[0].ID)
	}
}

func TestListNotifications_Empty(t *testing.T) {
	repo := &mockRepo{}
	router := setupNotificationRouter(repo, "")

	req := httptest.NewRequest("GET", "/api/v2/notifications", nil)
	req = withTestUser(req, "alice")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data []oms.Notification `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty array, got %d", len(resp.Data))
	}
}

func TestListNotifications_DevUser(t *testing.T) {
	now := time.Now()
	repo := &mockRepo{
		notifications: []oms.Notification{
			{ID: "n1", UserID: "dev-user", Title: "Alert", Body: "body", Type: "info", Read: false, CreatedAt: now},
		},
	}
	// No actorFn or empty user → falls back to "dev-user"
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/notifications", handler.ListNotifications)

	req := httptest.NewRequest("GET", "/api/v2/notifications", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data []oms.Notification `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 notification for dev-user, got %d", len(resp.Data))
	}
}

func TestMarkNotificationRead_Success(t *testing.T) {
	now := time.Now()
	repo := &mockRepo{
		notifications: []oms.Notification{
			{ID: "n1", UserID: "alice", Title: "Alert", Body: "body", Type: "info", Read: false, CreatedAt: now},
		},
	}
	router := setupNotificationRouter(repo, "")

	req := httptest.NewRequest("POST", "/api/v2/notifications/n1/read", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify notification is now marked as read
	if !repo.notifications[0].Read {
		t.Fatal("expected notification to be marked as read")
	}
}

func TestMarkNotificationRead_NotFound(t *testing.T) {
	repo := &mockRepo{}
	router := setupNotificationRouter(repo, "")

	req := httptest.NewRequest("POST", "/api/v2/notifications/nonexistent/read", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateNotificationForUser(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	err := handler.CreateNotificationForUser(
		context.Background(),
		"alice", "Test Title", "Test Body", "automation", "/actions",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(repo.notifications))
	}
	n := repo.notifications[0]
	if n.UserID != "alice" {
		t.Fatalf("expected userID 'alice', got %q", n.UserID)
	}
	if n.Title != "Test Title" {
		t.Fatalf("expected title 'Test Title', got %q", n.Title)
	}
	if n.Body != "Test Body" {
		t.Fatalf("expected body 'Test Body', got %q", n.Body)
	}
	if n.Type != "automation" {
		t.Fatalf("expected type 'automation', got %q", n.Type)
	}
	if n.Read {
		t.Fatal("expected notification to be unread")
	}
}

func TestFullNotificationLifecycle(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	handler.SetActorFunc(func(ctx context.Context) string {
		if id, ok := ctx.Value(testUserKey).(string); ok {
			return id
		}
		return ""
	})

	// 1. Create notification via effect
	err := handler.CreateNotificationForUser(context.Background(), "alice", "New Alert", "Something happened", "automation", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// 2. List notifications
	r := chi.NewRouter()
	r.Get("/api/v2/notifications", handler.ListNotifications)
	r.Post("/api/v2/notifications/{notificationId}/read", handler.MarkNotificationRead)

	req := httptest.NewRequest("GET", "/api/v2/notifications", nil)
	req = withTestUser(req, "alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d", w.Code)
	}
	var resp struct {
		Data []oms.Notification `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(resp.Data))
	}

	notifID := resp.Data[0].ID

	// 3. List unread-only → 1 result
	req2 := httptest.NewRequest("GET", "/api/v2/notifications?unread=true", nil)
	req2 = withTestUser(req2, "alice")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(resp.Data))
	}

	// 4. Mark as read
	req3 := httptest.NewRequest("POST", "/api/v2/notifications/"+notifID+"/read", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("mark-read expected 204, got %d", w3.Code)
	}

	// 5. List unread-only → 0 results
	req4 := httptest.NewRequest("GET", "/api/v2/notifications?unread=true", nil)
	req4 = withTestUser(req4, "alice")
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	json.Unmarshal(w4.Body.Bytes(), &resp)
	if len(resp.Data) != 0 {
		t.Fatalf("expected 0 unread after mark-read, got %d", len(resp.Data))
	}
}
