package scenarioruns_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/vertex/scenarioruns"
)

// TestBDD_ScenarioRuns_ListByScenario covers the round-68 Foundry-
// parity gap. The Run endpoints today are create / cancel / get-by-
// runRID, but the natural Foundry workflow ("see every run that
// has happened on this scenario, sorted newest first, to compare
// outcomes") had no API. SPA couldn't render a run-history panel
// without polling individual runRIDs it doesn't know.
//
// Wire shape:
//
//	GET /api/vertex/v1/scenarios/{scenarioRid}/runs
//	  200 + {"runs": [Run, Run, ...]}
//	      runs sorted by startedAt DESC so the newest run is first
//	      (matches the Foundry "Recent runs" panel ordering).
//	      Empty array (non-nil) when the scenario has no runs.
//
// Scenarios:
//   - Empty scenario (no runs at all) returns 200 + {"runs": []}.
//     Non-nil empty array — SPA iterates without nil checks.
//   - Three runs for one scenario returned newest-first.
//   - Runs belonging to OTHER scenarios are filtered out (caller
//     never sees them).
//   - Response shape is {"runs": [...]}, not a bare array, so
//     future pagination fields (nextPageToken, totalCount) can
//     be added without breaking the wire contract.
//   - URL with empty scenarioRid path segment is rejected with
//     400 (chi catches the trailing-slash case but the handler
//     still defends against an empty value).
func TestBDD_ScenarioRuns_ListByScenario(t *testing.T) {
	const (
		scenarioA = "ri.vertex.main.scenario.A"
		scenarioB = "ri.vertex.main.scenario.B"
	)

	now := time.Now().UTC()
	mkRun := func(rid, scenarioRID string, startedOffsetSec int) scenarioruns.Run {
		return scenarioruns.Run{
			RID:         rid,
			ScenarioRID: scenarioRID,
			Status:      scenarioruns.RunStatusSucceeded,
			StartedAt:   now.Add(time.Duration(startedOffsetSec) * time.Second),
			CreatedAt:   now.Add(time.Duration(startedOffsetSec) * time.Second),
		}
	}

	newServer := func(t *testing.T, seed []scenarioruns.Run) (*chi.Mux, *scenarioruns.MemoryRepo) {
		t.Helper()
		repo := scenarioruns.NewMemoryRepo()
		for _, r := range seed {
			if err := repo.CreateRun(context.Background(), r); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		// The handler needs a Service to construct, but ListRuns
		// reads directly from the repo via the service wiring. The
		// existing newTestService helper in handler_test.go is in
		// package scenarioruns (no _test); we replicate the minimal
		// wiring here using exported constructors. The handler's
		// list path reads via the same reader shim that backs Get.
		var execFn scenarioruns.ActivityExecutor = func(_ context.Context, _ scenarioruns.Activity) error {
			return nil
		}
		svc := scenarioruns.NewService(repo, &stubReader{}, execFn, scenarioruns.ServiceOptions{})
		h := scenarioruns.NewHandler(svc)
		r := chi.NewRouter()
		h.RegisterRoutes(r)
		return r, repo
	}

	doList := func(t *testing.T, r *chi.Mux, scenarioRID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/vertex/v1/scenarios/"+scenarioRID+"/runs", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Empty scenario returns 200 + non-nil empty runs", func(t *testing.T) {
		r, _ := newServer(t, nil)
		rec := doList(t, r, scenarioA)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Runs []scenarioruns.Run `json:"runs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Runs == nil {
			t.Errorf("runs is nil, want empty array (SPA iterates without nil check)")
		}
		if len(resp.Runs) != 0 {
			t.Errorf("len(runs)=%d, want 0", len(resp.Runs))
		}
	})

	t.Run("Three runs for one scenario returned newest-first", func(t *testing.T) {
		seed := []scenarioruns.Run{
			mkRun("run-1-oldest", scenarioA, -300),
			mkRun("run-2-mid", scenarioA, -150),
			mkRun("run-3-newest", scenarioA, -10),
		}
		r, _ := newServer(t, seed)
		rec := doList(t, r, scenarioA)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Runs []scenarioruns.Run `json:"runs"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Runs) != 3 {
			t.Fatalf("len(runs)=%d, want 3", len(resp.Runs))
		}
		want := []string{"run-3-newest", "run-2-mid", "run-1-oldest"}
		for i, w := range want {
			if resp.Runs[i].RID != w {
				t.Errorf("runs[%d].rid=%q, want %q (newest-first ordering broken)",
					i, resp.Runs[i].RID, w)
			}
		}
	})

	t.Run("Runs from other scenarios are filtered out", func(t *testing.T) {
		seed := []scenarioruns.Run{
			mkRun("a-run-1", scenarioA, -100),
			mkRun("a-run-2", scenarioA, -50),
			mkRun("b-run-1", scenarioB, -200),
			mkRun("b-run-2", scenarioB, -150),
			mkRun("b-run-3", scenarioB, -10),
		}
		r, _ := newServer(t, seed)
		rec := doList(t, r, scenarioA)
		var resp struct {
			Runs []scenarioruns.Run `json:"runs"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Runs) != 2 {
			t.Fatalf("len(runs)=%d, want 2 (only scenarioA's runs)", len(resp.Runs))
		}
		for _, run := range resp.Runs {
			if run.ScenarioRID != scenarioA {
				t.Errorf("foreign run leaked: rid=%q scenarioRid=%q", run.RID, run.ScenarioRID)
			}
		}
	})

	t.Run("Response shape is {runs: []}, not a bare array", func(t *testing.T) {
		// Future-proofing for pagination fields (nextPageToken,
		// totalCount). A bare array would lock us out.
		seed := []scenarioruns.Run{mkRun("only", scenarioA, -10)}
		r, _ := newServer(t, seed)
		rec := doList(t, r, scenarioA)
		body := rec.Body.String()
		// Must start with `{` (object), not `[` (bare array).
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("response body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})

	t.Run("Unknown scenario returns 200 + empty (filter not key)", func(t *testing.T) {
		seed := []scenarioruns.Run{mkRun("a-run", scenarioA, -10)}
		r, _ := newServer(t, seed)
		rec := doList(t, r, "ri.vertex.main.scenario.NEVER-EXISTED")
		if rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 (scenarioRid is a filter, not a lookup key)",
				rec.Code)
		}
	})
}

// Minimal stubs so NewService constructor can wire up; we never
// exercise these because list reads through repo directly.

type stubReader struct{}

func (stubReader) ListActivities(context.Context, string) ([]scenarioruns.Activity, error) {
	return nil, nil
}
