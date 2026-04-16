package actions

import "github.com/liyang/weave/pkg/funnel"

// CollapseEdits optimizes a list of edits by collapsing redundant operations.
func CollapseEdits(edits []funnel.Edit) []funnel.Edit {
	type editKey struct {
		objectType string
		primaryKey string
	}

	// Track edits by their key
	type trackedEdit struct {
		edit    funnel.Edit
		removed bool
	}

	tracked := make(map[editKey]*trackedEdit)
	var order []editKey // maintain order

	// Track link edits separately — keyed by (linkTypeRID, sourcePK, targetPK).
	type linkKey struct {
		linkTypeRID string
		sourcePK    string
		targetPK    string
	}
	type trackedLinkEdit struct {
		edit    funnel.Edit
		removed bool
	}
	linkTracked := make(map[linkKey]*trackedLinkEdit)
	var linkOrder []linkKey

	for _, edit := range edits {
		// Route link edits to the separate tracker.
		if edit.Type == funnel.EditTypeLinkCreate || edit.Type == funnel.EditTypeLinkDelete {
			lk := linkKey{edit.LinkTypeRID, edit.PrimaryKey, edit.TargetPrimaryKey}
			existing, exists := linkTracked[lk]
			if !exists {
				linkTracked[lk] = &trackedLinkEdit{edit: edit}
				linkOrder = append(linkOrder, lk)
			} else {
				switch {
				case existing.edit.Type == funnel.EditTypeLinkCreate && edit.Type == funnel.EditTypeLinkDelete:
					// LINK_CREATE + LINK_DELETE = cancel both
					existing.removed = true
				case existing.edit.Type == funnel.EditTypeLinkDelete && edit.Type == funnel.EditTypeLinkCreate:
					// LINK_DELETE + LINK_CREATE = last one wins (recreate)
					existing.edit = edit
				// Duplicate LINK_CREATE or LINK_DELETE on same triple → keep first (idempotent).
				}
			}
			continue
		}

		key := editKey{edit.ObjectType, edit.PrimaryKey}
		existing, exists := tracked[key]

		if !exists {
			tracked[key] = &trackedEdit{edit: edit}
			order = append(order, key)
			continue
		}

		switch {
		case existing.edit.Type == funnel.EditTypeCreate && edit.Type == funnel.EditTypeDelete:
			// CREATE + DELETE = cancel both
			existing.removed = true

		case existing.edit.Type == funnel.EditTypeCreate && edit.Type == funnel.EditTypeModify:
			// CREATE + MODIFY = CREATE with merged properties
			for k, v := range edit.Properties {
				existing.edit.Properties[k] = v
			}

		case existing.edit.Type == funnel.EditTypeModify && edit.Type == funnel.EditTypeModify:
			// MODIFY + MODIFY = single MODIFY with merged properties
			for k, v := range edit.Properties {
				existing.edit.Properties[k] = v
			}

		case existing.edit.Type == funnel.EditTypeModify && edit.Type == funnel.EditTypeDelete:
			// MODIFY + DELETE = just DELETE
			existing.edit = edit

		default:
			// Replace with latest
			existing.edit = edit
		}
	}

	// Collect results in order: object edits first, then link edits.
	var result []funnel.Edit
	for _, key := range order {
		if t := tracked[key]; !t.removed {
			result = append(result, t.edit)
		}
	}
	for _, lk := range linkOrder {
		if t := linkTracked[lk]; !t.removed {
			result = append(result, t.edit)
		}
	}

	return result
}
