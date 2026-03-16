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

	for _, edit := range edits {
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

	// Collect results in order
	var result []funnel.Edit
	for _, key := range order {
		if t := tracked[key]; !t.removed {
			result = append(result, t.edit)
		}
	}

	return result
}
