package weavesdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestScenariosRun_Given_SSEStream_When_Run_Then_ChannelEmitsEventsInOrder(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, raw := range []string{
			`{"kind":"progress","percent":25}`,
			`{"kind":"progress","percent":100}`,
			`{"kind":"completed","scenarioRunRid":"ri.vertex.main.scenario-run.r1"}`,
		} {
			_, _ = w.Write([]byte("data: " + raw + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	ch, err := c.Vertex.Scenarios.Run(context.Background(), "ri.vertex.main.scenario.s1", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var kinds []string
	for ev := range ch {
		kinds = append(kinds, ev.Kind)
	}
	want := []string{"progress", "progress", "completed"}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("kinds[%d] = %s, want %s", i, kinds[i], want[i])
		}
	}
	if gotPath != "/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs" {
		t.Errorf("path = %s, want /api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs", gotPath)
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
