package scenarioruns_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/vertex/scenarioruns"
)

func TestHandler_Given_ScenarioWithActivities_When_PostRun_Then_Returns202WithRunRID(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{
		activities: map[string][]scenarioruns.Activity{
			"scn1": {{ID: "a1", Layer: 0}},
		},
	}
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{
		Sleep: func(time.Duration) {},
	})
	defer svc.Stop(context.Background())

	router := chi.NewRouter()
	scenarioruns.NewHandler(svc).RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/vertex/v1/scenarios/scn1/runs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202\nbody: %s", w.Code, w.Body.String())
	}
	var resp struct {
		RunRID string `json:"runRid"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, w.Body.String())
	}
	if resp.RunRID == "" {
		t.Fatal("empty runRid")
	}
	if resp.Status != string(scenarioruns.RunStatusPending) && resp.Status != string(scenarioruns.RunStatusRunning) {
		t.Fatalf("unexpected initial status: %q", resp.Status)
	}
}

func TestHandler_Given_RunInProgress_When_PostCancel_Then_Returns202(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{
		activities: map[string][]scenarioruns.Activity{
			"scn1": {{ID: "long", Layer: 0}},
		},
	}
	started := make(chan struct{})
	once := &sync.Once{}
	exec := func(ctx context.Context, _ scenarioruns.Activity) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	}
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{
		Policy: scenarioruns.RetryPolicy{MaxAttempts: 1},
		Sleep:  func(time.Duration) {},
	})
	defer svc.Stop(context.Background())

	router := chi.NewRouter()
	scenarioruns.NewHandler(svc).RegisterRoutes(router)

	// Kick off run
	startReq := httptest.NewRequest(http.MethodPost, "/api/vertex/v1/scenarios/scn1/runs", nil)
	startW := httptest.NewRecorder()
	router.ServeHTTP(startW, startReq)
	if startW.Code != http.StatusAccepted {
		t.Fatalf("start status: got %d want 202", startW.Code)
	}
	var startResp struct {
		RunRID string `json:"runRid"`
	}
	_ = json.Unmarshal(startW.Body.Bytes(), &startResp)

	<-started

	cancelReq := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/scenarios/scn1/runs/"+startResp.RunRID+"/cancel", nil)
	cancelW := httptest.NewRecorder()
	router.ServeHTTP(cancelW, cancelReq)
	if cancelW.Code != http.StatusAccepted {
		t.Fatalf("cancel status: got %d want 202\nbody: %s", cancelW.Code, cancelW.Body.String())
	}
}

func TestHandler_Given_UnknownRun_When_PostCancel_Then_404(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{}
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{})
	defer svc.Stop(context.Background())

	router := chi.NewRouter()
	scenarioruns.NewHandler(svc).RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/scenarios/scn1/runs/ri.vertex.main.scenario-run.zzz/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404\nbody: %s", w.Code, w.Body.String())
	}
}

func TestHandler_Given_FinishedRun_When_GetRun_Then_ReturnsCheckpoint(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{
		activities: map[string][]scenarioruns.Activity{
			"scn1": {{ID: "a1", Layer: 0}},
		},
	}
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{
		Sleep: func(time.Duration) {},
	})
	defer svc.Stop(context.Background())

	runRID, err := svc.Run(context.Background(), "scn1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		r, err := repo.GetRun(context.Background(), runRID)
		return err == nil && scenarioruns.IsTerminal(r.Status)
	}) {
		t.Fatal("run never finished")
	}

	router := chi.NewRouter()
	scenarioruns.NewHandler(svc).RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodGet,
		"/api/vertex/v1/scenarios/scn1/runs/"+runRID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}
	var resp struct {
		RunRID string `json:"runRid"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != string(scenarioruns.RunStatusSucceeded) {
		t.Fatalf("status: got %q want succeeded", resp.Status)
	}
}

func TestHandler_Given_UnknownScenario_When_PostRun_Then_404(t *testing.T) {
	repo := newMemRepo()
	reader := &stubScenarioReader{}
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	svc := scenarioruns.NewService(repo, reader, exec, scenarioruns.ServiceOptions{})
	defer svc.Stop(context.Background())

	router := chi.NewRouter()
	scenarioruns.NewHandler(svc).RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/vertex/v1/scenarios/zzz/runs", bytes.NewReader(nil))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 404 or 400", w.Code)
	}
}
