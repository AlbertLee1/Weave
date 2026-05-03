package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fixedSource is the canonical fake IncrementalSource the tests below
// drive RunIncremental against. Schema is fixed at construction time;
// rows can be replayed on every ReadAfter to mimic a connector that
// keeps appending into a log.
type fixedSource struct {
	schema     []SchemaField
	rows       []IncrementalRow
	readErr    error
	schemaErr  error
	readCount  int
	readCutoff int64
}

func (s *fixedSource) Schema(_ context.Context) ([]SchemaField, error) {
	if s.schemaErr != nil {
		return nil, s.schemaErr
	}
	return s.schema, nil
}

func (s *fixedSource) ReadAfter(_ context.Context, after int64) ([]IncrementalRow, error) {
	s.readCount++
	s.readCutoff = after
	if s.readErr != nil {
		return nil, s.readErr
	}
	out := make([]IncrementalRow, 0, len(s.rows))
	for _, r := range s.rows {
		if r.Offset > after {
			out = append(out, r)
		}
	}
	return out, nil
}

// recordingSink captures each ApplyAddedColumns + WriteRows invocation
// so tests can assert on side-effect ordering and content.
type recordingSink struct {
	addedBatches [][]SchemaField
	rowBatches   [][]IncrementalRow
	addErr       error
	writeErr     error
}

func (s *recordingSink) ApplyAddedColumns(_ context.Context, added []SchemaField) error {
	if s.addErr != nil {
		return s.addErr
	}
	s.addedBatches = append(s.addedBatches, append([]SchemaField(nil), added...))
	return nil
}

func (s *recordingSink) WriteRows(_ context.Context, rows []IncrementalRow) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.rowBatches = append(s.rowBatches, append([]IncrementalRow(nil), rows...))
	return nil
}

// US-378 PRD acceptance gate: an APPEND-mode incremental run that
// follows a successful 100-row run only processes the next 100 rows.
func TestUS378_IncrementalRun_OnlyProcessesNewRows(t *testing.T) {
	rows := make([]IncrementalRow, 200)
	for i := 0; i < 200; i++ {
		rows[i] = IncrementalRow{Offset: int64(i + 1), Data: map[string]any{"id": i + 1}}
	}
	src := &fixedSource{
		schema: []SchemaField{{Name: "id", Type: "long"}},
		rows:   rows,
	}
	sink := &recordingSink{}
	res, err := RunIncremental(context.Background(), IncrementalRunOptions{
		LastCommittedOffset: 100,
		PriorSchema:         []SchemaField{{Name: "id", Type: "long"}},
		Source:              src,
		Sink:                sink,
	})
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}
	if res.ProcessedRows != 100 {
		t.Fatalf("ProcessedRows = %d, want 100", res.ProcessedRows)
	}
	if res.NewLastCommittedOffset != 200 {
		t.Fatalf("NewLastCommittedOffset = %d, want 200", res.NewLastCommittedOffset)
	}
	if len(sink.rowBatches) != 1 {
		t.Fatalf("WriteRows fired %d times, want 1", len(sink.rowBatches))
	}
	for _, r := range sink.rowBatches[0] {
		if r.Offset <= 100 {
			t.Fatalf("row offset %d <= cutoff 100 surfaced post-filter", r.Offset)
		}
	}
}

func TestUS378_IncrementalRun_FromZeroOnFirstRun(t *testing.T) {
	rows := []IncrementalRow{
		{Offset: 1, Data: map[string]any{"id": 1}},
		{Offset: 2, Data: map[string]any{"id": 2}},
		{Offset: 3, Data: map[string]any{"id": 3}},
	}
	src := &fixedSource{
		schema: []SchemaField{{Name: "id", Type: "long"}},
		rows:   rows,
	}
	res, err := RunIncremental(context.Background(), IncrementalRunOptions{
		LastCommittedOffset: 0,
		PriorSchema:         nil,
		Source:              src,
		Sink:                nil,
	})
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}
	if res.ProcessedRows != 3 {
		t.Fatalf("ProcessedRows = %d, want 3", res.ProcessedRows)
	}
	if res.NewLastCommittedOffset != 3 {
		t.Fatalf("NewLastCommittedOffset = %d, want 3", res.NewLastCommittedOffset)
	}
	if len(res.AddedColumns) != 1 || res.AddedColumns[0] != "id" {
		t.Fatalf("AddedColumns = %v, want [id]", res.AddedColumns)
	}
	if len(res.MergedSchema) != 1 || res.MergedSchema[0].Name != "id" {
		t.Fatalf("MergedSchema = %+v, want [{id long}]", res.MergedSchema)
	}
}

func TestUS378_IncrementalRun_NoNewRowsKeepsOffset(t *testing.T) {
	src := &fixedSource{
		schema: []SchemaField{{Name: "id", Type: "long"}},
		rows: []IncrementalRow{
			{Offset: 1, Data: map[string]any{"id": 1}},
		},
	}
	res, err := RunIncremental(context.Background(), IncrementalRunOptions{
		LastCommittedOffset: 50,
		PriorSchema:         []SchemaField{{Name: "id", Type: "long"}},
		Source:              src,
	})
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}
	if res.ProcessedRows != 0 {
		t.Fatalf("ProcessedRows = %d, want 0", res.ProcessedRows)
	}
	if res.NewLastCommittedOffset != 50 {
		t.Fatalf("NewLastCommittedOffset = %d, want 50", res.NewLastCommittedOffset)
	}
}

// US-378 PRD acceptance gate: schema diff detects new columns and adds
// them to the downstream index automatically.
func TestUS378_SchemaDiff_NewColumnAutoAdds(t *testing.T) {
	src := &fixedSource{
		schema: []SchemaField{
			{Name: "id", Type: "long"},
			{Name: "email", Type: "string"},
		},
		rows: []IncrementalRow{
			{Offset: 1, Data: map[string]any{"id": 1, "email": "a@x.com"}},
		},
	}
	sink := &recordingSink{}
	res, err := RunIncremental(context.Background(), IncrementalRunOptions{
		LastCommittedOffset: 0,
		PriorSchema:         []SchemaField{{Name: "id", Type: "long"}},
		Source:              src,
		Sink:                sink,
	})
	if err != nil {
		t.Fatalf("RunIncremental: %v", err)
	}
	if len(res.AddedColumns) != 1 || res.AddedColumns[0] != "email" {
		t.Fatalf("AddedColumns = %v, want [email]", res.AddedColumns)
	}
	if len(sink.addedBatches) != 1 {
		t.Fatalf("ApplyAddedColumns fired %d times, want 1", len(sink.addedBatches))
	}
	got := sink.addedBatches[0]
	if len(got) != 1 || got[0].Name != "email" || got[0].Type != "string" {
		t.Fatalf("ApplyAddedColumns batch = %+v, want [{email string}]", got)
	}
	// Merged schema preserves prior order then appends new columns.
	if len(res.MergedSchema) != 2 ||
		res.MergedSchema[0].Name != "id" ||
		res.MergedSchema[1].Name != "email" {
		t.Fatalf("MergedSchema = %+v, want [{id long} {email string}]", res.MergedSchema)
	}
}

// US-378 PRD acceptance gate: schema diff detects dropped columns and
// raises BREAKING_CHANGE before any row is read.
func TestUS378_SchemaDiff_DroppedColumnReturnsBreakingChange(t *testing.T) {
	src := &fixedSource{
		schema: []SchemaField{
			{Name: "id", Type: "long"},
		},
		rows: []IncrementalRow{
			{Offset: 1, Data: map[string]any{"id": 1}},
		},
	}
	sink := &recordingSink{}
	_, err := RunIncremental(context.Background(), IncrementalRunOptions{
		LastCommittedOffset: 0,
		PriorSchema: []SchemaField{
			{Name: "id", Type: "long"},
			{Name: "email", Type: "string"},
		},
		Source: src,
		Sink:   sink,
	})
	if err == nil {
		t.Fatal("RunIncremental: expected BREAKING_CHANGE error, got nil")
	}
	if !errors.Is(err, ErrSchemaBreakingChange) {
		t.Fatalf("err = %v, want ErrSchemaBreakingChange", err)
	}
	if !strings.Contains(err.Error(), "email") {
		t.Fatalf("err = %v, want it to mention dropped column 'email'", err)
	}
	// Critically: no rows must have been read, no data written, no
	// schema-extension hook fired — the run aborts BEFORE any side
	// effect lands.
	if src.readCount != 0 {
		t.Fatalf("ReadAfter fired %d times before BREAKING_CHANGE; want 0", src.readCount)
	}
	if len(sink.addedBatches) != 0 || len(sink.rowBatches) != 0 {
		t.Fatalf("sink mutations leaked through BREAKING_CHANGE: added=%d rows=%d", len(sink.addedBatches), len(sink.rowBatches))
	}
}

func TestUS378_SchemaDiff_TypeConflictReturnsBreakingChange(t *testing.T) {
	src := &fixedSource{
		schema: []SchemaField{
			{Name: "id", Type: "string"},
		},
	}
	_, err := RunIncremental(context.Background(), IncrementalRunOptions{
		LastCommittedOffset: 0,
		PriorSchema:         []SchemaField{{Name: "id", Type: "long"}},
		Source:              src,
	})
	if !errors.Is(err, ErrSchemaBreakingChange) {
		t.Fatalf("err = %v, want ErrSchemaBreakingChange", err)
	}
	if !strings.Contains(err.Error(), "long") || !strings.Contains(err.Error(), "string") {
		t.Fatalf("err = %v, want it to surface the conflicting types", err)
	}
}

func TestDiffSchemas_AdditionsAndDropsAndConflicts(t *testing.T) {
	prior := []SchemaField{
		{Name: "id", Type: "long"},
		{Name: "email", Type: "string"},
		{Name: "country", Type: "string"},
	}
	current := []SchemaField{
		{Name: "id", Type: "long"},
		{Name: "country", Type: "long"}, // type conflict
		{Name: "city", Type: "string"},  // added
	}
	diff := DiffSchemas(prior, current)
	if !diff.IsBreaking() {
		t.Fatal("IsBreaking = false, want true")
	}
	if len(diff.AddedColumns) != 1 || diff.AddedColumns[0].Name != "city" {
		t.Fatalf("AddedColumns = %+v, want [{city string}]", diff.AddedColumns)
	}
	if len(diff.DroppedColumns) != 1 || diff.DroppedColumns[0].Name != "email" {
		t.Fatalf("DroppedColumns = %+v, want [{email string}]", diff.DroppedColumns)
	}
	if len(diff.ConflictedColumns) != 1 || diff.ConflictedColumns[0].Name != "country" {
		t.Fatalf("ConflictedColumns = %+v, want [{country long}]", diff.ConflictedColumns)
	}
	if diff.MismatchedPriorTypes["country"] != "string" {
		t.Fatalf("MismatchedPriorTypes[country] = %q, want %q", diff.MismatchedPriorTypes["country"], "string")
	}
}

func TestDiffSchemas_TypeCaseInsensitive(t *testing.T) {
	prior := []SchemaField{{Name: "id", Type: "Long"}}
	current := []SchemaField{{Name: "id", Type: "LONG"}}
	diff := DiffSchemas(prior, current)
	if diff.IsBreaking() {
		t.Fatalf("Case-insensitive equal types must not register as conflict; diff = %+v", diff)
	}
}

func TestResolveSchemaEvolution_NoPriorYieldsAllAdds(t *testing.T) {
	current := []SchemaField{{Name: "id", Type: "long"}, {Name: "email", Type: "string"}}
	res, err := ResolveSchemaEvolution(nil, current)
	if err != nil {
		t.Fatalf("ResolveSchemaEvolution: %v", err)
	}
	if len(res.AddedColumns) != 2 {
		t.Fatalf("AddedColumns = %v, want 2 entries", res.AddedColumns)
	}
	if len(res.MergedSchema) != 2 {
		t.Fatalf("MergedSchema = %+v, want 2 entries", res.MergedSchema)
	}
}

func TestResolveSchemaEvolution_PreservesPriorOrder(t *testing.T) {
	prior := []SchemaField{
		{Name: "id", Type: "long"},
		{Name: "name", Type: "string"},
	}
	current := []SchemaField{
		{Name: "name", Type: "string"},
		{Name: "id", Type: "long"},
		{Name: "age", Type: "long"},
	}
	res, err := ResolveSchemaEvolution(prior, current)
	if err != nil {
		t.Fatalf("ResolveSchemaEvolution: %v", err)
	}
	// Prior columns must keep their input order; new columns append.
	if len(res.MergedSchema) != 3 {
		t.Fatalf("MergedSchema length = %d, want 3", len(res.MergedSchema))
	}
	if res.MergedSchema[0].Name != "id" || res.MergedSchema[1].Name != "name" || res.MergedSchema[2].Name != "age" {
		t.Fatalf("MergedSchema order = %+v, want [id name age]", res.MergedSchema)
	}
}

func TestPipeline_Validate_RejectsUnknownMode(t *testing.T) {
	p := newTestPipeline("demo")
	p.Mode = "STREAMING"
	if err := p.Validate(); err == nil {
		t.Fatal("Validate accepted unknown mode 'STREAMING'")
	}
}

func TestPipeline_Validate_AcceptsCanonicalModes(t *testing.T) {
	for _, mode := range []string{"", "FULL", "APPEND"} {
		p := newTestPipeline("demo-" + strings.ToLower(mode))
		p.ID = "demo"
		p.Mode = mode
		if err := p.Validate(); err != nil {
			t.Fatalf("Validate rejected mode %q: %v", mode, err)
		}
	}
}

func TestMemoryStore_LatestCommittedOffset_PicksMaxAcrossSuccessfulRuns(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	// One successful run at offset=100.
	r1 := newRunFixture("demo")
	r1.Status = "success"
	r1.LastCommittedOffset = 100
	if err := s.AppendPipelineRun(ctx, r1); err != nil {
		t.Fatalf("AppendPipelineRun r1: %v", err)
	}
	// One failed run at offset=200 — must NOT advance the watermark.
	r2 := newRunFixture("demo")
	r2.Status = "failed"
	r2.LastCommittedOffset = 200
	if err := s.AppendPipelineRun(ctx, r2); err != nil {
		t.Fatalf("AppendPipelineRun r2: %v", err)
	}
	// Another successful run at offset=150.
	r3 := newRunFixture("demo")
	r3.Status = "success"
	r3.LastCommittedOffset = 150
	if err := s.AppendPipelineRun(ctx, r3); err != nil {
		t.Fatalf("AppendPipelineRun r3: %v", err)
	}
	got, err := s.LatestCommittedOffset(ctx, "demo")
	if err != nil {
		t.Fatalf("LatestCommittedOffset: %v", err)
	}
	if got != 150 {
		t.Fatalf("LatestCommittedOffset = %d, want 150 (max across SUCCESSFUL runs)", got)
	}
}

func TestMemoryStore_LatestCommittedOffset_NoRunsReturnsZero(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	got, err := s.LatestCommittedOffset(ctx, "demo")
	if err != nil {
		t.Fatalf("LatestCommittedOffset: %v", err)
	}
	if got != 0 {
		t.Fatalf("LatestCommittedOffset = %d, want 0", got)
	}
}

func TestMemoryStore_LatestCommittedOffset_UnknownPipeline(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.LatestCommittedOffset(context.Background(), "ghost")
	if !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("err = %v, want ErrPipelineNotFound", err)
	}
}

func TestMemoryStore_UpdatePipeline_AcceptsModeAndSchema(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.CreatePipeline(ctx, newTestPipeline("demo"))
	mode := ModeAppend
	schema := []SchemaField{{Name: "id", Type: "long"}}
	if err := s.UpdatePipeline(ctx, "demo", PipelineUpdate{Mode: &mode, LastKnownSchema: &schema}); err != nil {
		t.Fatalf("UpdatePipeline: %v", err)
	}
	got, err := s.GetPipeline(ctx, "demo")
	if err != nil {
		t.Fatalf("GetPipeline: %v", err)
	}
	if got.Mode != ModeAppend {
		t.Fatalf("Mode = %q, want APPEND", got.Mode)
	}
	if len(got.LastKnownSchema) != 1 || got.LastKnownSchema[0].Name != "id" {
		t.Fatalf("LastKnownSchema = %+v, want [{id long}]", got.LastKnownSchema)
	}
}

// End-to-end gate: simulate two consecutive APPEND-mode runs against
// the same source. Second run's LastCommittedOffset comes from the
// MemoryStore — only the freshly-appended rows must surface to the sink.
func TestUS378_E2E_TwoSuccessiveAppendRuns_OnlySecondBatchProcessed(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	p := newTestPipeline("demo")
	p.Mode = ModeAppend
	if err := store.CreatePipeline(ctx, p); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	rows := func(start, count int) []IncrementalRow {
		out := make([]IncrementalRow, count)
		for i := 0; i < count; i++ {
			off := int64(start + i)
			out[i] = IncrementalRow{Offset: off, Data: map[string]any{"id": off}}
		}
		return out
	}

	// First run: full source has 100 rows.
	src1 := &fixedSource{schema: []SchemaField{{Name: "id", Type: "long"}}, rows: rows(1, 100)}
	off1, err := store.LatestCommittedOffset(ctx, "demo")
	if err != nil {
		t.Fatalf("LatestCommittedOffset r1: %v", err)
	}
	prior, _ := store.GetPipeline(ctx, "demo")
	res1, err := RunIncremental(ctx, IncrementalRunOptions{
		LastCommittedOffset: off1,
		PriorSchema:         prior.LastKnownSchema,
		Source:              src1,
	})
	if err != nil {
		t.Fatalf("RunIncremental r1: %v", err)
	}
	if res1.ProcessedRows != 100 {
		t.Fatalf("r1 processed %d rows, want 100", res1.ProcessedRows)
	}
	// Persist the run + advance the pipeline schema watermark.
	r1 := newRunFixture("demo")
	r1.Status = "success"
	r1.LastCommittedOffset = res1.NewLastCommittedOffset
	if err := store.AppendPipelineRun(ctx, r1); err != nil {
		t.Fatalf("AppendPipelineRun r1: %v", err)
	}
	merged := res1.MergedSchema
	if err := store.UpdatePipeline(ctx, "demo", PipelineUpdate{LastKnownSchema: &merged}); err != nil {
		t.Fatalf("UpdatePipeline r1 schema: %v", err)
	}

	// Second run: the source now has 200 rows (first 100 unchanged,
	// next 100 freshly appended). The PRD acceptance gate: the
	// second run only processes the new 100 rows.
	src2 := &fixedSource{schema: []SchemaField{{Name: "id", Type: "long"}}, rows: rows(1, 200)}
	off2, err := store.LatestCommittedOffset(ctx, "demo")
	if err != nil {
		t.Fatalf("LatestCommittedOffset r2: %v", err)
	}
	if off2 != 100 {
		t.Fatalf("LatestCommittedOffset after r1 = %d, want 100", off2)
	}
	prior2, _ := store.GetPipeline(ctx, "demo")
	res2, err := RunIncremental(ctx, IncrementalRunOptions{
		LastCommittedOffset: off2,
		PriorSchema:         prior2.LastKnownSchema,
		Source:              src2,
	})
	if err != nil {
		t.Fatalf("RunIncremental r2: %v", err)
	}
	if res2.ProcessedRows != 100 {
		t.Fatalf("r2 processed %d rows, want 100 (only freshly-appended rows)", res2.ProcessedRows)
	}
	if res2.NewLastCommittedOffset != 200 {
		t.Fatalf("r2 NewLastCommittedOffset = %d, want 200", res2.NewLastCommittedOffset)
	}
}
