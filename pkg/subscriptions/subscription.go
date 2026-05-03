package subscriptions

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/oss/where"
)

// MaxSubscriptionsPerConnection is the hard limit on how many active
// subscriptions a single WebSocket connection may hold. Attempting to
// subscribe beyond this limit returns an error message.
const MaxSubscriptionsPerConnection = 10

// SubscribeRequest is the payload of a { type: "subscribe" } message
// sent from client to server.
type SubscribeRequest struct {
	ObjectType string             `json:"objectType"`
	Where      *where.WhereClause `json:"where,omitempty"`
	Select     []string           `json:"select,omitempty"`
}

// Subscription is an active subscription on a connection. It stores the
// filter criteria and projection fields so the Hub can evaluate incoming
// change events against it.
//
// A subscription is in one of four modes:
//   - ObjectType + Where: the legacy single-type filter ({type:"subscribe"}).
//   - Definition: an ObjectSet membership filter ({type:"subscribeObjectSet"})
//     whose Matches walks the Definition tree per change event. When
//     Definition is non-nil it supersedes ObjectType + Where.
//   - Aggregator: an incremental aggregation ({type:"subscribeAggregation"})
//     whose Apply mutates the running totals on every matching change event
//     and emits "aggregationChanged" messages instead of "objectChanged".
//   - JobID: an action-job progress feed ({type:"subscribeActionJob"}) whose
//     events are dispatched by Hub.HandleActionJobProgress instead of the
//     change-event router. Indexed under the jobIndex map. US-318.
type Subscription struct {
	ID         string
	ObjectType string
	Where      *where.WhereClause
	Definition *objectset.Definition
	Aggregator *IncrementalAggregator
	JobID      string
	Select     []string
}

// ObjectChangeEvent is the data payload for a { type: "objectChanged" }
// message pushed from server to client.
type ObjectChangeEvent struct {
	State  string                 `json:"state"` // ADDED_OR_UPDATED | DELETED
	Object map[string]interface{} `json:"object"`
}

// NewSubscription creates a Subscription with a generated UUID.
func NewSubscription(req SubscribeRequest) *Subscription {
	return &Subscription{
		ID:         uuid.New().String(),
		ObjectType: req.ObjectType,
		Where:      req.Where,
		Select:     req.Select,
	}
}

// Matches returns true if the given objectType and properties satisfy
// this subscription's filter criteria. ObjectSet subscriptions evaluate
// membership via matchesDefinition; legacy single-type subscriptions fall
// back to the ObjectType + Where pair.
func (s *Subscription) Matches(objectType string, properties map[string]interface{}) bool {
	return s.matches(objectType, "", properties)
}

// matches is the primaryKey-aware variant of Matches; used by HandleObjectChange
// for ObjectSet definitions whose membership depends on the primary key
// (notably "static" sets).
func (s *Subscription) matches(objectType, primaryKey string, properties map[string]interface{}) bool {
	if s.Definition != nil {
		return matchesDefinition(s.Definition, objectType, primaryKey, properties)
	}
	if s.ObjectType != objectType {
		return false
	}
	return where.MatchClause(s.Where, properties)
}

// ProjectProperties returns a copy of properties filtered to only include
// the fields listed in the Select clause. If Select is empty/nil, all
// properties are returned unchanged.
func (s *Subscription) ProjectProperties(properties map[string]interface{}) map[string]interface{} {
	if len(s.Select) == 0 {
		return properties
	}
	projected := make(map[string]interface{}, len(s.Select))
	for _, field := range s.Select {
		if val, ok := properties[field]; ok {
			projected[field] = val
		}
	}
	return projected
}

// editTypeToState maps funnel EditType strings to the WebSocket subscription
// event state values.
func editTypeToState(editType string) string {
	switch editType {
	case "DELETE":
		return "DELETED"
	default:
		return "ADDED_OR_UPDATED"
	}
}

// HandleObjectChange evaluates a change event against the subscriptions
// indexed under objectType and pushes matching events to their connections.
// Properties are projected per subscription's Select clause. Dispatch cost is
// O(K) where K is the number of subscriptions registered for this specific
// objectType — independent of the total active subscription count (US-306).
//
// US-380: every change is appended to the Hub's replay log under hub.mu so a
// reconnecting client supplying ?since=<cursor> can recover missed events.
// The assigned cursor is stamped on the outbound objectChanged envelope.
func (h *Hub) HandleObjectChange(objectType, primaryKey, editType string, properties map[string]interface{}) {
	state := editTypeToState(editType)

	h.mu.Lock()
	cursor := h.eventLog.Append(EventLogEntry{
		Kind:       "objectChange",
		ObjectType: objectType,
		PrimaryKey: primaryKey,
		EditType:   editType,
		Properties: properties,
	})
	entries := h.subscriptionsForObjectTypeLocked(objectType)
	h.mu.Unlock()

	for _, e := range entries {
		sub := e.sub
		conn := e.conn
		if sub.Aggregator != nil {
			// US-305: incremental aggregation subscriptions consume every
			// event for their objectType (the Where filter is applied inside
			// Apply against both the previous snapshot and the new payload so
			// contributions revert correctly even when an update moves an
			// object out of scope).
			if sub.Aggregator.Apply(state, objectType, primaryKey, properties) {
				if !conn.allowEvent() {
					conn.markOverflow(sub.ID)
					continue
				}
				sendAggregationChanged(conn, sub.ID, sub.Aggregator.Snapshot(), cursor)
			}
			continue
		}
		if !sub.matches(objectType, primaryKey, properties) {
			continue
		}
		// US-308: per-connection event rate limit. Excess events drop and
		// surface to the client via the existing onOutOfDate path so the
		// client can resync rather than silently miss state.
		if !conn.allowEvent() {
			conn.markOverflow(sub.ID)
			continue
		}
		projected := sub.ProjectProperties(properties)
		evt := ObjectChangeEvent{
			State:  state,
			Object: projected,
		}
		data, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		msg := Message{
			Type:           "objectChanged",
			SubscriptionID: sub.ID,
			Cursor:         cursor,
			Data:           data,
		}
		select {
		case conn.send <- msg:
		default:
			// Buffer full — mark subscription as out of date
			conn.markOverflow(sub.ID)
		}
	}
}

// handleSubscribe processes a subscribe request for a connection.
// Returns the response message to send back.
func (h *Hub) handleSubscribe(c *Connection, raw json.RawMessage) Message {
	var req SubscribeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return Message{Type: "error", Error: "invalid subscribe request: " + err.Error()}
	}
	if req.ObjectType == "" {
		return Message{Type: "error", Error: "objectType is required"}
	}

	// Lock order: hub.mu → conn.subMu, matching HandleObjectChange so routing
	// index updates and dispatch never deadlock. Explicit locks (not defer)
	// so the US-380 replay path can fire after the registration is durable
	// without holding either mutex while it drains buffered messages onto
	// c.send.
	h.mu.Lock()
	c.subMu.Lock()

	if len(c.subscriptions) >= MaxSubscriptionsPerConnection {
		c.subMu.Unlock()
		h.mu.Unlock()
		return Message{
			Type:  "error",
			Error: "maximum subscriptions per connection reached (10)",
		}
	}
	if !h.reserveUserSubLocked(c.userID) {
		c.subMu.Unlock()
		h.mu.Unlock()
		return Message{
			Type:  "error",
			Error: "maximum subscriptions per user reached",
		}
	}

	sub := NewSubscription(req)
	c.subscriptions[sub.ID] = sub
	h.addToIndexLocked(c, sub)
	doReplay := c.replayCursor > 0
	c.subMu.Unlock()
	h.mu.Unlock()

	if !doReplay {
		return Message{
			Type:           "subscribed",
			SubscriptionID: sub.ID,
		}
	}

	// Push the subscribed reply directly so the client sees
	// subscribed → replayed objectChanged events in order. Returning
	// Message{} signals the readPump to skip the default dispatch.
	select {
	case c.send <- Message{Type: "subscribed", SubscriptionID: sub.ID}:
	default:
	}
	h.replayObjectSubscription(c, sub)
	return Message{}
}

// handleUnsubscribe removes a subscription from a connection.
func (h *Hub) handleUnsubscribe(c *Connection, raw json.RawMessage) Message {
	var req struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return Message{Type: "error", Error: "invalid unsubscribe request: " + err.Error()}
	}
	if req.SubscriptionID == "" {
		return Message{Type: "error", Error: "subscriptionId is required"}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	c.subMu.Lock()
	defer c.subMu.Unlock()

	sub, ok := c.subscriptions[req.SubscriptionID]
	if !ok {
		return Message{Type: "error", Error: "subscription not found: " + req.SubscriptionID}
	}
	delete(c.subscriptions, req.SubscriptionID)
	h.removeFromIndexLocked(sub)
	h.removeJobSubLocked(sub)
	h.releaseUserSubsLocked(c.userID, 1)

	return Message{
		Type:           "unsubscribed",
		SubscriptionID: req.SubscriptionID,
	}
}
