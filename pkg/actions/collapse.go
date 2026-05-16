package actions

import "github.com/liyang/weave/pkg/funnel"

// propertyWrite is one entry in an object's per-property timeline. Position
// is the index of the source Edit in the input slice; later positions
// overwrite earlier ones, giving us deterministic last-write-wins semantics
// during batch collapse (US-473).
type propertyWrite struct {
	value    interface{}
	position int
}

// CollapseEdits optimizes a list of edits by collapsing redundant operations.
//
// US-473: each object (objectType, primaryKey) maintains an explicit
// per-property timeline; the merged edit's Properties map is rebuilt fresh
// from that timeline so input Properties maps are never mutated in place
// and downstream callers see a clean LWW resolution.
func CollapseEdits(edits []funnel.Edit) []funnel.Edit {
	objects, objectOrder, links, linkOrder := collapseEditsTimeline(edits)
	return materializeCollapsedEdits(objects, objectOrder, links, linkOrder)
}

type editKey struct {
	objectType string
	primaryKey string
}

type linkKey struct {
	linkTypeRID string
	sourcePK    string
	targetPK    string
}

type trackedEdit struct {
	// canonical carries the non-property fields of the surviving edit
	// (Type, Source, Markings, EditVersion, …). Properties on this struct
	// is always left nil; the final map is rebuilt from timeline at
	// emit time so input Properties maps are never mutated in place.
	canonical funnel.Edit
	timeline  map[string]propertyWrite
	removed   bool
}

type trackedLinkEdit struct {
	edit    funnel.Edit
	removed bool
}

// collapseEditsTimeline performs the core collapse loop and returns the
// per-object timeline state alongside link tracking. Exposed to package
// callers (collapse_schema.go) so the timeline can be queried without
// rerunning the loop.
func collapseEditsTimeline(edits []funnel.Edit) (map[editKey]*trackedEdit, []editKey, map[linkKey]*trackedLinkEdit, []linkKey) {
	objects := make(map[editKey]*trackedEdit)
	var objectOrder []editKey

	links := make(map[linkKey]*trackedLinkEdit)
	var linkOrder []linkKey

	for pos, edit := range edits {
		if edit.Type == funnel.EditTypeLinkCreate || edit.Type == funnel.EditTypeLinkDelete {
			lk := linkKey{edit.LinkTypeRID, edit.PrimaryKey, edit.TargetPrimaryKey}
			existing, exists := links[lk]
			if !exists {
				links[lk] = &trackedLinkEdit{edit: edit}
				linkOrder = append(linkOrder, lk)
				continue
			}
			switch {
			case existing.edit.Type == funnel.EditTypeLinkCreate && edit.Type == funnel.EditTypeLinkDelete:
				// LINK_CREATE + LINK_DELETE = cancel both.
				existing.removed = true
			case existing.edit.Type == funnel.EditTypeLinkDelete && edit.Type == funnel.EditTypeLinkCreate:
				// LINK_DELETE + LINK_CREATE = last one wins (recreate).
				existing.edit = edit
				existing.removed = false
			// Duplicate LINK_CREATE or LINK_DELETE on same triple → keep first.
			}
			continue
		}

		key := editKey{edit.ObjectType, edit.PrimaryKey}
		existing, exists := objects[key]
		if !exists {
			te := &trackedEdit{
				canonical: edit,
				timeline:  make(map[string]propertyWrite, len(edit.Properties)),
			}
			// Drop the Properties reference on canonical — we rebuild a
			// fresh map at emit time so we never mutate caller maps.
			te.canonical.Properties = nil
			for k, v := range edit.Properties {
				te.timeline[k] = propertyWrite{value: v, position: pos}
			}
			objects[key] = te
			objectOrder = append(objectOrder, key)
			continue
		}

		switch {
		case existing.canonical.Type == funnel.EditTypeCreate && edit.Type == funnel.EditTypeDelete:
			// CREATE + DELETE = cancel both. Clear timeline so a later
			// resurrection-CREATE starts from an empty slate.
			existing.removed = true
			existing.timeline = make(map[string]propertyWrite)

		case existing.canonical.Type == funnel.EditTypeModify && edit.Type == funnel.EditTypeDelete:
			// MODIFY + DELETE = just DELETE; the modify's accumulated
			// properties are discarded along with the object.
			existing.canonical = edit
			existing.canonical.Properties = nil
			existing.timeline = make(map[string]propertyWrite)

		case existing.removed && edit.Type == funnel.EditTypeCreate:
			// Resurrection after a CREATE+DELETE cancellation. Replace
			// the canonical edit and seed the timeline with the new
			// CREATE's properties.
			existing.canonical = edit
			existing.canonical.Properties = nil
			existing.removed = false
			existing.timeline = make(map[string]propertyWrite, len(edit.Properties))
			for k, v := range edit.Properties {
				existing.timeline[k] = propertyWrite{value: v, position: pos}
			}

		case edit.Type == funnel.EditTypeCreate || edit.Type == funnel.EditTypeModify:
			// Property timeline LWW merge. CREATE+MODIFY keeps CREATE
			// (the existing canonical), MODIFY+MODIFY keeps MODIFY.
			// Source / Markings / EditVersion etc. on canonical are
			// preserved from whichever edit established the object.
			for k, v := range edit.Properties {
				existing.timeline[k] = propertyWrite{value: v, position: pos}
			}

		default:
			// Fallback for unexpected transitions: replace canonical
			// but keep the timeline as the cleanest snapshot we have.
			existing.canonical = edit
			existing.canonical.Properties = nil
		}
	}

	return objects, objectOrder, links, linkOrder
}

// materializeCollapsedEdits walks the tracker state in original-insert order
// and rebuilds a fresh Edit slice. Object edits come first, then link edits.
func materializeCollapsedEdits(
	objects map[editKey]*trackedEdit,
	objectOrder []editKey,
	links map[linkKey]*trackedLinkEdit,
	linkOrder []linkKey,
) []funnel.Edit {
	var result []funnel.Edit
	for _, key := range objectOrder {
		t := objects[key]
		if t.removed {
			continue
		}
		out := t.canonical
		if len(t.timeline) > 0 {
			props := make(map[string]interface{}, len(t.timeline))
			for k, w := range t.timeline {
				props[k] = w.value
			}
			out.Properties = props
		}
		result = append(result, out)
	}
	for _, lk := range linkOrder {
		t := links[lk]
		if t.removed {
			continue
		}
		result = append(result, t.edit)
	}
	return result
}
