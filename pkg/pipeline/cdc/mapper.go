package cdc

import (
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/funnel"
)

// EventToEdit converts one ChangeEvent into a funnel.Edit using the
// supplied mapping. The Source field on the result is always
// EditSourceIngest because CDC streams ingested external state — the
// funnel consumer's user-edit-wins conflict resolver (US-021) treats
// these edits as background ingestion and refuses to overwrite a
// concurrent user write made after the CDC event's timestamp.
//
// Returns an error when:
//   - the event's Op is unknown,
//   - the mapping cannot resolve a primary key from the change event
//     (missing PK column, or after/before tuple is empty for the
//     applicable op),
//   - mapping.Validate() rejects the mapping (caller should validate
//     once at config-load time and avoid revalidating per-event, but
//     EventToEdit double-checks the most load-bearing invariants so a
//     misconfigured row never produces a malformed edit).
func EventToEdit(event *ChangeEvent, mapping TableMapping) (funnel.Edit, error) {
	if event == nil {
		return funnel.Edit{}, errors.New("cdc: ChangeEvent is nil")
	}
	if mapping.ObjectType == "" {
		return funnel.Edit{}, errors.New("cdc: TableMapping.ObjectType must not be empty")
	}
	switch event.Op {
	case ChangeOpInsert:
		return insertEdit(event, mapping)
	case ChangeOpUpdate:
		return updateEdit(event, mapping)
	case ChangeOpDelete:
		return deleteEdit(event, mapping)
	default:
		return funnel.Edit{}, fmt.Errorf("cdc: unknown ChangeOp %q", event.Op)
	}
}

func insertEdit(event *ChangeEvent, mapping TableMapping) (funnel.Edit, error) {
	if len(event.After) == 0 {
		return funnel.Edit{}, errors.New("cdc: INSERT event has no After tuple")
	}
	pk, err := mapping.PrimaryKeyFor(event.After)
	if err != nil {
		return funnel.Edit{}, err
	}
	return funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: mapping.ObjectType,
		PrimaryKey: pk,
		Properties: collectProperties(event.After, mapping, false),
		Source:     funnel.EditSourceIngest,
	}, nil
}

func updateEdit(event *ChangeEvent, mapping TableMapping) (funnel.Edit, error) {
	if len(event.After) == 0 {
		return funnel.Edit{}, errors.New("cdc: UPDATE event has no After tuple")
	}
	pk, err := mapping.PrimaryKeyFor(event.After)
	if err != nil {
		return funnel.Edit{}, err
	}
	return funnel.Edit{
		Type:       funnel.EditTypeModify,
		ObjectType: mapping.ObjectType,
		PrimaryKey: pk,
		Properties: collectProperties(event.After, mapping, mapping.IncludeNullProperties),
		Source:     funnel.EditSourceIngest,
	}, nil
}

func deleteEdit(event *ChangeEvent, mapping TableMapping) (funnel.Edit, error) {
	tuple := event.Before
	if len(tuple) == 0 {
		// REPLICA IDENTITY DEFAULT only carries key columns, but
		// pglogrepl normalises that into Before with the key columns
		// populated. If the event has neither tuple, fall back to
		// After (some logical decoders fill it for DELETEs that
		// arrive with the row still in the WAL buffer).
		tuple = event.After
	}
	if len(tuple) == 0 {
		return funnel.Edit{}, errors.New("cdc: DELETE event has no Before/After tuple")
	}
	pk, err := mapping.PrimaryKeyFor(tuple)
	if err != nil {
		return funnel.Edit{}, err
	}
	return funnel.Edit{
		Type:       funnel.EditTypeDelete,
		ObjectType: mapping.ObjectType,
		PrimaryKey: pk,
		Source:     funnel.EditSourceIngest,
	}, nil
}

// collectProperties projects the change-event tuple through the
// mapping's property column filter. NULL values are emitted as nil
// only when includeNulls is true; otherwise they are dropped.
//
// The funnel consumer's filterWritableProperties downstream will drop
// any property the OMS doesn't recognise, so a mapping that lists more
// columns than the destination ObjectType declares is harmless — the
// extra fields are silently ignored at apply time.
func collectProperties(tuple map[string]string, mapping TableMapping, includeNulls bool) map[string]interface{} {
	if len(mapping.PropertyColumns) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(mapping.PropertyColumns))
	for col, prop := range mapping.PropertyColumns {
		raw, ok := tuple[col]
		if !ok {
			// Column absent from the tuple (TOASTed / unchanged). Skip
			// silently — the consumer's "no key, no change" semantics
			// preserve the existing indexed value.
			continue
		}
		if raw == nullSentinel {
			if includeNulls {
				out[prop] = nil
			}
			continue
		}
		out[prop] = raw
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
