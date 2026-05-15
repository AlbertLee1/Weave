package scenarios

import (
	"encoding/json"
	"sort"
)

// ObjectKey identifies one object during a fold pass: (objectType, objectID)
// is the natural key in scenario_edits.
type ObjectKey struct {
	ObjectType string
	ObjectID   string
}

// ObjectView is the materialized view of a single object after scenario edits
// are folded over a base. Property values stay as json.RawMessage so callers
// can json.Unmarshal them into whatever concrete shape they expect.
type ObjectView struct {
	ObjectType string
	ObjectID   string
	Properties map[string]json.RawMessage
}

// LinkView is one directed edge in the per-link-type adjacency.
type LinkView struct {
	LinkType string
	SrcID    string
	DstID    string
}

// FoldObject replays scenario edits over a base object view for one
// (objectType, objectID) target and returns the folded view.
//
//   - (folded, false) — object exists after replay
//   - (nil, true)     — a deleteObject edit removed it (and no later
//     createObject re-created it)
//   - (nil, false)    — no base and no createObject targeting this key
//
// Edits unrelated to the target key are skipped, as are addLink / deleteLink
// edits (use FoldLinks). A modifyProperty edit between a deleteObject and a
// re-creating createObject is dropped, matching Vertex semantics. The caller
// does not need to pre-sort: FoldObject sorts a copy of edits by Seq ASC.
func FoldObject(target ObjectKey, base *ObjectView, edits []ScenarioEdit) (*ObjectView, bool) {
	var view *ObjectView
	if base != nil && base.ObjectType == target.ObjectType && base.ObjectID == target.ObjectID {
		view = cloneObjectView(base)
	}
	deleted := false

	sorted := append([]ScenarioEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	for _, e := range sorted {
		if e.ObjectType != target.ObjectType || e.ObjectID != target.ObjectID {
			continue
		}
		switch e.Op {
		case "createObject":
			props := map[string]json.RawMessage{}
			if len(e.NewValue) > 0 {
				_ = json.Unmarshal(e.NewValue, &props)
			}
			view = &ObjectView{
				ObjectType: target.ObjectType,
				ObjectID:   target.ObjectID,
				Properties: props,
			}
			deleted = false
		case "modifyProperty":
			if view == nil || deleted {
				continue
			}
			if view.Properties == nil {
				view.Properties = map[string]json.RawMessage{}
			}
			view.Properties[e.Property] = append(json.RawMessage(nil), e.NewValue...)
		case "deleteObject":
			view = nil
			deleted = true
		}
	}

	if view == nil {
		return nil, deleted
	}
	return view, false
}

// FoldLinks replays addLink / deleteLink edits over a base adjacency and
// returns the merged set, deterministically sorted by (LinkType, SrcID, DstID).
// Object-shaped edits are ignored. Edits do not need to be pre-sorted —
// FoldLinks sorts a copy by Seq ASC.
func FoldLinks(base []LinkView, edits []ScenarioEdit) []LinkView {
	type key struct{ linkType, src, dst string }
	set := make(map[key]struct{}, len(base))
	for _, l := range base {
		set[key{l.LinkType, l.SrcID, l.DstID}] = struct{}{}
	}

	sorted := append([]ScenarioEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	for _, e := range sorted {
		k := key{e.LinkType, e.SrcID, e.DstID}
		switch e.Op {
		case "addLink":
			set[k] = struct{}{}
		case "deleteLink":
			delete(set, k)
		}
	}

	out := make([]LinkView, 0, len(set))
	for k := range set {
		out = append(out, LinkView{LinkType: k.linkType, SrcID: k.src, DstID: k.dst})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LinkType != out[j].LinkType {
			return out[i].LinkType < out[j].LinkType
		}
		if out[i].SrcID != out[j].SrcID {
			return out[i].SrcID < out[j].SrcID
		}
		return out[i].DstID < out[j].DstID
	})
	return out
}

func cloneObjectView(v *ObjectView) *ObjectView {
	cp := &ObjectView{
		ObjectType: v.ObjectType,
		ObjectID:   v.ObjectID,
		Properties: make(map[string]json.RawMessage, len(v.Properties)),
	}
	for k, val := range v.Properties {
		cp.Properties[k] = append(json.RawMessage(nil), val...)
	}
	return cp
}
