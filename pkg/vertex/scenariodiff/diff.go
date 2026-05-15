// Package scenariodiff computes a Vertex Scenario's diff against its base
// ontology and exposes it over HTTP at GET /api/vertex/v1/scenarios/{rid}/diff.
//
// A scenario fork is materialized as an ordered log of ScenarioEdits
// (modifyProperty / createObject / deleteObject / addLink / deleteLink); the
// diff partitions the object-shaped subset of that log into three identity
// buckets (createdObjects / editedObjects / deletedObjects) plus a flat
// deltas[] view tailored for table-style consumers. Link edits do not
// surface in this object-level diff — they are a separate concern.
//
// Compute is a pure function: take the edit log + a BaseLoader and return
// a Diff. The HTTP handler is a thin shell that loads edits from a
// scenarios.Repo-shaped reader and forwards everything else to Compute.
package scenariodiff

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/liyang/weave/pkg/scenarios"
)

// ObjectRef is a (objectType, objectId) identity tuple used in
// createdObjects / deletedObjects (where there is no "change" payload).
type ObjectRef struct {
	ObjectType string `json:"objectType"`
	ObjectID   string `json:"objectId"`
}

// CreatedObject is one entry in Diff.CreatedObjects. Properties carries the
// staged property bag accumulated by createObject (+ later modifyProperty)
// edits against the same key.
type CreatedObject struct {
	ObjectType string                     `json:"objectType"`
	ObjectID   string                     `json:"objectId"`
	Properties map[string]json.RawMessage `json:"properties"`
}

// PropertyDelta is one (objectType, objectId, property, oldValue, newValue)
// change row. OldValue is JSON null when no base value existed for this
// property. NewValue mirrors the latest modifyProperty edit on that key.
type PropertyDelta struct {
	ObjectType string          `json:"objectType"`
	ObjectID   string          `json:"objectId"`
	Property   string          `json:"property"`
	OldValue   json.RawMessage `json:"oldValue"`
	NewValue   json.RawMessage `json:"newValue"`
}

// EditedObject groups all PropertyDeltas for one (objectType, objectId)
// identity. Sorting inside Changes is property-name lexicographic.
type EditedObject struct {
	ObjectType string          `json:"objectType"`
	ObjectID   string          `json:"objectId"`
	Changes    []PropertyDelta `json:"changes"`
}

// Diff is the wire shape returned by the /diff endpoint. The four buckets are
// always non-nil so JSON clients can iterate without nil-guards.
type Diff struct {
	EditedObjects  []EditedObject  `json:"editedObjects"`
	CreatedObjects []CreatedObject `json:"createdObjects"`
	DeletedObjects []ObjectRef     `json:"deletedObjects"`
	Deltas         []PropertyDelta `json:"deltas"`
}

// BaseLoader fetches the base ObjectView for a key. The bool result is true
// when a view existed at the base; false means "no such object" (no error).
// A non-nil error indicates a real load failure and aborts Compute.
type BaseLoader interface {
	LoadBase(ctx context.Context, key scenarios.ObjectKey) (*scenarios.ObjectView, bool, error)
}

// NoBaseLoader is a BaseLoader that always reports "no base view." It exists
// so callers who only care about created/deleted bookkeeping (or who run
// against a fresh ontology) can wire Compute without a real overlay reader.
type NoBaseLoader struct{}

// LoadBase always returns (nil, false, nil).
func (NoBaseLoader) LoadBase(context.Context, scenarios.ObjectKey) (*scenarios.ObjectView, bool, error) {
	return nil, false, nil
}

// Compute partitions an edit log into the Diff wire shape. The edits do not
// need to be pre-sorted — Compute replays them in Seq ASC order so caller
// insertion order is irrelevant. Link edits (addLink/deleteLink) are silently
// dropped: object-level diff has nothing to say about graph topology.
func Compute(ctx context.Context, edits []scenarios.ScenarioEdit, base BaseLoader) (Diff, error) {
	if base == nil {
		base = NoBaseLoader{}
	}

	sorted := append([]scenarios.ScenarioEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	// Per-key state during the replay. `created` and `deleted` are flags
	// reflecting the net effect of all replayed edits so far on this key:
	// a create-after-delete leaves created=true, deleted=false.
	type state struct {
		created bool
		deleted bool
		// modifiedProps captures the latest NewValue for each property
		// touched by a modifyProperty edit while NOT in the created state.
		// Once `created` flips, we move authority to createdProps below.
		modifiedProps map[string]json.RawMessage
		// createdProps is the staged property bag for a created object —
		// merged from the createObject NewValue plus any subsequent
		// modifyProperty edits on the same key.
		createdProps map[string]json.RawMessage
	}
	states := map[scenarios.ObjectKey]*state{}
	order := []scenarios.ObjectKey{}
	get := func(k scenarios.ObjectKey) *state {
		s, ok := states[k]
		if !ok {
			s = &state{}
			states[k] = s
			order = append(order, k)
		}
		return s
	}

	for _, e := range sorted {
		switch e.Op {
		case "createObject", "modifyProperty", "deleteObject":
		default:
			// addLink / deleteLink / unknown ops have no object-level effect.
			continue
		}
		k := scenarios.ObjectKey{ObjectType: e.ObjectType, ObjectID: e.ObjectID}
		s := get(k)
		switch e.Op {
		case "createObject":
			props := map[string]json.RawMessage{}
			if len(e.NewValue) > 0 {
				_ = json.Unmarshal(e.NewValue, &props)
			}
			s.created = true
			s.deleted = false
			s.createdProps = props
			s.modifiedProps = nil
		case "modifyProperty":
			if s.deleted {
				// Fold semantics: modify-after-delete is ignored.
				continue
			}
			if s.created {
				if s.createdProps == nil {
					s.createdProps = map[string]json.RawMessage{}
				}
				s.createdProps[e.Property] = cloneJSON(e.NewValue)
				continue
			}
			if s.modifiedProps == nil {
				s.modifiedProps = map[string]json.RawMessage{}
			}
			s.modifiedProps[e.Property] = cloneJSON(e.NewValue)
		case "deleteObject":
			s.deleted = true
			s.created = false
			s.createdProps = nil
			s.modifiedProps = nil
		}
	}

	// Stable output order: lexicographic by (objectType, objectId).
	keys := make([]scenarios.ObjectKey, 0, len(order))
	for _, k := range order {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ObjectType != keys[j].ObjectType {
			return keys[i].ObjectType < keys[j].ObjectType
		}
		return keys[i].ObjectID < keys[j].ObjectID
	})

	out := Diff{
		EditedObjects:  []EditedObject{},
		CreatedObjects: []CreatedObject{},
		DeletedObjects: []ObjectRef{},
		Deltas:         []PropertyDelta{},
	}

	for _, k := range keys {
		s := states[k]
		switch {
		case s.created:
			out.CreatedObjects = append(out.CreatedObjects, CreatedObject{
				ObjectType: k.ObjectType,
				ObjectID:   k.ObjectID,
				Properties: s.createdProps,
			})
		case s.deleted:
			out.DeletedObjects = append(out.DeletedObjects, ObjectRef{
				ObjectType: k.ObjectType,
				ObjectID:   k.ObjectID,
			})
		case len(s.modifiedProps) > 0:
			baseView, _, err := base.LoadBase(ctx, k)
			if err != nil {
				return Diff{}, err
			}
			changes := make([]PropertyDelta, 0, len(s.modifiedProps))
			propNames := make([]string, 0, len(s.modifiedProps))
			for p := range s.modifiedProps {
				propNames = append(propNames, p)
			}
			sort.Strings(propNames)
			for _, p := range propNames {
				newVal := s.modifiedProps[p]
				oldVal := json.RawMessage("null")
				if baseView != nil {
					if v, ok := baseView.Properties[p]; ok {
						oldVal = cloneJSON(v)
					}
				}
				if jsonEqual(oldVal, newVal) {
					// Non-change — drop.
					continue
				}
				changes = append(changes, PropertyDelta{
					ObjectType: k.ObjectType,
					ObjectID:   k.ObjectID,
					Property:   p,
					OldValue:   oldVal,
					NewValue:   newVal,
				})
			}
			if len(changes) == 0 {
				continue
			}
			out.EditedObjects = append(out.EditedObjects, EditedObject{
				ObjectType: k.ObjectType,
				ObjectID:   k.ObjectID,
				Changes:    changes,
			})
			out.Deltas = append(out.Deltas, changes...)
		}
	}

	return out, nil
}

func cloneJSON(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("null")
	}
	cp := make(json.RawMessage, len(v))
	copy(cp, v)
	return cp
}

// jsonEqual returns true when two json.RawMessages encode the same JSON value.
// Byte-equal comparison is too strict (whitespace differences would split
// equal values); a parse-then-marshal round-trip canonicalizes both sides.
func jsonEqual(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	ab, err := json.Marshal(av)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(bv)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}
