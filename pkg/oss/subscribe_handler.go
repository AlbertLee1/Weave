package oss

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/liyang/weave/pkg/funnel"
)

// SetBroadcast wires the in-process broadcast hub so the SSE subscribe
// endpoint can fan out edit events to live clients. Pass nil (or never
// call) to leave the endpoint disabled — in that mode the route still
// registers but returns 503 so callers can detect feature absence.
func (h *Handler) SetBroadcast(b *funnel.Broadcast) {
	h.broadcast = b
}

// SubscribeChanges handles
//
//	GET /api/v2/ontologies/{ontologyApiName}/subscribe?objectType=X
//
// It opens a Server-Sent Events stream that emits one frame per applied
// edit (CREATE/MODIFY/DELETE). The query parameter ?objectType= optionally
// restricts the stream to a single object type; otherwise every event for
// the ontology is forwarded.
//
// The handler runs until the request context is cancelled (client
// disconnect) and unsubscribes from the broadcast hub on the way out.
// Slow clients miss events rather than blocking the funnel consumer —
// the broadcast hub drops on full buffers.
func (h *Handler) SubscribeChanges(w http.ResponseWriter, r *http.Request) {
	if h.broadcast == nil {
		http.Error(w, "subscribe: broadcast not configured", http.StatusServiceUnavailable)
		return
	}

	// Optional objectType filter from the query string. Empty means
	// "stream every event for this ontology".
	objectTypeFilter := r.URL.Query().Get("objectType")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "subscribe: streaming unsupported", http.StatusInternalServerError)
		return
	}

	// SSE headers must be set before any body bytes are written.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Send an initial comment line so proxies/clients see the stream is
	// alive even before the first real event arrives. SSE comments start
	// with ':' and are ignored by EventSource consumers.
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	// Buffer of 16 keeps short bursts smooth without ballooning memory.
	id, ch := h.broadcast.Subscribe(16)
	defer h.broadcast.Unsubscribe(id)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				// Channel closed by Unsubscribe (e.g. server shutdown).
				return
			}
			if objectTypeFilter != "" && event.ObjectType != objectTypeFilter {
				continue
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
