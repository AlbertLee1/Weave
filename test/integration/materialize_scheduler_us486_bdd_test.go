//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/materialize"
)

// US-486 BDD — materialized datasets run on schedule, retry on failure,
// and persist state. The scenarios assemble the production wire path
// (Materializer → Parquet on disk → BuildSnapshot → derived JSON output)
// and let the Scheduler drive it, asserting external artefacts
// (snapshot JSON file content + JobStatus counters) rather than
// scheduler internals. The PRD's three acceptance criteria are covered
// by three Given/When/Then scenarios:
//
//   1. Schedule trigger      — a registered job runs on its interval,
//                              produces the snapshot artefact, and
//                              advances LastSuccess + TotalRuns.
//   2. Failure retry          — a Compute that fails the first two
//                              attempts succeeds on the third within
//                              a single scheduled tick; state shows
//                              ConsecutiveFailures back at 0.
//   3. Failure state recording — a Compute that always fails records
//                              LastFailure / TotalFailures / LastError
//                              and the cron loop reports the named job
//                              via onError without aborting.

// us486SnapshotJob composes a Materializer-backed BuildSnapshot into a
// Compute func: every tick rebuilds the live set for one (ontology,
// objectType) tuple and writes it to outPath as a stable JSON list.
// This mirrors the real-world "materialized view" workflow PRD US-486
// targets — operators run a scheduled job to materialise dataset rows
// from the change log so downstream consumers can read a flat file
// instead of replaying the Parquet history.
func us486SnapshotJob(mat *materialize.Materializer, ontology, objectType, outPath string) func(context.Context) error {
	return func(ctx context.Context) error {
		rows, err := mat.BuildSnapshot(ctx, ontology, objectType, time.Time{})
		if err != nil {
			return fmt.Errorf("BuildSnapshot: %w", err)
		}
		payload, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("encode snapshot: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(outPath, payload, 0o644)
	}
}

func us486SeedBatch(t *testing.T, mat *materialize.Materializer, edits []funnel.Edit, batchID string) {
	t.Helper()
	if err := mat.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              batchID,
		OntologyAPIName: "us486",
		Timestamp:       time.Now().UTC().Add(-time.Hour),
		Edits:           edits,
	}); err != nil {
		t.Fatalf("seed batch %s: %v", batchID, err)
	}
}

// Given a materialized dataset job registered on a 30ms interval,
// When the scheduler loop runs for two ticks,
// Then the snapshot JSON file on disk contains the seeded objects and
//      JobStatus.LastSuccess + TotalRuns advance.
func TestBDD_US486_ScheduleTrigger_RebuildsSnapshotOnInterval(t *testing.T) {
	root := t.TempDir()
	mat := materialize.NewMaterializer(filepath.Join(root, "parquet"))
	us486SeedBatch(t, mat, []funnel.Edit{
		{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Order",
			PrimaryKey: "o-1",
			Properties: map[string]interface{}{"status": "shipped", "qty": 7},
		},
		{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Order",
			PrimaryKey: "o-2",
			Properties: map[string]interface{}{"status": "pending", "qty": 3},
		},
	}, "b1")

	outPath := filepath.Join(root, "snapshots", "Order.json")
	s := materialize.NewScheduler()
	if err := s.Add(materialize.MaterializeJob{
		Name:        "us486.Order.snapshot",
		Interval:    30 * time.Millisecond,
		Compute:     us486SnapshotJob(mat, "us486", "Order", outPath),
		MaxAttempts: 1,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunLoop(ctx, func(name string, err error) {
			t.Errorf("unexpected onError(%q, %v)", name, err)
		})
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	// Wait until at least two ticks have stamped success.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := s.Status("us486.Order.snapshot")
		if st.TotalRuns >= 2 && !st.LastSuccess.IsZero() {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	st, _ := s.Status("us486.Order.snapshot")
	if st.TotalRuns < 2 {
		t.Fatalf("TotalRuns=%d; want >=2 ticks within 3s", st.TotalRuns)
	}
	if st.LastSuccess.IsZero() {
		t.Fatalf("LastSuccess is zero after scheduled ticks: %+v", st)
	}
	if st.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures=%d, want 0", st.ConsecutiveFailures)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap []map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("decode snapshot: %v; raw=%s", err, raw)
	}
	if len(snap) != 2 {
		t.Fatalf("snapshot rows = %d, want 2; payload=%s", len(snap), raw)
	}
	pks := []string{snap[0]["PrimaryKey"].(string), snap[1]["PrimaryKey"].(string)}
	if !((pks[0] == "o-1" && pks[1] == "o-2") || (pks[0] == "o-2" && pks[1] == "o-1")) {
		t.Fatalf("snapshot PKs = %v, want o-1 + o-2", pks)
	}
}

// Given a Compute that fails twice and then succeeds,
// When the scheduler invokes it (a single tick worth of retries),
// Then the artefact eventually lands on disk and JobStatus shows
//      ConsecutiveFailures==0 + TotalRuns==1 + LastSuccess set.
func TestBDD_US486_FailureRetry_EventuallySucceedsWithinSingleTick(t *testing.T) {
	root := t.TempDir()
	mat := materialize.NewMaterializer(filepath.Join(root, "parquet"))
	us486SeedBatch(t, mat, []funnel.Edit{
		{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Customer",
			PrimaryKey: "c-1",
			Properties: map[string]interface{}{"name": "Acme"},
		},
	}, "b1")

	outPath := filepath.Join(root, "snapshots", "Customer.json")
	var attempts int32
	flaky := func(ctx context.Context) error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return fmt.Errorf("transient failure attempt %d", n)
		}
		return us486SnapshotJob(mat, "us486", "Customer", outPath)(ctx)
	}

	s := materialize.NewScheduler()
	s.SetSleepFunc(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	if err := s.Add(materialize.MaterializeJob{
		Name:        "us486.Customer.flaky",
		Interval:    time.Hour,
		MaxAttempts: 5,
		BaseBackoff: time.Microsecond,
		Compute:     flaky,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := s.RunOnce(context.Background(), "us486.Customer.flaky"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("compute attempts = %d, want 3", got)
	}
	st, _ := s.Status("us486.Customer.flaky")
	if st.TotalRuns != 1 {
		t.Fatalf("TotalRuns=%d, want 1 (one RunOnce regardless of internal retries)", st.TotalRuns)
	}
	if st.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures=%d after eventual success, want 0", st.ConsecutiveFailures)
	}
	if st.LastSuccess.IsZero() {
		t.Fatalf("LastSuccess is zero after eventual success: %+v", st)
	}
	if st.LastError != "" {
		t.Fatalf("LastError=%q after eventual success, want empty", st.LastError)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap []map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snap) != 1 {
		t.Fatalf("snapshot rows = %d, want 1", len(snap))
	}
}

// Given a Compute that always fails,
// When the scheduler runs the job (loop or RunOnce),
// Then JobStatus.LastFailure / TotalFailures / LastError are recorded
//      and onError(jobName, err) is invoked from RunLoop without
//      tearing down the loop.
func TestBDD_US486_PersistentFailure_RecordsStateAndReportsToOnError(t *testing.T) {
	root := t.TempDir()
	mat := materialize.NewMaterializer(filepath.Join(root, "parquet"))
	us486SeedBatch(t, mat, []funnel.Edit{
		{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Order",
			PrimaryKey: "o-x",
			Properties: map[string]interface{}{"qty": 1},
		},
	}, "b1")

	boom := errors.New("compute always fails")
	s := materialize.NewScheduler()
	s.SetSleepFunc(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	if err := s.Add(materialize.MaterializeJob{
		Name:        "us486.broken",
		Interval:    20 * time.Millisecond,
		MaxAttempts: 2,
		BaseBackoff: time.Microsecond,
		Compute:     func(ctx context.Context) error { return boom },
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	errCh := make(chan struct {
		name string
		err  error
	}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunLoop(ctx, func(name string, err error) {
			select {
			case errCh <- struct {
				name string
				err  error
			}{name, err}:
			default:
			}
		})
		close(done)
	}()
	defer func() { cancel(); <-done }()

	// Wait for at least one tick to fire and report.
	var report struct {
		name string
		err  error
	}
	select {
	case report = <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("RunLoop never invoked onError after a failing job")
	}
	if report.name != "us486.broken" {
		t.Fatalf("onError name=%q, want %q", report.name, "us486.broken")
	}
	if !errors.Is(report.err, boom) {
		t.Fatalf("onError err = %v, want wraps %v", report.err, boom)
	}

	// Loop must survive the failure and tick again.
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop stopped after first failure; expected continued ticks")
	}

	st, _ := s.Status("us486.broken")
	if st.TotalFailures < 2 {
		t.Fatalf("TotalFailures=%d after >=2 failing ticks, want >=2", st.TotalFailures)
	}
	if st.ConsecutiveFailures < 2 {
		t.Fatalf("ConsecutiveFailures=%d, want >=2", st.ConsecutiveFailures)
	}
	if st.LastFailure.IsZero() {
		t.Fatalf("LastFailure is zero after failing ticks: %+v", st)
	}
	if st.LastError == "" {
		t.Fatal("LastError is empty after failing ticks")
	}
}
