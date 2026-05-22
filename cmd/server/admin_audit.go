package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
)

const (
	auditDefaultPageSize = 100
	auditMaxPageSize     = 1000
)

// auditPageCursor is the cursor shape persisted in pageToken.
type auditPageCursor struct {
	Offset int `json:"o"`
}

func encodeAuditCursor(offset int) string {
	data, _ := json.Marshal(auditPageCursor{Offset: offset})
	return base64.URLEncoding.EncodeToString(data)
}

func decodeAuditCursor(s string) (auditPageCursor, error) {
	if s == "" {
		return auditPageCursor{}, nil
	}
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return auditPageCursor{}, err
	}
	var wire struct {
		Offset *int `json:"o"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return auditPageCursor{}, err
	}
	if wire.Offset == nil {
		return auditPageCursor{}, errors.New("audit cursor missing offset")
	}
	if *wire.Offset < 0 {
		return auditPageCursor{}, errors.New("audit cursor offset must be non-negative")
	}
	return auditPageCursor{Offset: *wire.Offset}, nil
}

// auditEventsResponse is the JSON shape of the list endpoint.
type auditEventsResponse struct {
	Data          []audit.AuditEvent `json:"data"`
	NextPageToken string             `json:"nextPageToken,omitempty"`
}

// NewAdminAuditEventsHandler returns an http.Handler that lists audit events
// with optional filters and cursor pagination.
//
// Query params: actor, action, resource_type, since, until, pageSize, pageToken.
//
// The handler does NOT enforce authentication on its own; the surrounding
// router is expected to wrap it with auth.RequirePermission(auth.PermUserManage).
func NewAdminAuditEventsHandler(store audit.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errorCode": "SERVICE_UNAVAILABLE",
				"errorName": "AuditStoreNotConfigured",
			})
			return
		}

		q := r.URL.Query()

		// Parse pagination.
		pageSize := auditDefaultPageSize
		if ps := q.Get("pageSize"); ps != "" {
			n, err := strconv.Atoi(ps)
			if err != nil || n < 1 {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:pageSize", map[string]string{
					"parameter": "pageSize",
					"reason":    "pageSize must be a positive integer",
				}))
				return
			}
			pageSize = n
		}
		if pageSize > auditMaxPageSize {
			pageSize = auditMaxPageSize
		}

		cursor, err := decodeAuditCursor(q.Get("pageToken"))
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:pageToken", map[string]string{
				"parameter": "pageToken",
				"reason":    "invalid pageToken",
			}))
			return
		}

		// Build filter. US-493 adds the resourceRid clause so admins can
		// pull every audit row for a single resource (ObjectType /
		// Session / Policy / ...). The PRD writes it camelCase; we
		// accept both camelCase and snake_case so the same query
		// string survives CLI/SDK rendering choices.
		resourceRID := q.Get("resourceRid")
		if resourceRID == "" {
			resourceRID = q.Get("resource_rid")
		}
		f := audit.ListFilter{
			ActorID:      q.Get("actor"),
			Action:       q.Get("action"),
			ResourceType: q.Get("resource_type"),
			ResourceRID:  resourceRID,
			PageSize:     pageSize + 1, // fetch one extra to detect next page
			Offset:       cursor.Offset,
		}

		if since := q.Get("since"); since != "" {
			t, err := time.Parse(time.RFC3339, since)
			if err != nil {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:since", map[string]string{
					"parameter": "since",
					"reason":    "since must be RFC3339 format",
				}))
				return
			}
			f.From = &t
		}
		if until := q.Get("until"); until != "" {
			t, err := time.Parse(time.RFC3339, until)
			if err != nil {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:until", map[string]string{
					"parameter": "until",
					"reason":    "until must be RFC3339 format",
				}))
				return
			}
			f.To = &t
		}

		events, err := store.List(r.Context(), f)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("AuditListFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		resp := auditEventsResponse{}
		if len(events) > pageSize {
			// There are more pages.
			resp.Data = events[:pageSize]
			resp.NextPageToken = encodeAuditCursor(cursor.Offset + pageSize)
		} else {
			resp.Data = events
		}

		if resp.Data == nil {
			resp.Data = []audit.AuditEvent{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
