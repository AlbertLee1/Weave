package oms

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// ListNotifications handles GET /api/v2/notifications.
// Returns the authenticated user's notifications.
func (h *OMSHandler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID := "dev-user"
	if h.actorFn != nil {
		if id := h.actorFn(r.Context()); id != "" {
			userID = id
		}
	}

	unreadOnly := r.URL.Query().Get("unread") == "true"

	list, err := h.repo.ListNotifications(r.Context(), userID, unreadOnly)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListNotificationsFailed", nil))
		return
	}

	if list == nil {
		list = []Notification{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
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
