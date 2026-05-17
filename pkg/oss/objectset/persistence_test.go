//go:build integration

package objectset_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// setupSavedStore boots a Postgres container, runs migrations and returns a
// PG-backed SavedStore tied to that container's lifecycle (cleaned up by
// testutil.StartPGContainer's t.Cleanup hooks).
func setupSavedStore(t *testing.T) *objectset.PGSavedStore {
	t.Helper()
	store, _ := setupSavedStoreWithPool(t)
	return store
}

// setupSavedStoreWithPool also exposes the underlying pgx pool so tests that
// need to mutate row state directly (e.g. backdating created_at to exercise
// the reaper) can do so without a public accessor on PGSavedStore.
func setupSavedStoreWithPool(t *testing.T) (*objectset.PGSavedStore, *pgxpool.Pool) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return objectset.NewPGSavedStore(pg.Pool), pg.Pool
}

// baseDef returns a minimal valid definition for tests.
func baseDef(t *testing.T, ot string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{
		"type":       "base",
		"objectType": ot,
	})
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}
	return raw
}

func TestPGSavedStore_Create_Persists(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	rec := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "engineers",
		Description:     "All engineers",
		Definition:      baseDef(t, "employee"),
		CreatedBy:       "user:alice",
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.ID == "" {
		t.Error("expected Create to populate ID")
	}
	if rec.CreatedAt.IsZero() {
		t.Error("expected Create to populate CreatedAt")
	}
	if rec.UpdatedAt.IsZero() {
		t.Error("expected Create to populate UpdatedAt")
	}

	got, err := store.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Name != "engineers" {
		t.Errorf("Name: got %q want %q", got.Name, "engineers")
	}
	if got.OntologyAPIName != "north" {
		t.Errorf("OntologyAPIName: got %q", got.OntologyAPIName)
	}
	if got.CreatedBy != "user:alice" {
		t.Errorf("CreatedBy: got %q", got.CreatedBy)
	}
}

func TestPGSavedStore_Create_DuplicateName_Fails(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	first := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "duplicate",
		Definition:      baseDef(t, "employee"),
	}
	if err := store.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	second := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "duplicate",
		Definition:      baseDef(t, "employee"),
	}
	err := store.Create(ctx, second)
	if err == nil {
		t.Fatal("expected duplicate name within ontology to fail")
	}
}

func TestPGSavedStore_Get_NotFound(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if !errors.Is(err, objectset.ErrSavedSetNotFound) {
		t.Errorf("expected ErrSavedSetNotFound, got %v", err)
	}
}

func TestPGSavedStore_GetByName(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	rec := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "byname",
		Definition:      baseDef(t, "employee"),
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByName(ctx, "north", "byname")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ID != rec.ID {
		t.Errorf("ID: got %q want %q", got.ID, rec.ID)
	}

	if _, err := store.GetByName(ctx, "north", "missing"); !errors.Is(err, objectset.ErrSavedSetNotFound) {
		t.Errorf("expected ErrSavedSetNotFound for missing name, got %v", err)
	}
}

func TestPGSavedStore_Update_ChangesUpdatedAt(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	rec := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "updateme",
		Description:     "old",
		Definition:      baseDef(t, "employee"),
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	originalUpdated := rec.UpdatedAt

	rec.Description = "new"
	rec.Definition = baseDef(t, "department")
	if err := store.Update(ctx, rec); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !rec.UpdatedAt.After(originalUpdated) {
		t.Errorf("UpdatedAt did not advance: original=%v new=%v", originalUpdated, rec.UpdatedAt)
	}

	got, err := store.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.Description != "new" {
		t.Errorf("Description: got %q want %q", got.Description, "new")
	}
	if !strings.Contains(string(got.Definition), "department") {
		t.Errorf("Definition: got %s, expected to contain 'department'", got.Definition)
	}
}

func TestPGSavedStore_Update_NotFound(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	rec := &objectset.SavedObjectSet{
		ID:              "00000000-0000-0000-0000-000000000000",
		OntologyAPIName: "north",
		Name:            "ghost",
		Definition:      baseDef(t, "employee"),
	}
	err := store.Update(ctx, rec)
	if !errors.Is(err, objectset.ErrSavedSetNotFound) {
		t.Errorf("expected ErrSavedSetNotFound, got %v", err)
	}
}

func TestPGSavedStore_Delete_Idempotent(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	rec := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "deleteme",
		Definition:      baseDef(t, "employee"),
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	// Second delete must not error.
	if err := store.Delete(ctx, rec.ID); err != nil {
		t.Errorf("second Delete should be idempotent, got %v", err)
	}

	if _, err := store.Get(ctx, rec.ID); !errors.Is(err, objectset.ErrSavedSetNotFound) {
		t.Errorf("expected ErrSavedSetNotFound after delete, got %v", err)
	}
}

func TestPGSavedStore_List_FiltersByOntology(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	north := []*objectset.SavedObjectSet{
		{OntologyAPIName: "north", Name: "n1", Definition: baseDef(t, "employee")},
		{OntologyAPIName: "north", Name: "n2", Definition: baseDef(t, "employee")},
	}
	south := []*objectset.SavedObjectSet{
		{OntologyAPIName: "south", Name: "s1", Definition: baseDef(t, "employee")},
	}
	for _, r := range append(north, south...) {
		if err := store.Create(ctx, r); err != nil {
			t.Fatalf("Create %s/%s: %v", r.OntologyAPIName, r.Name, err)
		}
	}

	list, err := store.List(ctx, "north", 0)
	if err != nil {
		t.Fatalf("List north: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 north entries, got %d", len(list))
	}
	for _, r := range list {
		if r.OntologyAPIName != "north" {
			t.Errorf("List returned entry with OntologyAPIName=%q", r.OntologyAPIName)
		}
	}

	list2, err := store.List(ctx, "south", 0)
	if err != nil {
		t.Fatalf("List south: %v", err)
	}
	if len(list2) != 1 {
		t.Errorf("expected 1 south entry, got %d", len(list2))
	}
}

func TestPGSavedStore_List_RespectsLimit(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		rec := &objectset.SavedObjectSet{
			OntologyAPIName: "north",
			Name:            "set-" + strings.Repeat("x", i+1),
			Definition:      baseDef(t, "employee"),
		}
		if err := store.Create(ctx, rec); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	list, err := store.List(ctx, "north", 3)
	if err != nil {
		t.Fatalf("List with limit: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 entries with limit=3, got %d", len(list))
	}
}

// US-365 — immutable snapshot persistence.

func TestPGSavedStore_Create_StampsUS365Fields(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	rec := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "us365-stamps",
		Definition:      baseDef(t, "employee"),
		IsImmutable:     true,
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.DefinitionHash == "" {
		t.Error("DefinitionHash empty after Create")
	}
	if rec.SnapshotAt == 0 {
		t.Error("SnapshotAt = 0 after Create; expected nextval() allocation")
	}

	got, err := store.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DefinitionHash != rec.DefinitionHash {
		t.Errorf("Get.DefinitionHash = %q, want %q", got.DefinitionHash, rec.DefinitionHash)
	}
	if got.SnapshotAt != rec.SnapshotAt {
		t.Errorf("Get.SnapshotAt = %d, want %d", got.SnapshotAt, rec.SnapshotAt)
	}
	if !got.IsImmutable {
		t.Error("Get.IsImmutable = false, want true")
	}
}

func TestPGSavedStore_Create_FrozenPrimaryKeysRoundTrip(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	pks := []string{"e1", "e2", "e3"}
	rec := &objectset.SavedObjectSet{
		OntologyAPIName:   "north",
		Name:              "us365-frozen",
		Definition:        baseDef(t, "employee"),
		IsImmutable:       true,
		FrozenObjectType:  "employee",
		FrozenPrimaryKeys: pks,
		FrozenTruncated:   false,
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FrozenObjectType != "employee" {
		t.Errorf("FrozenObjectType = %q, want employee", got.FrozenObjectType)
	}
	if len(got.FrozenPrimaryKeys) != len(pks) {
		t.Fatalf("FrozenPrimaryKeys len = %d, want %d", len(got.FrozenPrimaryKeys), len(pks))
	}
	for i, pk := range pks {
		if got.FrozenPrimaryKeys[i] != pk {
			t.Errorf("FrozenPrimaryKeys[%d] = %q, want %q", i, got.FrozenPrimaryKeys[i], pk)
		}
	}
}

func TestPGSavedStore_ReapExpired_HonorsImmutability(t *testing.T) {
	store, pool := setupSavedStoreWithPool(t)
	ctx := context.Background()

	immutable := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "kept-forever",
		Definition:      baseDef(t, "employee"),
		IsImmutable:     true,
	}
	ephemeral := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "ephemeral",
		Definition:      baseDef(t, "employee"),
		IsImmutable:     false,
	}
	for _, r := range []*objectset.SavedObjectSet{immutable, ephemeral} {
		if err := store.Create(ctx, r); err != nil {
			t.Fatalf("Create %s: %v", r.Name, err)
		}
	}

	// Backdate both rows so they are eligible for the reaper. The reaper
	// must drop only the non-immutable one.
	if _, err := pool.Exec(ctx,
		`UPDATE saved_object_sets SET created_at = NOW() - INTERVAL '2 hours' WHERE id IN ($1, $2)`,
		immutable.ID, ephemeral.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	dropped, err := store.ReapExpired(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ReapExpired: %v", err)
	}
	if dropped != 1 {
		t.Errorf("ReapExpired dropped %d rows, want 1 (the ephemeral one)", dropped)
	}

	if _, err := store.Get(ctx, immutable.ID); err != nil {
		t.Errorf("immutable row lost: %v", err)
	}
	if _, err := store.Get(ctx, ephemeral.ID); !errors.Is(err, objectset.ErrSavedSetNotFound) {
		t.Errorf("ephemeral row should be reaped, got err=%v", err)
	}
}

func TestPGSavedStore_ReapExpired_KeepsRecent(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	rec := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "fresh",
		Definition:      baseDef(t, "employee"),
		IsImmutable:     false,
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dropped, err := store.ReapExpired(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ReapExpired: %v", err)
	}
	if dropped != 0 {
		t.Errorf("ReapExpired dropped %d rows, want 0 (row was just created)", dropped)
	}
}

// TestPGSavedStore_RunReaperLoop_DropsEphemeralKeepsImmutable is the US-462
// integration: 写入 → 等待 → 验证已/未回收. With a short tick interval the
// loop must drop a backdated ephemeral row while leaving the immutable one
// untouched, then exit cleanly on context cancel.
func TestPGSavedStore_RunReaperLoop_DropsEphemeralKeepsImmutable(t *testing.T) {
	store, pool := setupSavedStoreWithPool(t)
	ctx := context.Background()

	immutable := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "loop-immutable",
		Definition:      baseDef(t, "employee"),
		IsImmutable:     true,
	}
	ephemeral := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "loop-ephemeral",
		Definition:      baseDef(t, "employee"),
		IsImmutable:     false,
	}
	for _, r := range []*objectset.SavedObjectSet{immutable, ephemeral} {
		if err := store.Create(ctx, r); err != nil {
			t.Fatalf("Create %s: %v", r.Name, err)
		}
	}

	if _, err := pool.Exec(ctx,
		`UPDATE saved_object_sets SET created_at = NOW() - INTERVAL '2 hours' WHERE id IN ($1, $2)`,
		immutable.ID, ephemeral.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var reapedTotal atomic.Int64
	reapped := make(chan int64, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		store.RunReaperLoop(loopCtx, 20*time.Millisecond, time.Hour,
			func(n int64) {
				reapedTotal.Add(n)
				if n > 0 {
					select {
					case reapped <- n:
					default:
					}
				}
			},
			func(err error) { t.Errorf("reaper loop error: %v", err) },
		)
	}()

	select {
	case n := <-reapped:
		if n != 1 {
			t.Errorf("first reap dropped %d rows, want 1 (the ephemeral one)", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reaper loop never dropped a row")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reaper loop did not exit after context cancel")
	}

	if _, err := store.Get(ctx, immutable.ID); err != nil {
		t.Errorf("immutable row lost: %v", err)
	}
	if _, err := store.Get(ctx, ephemeral.ID); !errors.Is(err, objectset.ErrSavedSetNotFound) {
		t.Errorf("ephemeral row should be reaped, got err=%v", err)
	}
}

func TestPGSavedStore_HashIsStable_AcrossKeyOrder(t *testing.T) {
	store := setupSavedStore(t)
	ctx := context.Background()

	a := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "hash-a",
		Definition:      json.RawMessage(`{"type":"base","objectType":"employee"}`),
		IsImmutable:     true,
	}
	b := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "hash-b",
		Definition:      json.RawMessage(`{"objectType":"employee","type":"base"}`),
		IsImmutable:     true,
	}
	if err := store.Create(ctx, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if err := store.Create(ctx, b); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	if a.DefinitionHash != b.DefinitionHash {
		t.Errorf("hash differs across key order: %q vs %q", a.DefinitionHash, b.DefinitionHash)
	}
	if a.SnapshotAt == b.SnapshotAt {
		t.Errorf("snapshot_at must be unique per save: a=%d b=%d", a.SnapshotAt, b.SnapshotAt)
	}
}
