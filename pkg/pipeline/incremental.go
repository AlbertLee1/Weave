package pipeline

import (
	"context"
	"errors"
	"fmt"
)

// IncrementalRow is one row a source emits during an APPEND-mode read.
// Offset is the per-source monotonic high-water mark — typically a
// 1-based row counter, an LSN, a Kafka offset, or a clock-stamped
// auto-increment id. The runtime treats any int64 ordering as opaque;
// the only invariant is that the next-run start point is "Offset >
// last_committed_offset".
type IncrementalRow struct {
	Offset int64
	Data   map[string]any
}

// IncrementalSource is the narrow interface an APPEND-mode connector
// implements so RunIncremental can drive it without knowing the
// underlying transport (CSV / JDBC / Kafka / objectset).
//
// Schema returns the source's CURRENT column shape — the runtime diffs
// it against pipeline.LastKnownSchema before reading any data so a
// breaking change short-circuits the run before the source allocates
// large result buffers.
//
// ReadAfter returns every row whose Offset is strictly greater than
// after, in ascending Offset order. Connectors that cannot natively
// scope to "after this offset" must filter client-side. The contract
// is "everything appended since"; partial pages are not supported in
// this US (US-378 keeps the executor simple — pagination lives in the
// connector layer if a future US needs it).
type IncrementalSource interface {
	Schema(ctx context.Context) ([]SchemaField, error)
	ReadAfter(ctx context.Context, after int64) ([]IncrementalRow, error)
}

// IncrementalSink consumes the rows the source produced AND the names
// of newly-added columns the runtime should fold into the downstream
// index. A connector implementation typically (a) extends its index
// schema with AddedColumns, then (b) writes the rows.
type IncrementalSink interface {
	ApplyAddedColumns(ctx context.Context, added []SchemaField) error
	WriteRows(ctx context.Context, rows []IncrementalRow) error
}

// IncrementalRunOptions controls one RunIncremental invocation.
//
// LastCommittedOffset is the prior-run high-water mark — the pipeline
// store's LatestCommittedOffset(pipelineId) result. RunIncremental will
// only forward rows whose Offset is strictly greater than this value.
//
// PriorSchema is pipelines.last_known_schema. nil/empty is treated as
// "no schema known yet": every column observed becomes an addition.
type IncrementalRunOptions struct {
	LastCommittedOffset int64
	PriorSchema         []SchemaField
	Source              IncrementalSource
	Sink                IncrementalSink
}

// IncrementalRunResult is the outcome of one RunIncremental call.
//
// NewLastCommittedOffset is the offset to persist on the run row; it
// equals max(LastCommittedOffset, max(row.Offset)) so a no-op run
// preserves the prior progress.
//
// MergedSchema is the post-run pipelines.last_known_schema: prior
// columns preserved in original order, freshly-added columns appended
// in observed order.
//
// AddedColumns is the list of column names that were newly added —
// useful for the audit log.
type IncrementalRunResult struct {
	ProcessedRows          int
	NewLastCommittedOffset int64
	MergedSchema           []SchemaField
	AddedColumns           []string
}

// RunIncremental drives one APPEND-mode pipeline run end-to-end:
//
//  1. fetch the current source schema
//  2. diff against opts.PriorSchema; abort on breaking change
//  3. read rows after opts.LastCommittedOffset
//  4. extend the sink's schema with any added columns
//  5. write the rows
//  6. return the post-run progress + schema
//
// Sink is optional — pass nil to perform a "preview" run that resolves
// the schema diff and pulls the row delta without persisting anything.
// Source is required.
func RunIncremental(ctx context.Context, opts IncrementalRunOptions) (*IncrementalRunResult, error) {
	if opts.Source == nil {
		return nil, errors.New("pipeline: IncrementalRunOptions.Source is required")
	}
	currentSchema, err := opts.Source.Schema(ctx)
	if err != nil {
		return nil, fmt.Errorf("pipeline: source schema: %w", err)
	}
	resolution, err := ResolveSchemaEvolution(opts.PriorSchema, currentSchema)
	if err != nil {
		return nil, err
	}
	rows, err := opts.Source.ReadAfter(ctx, opts.LastCommittedOffset)
	if err != nil {
		return nil, fmt.Errorf("pipeline: source read: %w", err)
	}
	// Defensive client-side filter — a connector that can't natively
	// scope by offset must not silently re-process rows below the
	// watermark.
	rows = filterAboveOffset(rows, opts.LastCommittedOffset)
	maxOffset := opts.LastCommittedOffset
	for _, r := range rows {
		if r.Offset > maxOffset {
			maxOffset = r.Offset
		}
	}
	if opts.Sink != nil {
		if err := opts.Sink.ApplyAddedColumns(ctx, addedSchemaFields(resolution.AddedColumns, currentSchema)); err != nil {
			return nil, fmt.Errorf("pipeline: apply added columns: %w", err)
		}
		if err := opts.Sink.WriteRows(ctx, rows); err != nil {
			return nil, fmt.Errorf("pipeline: write rows: %w", err)
		}
	}
	return &IncrementalRunResult{
		ProcessedRows:          len(rows),
		NewLastCommittedOffset: maxOffset,
		MergedSchema:           resolution.MergedSchema,
		AddedColumns:           resolution.AddedColumns,
	}, nil
}

// filterAboveOffset drops every row whose Offset is <= cutoff.
func filterAboveOffset(rows []IncrementalRow, cutoff int64) []IncrementalRow {
	out := rows[:0]
	for _, r := range rows {
		if r.Offset > cutoff {
			out = append(out, r)
		}
	}
	return out
}

// addedSchemaFields resolves the AddedColumns name list back to the
// SchemaField shape via the CURRENT schema (the source of truth on the
// new columns' types).
func addedSchemaFields(added []string, current []SchemaField) []SchemaField {
	if len(added) == 0 {
		return nil
	}
	currentByName := make(map[string]SchemaField, len(current))
	for _, f := range current {
		currentByName[f.Name] = f
	}
	out := make([]SchemaField, 0, len(added))
	for _, name := range added {
		if f, ok := currentByName[name]; ok {
			out = append(out, f)
		}
	}
	return out
}
