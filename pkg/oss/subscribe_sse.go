package oss

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss/where"
)

// DefaultSSEHeartbeatInterval is the US-058 default gap between heartbeat
// `:ping` comment lines written to idle SSE connections. Configurable per
// handler via SetHeartbeatInterval; a zero or negative interval disables
// heartbeats entirely (tests that do not care about heartbeats can leave
// the default in place — the 30s cadence never fires inside normal go-test
// timeouts).
const DefaultSSEHeartbeatInterval = 30 * time.Second

// DefaultSSEMaxConnectionsPerUser is the US-058 default cap on concurrent
// SSE subscribe requests from a single authenticated user. Breaching this
// cap yields an HTTP 429 RESOURCE_EXHAUSTED response instead of starving
// the hub of file descriptors. A zero or negative value disables the cap.
const DefaultSSEMaxConnectionsPerUser = 10

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

// SSEEventFilter is an optional per-event policy filter that the SSE handler
// applies before delivering each event to a subscriber. Implementations
// typically check the subscribing user's markings / ABAC attributes against
// the event's properties to enforce the same visibility rules used by the
// Load / Search / Aggregate read paths. A nil filter passes everything.
type SSEEventFilter interface {
	// AllowEvent returns true if the given event should be visible to the
	// subscribing user. Called inside the hot event loop — implementations
	// must be fast and allocation-light.
	AllowEvent(user *auth.User, evt funnel.BroadcastEvent) bool
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

	// US-058 configuration. Both fields are seeded to sane defaults in
	// NewSubscribeSSEHandler and can be overridden via their setters
	// before the handler is installed on a router.
	heartbeatInterval time.Duration
	maxPerUser        int

	// US-073: optional per-event policy filter. When non-nil the handler
	// calls AllowEvent for each candidate SSE frame before writing it to
	// the subscriber; events the filter rejects are silently dropped.
	eventFilter SSEEventFilter

	// connMu guards counts — the per-connection-key live subscriber
	// tally. Keys are derived from the authenticated user (preferred) or
	// the request's remote address (fallback), so unauthenticated dev
	// traffic still gets a bounded bucket rather than unlimited fan-out.
	connMu sync.Mutex
	counts map[string]int
}

// NewSubscribeSSEHandler wires a fresh SSE handler. Both dependencies are
// required — nil lookup or nil broadcast cause the handler to fail every
// request with a 500 so the operational misconfiguration is loud rather
// than silently yielding an empty stream.
func NewSubscribeSSEHandler(lookup ObjectSetLookup, b *funnel.Broadcast) *SubscribeSSEHandler {
	return &SubscribeSSEHandler{
		lookup:            lookup,
		broadcast:         b,
		heartbeatInterval: DefaultSSEHeartbeatInterval,
		maxPerUser:        DefaultSSEMaxConnectionsPerUser,
		counts:            map[string]int{},
	}
}

// SetHeartbeatInterval overrides the default 30-second heartbeat cadence.
// A non-positive duration disables heartbeats on this handler — useful for
// tests that do not care about idle-channel detection. Safe to call before
// the handler is installed on a router; concurrent reconfiguration after
// live connections attach is not supported and would race with the fan-out
// loop.
func (h *SubscribeSSEHandler) SetHeartbeatInterval(d time.Duration) {
	h.heartbeatInterval = d
}

// SetMaxConnectionsPerUser overrides the default per-user connection cap.
// A non-positive value disables the cap entirely. The cap is enforced per
// connection key (authenticated user ID when available, otherwise the
// request's remote host) so one client cannot monopolise the broadcast
// hub. Safe to call before the handler is installed on a router.
func (h *SubscribeSSEHandler) SetMaxConnectionsPerUser(n int) {
	h.maxPerUser = n
}

// SetEventFilter installs an optional per-event policy filter. When set,
// the handler calls AllowEvent(user, evt) for each candidate SSE frame and
// drops events the filter rejects. This is the SSE equivalent of the
// applyMarkingFilter / applyPolicyFilter passes on the Load / Search paths.
// Safe to call before the handler is installed on a router.
func (h *SubscribeSSEHandler) SetEventFilter(f SSEEventFilter) {
	h.eventFilter = f
}

// acquireSlot attempts to reserve a connection slot for the given key. It
// returns true on success; on success the caller must invoke releaseSlot
// via defer when the subscription ends. A non-positive maxPerUser disables
// the cap and always succeeds without touching the counter.
func (h *SubscribeSSEHandler) acquireSlot(key string) bool {
	if h.maxPerUser <= 0 || key == "" {
		return true
	}
	h.connMu.Lock()
	defer h.connMu.Unlock()
	if h.counts[key] >= h.maxPerUser {
		return false
	}
	h.counts[key]++
	return true
}

// releaseSlot decrements the live-subscriber count for the given key. It
// is a no-op when the cap is disabled or the key was never seen (e.g. the
// caller skipped acquireSlot because of a nil user context).
func (h *SubscribeSSEHandler) releaseSlot(key string) {
	if h.maxPerUser <= 0 || key == "" {
		return
	}
	h.connMu.Lock()
	defer h.connMu.Unlock()
	if h.counts[key] > 0 {
		h.counts[key]--
	}
	if h.counts[key] == 0 {
		delete(h.counts, key)
	}
}

// connectionKey derives the bucket used for per-user connection capping.
// Authenticated requests key on auth.User.ID so the cap scales across
// processes that share a user directory; unauthenticated / dev requests
// fall back to the request's remote host so local testing still gets a
// bounded bucket rather than unlimited fan-out.
func connectionKey(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil && u.ID != "" {
		return "user:" + u.ID
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return ""
	}
	return "ip:" + host
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

	// US-058: enforce the per-user connection cap BEFORE any SSE
	// streaming headers are written so a breach surfaces as a clean JSON
	// 429 rather than a half-initialised text/event-stream response.
	connKey := connectionKey(r)
	if !h.acquireSlot(connKey) {
		apierror.WriteJSON(w, apierror.NewTooManyRequests("SSEConnectionLimitExceeded", map[string]string{
			"reason":        "per-user SSE connection limit reached",
			"maxPerUser":    strconv.Itoa(h.maxPerUser),
			"connectionKey": connKey,
		}))
		return
	}
	defer h.releaseSlot(connKey)

	flusher, ok := w.(http.Flusher)
	if !ok {
		apierror.WriteJSON(w, apierror.NewInternal("SSEStreamingUnsupported", map[string]string{
			"reason": "ResponseWriter does not implement http.Flusher",
		}))
		return
	}

	// US-057 / US-307 / US-459: parse the resume cursor for SSE replay.
	// The cursor carries the NATS stream sequence the client last observed;
	// the hub replays any buffered events with Sequence > fromSeq before
	// attaching the live subscription. Three channels are accepted, in
	// precedence order:
	//   1. Last-Event-ID HTTP header — the SSE-protocol-canonical channel
	//      browsers send automatically on EventSource auto-reconnect.
	//   2. ?since= query parameter — the US-459 canonical SDK-facing alias,
	//      used by non-browser clients that recreate the connection.
	//   3. ?lastEventId= query parameter — legacy fallback retained so the
	//      existing React EventSource client (web/src/hooks/useObjectSetSubscription.ts)
	//      continues to resume across reconnects without code changes.
	// The header wins when present so a stale URL never overrides a fresher
	// header value. Malformed values degrade to fromSeq == 0 so a broken
	// cursor never silently disables replay by erroring out the request.
	var fromSeq uint64
	cursorVal := r.Header.Get("Last-Event-ID")
	if cursorVal == "" {
		cursorVal = r.URL.Query().Get("since")
	}
	if cursorVal == "" {
		cursorVal = r.URL.Query().Get("lastEventId")
	}
	if cursorVal != "" {
		if parsed, err := strconv.ParseUint(cursorVal, 10, 64); err == nil {
			fromSeq = parsed
		}
	}

	// US-459: SubscribeWithReplayWindow signals outOfWindow=true when the
	// supplied fromSeq is older than the oldest event the hub still retains
	// (after the 5-minute time-bounded prune). In that case the handler MUST
	// emit a 410 Gone with the typed apierror body BEFORE writing any SSE
	// streaming headers — once headers are flushed the status line is fixed
	// and the SDK has no clean way to tell "stream ended" from "replay
	// window exceeded".
	id, ch, outOfWindow := h.broadcast.SubscribeWithReplayWindow(16, fromSeq)
	if outOfWindow {
		apierror.WriteJSON(w, apierror.NewGone("SSEReplayWindowExceeded", map[string]string{
			"objectSetRid": objectSetRid,
			"since":        cursorVal,
			"reason":       "cursor is older than the replay retention window; refetch the ObjectSet and resume from the latest seq",
		}))
		return
	}
	defer h.broadcast.Unsubscribe(id)

	// Replay window OK — only now write SSE streaming headers. Doing this
	// AFTER the out-of-window check preserves a clean JSON 410 body when
	// the cursor is too old, instead of half-initialising a text/event-stream
	// response and then trying to convey the error through the data channel.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// US-058: heartbeat ticker. SSE comment lines (`:ping\n\n`) are a
	// protocol-standard way to probe the connection without delivering a
	// data frame — intermediaries and the browser's EventSource both
	// treat them as keepalive noise. A non-positive interval disables the
	// ticker entirely so the select falls back to "data or ctx.Done" only.
	var heartbeatCh <-chan time.Time
	if h.heartbeatInterval > 0 {
		ticker := time.NewTicker(h.heartbeatInterval)
		defer ticker.Stop()
		heartbeatCh = ticker.C
	}

	ctx := r.Context()
	// US-073: capture the subscribing user once so the per-event filter
	// does not need to re-extract on every iteration.
	var sseUser *auth.User
	if h.eventFilter != nil {
		sseUser = auth.UserFromContext(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatCh:
			if _, err := fmt.Fprint(w, ":ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
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
			// US-073: per-event policy filter (marking / ABAC).
			if h.eventFilter != nil && !h.eventFilter.AllowEvent(sseUser, evt) {
				continue
			}
			payload := sseEventPayload(evt)
			buf, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			if evt.Sequence > 0 {
				if _, err := fmt.Fprintf(w, "id: %d\n", evt.Sequence); err != nil {
					return
				}
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", buf); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// sseEventPayload maps a funnel.BroadcastEvent into the public SSE
// payload shape. The frame carries two parallel views of the same edit:
//
//   - US-459 canonical keys ({seq, type, rid, properties}) — the SDK-facing
//     contract spelled out in the PRD acceptance criteria. type collapses
//     onto the lower-case verbs created/modified/deleted; rid composes the
//     objectType and primaryKey so SDK clients have a single addressable
//     identifier; seq mirrors evt.Sequence so the client can resume by
//     passing it back via ?since=.
//   - Legacy keys ({eventType, object}) — preserved so the existing React
//     hook (web/src/hooks/useObjectSetSubscription.ts) continues to parse
//     the same payload without code changes. CREATE / MODIFY collapse to
//     ADDED_OR_UPDATED and the object map embeds __primaryKey / __apiName
//     in the reserved-key convention LoadObjects uses
//     (see pkg/oss/wire.go FormatObject).
//
// Emitting both views keeps the migration zero-friction for browser
// consumers while satisfying the canonical {seq, type, rid, properties}
// shape downstream tooling now relies on.
func sseEventPayload(evt funnel.BroadcastEvent) map[string]interface{} {
	eventType := "ADDED_OR_UPDATED"
	if evt.Type == "DELETE" {
		eventType = "DELETED"
	}
	canonicalType := "modified"
	switch evt.Type {
	case "CREATE":
		canonicalType = "created"
	case "DELETE":
		canonicalType = "deleted"
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
	// US-459 properties view: deliver an empty map (never nil) so SDK clients
	// can iterate without a nil guard. Reserved __-prefixed keys are NOT
	// included here — the rid already encodes the addressing info.
	properties := make(map[string]interface{}, len(evt.Properties))
	for k, v := range evt.Properties {
		properties[k] = v
	}
	return map[string]interface{}{
		// US-459 canonical view
		"seq":        evt.Sequence,
		"type":       canonicalType,
		"rid":        evt.ObjectType + ":" + evt.PrimaryKey,
		"properties": properties,
		// Legacy view (kept for backwards compatibility with the React hook).
		"eventType": eventType,
		"object":    obj,
	}
}
