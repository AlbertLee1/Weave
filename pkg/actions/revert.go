package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/funnel"
)

// AlreadyRevertedError is returned when a caller attempts to revert an
// ActionLog that has already been reverted. The HTTP handler converts this
// to a 409 Conflict response.
type AlreadyRevertedError struct {
	ActionLogID int64
}

func (e *AlreadyRevertedError) Error() string {
	return fmt.Sprintf("action log %d has already been reverted", e.ActionLogID)
}

// RevertResult is the response payload for a successful revert operation.
type RevertResult struct {
	ActionLogID int64         `json:"actionLogId"`
	Edits       []funnel.Edit `json:"edits"`
	BatchID     string        `json:"batchId,omitempty"`
}

// Revert undoes a previously executed action by loading its ActionLog,
// generating reverse edits from the recorded Edits and PrevEdits, and
// publishing the reverse EditBatch to NATS. The original ActionLog is
// marked as REVERTED; double-revert returns *AlreadyRevertedError.
func (e *Executor) Revert(ctx context.Context, ontologyAPIName string, actionLogID int64) (*RevertResult, error) {
	// Step 1: Load ActionLog by ID.
	al, err := e.omsRepo.GetActionLog(ctx, actionLogID)
	if err != nil {
		return nil, fmt.Errorf("load action log %d: %w", actionLogID, err)
	}

	// Step 2: Check for double-revert.
	if al.Status == "REVERTED" {
		return nil, &AlreadyRevertedError{ActionLogID: actionLogID}
	}

	// Step 3: Deserialize original edits.
	var edits []funnel.Edit
	if err := json.Unmarshal(al.Edits, &edits); err != nil {
		return nil, fmt.Errorf("unmarshal edits: %w", err)
	}

	// Step 4: Deserialize PrevEdits (may be null/empty for old action logs).
	var prevEdits []map[string]interface{}
	if len(al.PrevEdits) > 0 && string(al.PrevEdits) != "null" {
		if err := json.Unmarshal(al.PrevEdits, &prevEdits); err != nil {
			return nil, fmt.Errorf("unmarshal prev edits: %w", err)
		}
	}

	// Step 5: Generate reverse edits.
	reverseEdits := generateReverseEdits(edits, prevEdits)

	// Step 6: Publish reverse EditBatch.
	result := &RevertResult{
		ActionLogID: actionLogID,
		Edits:       reverseEdits,
	}

	if len(reverseEdits) > 0 && e.publisher != nil {
		batch := &funnel.EditBatch{
			ID:              uuid.New().String(),
			OntologyAPIName: ontologyAPIName,
			Edits:           reverseEdits,
			UserID:          al.UserID,
			Timestamp:       time.Now(),
		}
		if _, err := e.publisher.Publish(batch); err != nil {
			return nil, fmt.Errorf("publish reverse batch: %w", err)
		}
		result.BatchID = batch.ID
	}

	// Step 7: Mark ActionLog as REVERTED.
	if err := e.omsRepo.UpdateActionLogStatus(ctx, actionLogID, "REVERTED"); err != nil {
		return nil, fmt.Errorf("update action log status: %w", err)
	}

	return result, nil
}

// generateReverseEdits produces the inverse of each edit:
//   - CREATE → DELETE
//   - MODIFY → MODIFY with PrevState properties
//   - DELETE → CREATE with PrevState properties
//   - LINK_CREATE → LINK_DELETE
//   - LINK_DELETE → LINK_CREATE
func generateReverseEdits(edits []funnel.Edit, prevEdits []map[string]interface{}) []funnel.Edit {
	reverse := make([]funnel.Edit, 0, len(edits))

	for i, edit := range edits {
		var prev map[string]interface{}
		if i < len(prevEdits) {
			prev = prevEdits[i]
		}

		switch edit.Type {
		case funnel.EditTypeCreate:
			// CREATE → DELETE: remove the created object.
			reverse = append(reverse, funnel.Edit{
				Type:       funnel.EditTypeDelete,
				ObjectType: edit.ObjectType,
				PrimaryKey: edit.PrimaryKey,
				Source:     funnel.EditSourceUser,
			})

		case funnel.EditTypeModify:
			// MODIFY → MODIFY: restore previous properties.
			reverse = append(reverse, funnel.Edit{
				Type:       funnel.EditTypeModify,
				ObjectType: edit.ObjectType,
				PrimaryKey: edit.PrimaryKey,
				Properties: prev,
				Source:     funnel.EditSourceUser,
			})

		case funnel.EditTypeDelete:
			// DELETE → CREATE: recreate from previous state.
			reverse = append(reverse, funnel.Edit{
				Type:       funnel.EditTypeCreate,
				ObjectType: edit.ObjectType,
				PrimaryKey: edit.PrimaryKey,
				Properties: prev,
				Source:     funnel.EditSourceUser,
			})

		case funnel.EditTypeLinkCreate:
			// LINK_CREATE → LINK_DELETE
			reverse = append(reverse, funnel.Edit{
				Type:             funnel.EditTypeLinkDelete,
				PrimaryKey:       edit.PrimaryKey,
				LinkTypeRID:      edit.LinkTypeRID,
				TargetPrimaryKey: edit.TargetPrimaryKey,
				Source:           funnel.EditSourceUser,
			})

		case funnel.EditTypeLinkDelete:
			// LINK_DELETE → LINK_CREATE
			reverse = append(reverse, funnel.Edit{
				Type:             funnel.EditTypeLinkCreate,
				PrimaryKey:       edit.PrimaryKey,
				LinkTypeRID:      edit.LinkTypeRID,
				TargetPrimaryKey: edit.TargetPrimaryKey,
				Source:           funnel.EditSourceUser,
			})
		}
	}

	return reverse
}
