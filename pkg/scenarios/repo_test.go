//go:build integration

package scenarios_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/scenarios"
)

// newRepo brings up a fresh PG container, runs migrations, seeds an ontology,
// and returns a wired ScenarioRepo together with the ontology RID. All
// integration tests in this package share this fixture so the per-case
// container cost (~1.2s) is paid only once per test.
func newRepo(t *testing.T) (scenarios.Repo, string, *testutil.PGContainer) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	ctx := context.Background()
	ontologyRID := "ri.ontology.main.ontology.vtxtest"
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)`,
		ontologyRID, "vtxtest", "Vertex Test"); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}
	return scenarios.NewPGRepo(pg.Pool), ontologyRID, pg
}

// TestScenarioRepo_Given_EmptyRepo_When_CreateCaseStudy_Then_RowPersistedWithVertexRID
func TestScenarioRepo_Given_EmptyRepo_When_CreateCaseStudy_Then_RowPersistedWithVertexRID(t *testing.T) {
	repo, ontologyRID, pg := newRepo(t)
	ctx := context.Background()

	cs, err := repo.CreateCaseStudy(ctx, "JFK Ops", ontologyRID, "alice")
	if err != nil {
		t.Fatalf("CreateCaseStudy: %v", err)
	}
	if cs == nil {
		t.Fatal("expected non-nil case study")
	}
	if !strings.HasPrefix(cs.RID, "ri.vertex.main.case-study.") {
		t.Errorf("expected RID prefix ri.vertex.main.case-study., got %q", cs.RID)
	}
	if cs.Name != "JFK Ops" {
		t.Errorf("Name = %q want %q", cs.Name, "JFK Ops")
	}
	if cs.OntologyRID != ontologyRID {
		t.Errorf("OntologyRID = %q want %q", cs.OntologyRID, ontologyRID)
	}
	if cs.CreatedBy != "alice" {
		t.Errorf("CreatedBy = %q want alice", cs.CreatedBy)
	}

	// Verify DB row by raw query.
	var dbName, dbOntology string
	if err := pg.Pool.QueryRow(ctx,
		`SELECT name, ontology_rid FROM case_studies WHERE rid = $1`, cs.RID,
	).Scan(&dbName, &dbOntology); err != nil {
		t.Fatalf("verify row: %v", err)
	}
	if dbName != "JFK Ops" || dbOntology != ontologyRID {
		t.Errorf("DB row mismatch: name=%q ontology=%q", dbName, dbOntology)
	}
}

// TestScenarioRepo_Given_CaseStudy_When_CreateScenario_Then_DraftAndMutable
func TestScenarioRepo_Given_CaseStudy_When_CreateScenario_Then_DraftAndMutable(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	cs, err := repo.CreateCaseStudy(ctx, "JFK Ops", ontologyRID, "alice")
	if err != nil {
		t.Fatalf("CreateCaseStudy: %v", err)
	}

	sc, err := repo.CreateScenario(ctx, cs.RID, "Snowstorm", "commit-abc", "alice")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	if !strings.HasPrefix(sc.RID, "ri.vertex.main.scenario.") {
		t.Errorf("expected RID prefix ri.vertex.main.scenario., got %q", sc.RID)
	}
	if sc.Status != "draft" {
		t.Errorf("Status = %q want draft", sc.Status)
	}
	if sc.Immutable {
		t.Errorf("Immutable = true, want false on fresh scenario")
	}
	if sc.CaseStudyRID != cs.RID {
		t.Errorf("CaseStudyRID = %q want %q", sc.CaseStudyRID, cs.RID)
	}
	if sc.ParentOntologyCommit != "commit-abc" {
		t.Errorf("ParentOntologyCommit = %q want commit-abc", sc.ParentOntologyCommit)
	}

	// GetScenario should round-trip.
	got, err := repo.GetScenario(ctx, sc.RID)
	if err != nil {
		t.Fatalf("GetScenario: %v", err)
	}
	if got.RID != sc.RID || got.Name != "Snowstorm" {
		t.Errorf("GetScenario round-trip mismatch: %+v", got)
	}
}

// TestScenarioRepo_Given_DraftScenario_When_AppendFiveEdits_Then_PersistsInInsertOrder
func TestScenarioRepo_Given_DraftScenario_When_AppendFiveEdits_Then_PersistsInInsertOrder(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	cs, _ := repo.CreateCaseStudy(ctx, "JFK Ops", ontologyRID, "alice")
	sc, _ := repo.CreateScenario(ctx, cs.RID, "Snowstorm", "commit-abc", "alice")

	for i := 0; i < 5; i++ {
		edit := scenarios.ScenarioEdit{
			Op:         "modifyProperty",
			ObjectType: "Airport",
			ObjectID:   "JFK",
			Property:   "capacity",
			NewValue:   []byte(`150`),
		}
		if err := repo.AppendEdit(ctx, sc.RID, edit); err != nil {
			t.Fatalf("AppendEdit %d: %v", i, err)
		}
	}

	edits, err := repo.ListEdits(ctx, sc.RID)
	if err != nil {
		t.Fatalf("ListEdits: %v", err)
	}
	if len(edits) != 5 {
		t.Fatalf("len(edits) = %d, want 5", len(edits))
	}
	// Sequence must be strictly monotonically increasing (insert order).
	for i := 1; i < len(edits); i++ {
		if edits[i].Seq <= edits[i-1].Seq {
			t.Errorf("seq not monotonic: edits[%d].Seq=%d <= edits[%d].Seq=%d",
				i, edits[i].Seq, i-1, edits[i-1].Seq)
		}
	}
}

// TestScenarioRepo_Given_DraftScenario_When_Freeze_Then_AppendReturnsErrScenarioImmutable
func TestScenarioRepo_Given_DraftScenario_When_Freeze_Then_AppendReturnsErrScenarioImmutable(t *testing.T) {
	repo, ontologyRID, pg := newRepo(t)
	ctx := context.Background()

	cs, _ := repo.CreateCaseStudy(ctx, "JFK Ops", ontologyRID, "alice")
	sc, _ := repo.CreateScenario(ctx, cs.RID, "Snowstorm", "commit-abc", "alice")

	// Pre-freeze: append works.
	if err := repo.AppendEdit(ctx, sc.RID, scenarios.ScenarioEdit{
		Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK",
		Property: "capacity", NewValue: []byte(`100`),
	}); err != nil {
		t.Fatalf("pre-freeze AppendEdit: %v", err)
	}

	if err := repo.Freeze(ctx, sc.RID); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	// DB row reflects immutable=true and status=frozen.
	var immutable bool
	var status string
	if err := pg.Pool.QueryRow(ctx,
		`SELECT immutable, status FROM scenarios WHERE rid = $1`, sc.RID,
	).Scan(&immutable, &status); err != nil {
		t.Fatalf("verify frozen row: %v", err)
	}
	if !immutable {
		t.Error("expected immutable=true after Freeze")
	}
	if status != "frozen" {
		t.Errorf("expected status=frozen, got %q", status)
	}

	// Post-freeze: append rejected with sentinel error.
	err := repo.AppendEdit(ctx, sc.RID, scenarios.ScenarioEdit{
		Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK",
		Property: "capacity", NewValue: []byte(`200`),
	})
	if !errors.Is(err, scenarios.ErrScenarioImmutable) {
		t.Errorf("expected ErrScenarioImmutable, got %v", err)
	}

	// Re-loaded view must show immutable=true.
	got, err := repo.GetScenario(ctx, sc.RID)
	if err != nil {
		t.Fatalf("GetScenario: %v", err)
	}
	if !got.Immutable {
		t.Error("GetScenario: Immutable=false after Freeze")
	}
}

// TestScenarioRepo_Given_UnknownScenario_When_AppendEdit_Then_ErrNotFound
func TestScenarioRepo_Given_UnknownScenario_When_AppendEdit_Then_ErrNotFound(t *testing.T) {
	repo, _, _ := newRepo(t)
	ctx := context.Background()

	err := repo.AppendEdit(ctx, "ri.vertex.main.scenario.00000000-0000-0000-0000-000000000000",
		scenarios.ScenarioEdit{Op: "modifyProperty", ObjectID: "X", Property: "p", NewValue: []byte(`1`)})
	if !errors.Is(err, scenarios.ErrScenarioNotFound) {
		t.Errorf("expected ErrScenarioNotFound, got %v", err)
	}
}

// TestScenarioRepo_Given_Override_When_SetOverride_Then_PersistsAndIdempotent verifies
// UpsertOverride writes a row and a second upsert on the same key replaces value
// (this also exercises scenario_overrides PK semantics for later VTX-005/006).
func TestScenarioRepo_Given_Override_When_SetOverride_Then_PersistsAndIdempotent(t *testing.T) {
	repo, ontologyRID, pg := newRepo(t)
	ctx := context.Background()

	cs, _ := repo.CreateCaseStudy(ctx, "JFK Ops", ontologyRID, "alice")
	sc, _ := repo.CreateScenario(ctx, cs.RID, "Snowstorm", "commit-abc", "alice")

	ov := scenarios.ScenarioOverride{
		ScenarioRID: sc.RID,
		ModelRID:    "ri.vertex.main.model.wx",
		Parameter:   "windSpeed",
		ObjectID:    "JFK",
		Value:       []byte(`50`),
	}
	if err := repo.UpsertOverride(ctx, ov); err != nil {
		t.Fatalf("UpsertOverride first: %v", err)
	}
	ov.Value = []byte(`60`)
	if err := repo.UpsertOverride(ctx, ov); err != nil {
		t.Fatalf("UpsertOverride second: %v", err)
	}

	var count int
	if err := pg.Pool.QueryRow(ctx,
		`SELECT count(*) FROM scenario_overrides WHERE scenario_rid = $1`, sc.RID,
	).Scan(&count); err != nil {
		t.Fatalf("count overrides: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 override row after two upserts, got %d", count)
	}
	var stored []byte
	if err := pg.Pool.QueryRow(ctx,
		`SELECT value::text FROM scenario_overrides WHERE scenario_rid = $1`, sc.RID,
	).Scan(&stored); err != nil {
		t.Fatalf("read override value: %v", err)
	}
	if string(stored) != "60" {
		t.Errorf("override value = %q want 60", stored)
	}
}
