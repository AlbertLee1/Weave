package scenarios

import (
	"encoding/json"
	"sort"
)

// ConflictType enumerates the operator-visible fold conflict shapes that
// US-481 surfaces from FoldObjectWithConflicts / FoldLinksWithConflicts.
// Strings (not enums) so audit_events.diff_json reads cleanly in SIEM.
const (
	// ConflictModifyAfterDelete: a modifyProperty edit landed on an object
	// that was already removed by an earlier deleteObject in the same
	// scenario (and not re-created before this modify). The edit is dropped
	// by fold; the audit row tells the operator one of their writes was
	// silently masked.
	ConflictModifyAfterDelete = "modify_after_delete"

	// ConflictDuplicateCreate: two createObject edits for the same
	// (objectType, objectID) with no intervening deleteObject. Fold keeps
	// last-write-wins; the audit row flags that an earlier intended state
	// was overwritten.
	ConflictDuplicateCreate = "duplicate_create"

	// ConflictDeleteAfterDelete: two deleteObject edits for the same target.
	// Idempotent (the object stays deleted) but two independent delete
	// intents on the same key usually means two operators stepped on each
	// other; surface it.
	ConflictDeleteAfterDelete = "delete_after_delete"

	// ConflictDuplicateAddLink: addLink edit for an edge that already
	// exists in the base adjacency or was added earlier in the same fold.
	// Fold dedupes; audit captures the operator-visible "second add".
	ConflictDuplicateAddLink = "duplicate_add_link"

	// ConflictDeleteMissingLink: deleteLink edit for an edge that neither
	// exists in the base adjacency nor was added earlier in the fold. The
	// delete is a no-op; audit records the unmet intent.
	ConflictDeleteMissingLink = "delete_missing_link"
)

// ScenarioConflict captures one fold-time conflict for audit. The shape is
// flat (no nested values) so audit consumers can render rows without an extra
// JSON dive. LinkType / SrcID / DstID are populated for link-side conflicts
// and empty for object-side conflicts; ObjectType / ObjectID / Property are
// the converse.
type ScenarioConflict struct {
	ConflictType string  `json:"conflictType"`
	Op           string  `json:"op"`
	ObjectType   string  `json:"objectType,omitempty"`
	ObjectID     string  `json:"objectId,omitempty"`
	Property     string  `json:"property,omitempty"`
	LinkType     string  `json:"linkType,omitempty"`
	SrcID        string  `json:"srcId,omitempty"`
	DstID        string  `json:"dstId,omitempty"`
	EditSeqs     []int64 `json:"editSeqs"`
}

// FoldObjectWithConflicts is the US-481 superset of FoldObject. It returns
// the same (view, deleted) bits plus any conflicts detected while replaying
// edits over the target object. The detection rules:
//
//   - modifyProperty whose deleted-state-on-entry is true AND no createObject
//     re-creates the object before this edit → ConflictModifyAfterDelete
//   - createObject when the object is already live (either via base or via an
//     earlier create in this scenario, with no intervening deleteObject) →
//     ConflictDuplicateCreate
//   - deleteObject when the object is already deleted in this scenario (and
//     no createObject re-created it) → ConflictDeleteAfterDelete
//
// Edits are folded in seq order (a stable copy is sorted internally), and the
// returned conflicts preserve the seq order in which they were observed —
// callers can rely on that to render audit rows chronologically.
func FoldObjectWithConflicts(target ObjectKey, base *ObjectView, edits []ScenarioEdit) (*ObjectView, bool, []ScenarioConflict) {
	var view *ObjectView
	if base != nil && base.ObjectType == target.ObjectType && base.ObjectID == target.ObjectID {
		view = cloneObjectView(base)
	}
	deleted := false
	var conflicts []ScenarioConflict
	// lastDeleteSeq remembers which deleteObject we'd cite as the suppressor
	// in a modify-after-delete or delete-after-delete conflict. Reset on
	// createObject because a recreate clears the deleted state for the
	// purposes of subsequent edits.
	var lastDeleteSeq int64
	// lastCreateSeq similarly remembers the prior createObject so duplicate-
	// create conflicts cite both seqs. Reset on deleteObject.
	var lastCreateSeq int64
	objectAlreadyLive := view != nil

	sorted := append([]ScenarioEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	for _, e := range sorted {
		if e.ObjectType != target.ObjectType || e.ObjectID != target.ObjectID {
			continue
		}
		switch e.Op {
		case "createObject":
			if objectAlreadyLive {
				priorSeq := lastCreateSeq
				if priorSeq == 0 {
					// base-as-the-prior — there is no edit seq, so just cite
					// this edit. EditSeqs[len==1] signals "this create
					// shadowed the base view".
					conflicts = append(conflicts, ScenarioConflict{
						ConflictType: ConflictDuplicateCreate,
						Op:           "createObject",
						ObjectType:   target.ObjectType,
						ObjectID:     target.ObjectID,
						EditSeqs:     []int64{e.Seq},
					})
				} else {
					conflicts = append(conflicts, ScenarioConflict{
						ConflictType: ConflictDuplicateCreate,
						Op:           "createObject",
						ObjectType:   target.ObjectType,
						ObjectID:     target.ObjectID,
						EditSeqs:     []int64{priorSeq, e.Seq},
					})
				}
			}
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
			objectAlreadyLive = true
			lastCreateSeq = e.Seq
			lastDeleteSeq = 0
		case "modifyProperty":
			if view == nil || deleted {
				if lastDeleteSeq != 0 {
					conflicts = append(conflicts, ScenarioConflict{
						ConflictType: ConflictModifyAfterDelete,
						Op:           "modifyProperty",
						ObjectType:   target.ObjectType,
						ObjectID:     target.ObjectID,
						Property:     e.Property,
						EditSeqs:     []int64{lastDeleteSeq, e.Seq},
					})
				}
				continue
			}
			if view.Properties == nil {
				view.Properties = map[string]json.RawMessage{}
			}
			view.Properties[e.Property] = append(json.RawMessage(nil), e.NewValue...)
		case "deleteObject":
			if deleted {
				priorSeq := lastDeleteSeq
				if priorSeq == 0 {
					conflicts = append(conflicts, ScenarioConflict{
						ConflictType: ConflictDeleteAfterDelete,
						Op:           "deleteObject",
						ObjectType:   target.ObjectType,
						ObjectID:     target.ObjectID,
						EditSeqs:     []int64{e.Seq},
					})
				} else {
					conflicts = append(conflicts, ScenarioConflict{
						ConflictType: ConflictDeleteAfterDelete,
						Op:           "deleteObject",
						ObjectType:   target.ObjectType,
						ObjectID:     target.ObjectID,
						EditSeqs:     []int64{priorSeq, e.Seq},
					})
				}
			}
			view = nil
			deleted = true
			objectAlreadyLive = false
			lastDeleteSeq = e.Seq
			lastCreateSeq = 0
		}
	}

	if view == nil {
		return nil, deleted, conflicts
	}
	return view, false, conflicts
}

// FoldLinksWithConflicts is the US-481 superset of FoldLinks. It returns the
// same deduplicated, sorted adjacency plus conflicts for:
//
//   - addLink whose (LinkType, SrcID, DstID) already exists at the time the
//     edit is replayed → ConflictDuplicateAddLink
//   - deleteLink whose target edge does not exist at the time the edit is
//     replayed → ConflictDeleteMissingLink
//
// Conflicts are returned in the seq order in which they were observed.
func FoldLinksWithConflicts(base []LinkView, edits []ScenarioEdit) ([]LinkView, []ScenarioConflict) {
	type key struct{ linkType, src, dst string }
	set := make(map[key]struct{}, len(base))
	for _, l := range base {
		set[key{l.LinkType, l.SrcID, l.DstID}] = struct{}{}
	}

	sorted := append([]ScenarioEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	var conflicts []ScenarioConflict
	for _, e := range sorted {
		k := key{e.LinkType, e.SrcID, e.DstID}
		switch e.Op {
		case "addLink":
			if _, exists := set[k]; exists {
				conflicts = append(conflicts, ScenarioConflict{
					ConflictType: ConflictDuplicateAddLink,
					Op:           "addLink",
					LinkType:     e.LinkType,
					SrcID:        e.SrcID,
					DstID:        e.DstID,
					EditSeqs:     []int64{e.Seq},
				})
			}
			set[k] = struct{}{}
		case "deleteLink":
			if _, exists := set[k]; !exists {
				conflicts = append(conflicts, ScenarioConflict{
					ConflictType: ConflictDeleteMissingLink,
					Op:           "deleteLink",
					LinkType:     e.LinkType,
					SrcID:        e.SrcID,
					DstID:        e.DstID,
					EditSeqs:     []int64{e.Seq},
				})
			}
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
	return out, conflicts
}
