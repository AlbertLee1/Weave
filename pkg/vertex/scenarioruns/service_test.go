package scenarioruns_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/vertex/scenarioruns"
)

// memRepo is an in-memory scenario_runs repo. It also serves as the
// Persister the workflow writes to — proving that the repo + persister
// shapes are compatible (the real PG impl will satisfy both).
type memRepo struct {
	mu   sync.Mutex
	runs map[string]scenarioruns.Run
}

func newMemRepo() *memRepo {
	return &memRepo{runs: make(map[string]scenarioruns.Run)}
}

func (m *memRepo) CreateRun(_ context.Context, r scenarioruns.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.RID] = r
	return nil
}

func (m *memRepo) GetRun(_ context.Context, rid string) (scenarioruns.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[rid]
	if !ok {
		return scenarioruns.Run{}, scenarioruns.ErrRunNotFound
	}
	return r, nil
}

func (m *memRepo) SaveCheckpoint(_ context.Context, cp scenarioruns.RunCheckpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[cp.RunRID]
	if !ok {
		return scenarioruns.ErrRunNotFound
	}
	r.Status = cp.Status
	r.Error = cp.Error
	r.Checkpoint = cp
	if scenarioruns.IsTerminal(cp.Status) {
		t := cp.UpdatedAt
		r.CompletedAt = &t
	}
	m.runs[cp.RunRID] = r
	return nil
}

func (m *memRepo) ListResumable(_ context.Context) ([]scenarioruns.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []scenarioruns.Run{}
	for _, r := range m.runs {
		if scenarioruns.IsResumable(r.Status) {
			out = append(out, r)
		}
	}
	return out, nil
}

// stubScenarioReader resolves a scenario into its activities. Real impl
// will join scenarios + actions + models from the OMS / Vertex tables.
type stubScenarioReader struct {
	activities map[string][]scenarioruns.Activity
}

func (s *stubScenarioReader) ListActivities(_ context.Context, scenarioRID string) ([]scenarioruns.Activity, error) {
	a, ok := s.activities[scenarioRID]
	if !ok {
		return nil, errors.New("scenario not found")
	}
	return a, nil
}

func TestService_Given_3ModelsAnd2Actions_When_Run_Then_WorkflowKickedOff(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{
		activities: map[string][]scenarioruns.Activity{
			"scn1": {
				{ID: "m1", Kind: scenarioruns.ActivityKindModel, Layer: 0},
				{ID: "m2", Kind: scenarioruns.ActivityKindModel, Layer: 1},
				{ID: "m3", Kind: scenarioruns.ActivityKindModel, Layer: 1},
				{ID: "a1", Kind: scenarioruns.ActivityKindAction, Layer: 2},
				{ID: "a2", Kind: scenarioruns.ActivityKindAction, Layer: 2},
			},
		},
	}
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{
		Policy: scenarioruns.RetryPolicy{MaxAttempts: 3},
		Sleep:  func(time.Duration) {},
	})
	defer svc.Stop(context.Background())

	runRID, err := svc.Run(context.Background(), "scn1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Wait for run to terminate (poll up to 2s).
	if !waitFor(t, 2*time.Second, func() bool {
		r, err := repo.GetRun(context.Background(), runRID)
		return err == nil && scenarioruns.IsTerminal(r.Status)
	}) {
		t.Fatal("run never reached terminal state")
	}
	r, _ := repo.GetRun(context.Background(), runRID)
	if r.Status != scenarioruns.RunStatusSucceeded {
		t.Fatalf("status: got %q want succeeded", r.Status)
	}
	if len(r.Checkpoint.Completed) != 5 {
		t.Fatalf("completed: got %d want 5", len(r.Checkpoint.Completed))
	}
}

func TestService_Given_LongRunningWorkflow_When_Cancel_Then_RunMarkedCanceled(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{
		activities: map[string][]scenarioruns.Activity{
			"scn1": {{ID: "long", Kind: scenarioruns.ActivityKindAction, Layer: 0}},
		},
	}
	started := make(chan struct{})
	exec := func(ctx context.Context, _ scenarioruns.Activity) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{
		Policy: scenarioruns.RetryPolicy{MaxAttempts: 1},
		Sleep:  func(time.Duration) {},
	})
	defer svc.Stop(context.Background())

	runRID, err := svc.Run(context.Background(), "scn1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	<-started
	if err := svc.Cancel(context.Background(), runRID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		r, err := repo.GetRun(context.Background(), runRID)
		return err == nil && scenarioruns.IsTerminal(r.Status)
	}) {
		t.Fatal("run never reached terminal state")
	}
	r, _ := repo.GetRun(context.Background(), runRID)
	if r.Status != scenarioruns.RunStatusCanceled {
		t.Fatalf("status: got %q want canceled", r.Status)
	}
}

func TestService_Given_AlreadyTerminalRun_When_Cancel_Then_ErrAlreadyTerminal(t *testing.T) {
	repo := newMemRepo()
	terminalRun := scenarioruns.Run{
		RID:         "ri.vertex.main.scenario-run.done",
		ScenarioRID: "scn1",
		Status:      scenarioruns.RunStatusSucceeded,
	}
	if err := repo.CreateRun(context.Background(), terminalRun); err != nil {
		t.Fatal(err)
	}
	reader := &stubScenarioReader{}
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{})
	defer svc.Stop(context.Background())

	err := svc.Cancel(context.Background(), terminalRun.RID)
	if !errors.Is(err, scenarioruns.ErrAlreadyTerminal) {
		t.Fatalf("err: got %v want ErrAlreadyTerminal", err)
	}
}

func TestService_Given_UnknownRun_When_Cancel_Then_ErrRunNotFound(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{}
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{})
	defer svc.Stop(context.Background())

	err := svc.Cancel(context.Background(), "ri.vertex.main.scenario-run.zzz")
	if !errors.Is(err, scenarioruns.ErrRunNotFound) {
		t.Fatalf("err: got %v want ErrRunNotFound", err)
	}
}

func TestService_Given_PriorCheckpointAfterCrash_When_Resume_Then_SkipsCompleted(t *testing.T) {
	// Simulate a crash mid-run: persisted Run has Checkpoint listing
	// a1 already completed; service.Resume picks it up and only runs a2.
	repo := newMemRepo()
	priorRun := scenarioruns.Run{
		RID:         "ri.vertex.main.scenario-run.r1",
		ScenarioRID: "scn1",
		Status:      scenarioruns.RunStatusRunning,
		Checkpoint: scenarioruns.RunCheckpoint{
			RunRID:       "ri.vertex.main.scenario-run.r1",
			ScenarioRID:  "scn1",
			Status:       scenarioruns.RunStatusRunning,
			Completed:    []string{"a1"},
			AttemptsByID: map[string]int{"a1": 1},
		},
	}
	if err := repo.CreateRun(context.Background(), priorRun); err != nil {
		t.Fatal(err)
	}
	reader := &stubScenarioReader{
		activities: map[string][]scenarioruns.Activity{
			"scn1": {
				{ID: "a1", Layer: 0},
				{ID: "a2", Layer: 1},
			},
		},
	}
	var ranIDs []string
	var mu sync.Mutex
	exec := func(_ context.Context, a scenarioruns.Activity) error {
		mu.Lock()
		ranIDs = append(ranIDs, a.ID)
		mu.Unlock()
		return nil
	}
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{
		Sleep: func(time.Duration) {},
	})
	defer svc.Stop(context.Background())

	resumed, err := svc.ResumeAll(context.Background())
	if err != nil {
		t.Fatalf("resumeAll: %v", err)
	}
	if len(resumed) != 1 || resumed[0] != priorRun.RID {
		t.Fatalf("resumed: got %v want [%s]", resumed, priorRun.RID)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		r, err := repo.GetRun(context.Background(), priorRun.RID)
		return err == nil && scenarioruns.IsTerminal(r.Status)
	}) {
		t.Fatal("resumed run never reached terminal state")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranIDs) != 1 || ranIDs[0] != "a2" {
		t.Fatalf("ran: got %v want only [a2]", ranIDs)
	}
	r, _ := repo.GetRun(context.Background(), priorRun.RID)
	if r.Status != scenarioruns.RunStatusSucceeded {
		t.Fatalf("status: got %q want succeeded", r.Status)
	}
}

func TestService_Given_ConcurrencyLimit_When_RunMany_Then_RespectsCap(t *testing.T) {
	repo := newMemRepo()
	const N = 8
	activities := map[string][]scenarioruns.Activity{}
	for i := 0; i < N; i++ {
		activities[scnID(i)] = []scenarioruns.Activity{{ID: "a", Layer: 0}}
	}
	reader := &stubScenarioReader{activities: activities}

	var inFlight, peak int32
	gate := make(chan struct{})
	exec := func(_ context.Context, _ scenarioruns.Activity) error {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
				break
			}
		}
		<-gate
		atomic.AddInt32(&inFlight, -1)
		return nil
	}
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{
		MaxConcurrentWorkflows: 3,
		Sleep:                  func(time.Duration) {},
	})
	defer svc.Stop(context.Background())

	rids := make([]string, 0, N)
	for i := 0; i < N; i++ {
		rid, err := svc.Run(context.Background(), scnID(i))
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		rids = append(rids, rid)
	}
	// Give workers time to ramp up to peak.
	if !waitFor(t, 1*time.Second, func() bool {
		return atomic.LoadInt32(&inFlight) >= 3
	}) {
		t.Fatalf("never reached 3 in flight (peak=%d)", atomic.LoadInt32(&peak))
	}
	if got := atomic.LoadInt32(&peak); got > 3 {
		t.Fatalf("peak: got %d want ≤ 3", got)
	}
	close(gate)
	for _, rid := range rids {
		if !waitFor(t, 2*time.Second, func() bool {
			r, err := repo.GetRun(context.Background(), rid)
			return err == nil && scenarioruns.IsTerminal(r.Status)
		}) {
			t.Fatalf("run %s never finished", rid)
		}
	}
}

func scnID(i int) string {
	return "scn-" + string(rune('0'+i))
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}
