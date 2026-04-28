package subscriptions

import "github.com/liyang/weave/pkg/oss/objectset"

// indexedSub pairs a subscription with its owning connection so the routing
// index can dispatch a change event to the right channel without a second
// connection lookup.
type indexedSub struct {
	conn *Connection
	sub  *Subscription
}

// subscriptionObjectTypes returns the set of objectType keys under which the
// given subscription should be indexed for change-event routing. The result
// is intentionally a SUPERSET of the actual matching set — matchesDefinition
// (for ObjectSet subscriptions) re-validates membership at dispatch time, so
// a false positive merely costs one wasted predicate eval; a false NEGATIVE
// would silently drop events. Keep the helper liberal.
func subscriptionObjectTypes(s *Subscription) []string {
	if s == nil {
		return nil
	}
	if s.Definition != nil {
		return collectDefinitionObjectTypes(s.Definition)
	}
	if s.ObjectType == "" {
		return nil
	}
	return []string{s.ObjectType}
}

// collectDefinitionObjectTypes walks an ObjectSet Definition tree and returns
// the deduplicated list of leaf ObjectType strings the definition could
// match. Mirrors the supported types in matchesDefinition / supportedDefinitionType.
func collectDefinitionObjectTypes(def *objectset.Definition) []string {
	if def == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	var walk func(d *objectset.Definition)
	walk = func(d *objectset.Definition) {
		if d == nil {
			return
		}
		switch d.Type {
		case "base", "static", "asType":
			if d.ObjectType != "" {
				if _, ok := seen[d.ObjectType]; !ok {
					seen[d.ObjectType] = struct{}{}
					out = append(out, d.ObjectType)
				}
			}
			// asType still wraps an inner ObjectSet whose types are the same
			// objectType narrowing — recurse for completeness in case a child
			// declares additional candidates (e.g. a polymorphic asType wrap).
			if d.ObjectSet != nil {
				walk(d.ObjectSet)
			}
		case "filter", "withProperties":
			walk(d.ObjectSet)
		case "union", "intersect", "subtract":
			for _, child := range d.ObjectSets {
				walk(child)
			}
		}
	}
	walk(def)
	return out
}

// addToIndexLocked registers (conn, sub) under every objectType key the
// subscription should be routed by. Caller must hold h.mu.
func (h *Hub) addToIndexLocked(c *Connection, sub *Subscription) {
	if h.subIndex == nil {
		h.subIndex = make(map[string][]*indexedSub)
	}
	keys := subscriptionObjectTypes(sub)
	entry := &indexedSub{conn: c, sub: sub}
	for _, k := range keys {
		h.subIndex[k] = append(h.subIndex[k], entry)
	}
}

// removeFromIndexLocked drops every entry pointing at the given subscription.
// Caller must hold h.mu. The match is by Subscription pointer identity, which
// is unique across a Hub's lifetime (NewSubscription mints a fresh struct).
func (h *Hub) removeFromIndexLocked(sub *Subscription) {
	if h.subIndex == nil || sub == nil {
		return
	}
	keys := subscriptionObjectTypes(sub)
	for _, k := range keys {
		bucket := h.subIndex[k]
		filtered := bucket[:0]
		for _, e := range bucket {
			if e.sub != sub {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(h.subIndex, k)
		} else {
			h.subIndex[k] = filtered
		}
	}
}

// removeConnectionFromIndexLocked drops every entry whose owning connection is
// c. Used at disconnect time when the per-connection subscriptions map is no
// longer trustworthy. Caller must hold h.mu.
func (h *Hub) removeConnectionFromIndexLocked(c *Connection) {
	if c == nil {
		return
	}
	dropConn := func(idx map[string][]*indexedSub) {
		if idx == nil {
			return
		}
		for k, bucket := range idx {
			filtered := bucket[:0]
			for _, e := range bucket {
				if e.conn != c {
					filtered = append(filtered, e)
				}
			}
			if len(filtered) == 0 {
				delete(idx, k)
			} else {
				idx[k] = filtered
			}
		}
	}
	dropConn(h.subIndex)
	dropConn(h.jobIndex)
}

// subscriptionsForObjectTypeLocked snapshots the current routing entries for
// the given objectType. Caller must hold h.mu. The returned slice is a copy
// so callers can iterate safely after the lock is released.
func (h *Hub) subscriptionsForObjectTypeLocked(objectType string) []*indexedSub {
	bucket := h.subIndex[objectType]
	if len(bucket) == 0 {
		return nil
	}
	snap := make([]*indexedSub, len(bucket))
	copy(snap, bucket)
	return snap
}

// subscriptionCountForObjectType returns the number of subscriptions currently
// indexed under the given objectType. Used by tests to verify routing-index
// invariants without inspecting hub internals directly.
func (h *Hub) subscriptionCountForObjectType(objectType string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subIndex[objectType])
}
