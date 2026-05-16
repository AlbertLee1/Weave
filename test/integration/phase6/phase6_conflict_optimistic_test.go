//go:build integration

package phase6_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// bridgePublisher implements actions.Publisher by routing EditBatch messages
// synchronously through a funnel.Consumer, bypassing NATS. The cross-US
// Phase 6 conflict/optimistic test needs the action executor, the user-edit
// -wins conflict filter, and the PG history recorder all on the same call
// stack so that GetObjectVersionCount (read by the next optimistic check)
// reflects the committed state immediately.
type bridgePublisher struct {
	consumer *funnel.Consumer
	ctx      context.Context
	offset   uint64
	mu       sync.Mutex
	// batches captures every EditBatch routed through Publish so US-471
	// cross-tests can assert on the post-collapse payload (e.g. that
	// Edit.EditVersion was stamped at prepare time). Pre-US-471 callers
	// ignore this field; the slice is goroutine-safe via b.mu.
	batches []funnel.EditBatch
}

func (b *bridgePublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.consumer.ApplyBatch(b.ctx, *batch); err != nil {
		return 0, err
	}
	b.offset++
	b.batches = append(b.batches, *batch)
	return b.offset, nil
}

// TestPhase6_ConflictOptimistic is the Phase 6 cross-US gate test for
// US-034. It races user-edit-wins (US-021) with expectedVersion (US-023)
// against a single target Employee row and asserts that both semantics
// compose correctly:
//
//  1. The first user edit supplies a matching expectedVersion and must
//     succeed. It becomes the authoritative state of the object.
//  2. An ingest flood runs concurrently with a strictly earlier batch
//     timestamp. Every ingest edit must be filtered by the US-021 guard —
//     zero ingest rows land in object_history, zero ingest values leak
//     into the Bleve index, and the post-race version count reflects only
//     user edits.
//  3. A second user edit re-uses the stale expectedVersion from step 1.
//     It must fail fast with a *StaleObjectError carrying the current
//     version, and must not publish a batch nor write an action log.
//
// Mechanism:
//   - Action executor + PG-backed OMS repo drive expectedVersion lookups
//     via GetObjectVersionCount (which counts object_history rows).
//   - A bridgePublisher routes executor edits directly into a funnel
//     Consumer whose history recorder is the same PG repo, so user
//     history rows are inserted synchronously and visible to the next
//     executor call on the same goroutine.
//   - Ingest batches carry Timestamps rooted in the distant past; the
//     seed user CREATE is stamped at time.Now() so LatestUserEditAt is
//     always ahead of every ingest batch.
func TestPhase6_ConflictOptimistic(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "phase6_conflict",
		DisplayName: "Phase 6 Conflict",
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

	// ActionType: renameEmployee — a single modifyObject rule that rewrites
	// name + title on the target row based on parameter bindings.
	actionParams, _ := json.Marshal([]map[string]interface{}{
		{"id": "primaryKey", "type": "string", "required": true},
		{"id": "name", "type": "string", "required": true},
		{"id": "title", "type": "string", "required": true},
	})
	actionRules, _ := json.Marshal([]map[string]interface{}{
		{
			"type":       "modifyObject",
			"objectType": "employee",
			"propertyBindings": map[string]interface{}{
				"name":  map[string]interface{}{"type": "parameter", "value": "name"},
				"title": map[string]interface{}{"type": "parameter", "value": "title"},
			},
		},
	})
	at := &oms.ActionType{
		RID:         rid.NewActionTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "renameEmployee",
		DisplayName: "Rename Employee",
		Status:      "ACTIVE",
		Parameters:  actionParams,
		Rules:       actionRules,
	}
	if err := repo.CreateActionType(ctx, at); err != nil {
		t.Fatalf("create action type: %v", err)
	}

	// Bleve index for the employee ObjectType, scoped per-ontology.
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
		{APIName: "title", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(scopedKey, props); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	consumer := funnel.NewConsumer(nil, mgr)
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		employee.APIName: employee.RID,
	})

	publisher := &bridgePublisher{consumer: consumer, ctx: ctx}
	executor := actions.NewExecutor(repo, publisher)

	const pk = "emp-1"

	// ------------------------------------------------------------------
	// Seed: user CREATE stamped at time.Now() so LatestUserEditAt is
	// newer than every ingest batch used later in the race.
	// ------------------------------------------------------------------
	seedTime := time.Now()
	seedBatch := funnel.EditBatch{
		ID:              "seed",
		OntologyAPIName: ont.APIName,
		UserID:          "seed",
		Timestamp:       seedTime,
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: employee.APIName,
				PrimaryKey: pk,
				Source:     funnel.EditSourceUser,
				Properties: map[string]interface{}{
					"id":    pk,
					"name":  "seed-name",
					"title": "seed-title",
				},
			},
		},
	}
	if err := consumer.ApplyBatch(ctx, seedBatch); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	// Sanity: version count should be 1 (one user CREATE row in history).
	count, err := repo.GetObjectVersionCount(ctx, employee.RID, pk)
	if err != nil {
		t.Fatalf("post-seed version count: %v", err)
	}
	if count != 1 {
		t.Fatalf("post-seed version count = %d, want 1", count)
	}

	// ------------------------------------------------------------------
	// Three-way race:
	//   goroutine A: first user Apply with expectedVersion=1 (must succeed)
	//   goroutine B: ingest flood with distant-past timestamps (must all
	//                be filtered by the US-021 user-edit-wins guard)
	//   goroutine C: second user Apply with stale expectedVersion=1
	//                (must fail fast with *StaleObjectError)
	//
	// We sequence A → C with a channel so the assertion on C's 409 is
	// deterministic, but the ingest flood overlaps both A and C so any
	// race-condition leakage (e.g. history rows written for ingest
	// batches, Bleve doc mutated by ingest, version counter inflated by
	// ingest) surfaces as a concrete test failure below.
	// ------------------------------------------------------------------
	const ingestFloodSize = 50
	// Ingest timestamps live well before seedTime so resolveConflicts
	// always classifies them as stale, regardless of goroutine
	// interleaving with user apply calls.
	ingestBase := seedTime.Add(-24 * time.Hour)

	userAName := "user-A-alice"
	userATitle := "user-A-director"
	userCName := "user-C-should-not-land"
	userCTitle := "user-C-should-not-land"

	var wg sync.WaitGroup
	aDone := make(chan struct{})
	var aErr error
	var cErr error

	wg.Add(3)

	// Goroutine A — first user edit with matching expectedVersion.
	go func() {
		defer wg.Done()
		defer close(aDone)
		_, aErr = executor.Apply(ctx, ont.APIName, &actions.ApplyRequest{
			ActionType: "renameEmployee",
			Parameters: map[string]interface{}{
				"primaryKey": pk,
				"name":       userAName,
				"title":      userATitle,
			},
			Options: &actions.ApplyOptions{ExpectedVersion: intPtrP6(1)},
		})
	}()

	// Goroutine B — ingest flood with distant-past timestamps.
	go func() {
		defer wg.Done()
		for i := 0; i < ingestFloodSize; i++ {
			batch := funnel.EditBatch{
				ID:              fmt.Sprintf("ingest-%d", i),
				OntologyAPIName: ont.APIName,
				UserID:          "pipeline",
				Timestamp:       ingestBase.Add(time.Duration(i) * time.Millisecond),
				Edits: []funnel.Edit{
					{
						Type:       funnel.EditTypeModify,
						ObjectType: employee.APIName,
						PrimaryKey: pk,
						Source:     funnel.EditSourceIngest,
						Properties: map[string]interface{}{
							"id":    pk,
							"name":  fmt.Sprintf("INGEST-WRONG-name-%d", i),
							"title": fmt.Sprintf("INGEST-WRONG-title-%d", i),
						},
					},
				},
			}
			if err := consumer.ApplyBatch(ctx, batch); err != nil {
				t.Errorf("ingest apply %d: %v", i, err)
				return
			}
		}
	}()

	// Goroutine C — second user edit with stale expectedVersion. Waits
	// for A to complete so the stale-version assertion is deterministic.
	go func() {
		defer wg.Done()
		<-aDone
		_, cErr = executor.Apply(ctx, ont.APIName, &actions.ApplyRequest{
			ActionType: "renameEmployee",
			Parameters: map[string]interface{}{
				"primaryKey": pk,
				"name":       userCName,
				"title":      userCTitle,
			},
			Options: &actions.ApplyOptions{ExpectedVersion: intPtrP6(1)},
		})
	}()

	wg.Wait()

	// ------------------------------------------------------------------
	// Assertions
	// ------------------------------------------------------------------
	if aErr != nil {
		t.Fatalf("first user edit (A) expected success, got: %v", aErr)
	}
	if cErr == nil {
		t.Fatalf("second user edit (C) expected *StaleObjectError, got nil")
	}
	var stale *actions.StaleObjectError
	if !errors.As(cErr, &stale) {
		t.Fatalf("second user edit (C) expected *StaleObjectError, got %T: %v", cErr, cErr)
	}
	if stale.CurrentVersion != 2 {
		t.Errorf("second user edit CurrentVersion = %d, want 2", stale.CurrentVersion)
	}
	if stale.ExpectedVersion != 1 {
		t.Errorf("second user edit ExpectedVersion = %d, want 1", stale.ExpectedVersion)
	}
	if stale.PrimaryKey != pk {
		t.Errorf("second user edit PrimaryKey = %q, want %q", stale.PrimaryKey, pk)
	}

	// Version count post-race: exactly two history rows (seed CREATE +
	// first user MODIFY). Ingest never incremented the counter because
	// every ingest edit was filtered out before history insertion, and
	// the stale second user edit short-circuited before publishing.
	count, err = repo.GetObjectVersionCount(ctx, employee.RID, pk)
	if err != nil {
		t.Fatalf("post-race version count: %v", err)
	}
	if count != 2 {
		t.Errorf("post-race version count = %d, want 2", count)
	}

	// Bleve final state reflects the first user edit (and ONLY the first
	// user edit). A single INGEST-WRONG marker anywhere in the doc means
	// the user-edit-wins filter failed; a user-C-* marker means the stale
	// second apply somehow reached CommitBatch.
	doc := fetchDocP6(t, mgr, scopedKey, pk)
	if got, _ := doc["name"].(string); got != userAName {
		t.Errorf("final name = %q, want %q", got, userAName)
	}
	if got, _ := doc["title"].(string); got != userATitle {
		t.Errorf("final title = %q, want %q", got, userATitle)
	}
	for _, field := range []string{"name", "title"} {
		v, _ := doc[field].(string)
		if strings.HasPrefix(v, "INGEST-WRONG") {
			t.Errorf("ingest leak detected on field %s: %q", field, v)
		}
		if strings.HasPrefix(v, "user-C-") {
			t.Errorf("stale user-C edit leaked onto field %s: %q", field, v)
		}
	}
}

// intPtrP6 is a local helper to build *int literals. Named to avoid
// collision with other Phase 6 test files in this package.
func intPtrP6(i int) *int { return &i }

// fetchDocP6 reads the current Bleve document for (scopedKey, pk) as a
// flat map. Returns an empty map when the doc is missing so assertions
// fail with explicit mismatches instead of panicking on a nil index.
func fetchDocP6(t *testing.T, mgr *index.Manager, scopedKey, pk string) map[string]interface{} {
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
