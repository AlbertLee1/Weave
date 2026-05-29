package oms

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// NotificationBulkStore is a narrow interface for bulk notification mutations
// (US-343). Implemented by *PGRepository directly so the wide oms.Repository
// interface and its many in-memory mocks across pkg/* / test/* aren't forced
// to grow a stub. When unset on OMSHandler the /notifications/read-all
// endpoint responds with 503 NotificationsBulkUnavailable so degraded-mode
// test routers boot cleanly.
type NotificationBulkStore interface {
	MarkAllNotificationsRead(ctx context.Context, userID string, types []string) (int, error)
}

// ListNotifications handles GET /api/v2/notifications.
// Returns the authenticated user's notifications.
//
// Query parameters:
//   - unread=true filters to unread-only rows (US-130).
//   - type=<csv> filters to one or more notification type tags. Multiple tags
//     can be supplied as a comma-separated list or by repeating the param
//     (?type=mention&type=watch). Empty values are ignored. (US-343)
func (h *OMSHandler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID := "dev-user"
	if h.actorFn != nil {
		if id := h.actorFn(r.Context()); id != "" {
			userID = id
		}
	}

	unreadOnly := r.URL.Query().Get("unread") == "true"
	typeFilter := parseNotificationTypeFilter(r.URL.Query()["type"])

	list, err := h.repo.ListNotifications(r.Context(), userID, unreadOnly)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListNotificationsFailed", nil))
		return
	}

	if len(typeFilter) > 0 {
		filtered := make([]Notification, 0, len(list))
		for _, n := range list {
			if _, ok := typeFilter[n.Type]; ok {
				filtered = append(filtered, n)
			}
		}
		list = filtered
	}

	if list == nil {
		list = []Notification{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetNotificationsUnreadCount handles GET /api/v2/notifications/unread-count.
// Returns the authenticated user's unread notification count without
// loading any rows. The Foundry navbar polls this endpoint every few
// seconds; backing it via Repository.CountNotifications + the
// idx_notifications_user_unread partial index keeps the request
// O(returned-count) regardless of table size.
//
// Response shape is intentionally minimal: {"count": <int>}. The
// endpoint MUST NOT return a "data" array — that would defeat the
// "lightweight badge" contract the SPA depends on.
func (h *OMSHandler) GetNotificationsUnreadCount(w http.ResponseWriter, r *http.Request) {
	userID := "dev-user"
	if h.actorFn != nil {
		if id := h.actorFn(r.Context()); id != "" {
			userID = id
		}
	}
	count, err := h.repo.CountNotifications(r.Context(), userID, true)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CountNotificationsFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"count": count,
	})
}

// MarkAllNotificationsRead handles POST /api/v2/notifications/read-all.
// Marks every unread notification belonging to the calling user as read.
// Optionally narrowed by ?type=<csv> to scope the action to one or more
// notification type tags (mention / watch / approval / system / ...).
func (h *OMSHandler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if h.notificationBulkStore == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"errorCode":       "UNAVAILABLE",
			"errorName":       "NotificationsBulkUnavailable",
			"errorInstanceId": "",
		})
		return
	}

	userID := "dev-user"
	if h.actorFn != nil {
		if id := h.actorFn(r.Context()); id != "" {
			userID = id
		}
	}

	types := flattenNotificationTypes(r.URL.Query()["type"])

	updated, err := h.notificationBulkStore.MarkAllNotificationsRead(r.Context(), userID, types)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MarkAllNotificationsReadFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"updated": updated,
	})
}

// parseNotificationTypeFilter normalises the ?type= query into a set used by
// the list handler's post-filter pass. Each raw value may itself be a
// comma-separated list of types. Empty strings are dropped so a missing
// param is a no-op rather than a "type=''" filter that matches nothing.
func parseNotificationTypeFilter(raw []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, v := range flattenNotificationTypes(raw) {
		out[v] = struct{}{}
	}
	return out
}

// flattenNotificationTypes flattens repeated and/or comma-separated ?type=
// values into a deduplicated slice in declaration order. Empty entries are
// dropped.
func flattenNotificationTypes(raw []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			t := strings.TrimSpace(part)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// MarkNotificationRead handles POST /api/v2/notifications/{notificationId}/read.
func (h *OMSHandler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "notificationId")

	if err := h.repo.MarkNotificationRead(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("NotificationNotFound", map[string]string{
				"notificationId": id,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("MarkNotificationReadFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CreateNotificationForUser creates a platform notification for a user.
// Implements the automate.NotificationCreator interface on OMSHandler.
func (h *OMSHandler) CreateNotificationForUser(ctx context.Context, userID, title, body, nType, link string) error {
	n := &Notification{
		ID:        rid.NewNotificationRID(),
		UserID:    userID,
		Title:     title,
		Body:      body,
		Type:      nType,
		Link:      link,
		Read:      false,
		CreatedAt: time.Now(),
	}
	return h.repo.CreateNotification(ctx, n)
}
