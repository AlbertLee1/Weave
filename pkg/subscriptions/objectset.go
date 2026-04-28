package subscriptions

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/oss/where"
)

// ObjectSetResolver resolves a stored ObjectSet reference id (the value
// returned by /objectSets/createTemporary) to the underlying Definition.
// pkg/oss/objectset.Store satisfies this contract via its Get method.
type ObjectSetResolver interface {
	Get(id string) (*objectset.Definition, error)
}

// ObjectSetSubscribeRequest is the payload of a { type: "subscribeObjectSet" }
// message. Clients supply EITHER an objectSetRid (resolved server-side via the
// injected ObjectSetResolver) OR an inline definition. Select projects emitted
// objects to a subset of fields, mirroring SubscribeRequest.Select.
type ObjectSetSubscribeRequest struct {
	ObjectSetRid string                `json:"objectSetRid,omitempty"`
	Definition   *objectset.Definition `json:"definition,omitempty"`
	Select       []string              `json:"select,omitempty"`
}

// newObjectSetSubscription wires a Subscription whose Definition supersedes
// ObjectType / Where matching. The returned subscription's ObjectType field is
// left blank — Matches() dispatches on Definition first.
func newObjectSetSubscription(def *objectset.Definition, sel []string) *Subscription {
	return &Subscription{
		ID:         uuid.New().String(),
		Definition: def,
		Select:     sel,
	}
}

// matchesDefinition recursively evaluates whether a single object identified
// by (objectType, primaryKey) with the given properties is a member of the
// ObjectSet described by def. Composite operators (union/intersect/subtract)
// recurse into their children. Complex types that need executor-driven
// evaluation (searchAround, nearestNeighbors, withProperties hops, etc.) are
// rejected at subscribe time; this helper is a pure-data evaluator.
func matchesDefinition(def *objectset.Definition, objectType, primaryKey string, properties map[string]interface{}) bool {
	if def == nil {
		return false
	}
	switch def.Type {
	case "base":
		return def.ObjectType == objectType
	case "filter":
		if !matchesDefinition(def.ObjectSet, objectType, primaryKey, properties) {
			return false
		}
		if len(def.Where) == 0 {
			return true
		}
		var w where.WhereClause
		if err := json.Unmarshal(def.Where, &w); err != nil {
			return false
		}
		return where.MatchClause(&w, properties)
	case "union":
		for _, child := range def.ObjectSets {
			if matchesDefinition(child, objectType, primaryKey, properties) {
				return true
			}
		}
		return false
	case "intersect":
		if len(def.ObjectSets) == 0 {
			return false
		}
		for _, child := range def.ObjectSets {
			if !matchesDefinition(child, objectType, primaryKey, properties) {
				return false
			}
		}
		return true
	case "subtract":
		if len(def.ObjectSets) == 0 {
			return false
		}
		if !matchesDefinition(def.ObjectSets[0], objectType, primaryKey, properties) {
			return false
		}
		for _, child := range def.ObjectSets[1:] {
			if matchesDefinition(child, objectType, primaryKey, properties) {
				return false
			}
		}
		return true
	case "static":
		if def.ObjectType != objectType {
			return false
		}
		for _, pk := range def.PrimaryKeys {
			if pk == primaryKey {
				return true
			}
		}
		return false
	case "asType":
		if def.ObjectType != objectType {
			return false
		}
		return matchesDefinition(def.ObjectSet, objectType, primaryKey, properties)
	case "withProperties":
		// withProperties is a per-row enrichment over the inner set; membership
		// is identical to the inner set's membership.
		return matchesDefinition(def.ObjectSet, objectType, primaryKey, properties)
	default:
		// searchAround / nearestNeighbors / interfaceBase /
		// interfaceLinkSearchAround / methodInput / sample /
		// asBaseObjectTypes / reference all require executor-driven membership
		// evaluation that this pure-data matcher cannot provide. Subscribe
		// validation rejects these up-front, so reaching this branch implies a
		// programming bug — fail closed.
		return false
	}
}

// supportedDefinitionType returns nil if the definition (and every child it
// contains) can be evaluated by matchesDefinition without an executor. Any
// unsupported type produces a descriptive error so the subscribe handler can
// surface a precise rejection message to clients.
func supportedDefinitionType(def *objectset.Definition) error {
	if def == nil {
		return fmt.Errorf("definition is required")
	}
	switch def.Type {
	case "base", "static":
		return nil
	case "filter", "asType", "withProperties":
		return supportedDefinitionType(def.ObjectSet)
	case "union", "intersect", "subtract":
		if len(def.ObjectSets) < 2 {
			return fmt.Errorf("%s requires at least 2 child objectSets", def.Type)
		}
		for _, child := range def.ObjectSets {
			if err := supportedDefinitionType(child); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported objectSet type for subscriptions: %q", def.Type)
	}
}

// resolveDefinition applies the request's chosen source: an inline definition
// is used as-is; otherwise the objectSetRid is looked up via the resolver. A
// nil resolver with an objectSetRid present returns an error so misconfigured
// hubs produce a clear failure rather than a silent no-op.
func resolveDefinition(req ObjectSetSubscribeRequest, resolver ObjectSetResolver) (*objectset.Definition, error) {
	if req.Definition != nil {
		return req.Definition, nil
	}
	if req.ObjectSetRid == "" {
		return nil, fmt.Errorf("definition or objectSetRid is required")
	}
	if resolver == nil {
		return nil, fmt.Errorf("objectSet resolver is not configured")
	}
	def, err := resolver.Get(req.ObjectSetRid)
	if err != nil {
		return nil, err
	}
	return def, nil
}

// handleSubscribeObjectSet processes a subscribeObjectSet request for a
// connection. Returns the response message to send back. On success the
// returned message has type "subscribed" with the new subscription id;
// validation failures emit type "error" with a human-readable Error.
func (h *Hub) handleSubscribeObjectSet(c *Connection, raw json.RawMessage) Message {
	var req ObjectSetSubscribeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return Message{Type: "error", Error: "invalid subscribeObjectSet request: " + err.Error()}
	}

	def, err := resolveDefinition(req, h.objectSetResolver())
	if err != nil {
		return Message{Type: "error", Error: err.Error()}
	}
	if err := def.Validate(); err != nil {
		return Message{Type: "error", Error: err.Error()}
	}
	if err := supportedDefinitionType(def); err != nil {
		return Message{Type: "error", Error: err.Error()}
	}

	c.subMu.Lock()
	defer c.subMu.Unlock()

	if len(c.subscriptions) >= MaxSubscriptionsPerConnection {
		return Message{
			Type:  "error",
			Error: "maximum subscriptions per connection reached (10)",
		}
	}

	sub := newObjectSetSubscription(def, req.Select)
	c.subscriptions[sub.ID] = sub

	return Message{
		Type:           "subscribed",
		SubscriptionID: sub.ID,
	}
}
