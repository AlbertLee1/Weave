package subscriptions

import (
	"encoding/json"

	"github.com/google/uuid"
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
type Subscription struct {
	ID         string
	ObjectType string
	Where      *where.WhereClause
	Select     []string
}

// ObjectChangeEvent is the data payload for a { type: "objectChanged" }
// message pushed from server to client.
type ObjectChangeEvent struct {
	State  string                 `json:"state"`  // ADDED_OR_UPDATED | DELETED
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
// this subscription's filter criteria.
func (s *Subscription) Matches(objectType string, properties map[string]interface{}) bool {
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

// HandleObjectChange evaluates a change event against all active subscriptions
// and pushes matching events to their connections. Properties are projected
// per subscription's Select clause.
func (h *Hub) HandleObjectChange(objectType, primaryKey, editType string, properties map[string]interface{}) {
	state := editTypeToState(editType)

	h.mu.Lock()
	// Snapshot connections to avoid holding the lock during writes.
	type connSubs struct {
		conn *Connection
		subs []*Subscription
	}
	var targets []connSubs
	for _, c := range h.conns {
		c.subMu.Lock()
		if len(c.subscriptions) > 0 {
			subs := make([]*Subscription, 0, len(c.subscriptions))
			for _, sub := range c.subscriptions {
				subs = append(subs, sub)
			}
			targets = append(targets, connSubs{conn: c, subs: subs})
		}
		c.subMu.Unlock()
	}
	h.mu.Unlock()

	for _, cs := range targets {
		for _, sub := range cs.subs {
			if !sub.Matches(objectType, properties) {
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
				Data:           data,
			}
			select {
			case cs.conn.send <- msg:
			default:
				// Buffer full — drop for this connection.
			}
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

	c.subMu.Lock()
	defer c.subMu.Unlock()

	if len(c.subscriptions) >= MaxSubscriptionsPerConnection {
		return Message{
			Type:  "error",
			Error: "maximum subscriptions per connection reached (10)",
		}
	}

	sub := NewSubscription(req)
	c.subscriptions[sub.ID] = sub

	return Message{
		Type:           "subscribed",
		SubscriptionID: sub.ID,
	}
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

	c.subMu.Lock()
	defer c.subMu.Unlock()

	if _, ok := c.subscriptions[req.SubscriptionID]; !ok {
		return Message{Type: "error", Error: "subscription not found: " + req.SubscriptionID}
	}
	delete(c.subscriptions, req.SubscriptionID)

	return Message{
		Type:           "unsubscribed",
		SubscriptionID: req.SubscriptionID,
	}
}
