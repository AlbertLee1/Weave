package weavesdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScenariosCreate_Given_StubServer_When_Create_Then_POSTsAndDecodes(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rid":          "ri.vertex.main.scenario.s1",
			"caseStudyRid": "ri.vertex.main.case-study.cs1",
			"name":         "snowstorm",
			"status":       "draft",
			"immutable":    false,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "wvk_test")
	got, err := c.Vertex.Scenarios.Create(context.Background(), ScenarioCreateInput{
		CaseStudyRID: "ri.vertex.main.case-study.cs1", Name: "snowstorm", ParentOntologyCommit: "commit-A",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.RID != "ri.vertex.main.scenario.s1" {
		t.Errorf("RID = %q", got.RID)
	}
	if gotMethod != "POST" || gotPath != "/api/vertex/v1/scenarios" {
		t.Errorf("got method=%s path=%s", gotMethod, gotPath)
	}
	if !strings.Contains(string(gotBody), "snowstorm") {
		t.Errorf("body did not include name; got %s", string(gotBody))
	}
}

func TestScenariosApplyToMain_Given_StubServer_When_Apply_Then_POSTsToApplyPath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"ontologyCommit": "commit-B"})
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Vertex.Scenarios.ApplyToMain(context.Background(), "ri.vertex.main.scenario.s1")
	if err != nil {
		t.Fatalf("ApplyToMain: %v", err)
	}
	if got["ontologyCommit"] != "commit-B" {
		t.Errorf("ontologyCommit = %v", got["ontologyCommit"])
	}
	if !strings.HasSuffix(path, "/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/apply") {
		t.Errorf("path = %s", path)
	}
}

func TestScenariosRun_Given_AcceptedStart_When_Run_Then_PollsUntilTerminalRecord(t *testing.T) {
	var paths []string
	var gotStartAccept string
	getPolls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			gotStartAccept = r.Header.Get("Accept")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"runRid": "ri.vertex.main.scenario-run.r1",
				"status": "pending",
			})
			return
		}
		getPolls++
		if getPolls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rid":         "ri.vertex.main.scenario-run.r1",
				"scenarioRid": "ri.vertex.main.scenario.s1",
				"status":      "pending",
				"startedAt":   "2026-05-20T00:00:00Z",
				"createdAt":   "2026-05-20T00:00:00Z",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rid":         "ri.vertex.main.scenario-run.r1",
			"scenarioRid": "ri.vertex.main.scenario.s1",
			"status":      "succeeded",
			"checkpoint": map[string]any{
				"runRid":       "ri.vertex.main.scenario-run.r1",
				"scenarioRid":  "ri.vertex.main.scenario.s1",
				"status":       "succeeded",
				"attemptsById": map[string]int{"score": 1},
				"updatedAt":    "2026-05-20T00:00:00Z",
			},
			"startedAt": "2026-05-20T00:00:00Z",
			"createdAt": "2026-05-20T00:00:00Z",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Vertex.Scenarios.Run(context.Background(), "ri.vertex.main.scenario.s1", &RunOptions{
		PollInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Status != "succeeded" || got.RID != "ri.vertex.main.scenario-run.r1" {
		t.Fatalf("got status=%q rid=%q, want succeeded/ri.vertex.main.scenario-run.r1", got.Status, got.RID)
	}
	if gotStartAccept == "text/event-stream" {
		t.Fatalf("POST /runs must not request SSE; Accept = %q", gotStartAccept)
	}
	wantPaths := []string{
		"POST /api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs",
		"GET /api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs/ri.vertex.main.scenario-run.r1",
		"GET /api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs/ri.vertex.main.scenario-run.r1",
	}
	if len(paths) != len(wantPaths) {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	for i := range wantPaths {
		if paths[i] != wantPaths[i] {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], wantPaths[i])
		}
	}
}

func TestScenariosRun_Given_ServerReturns500_When_Run_Then_ErrorsOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	_, err := c.Vertex.Scenarios.Run(context.Background(), "ri.vertex.main.scenario.s1", nil)
	if err == nil {
		t.Fatal("expected error from 500")
	}
}

func TestScenariosWaitRun_Given_PendingThenFailed_When_Wait_Then_PollsGetRouteAndReturnsFailureDetails(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if len(paths) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rid":         "ri.vertex.main.scenario-run.r1",
				"scenarioRid": "ri.vertex.main.scenario.s1",
				"status":      "pending",
				"checkpoint": map[string]any{
					"runRid":       "ri.vertex.main.scenario-run.r1",
					"scenarioRid":  "ri.vertex.main.scenario.s1",
					"status":       "pending",
					"attemptsById": map[string]int{},
					"updatedAt":    "2026-05-20T00:00:00Z",
				},
				"startedAt": "2026-05-20T00:00:00Z",
				"createdAt": "2026-05-20T00:00:00Z",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rid":         "ri.vertex.main.scenario-run.r1",
			"scenarioRid": "ri.vertex.main.scenario.s1",
			"status":      "failed",
			"error":       "scoring failed",
			"checkpoint": map[string]any{
				"runRid":       "ri.vertex.main.scenario-run.r1",
				"scenarioRid":  "ri.vertex.main.scenario.s1",
				"status":       "failed",
				"attemptsById": map[string]int{"score": 3},
				"error":        "scoring failed",
				"updatedAt":    "2026-05-20T00:00:00Z",
			},
			"startedAt":   "2026-05-20T00:00:00Z",
			"completedAt": "2026-05-20T00:00:01Z",
			"createdAt":   "2026-05-20T00:00:00Z",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Vertex.Scenarios.WaitRun(context.Background(), "ri.vertex.main.scenario.s1", "ri.vertex.main.scenario-run.r1", &WaitRunOptions{
		PollInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("WaitRun: %v", err)
	}
	if got.Status != "failed" || got.Error != "scoring failed" {
		t.Fatalf("got status=%q error=%q, want failed/scoring failed", got.Status, got.Error)
	}
	wantPath := "GET /api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs/ri.vertex.main.scenario-run.r1"
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want 2 GET polls", paths)
	}
	for _, gotPath := range paths {
		if gotPath != wantPath {
			t.Fatalf("path = %q, want %q", gotPath, wantPath)
		}
	}
	if got.Checkpoint == nil {
		t.Fatal("expected checkpoint details to be preserved")
	}
}

func TestScenariosWaitRun_Given_CanceledRun_When_Wait_Then_ReturnsCanceledRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rid":         "ri.vertex.main.scenario-run.r1",
			"scenarioRid": "ri.vertex.main.scenario.s1",
			"status":      "canceled",
			"error":       "operator canceled",
			"checkpoint": map[string]any{
				"runRid":       "ri.vertex.main.scenario-run.r1",
				"scenarioRid":  "ri.vertex.main.scenario.s1",
				"status":       "canceled",
				"attemptsById": map[string]int{},
				"error":        "operator canceled",
				"updatedAt":    "2026-05-20T00:00:00Z",
			},
			"startedAt":   "2026-05-20T00:00:00Z",
			"completedAt": "2026-05-20T00:00:01Z",
			"createdAt":   "2026-05-20T00:00:00Z",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	got, err := c.Vertex.Scenarios.WaitRun(context.Background(), "ri.vertex.main.scenario.s1", "ri.vertex.main.scenario-run.r1", nil)
	if err != nil {
		t.Fatalf("WaitRun: %v", err)
	}
	if got.Status != "canceled" || got.Error != "operator canceled" {
		t.Fatalf("got status=%q error=%q, want canceled/operator canceled", got.Status, got.Error)
	}
}

func TestScenariosWaitRun_Given_ContextTimeout_When_Wait_Then_ReturnsContextError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rid":         "ri.vertex.main.scenario-run.r1",
			"scenarioRid": "ri.vertex.main.scenario.s1",
			"status":      "running",
			"checkpoint": map[string]any{
				"runRid":       "ri.vertex.main.scenario-run.r1",
				"scenarioRid":  "ri.vertex.main.scenario.s1",
				"status":       "running",
				"attemptsById": map[string]int{},
				"updatedAt":    "2026-05-20T00:00:00Z",
			},
			"startedAt": "2026-05-20T00:00:00Z",
			"createdAt": "2026-05-20T00:00:00Z",
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	c := New(srv.URL, "")
	_, err := c.Vertex.Scenarios.WaitRun(ctx, "ri.vertex.main.scenario.s1", "ri.vertex.main.scenario-run.r1", &WaitRunOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected context timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("err = %v, want context deadline exceeded", err)
	}
}
