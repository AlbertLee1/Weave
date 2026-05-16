//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/pipeline"
)

// US-484 BDD — Pipeline 增量构建（offset + schema evolution）.
//
// PRD acceptance criteria:
//   - pipeline_runs 表加 offset 字段
//   - schema diff 检测：上游加列 → 下游 schema 自动扩展
//   - 测试：模拟增量 + 加列两种场景
//
// The PRD-literal implementation already landed in US-378 (migrations
// 000089_pipeline_incremental adds pipeline_runs.last_committed_offset
// and pipelines.last_known_schema; pkg/pipeline.RunIncremental drives
// the schema-evolution gate; cmd/server.pgPipelineStore round-trips
// both columns). This BDD lives at the PG-roundtrip layer to lock the
// PRD-literal acceptance against:
//
//   1. The persisted column actually exists and is read back via the
//      production Store path (NOT the MemoryStore unit-test fixture).
//   2. A second APPEND run actually skips the rows the first run
//      processed by consulting LatestCommittedOffset on PG.
//   3. Source-side column adds propagate into pipelines.last_known_schema
//      JSONB after a successful APPEND run.
//
// A negative-control scenario (DROP column → BREAKING_CHANGE leaves the
// PG-persisted schema untouched) prevents an always-pass regression
// where ResolveSchemaEvolution silently accepts drops in the future.

// us484Pipeline returns a minimal APPEND-mode pipeline fixture suitable
// for the pgPipelineStore round-trip.
func us484Pipeline(id string, prior []pipeline.SchemaField) *pipeline.Pipeline {
	return &pipeline.Pipeline{
		ID: id,
		Inputs: []pipeline.Input{
			{Name: "src", Type: "objectset"},
		},
		Outputs: []pipeline.Output{
			{Name: "sink", Type: "jdbc", Input: "src"},
		},
		Enabled:         true,
		Mode:            pipeline.ModeAppend,
		LastKnownSchema: prior,
		CreatedBy:       "user:alice",
	}
}

// us484FixedSource is the canonical fake IncrementalSource the
// scenarios drive RunIncremental against. Same shape as the existing
// fixedSource in pkg/pipeline; redeclared here because that one is
// package-private to pkg/pipeline.
type us484FixedSource struct {
	schema []pipeline.SchemaField
	rows   []pipeline.IncrementalRow
}

func (s *us484FixedSource) Schema(_ context.Context) ([]pipeline.SchemaField, error) {
	return s.schema, nil
}

func (s *us484FixedSource) ReadAfter(_ context.Context, after int64) ([]pipeline.IncrementalRow, error) {
	out := make([]pipeline.IncrementalRow, 0, len(s.rows))
	for _, r := range s.rows {
		if r.Offset > after {
			out = append(out, r)
		}
	}
	return out, nil
}

// us484Rows builds a contiguous offset 1..count slice of rows.
func us484Rows(count int) []pipeline.IncrementalRow {
	out := make([]pipeline.IncrementalRow, count)
	for i := 0; i < count; i++ {
		off := int64(i + 1)
		out[i] = pipeline.IncrementalRow{Offset: off, Data: map[string]any{"id": off}}
	}
	return out
}

// readPipelineRunOffsetRaw reads pipeline_runs.last_committed_offset
// via raw SQL so the assertion is impossible to satisfy by mutating the
// Go-side struct alone — the column must actually exist on the table
// AND the INSERT must have populated it.
func readPipelineRunOffsetRaw(t *testing.T, pg *testutil.PGContainer, runID int64) int64 {
	t.Helper()
	var got int64
	if err := pg.Pool.QueryRow(context.Background(),
		`SELECT last_committed_offset FROM pipeline_runs WHERE id = $1`, runID).Scan(&got); err != nil {
		t.Fatalf("raw SQL read pipeline_runs.last_committed_offset: %v", err)
	}
	return got
}

// readPipelineSchemaRaw reads pipelines.last_known_schema via raw SQL
// for the same reason: the column must be persisted, not just mirrored
// in Go memory.
func readPipelineSchemaRaw(t *testing.T, pg *testutil.PGContainer, pipelineID string) []pipeline.SchemaField {
	t.Helper()
	var raw []byte
	if err := pg.Pool.QueryRow(context.Background(),
		`SELECT last_known_schema FROM pipelines WHERE id = $1`, pipelineID).Scan(&raw); err != nil {
		t.Fatalf("raw SQL read pipelines.last_known_schema: %v", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var fields []pipeline.SchemaField
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal last_known_schema: %v", err)
	}
	return fields
}

// TestBDD_US484_IncrementalRun_OnlyProcessesNewRows_PGRoundTrip is the
// PRD-literal "模拟增量" acceptance gate, run end-to-end against the
// production pgPipelineStore.
//
// Given a PG-backed pipeline whose prior successful APPEND run committed
// offset=100
//
// When a second APPEND run drives RunIncremental against a source
// whose append-log now has 200 rows
//
// Then only the freshly-appended 100 rows surface to the sink AND
//
//	the run row PERSISTS last_committed_offset=200 to PG (verified
//	by raw SELECT — not the Go struct round-trip) AND
//	LatestCommittedOffset(ctx, pipelineID) returns 200 for the next run.
func TestBDD_US484_IncrementalRun_OnlyProcessesNewRows_PGRoundTrip(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := newPGPipelineStore(pg.Pool)

	// Given: an APPEND-mode pipeline persisted in PG.
	pipelineID := "us484-incremental"
	prior := []pipeline.SchemaField{{Name: "id", Type: "long"}}
	if err := store.CreatePipeline(ctx, us484Pipeline(pipelineID, prior)); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	// And a prior successful run at offset=100.
	r1 := &pipeline.PipelineRun{
		PipelineID:          pipelineID,
		Status:              "success",
		StartedAt:           time.Now().UTC().Add(-time.Hour),
		TriggeredBy:         "user:alice",
		LastCommittedOffset: 100,
	}
	if err := store.AppendPipelineRun(ctx, r1); err != nil {
		t.Fatalf("AppendPipelineRun r1: %v", err)
	}

	// And the persisted offset is visible via the raw SQL column.
	if got := readPipelineRunOffsetRaw(t, pg, r1.ID); got != 100 {
		t.Fatalf("pipeline_runs.last_committed_offset (raw SQL) = %d, want 100", got)
	}

	// And the production LatestCommittedOffset path returns 100.
	off, err := store.LatestCommittedOffset(ctx, pipelineID)
	if err != nil {
		t.Fatalf("LatestCommittedOffset: %v", err)
	}
	if off != 100 {
		t.Fatalf("LatestCommittedOffset = %d, want 100", off)
	}

	// When: a second run drives RunIncremental against a 200-row source.
	src := &us484FixedSource{
		schema: []pipeline.SchemaField{{Name: "id", Type: "long"}},
		rows:   us484Rows(200),
	}
	pln, err := store.GetPipeline(ctx, pipelineID)
	if err != nil {
		t.Fatalf("GetPipeline: %v", err)
	}
	res, err := pipeline.RunIncremental(ctx, pipeline.IncrementalRunOptions{
		LastCommittedOffset: off,
		PriorSchema:         pln.LastKnownSchema,
		Source:              src,
	})
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}

	// Then: only freshly-appended rows (offsets 101..200) are processed.
	if res.ProcessedRows != 100 {
		t.Fatalf("ProcessedRows = %d, want 100 (rows 101..200 only)", res.ProcessedRows)
	}
	if res.NewLastCommittedOffset != 200 {
		t.Fatalf("NewLastCommittedOffset = %d, want 200", res.NewLastCommittedOffset)
	}

	// And the run record persists the advanced offset to PG.
	r2 := &pipeline.PipelineRun{
		PipelineID:          pipelineID,
		Status:              "success",
		StartedAt:           time.Now().UTC(),
		TriggeredBy:         "user:alice",
		LastCommittedOffset: res.NewLastCommittedOffset,
	}
	if err := store.AppendPipelineRun(ctx, r2); err != nil {
		t.Fatalf("AppendPipelineRun r2: %v", err)
	}
	if got := readPipelineRunOffsetRaw(t, pg, r2.ID); got != 200 {
		t.Fatalf("pipeline_runs.last_committed_offset (raw SQL) after r2 = %d, want 200", got)
	}

	// And LatestCommittedOffset now reads 200 for any subsequent run.
	off2, err := store.LatestCommittedOffset(ctx, pipelineID)
	if err != nil {
		t.Fatalf("LatestCommittedOffset r2: %v", err)
	}
	if off2 != 200 {
		t.Fatalf("LatestCommittedOffset after r2 = %d, want 200", off2)
	}
}

// TestBDD_US484_FailedRun_DoesNotAdvanceLatestCommittedOffset is the
// negative-control for the increment scenario. Without it, a regression
// that ignored Status='success' in the MAX() query would silently let
// failed runs leak progress into the next run's read cutoff — the
// positive scenario above would still pass because it only has
// successful runs.
//
// Given a prior failed run at offset=999 AND a prior successful run
// at offset=100,
//
// Then LatestCommittedOffset returns 100, not 999.
func TestBDD_US484_FailedRun_DoesNotAdvanceLatestCommittedOffset(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := newPGPipelineStore(pg.Pool)

	pipelineID := "us484-failed-no-advance"
	if err := store.CreatePipeline(ctx, us484Pipeline(pipelineID, nil)); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	successRun := &pipeline.PipelineRun{
		PipelineID:          pipelineID,
		Status:              "success",
		StartedAt:           time.Now().UTC().Add(-time.Hour),
		TriggeredBy:         "user:alice",
		LastCommittedOffset: 100,
	}
	if err := store.AppendPipelineRun(ctx, successRun); err != nil {
		t.Fatalf("AppendPipelineRun success: %v", err)
	}
	failedRun := &pipeline.PipelineRun{
		PipelineID:          pipelineID,
		Status:              "failed",
		StartedAt:           time.Now().UTC(),
		TriggeredBy:         "user:alice",
		LastCommittedOffset: 999,
		ErrorMessage:        "synthetic failure for BDD",
	}
	if err := store.AppendPipelineRun(ctx, failedRun); err != nil {
		t.Fatalf("AppendPipelineRun failed: %v", err)
	}

	off, err := store.LatestCommittedOffset(ctx, pipelineID)
	if err != nil {
		t.Fatalf("LatestCommittedOffset: %v", err)
	}
	if off != 100 {
		t.Fatalf("LatestCommittedOffset = %d, want 100 (failed run must NOT advance)", off)
	}
}

// TestBDD_US484_SchemaEvolution_AddColumn_PersistsToPG is the PRD-literal
// "加列" acceptance gate. The source grows a new column between runs;
// the run merges it into pipelines.last_known_schema without operator
// intervention, and the persisted JSONB column reflects the merge.
//
// Given an APPEND-mode pipeline whose persisted last_known_schema is
//
//	[{id, long}]
//
// When the upstream source schema becomes [{id, long}, {email, string}]
//
//	And RunIncremental drives the run
//	And the runner persists the merged schema back via UpdatePipeline
//
// Then ResolveSchemaEvolution AddedColumns = [email]
//
//	And pipelines.last_known_schema (read via RAW SQL) ==
//	    [{id, long}, {email, string}] in the same order — prior columns
//	    preserved, new columns appended.
func TestBDD_US484_SchemaEvolution_AddColumn_PersistsToPG(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := newPGPipelineStore(pg.Pool)

	pipelineID := "us484-add-column"
	prior := []pipeline.SchemaField{{Name: "id", Type: "long"}}
	if err := store.CreatePipeline(ctx, us484Pipeline(pipelineID, prior)); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	// Sanity: raw SQL read of the seeded schema.
	if got := readPipelineSchemaRaw(t, pg, pipelineID); len(got) != 1 || got[0].Name != "id" {
		t.Fatalf("seeded last_known_schema (raw SQL) = %+v, want [{id long}]", got)
	}

	// Source now exposes a freshly-added "email" column.
	src := &us484FixedSource{
		schema: []pipeline.SchemaField{
			{Name: "id", Type: "long"},
			{Name: "email", Type: "string"},
		},
		rows: []pipeline.IncrementalRow{
			{Offset: 1, Data: map[string]any{"id": int64(1), "email": "alice@example.com"}},
		},
	}

	res, err := pipeline.RunIncremental(ctx, pipeline.IncrementalRunOptions{
		LastCommittedOffset: 0,
		PriorSchema:         prior,
		Source:              src,
	})
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}
	if len(res.AddedColumns) != 1 || res.AddedColumns[0] != "email" {
		t.Fatalf("AddedColumns = %v, want [email]", res.AddedColumns)
	}
	if len(res.MergedSchema) != 2 ||
		res.MergedSchema[0].Name != "id" ||
		res.MergedSchema[1].Name != "email" {
		t.Fatalf("MergedSchema = %+v, want [{id long} {email string}] (prior preserved, new appended)", res.MergedSchema)
	}

	// Persist the merged schema via the production Store path.
	merged := res.MergedSchema
	if err := store.UpdatePipeline(ctx, pipelineID, pipeline.PipelineUpdate{LastKnownSchema: &merged}); err != nil {
		t.Fatalf("UpdatePipeline merged schema: %v", err)
	}

	// Raw SQL verifies the JSONB column was actually written, in order.
	persisted := readPipelineSchemaRaw(t, pg, pipelineID)
	if len(persisted) != 2 {
		t.Fatalf("persisted last_known_schema length = %d, want 2 (%+v)", len(persisted), persisted)
	}
	if persisted[0].Name != "id" || persisted[0].Type != "long" {
		t.Fatalf("persisted[0] = %+v, want {id long}", persisted[0])
	}
	if persisted[1].Name != "email" || persisted[1].Type != "string" {
		t.Fatalf("persisted[1] = %+v, want {email string}", persisted[1])
	}
}

// TestBDD_US484_SchemaEvolution_DropColumn_BreakingChange_PreservesPG
// is the negative-control for the schema-evolution scenario. Without
// it, a regression that silently accepted column drops would let the
// positive add-column scenario pass while quietly losing downstream
// queryability for the dropped column.
//
// Given an APPEND-mode pipeline whose persisted last_known_schema is
//
//	[{id, long}, {email, string}]
//
// When the upstream source removes "email"
//
// Then RunIncremental returns ErrSchemaBreakingChange
//
//	And pipelines.last_known_schema in PG is UNCHANGED (the run aborts
//	before any side effect — including the schema write — lands).
func TestBDD_US484_SchemaEvolution_DropColumn_BreakingChange_PreservesPG(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := newPGPipelineStore(pg.Pool)

	pipelineID := "us484-drop-column"
	prior := []pipeline.SchemaField{
		{Name: "id", Type: "long"},
		{Name: "email", Type: "string"},
	}
	if err := store.CreatePipeline(ctx, us484Pipeline(pipelineID, prior)); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	// Source drops "email".
	src := &us484FixedSource{
		schema: []pipeline.SchemaField{{Name: "id", Type: "long"}},
		rows:   []pipeline.IncrementalRow{{Offset: 1, Data: map[string]any{"id": int64(1)}}},
	}

	_, err := pipeline.RunIncremental(ctx, pipeline.IncrementalRunOptions{
		LastCommittedOffset: 0,
		PriorSchema:         prior,
		Source:              src,
	})
	if err == nil {
		t.Fatal("RunIncremental: expected BREAKING_CHANGE error, got nil")
	}
	if !errors.Is(err, pipeline.ErrSchemaBreakingChange) {
		t.Fatalf("err = %v, want wrap of ErrSchemaBreakingChange", err)
	}

	// PG schema must remain untouched (no UpdatePipeline call was made
	// — the runner aborts the run before persisting anything).
	persisted := readPipelineSchemaRaw(t, pg, pipelineID)
	if len(persisted) != 2 {
		t.Fatalf("persisted last_known_schema length = %d, want 2 (drop scenario must NOT mutate PG)", len(persisted))
	}
	if persisted[0].Name != "id" || persisted[1].Name != "email" {
		t.Fatalf("persisted last_known_schema = %+v, want unchanged [{id long} {email string}]", persisted)
	}
}
