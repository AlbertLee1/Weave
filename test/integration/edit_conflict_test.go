//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// TestEditConflict_UserEditWinsUnderConcurrency is the US-022 acceptance
// scenario: a real PG-backed history recorder sits behind a funnel Consumer
// and is raced by two goroutines — one applying user edits and one applying
// stale ingest edits — against a shared set of 20 employees. The test runs
// 10 iterations with different seeds and asserts that after every iteration
// the Bleve document for each PK reflects the last USER edit that touched
// it, never the ingest value, proving user-edit-wins holds under
// concurrency.
//
// Mechanism: user batches carry monotonically increasing timestamps rooted
// at iterationStart + 1h; ingest batches carry timestamps in the iteration
// "past window" (iterationStart - 1h .. iterationStart). Because every PK
// is pre-seeded with a user CREATE at iterationStart, the funnel consumer's
// LatestUserEditAt lookup always surfaces a user row newer than any ingest
// batch timestamp and the resolveConflicts path skips every ingest edit
// (alwaysApplyField is left unset so no ingest field is exempt).
//
// The final assertion reads each PK's document straight out of the Bleve
// index and checks field-level equality against the expected state computed
// by replaying the user edit sequence in order — any ingest leakage would
// break the equality because user and ingest values intentionally live in
// disjoint numeric ranges.
func TestEditConflict_UserEditWinsUnderConcurrency(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "edit_conflict",
		DisplayName: "Edit Conflict",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	employee := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, employee); err != nil {
		t.Fatalf("create employee: %v", err)
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	scopedKey := index.ScopedKey(ont.APIName, employee.APIName)
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "age", BaseType: "integer", IsSearchable: true},
		{APIName: "title", BaseType: "string", IsSearchable: true},
		{APIName: "salary", BaseType: "double", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(scopedKey, props); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	consumer := funnel.NewConsumer(nil, mgr)
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		employee.APIName: employee.RID,
	})

	const (
		pkCount       = 20
		editsPerActor = 100
		iterations    = 10
	)
	pks := make([]string, pkCount)
	for i := range pks {
		pks[i] = fmt.Sprintf("emp-%03d", i)
	}

	fields := []string{"name", "age", "title", "salary"}

	for iter := 0; iter < iterations; iter++ {
		iterStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(iter) * 24 * time.Hour)

		// Seed each PK with a user CREATE anchored at iterStart so the
		// LatestUserEditAt guard fires for every ingest attempt regardless of
		// goroutine interleaving.
		seedBatch := funnel.EditBatch{
			ID:              fmt.Sprintf("seed-%d", iter),
			OntologyAPIName: ont.APIName,
			UserID:          "seed",
			Timestamp:       iterStart,
		}
		for i, pk := range pks {
			seedBatch.Edits = append(seedBatch.Edits, funnel.Edit{
				Type:       funnel.EditTypeCreate,
				ObjectType: employee.APIName,
				PrimaryKey: pk,
				Source:     funnel.EditSourceUser,
				Properties: map[string]interface{}{
					"id":     pk,
					"name":   fmt.Sprintf("user-seed-%d", i),
					"age":    float64(20 + i),
					"title":  fmt.Sprintf("user-seed-title-%d", i),
					"salary": float64(1000 + i),
				},
			})
		}
		if err := consumer.ApplyBatch(ctx, seedBatch); err != nil {
			t.Fatalf("iter %d seed apply: %v", iter, err)
		}

		// Generate the user edit sequence sequentially so the expected state
		// is deterministic. Each edit is a full upsert (MODIFY replaces the
		// Bleve doc) so the "winning" value for every field is simply the
		// last user edit that touched the PK.
		userRand := rand.New(rand.NewSource(int64(iter)*2 + 1))
		userEdits := make([]funnel.EditBatch, editsPerActor)
		expectedState := make(map[string]map[string]interface{}, pkCount)
		for i, pk := range pks {
			expectedState[pk] = map[string]interface{}{
				"id":     pk,
				"name":   fmt.Sprintf("user-seed-%d", i),
				"age":    float64(20 + i),
				"title":  fmt.Sprintf("user-seed-title-%d", i),
				"salary": float64(1000 + i),
			}
		}
		for i := 0; i < editsPerActor; i++ {
			pk := pks[userRand.Intn(pkCount)]
			props := map[string]interface{}{
				"id":     pk,
				"name":   fmt.Sprintf("user-%d-%d", iter, i),
				"age":    float64(30 + userRand.Intn(50)),
				"title":  fmt.Sprintf("user-title-%d-%d", iter, i),
				"salary": float64(2000 + userRand.Intn(5000)),
			}
			expectedState[pk] = props
			userEdits[i] = funnel.EditBatch{
				ID:              fmt.Sprintf("user-%d-%d", iter, i),
				OntologyAPIName: ont.APIName,
				UserID:          "alice",
				// Strictly after iterStart so LatestUserEditAt beats every
				// ingest batch timestamp below.
				Timestamp: iterStart.Add(time.Hour + time.Duration(i)*time.Millisecond),
				Edits: []funnel.Edit{
					{
						Type:       funnel.EditTypeModify,
						ObjectType: employee.APIName,
						PrimaryKey: pk,
						Source:     funnel.EditSourceUser,
						Properties: props,
					},
				},
			}
		}

		// Ingest batches all claim timestamps strictly before iterStart so
		// the conflict resolver classifies them as stale. Values live in a
		// disjoint numeric range (ingest name/title literals contain the
		// "INGEST-WRONG" marker; age/salary use large magic numbers) so any
		// leakage into the final Bleve doc is immediately detectable.
		ingestRand := rand.New(rand.NewSource(int64(iter)*2 + 2))
		ingestEdits := make([]funnel.EditBatch, editsPerActor)
		for i := 0; i < editsPerActor; i++ {
			pk := pks[ingestRand.Intn(pkCount)]
			props := map[string]interface{}{
				"id":     pk,
				"name":   fmt.Sprintf("INGEST-WRONG-%d-%d", iter, i),
				"age":    float64(900 + ingestRand.Intn(99)),
				"title":  fmt.Sprintf("INGEST-WRONG-title-%d-%d", iter, i),
				"salary": float64(99000 + ingestRand.Intn(999)),
			}
			ingestEdits[i] = funnel.EditBatch{
				ID:              fmt.Sprintf("ingest-%d-%d", iter, i),
				OntologyAPIName: ont.APIName,
				UserID:          "stream",
				Timestamp:       iterStart.Add(-time.Hour - time.Duration(i)*time.Millisecond),
				Edits: []funnel.Edit{
					{
						Type:       funnel.EditTypeModify,
						ObjectType: employee.APIName,
						PrimaryKey: pk,
						Source:     funnel.EditSourceIngest,
						Properties: props,
					},
				},
			}
		}

		// Race: both goroutines apply their per-actor batches sequentially
		// against a shared consumer. The Bleve index + PG history recorder
		// serialise writes per-PK via their own concurrency primitives so
		// the goroutines may freely interleave.
		var wg sync.WaitGroup
		wg.Add(2)
		errCh := make(chan error, 2)
		go func() {
			defer wg.Done()
			for _, b := range userEdits {
				if err := consumer.ApplyBatch(ctx, b); err != nil {
					errCh <- fmt.Errorf("user apply: %w", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for _, b := range ingestEdits {
				if err := consumer.ApplyBatch(ctx, b); err != nil {
					errCh <- fmt.Errorf("ingest apply: %w", err)
					return
				}
			}
		}()
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				t.Fatalf("iter %d: %v", iter, err)
			}
		}

		// Final-state assertion: every PK's Bleve doc must equal the last
		// user edit's properties (or the seed when no user MODIFY touched
		// the PK). A single field match against an "INGEST-WRONG" marker or
		// a magic ingest number would fail the equality and surface the
		// regression immediately.
		for _, pk := range pks {
			got := fetchDoc(t, mgr, scopedKey, pk)
			want := expectedState[pk]
			for _, f := range fields {
				if got[f] != want[f] {
					t.Fatalf("iter %d pk %s field %s: got %v, want %v", iter, pk, f, got[f], want[f])
				}
			}
			// Name field carries the source marker — guard that ingest
			// leakage is impossible even with an identical random dice
			// roll somehow landing on a matching value.
			if name, _ := got["name"].(string); len(name) >= len("INGEST-WRONG") && name[:len("INGEST-WRONG")] == "INGEST-WRONG" {
				t.Fatalf("iter %d pk %s: ingest name leaked into final doc: %q", iter, pk, name)
			}
		}
	}
}

// fetchDoc reads the current document for (scopedKey, pk) out of the Bleve
// index as a flat map[string]interface{}. Returns an empty map when the
// doc is missing so the caller sees a field-by-field mismatch instead of a
// panic on nil index — this mirrors the unexported helper used by the
// funnel consumer.
func fetchDoc(t *testing.T, mgr *index.Manager, scopedKey, pk string) map[string]interface{} {
	t.Helper()
	q := bleve.NewDocIDQuery([]string{pk})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(scopedKey, req)
	if err != nil {
		t.Fatalf("fetchDoc %s: %v", pk, err)
	}
	if res == nil || res.Total == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(res.Hits[0].Fields))
	for k, v := range res.Hits[0].Fields {
		out[k] = v
	}
	return out
}
