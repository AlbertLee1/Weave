package oss

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss/where"
)

// ErrObjectSetNotFound is returned by ObjectSetLookup implementations when
// the requested ObjectSet rid does not resolve to a known definition. The
// SSE handler maps it to a 404 ObjectSetNotFound response.
var ErrObjectSetNotFound = errors.New("objectSet not found")

// SubscriptionSpec is the decoded view of an ObjectSet definition needed by
// the SSE subscribe handler. ObjectType names the single base ObjectType the
// stream filters on; Where carries the AND-collapsed filter tree extracted
// from any filter hops along the path from the outermost definition down to
// the base. Both Where and the row-level matcher are optional — an empty
// Where streams every event for the ObjectType.
type SubscriptionSpec struct {
	ObjectType string
	Where      *where.WhereClause
}

// ObjectSetLookup is the narrow contract the SSE subscribe handler needs
// from the ObjectSet store. It is a local interface rather than a direct
// dependency on pkg/oss/objectset because that package imports pkg/oss
// (handler.go), which would create an import cycle. main.go wires a
// tiny adapter around *objectset.Store when building the handler.
type ObjectSetLookup interface {
	// ResolveSubscription returns the SubscriptionSpec (base ObjectType + any
	// Where filter extracted from filter hops) the SSE stream should use for
	// the given ObjectSet rid. Returns an ObjectType of "" when the
	// definition does not reduce to a single base type (union /
	// interfaceBase / subtract / ...); the handler emits a clean 400
	// rather than silently producing a wrong stream. Returns
	// ErrObjectSetNotFound when the rid is unknown so the handler can emit
	// a dedicated 404.
	ResolveSubscription(objectSetRid string) (SubscriptionSpec, error)
}

// SubscribeSSEHandler is the US-055 ObjectSet Server-Sent Events scaffold.
// It resolves a stored ObjectSet by rid, subscribes to the in-process funnel
// broadcaster, filters events down to the ObjectSet's base ObjectType and
// streams them to the client as text/event-stream data lines.
//
// This story scopes the handler to the simplest "base" ObjectSet shape —
// unwrapping a single hop of filter / withProperties / searchAround /
// asType so the happy path still works for trivially nested definitions.
// Where-clause filtering (US-056), Last-Event-ID replay (US-057) and
// heartbeats + connection caps (US-058) layer on top of this scaffold.
type SubscribeSSEHandler struct {
	lookup    ObjectSetLookup
	broadcast *funnel.Broadcast
}

// NewSubscribeSSEHandler wires a fresh SSE handler. Both dependencies are
// required — nil lookup or nil broadcast cause the handler to fail every
// request with a 500 so the operational misconfiguration is loud rather
// than silently yielding an empty stream.
func NewSubscribeSSEHandler(lookup ObjectSetLookup, b *funnel.Broadcast) *SubscribeSSEHandler {
	return &SubscribeSSEHandler{lookup: lookup, broadcast: b}
}

// ServeHTTP implements http.Handler.
func (h *SubscribeSSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.lookup == nil || h.broadcast == nil {
		apierror.WriteJSON(w, apierror.NewInternal("SSESubscribeNotConfigured", map[string]string{
			"reason": "lookup or broadcast is nil",
		}))
		return
	}

	objectSetRid := chi.URLParam(r, "objectSetRid")
	if objectSetRid == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidObjectSetRid", map[string]string{
			"objectSetRid": objectSetRid,
		}))
		return
	}

	spec, err := h.lookup.ResolveSubscription(objectSetRid)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ObjectSetNotFound", map[string]string{
			"objectSetRid": objectSetRid,
		}))
		return
	}
	if spec.ObjectType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SSEUnsupportedObjectSet", map[string]string{
			"objectSetRid": objectSetRid,
			"reason":       "SSE subscribe currently requires a base ObjectSet type",
		}))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		apierror.WriteJSON(w, apierror.NewInternal("SSEStreamingUnsupported", map[string]string{
			"reason": "ResponseWriter does not implement http.Flusher",
		}))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	id, ch := h.broadcast.Subscribe(16)
	defer h.broadcast.Unsubscribe(id)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, open := <-ch:
			if !open {
				return
			}
			if evt.ObjectType != spec.ObjectType {
				continue
			}
			if spec.Where != nil && !where.MatchClause(spec.Where, evt.Properties) {
				continue
			}
			payload := sseEventPayload(evt)
			buf, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", buf); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// sseEventPayload maps a funnel.BroadcastEvent into the public SSE
// payload shape. CREATE / MODIFY collapse to ADDED_OR_UPDATED so frontend
// consumers can treat both as "refresh this row"; DELETE maps to DELETED.
// The object side carries __primaryKey / __apiName in the same reserved-key
// convention LoadObjects uses (see pkg/oss/wire.go FormatObject), so the
// React hook can reuse the existing WireObject parsing path unchanged.
func sseEventPayload(evt funnel.BroadcastEvent) map[string]interface{} {
	eventType := "ADDED_OR_UPDATED"
	if evt.Type == "DELETE" {
		eventType = "DELETED"
	}
	obj := map[string]interface{}{
		"__primaryKey": evt.PrimaryKey,
		"__apiName":    evt.ObjectType,
	}
	for k, v := range evt.Properties {
		if _, reserved := obj[k]; reserved {
			continue
		}
		obj[k] = v
	}
	return map[string]interface{}{
		"eventType": eventType,
		"object":    obj,
	}
}
