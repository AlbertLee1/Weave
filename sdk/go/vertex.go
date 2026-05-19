// Package weavesdk is the Weave Go SDK. The vertex namespace (VTX-110)
// wires Scenario create / run / apply_to_main against the same HTTP API
// the Python and TypeScript SDKs hit. Run uses a buffered channel so
// callers can `for ev := range ch` to consume SSE Run events without
// owning the underlying SSE wire format.
//
// The transport is decoupled via the Doer interface; the unit suite
// drives the client with a httptest.Server.
package weavesdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Doer is the minimal HTTP interface the client depends on so tests can
// inject httptest.Server clients.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is the top-level SDK entry point.
type Client struct {
	BaseURL string
	HTTP    Doer
	APIKey  string

	Vertex *VertexService
}

// New constructs a Client with sensible defaults.
func New(baseURL string, apiKey string) *Client {
	c := &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    http.DefaultClient,
		APIKey:  apiKey,
	}
	c.Vertex = &VertexService{client: c, Scenarios: &ScenariosService{client: c}}
	return c
}

// VertexService is exposed at client.Vertex.
type VertexService struct {
	client    *Client
	Scenarios *ScenariosService
}

// ScenariosService is exposed at client.Vertex.Scenarios.
type ScenariosService struct {
	client *Client
}

// Scenario mirrors the wire shape returned by /api/vertex/v1/scenarios.
type Scenario struct {
	RID                  string `json:"rid"`
	CaseStudyRID         string `json:"caseStudyRid"`
	Name                 string `json:"name"`
	Status               string `json:"status"`
	Immutable            bool   `json:"immutable"`
	ParentOntologyCommit string `json:"parentOntologyCommit,omitempty"`
}

// ScenarioCreateInput names the fields used by Scenarios.Create.
type ScenarioCreateInput struct {
	CaseStudyRID         string `json:"caseStudyRid"`
	Name                 string `json:"name"`
	ParentOntologyCommit string `json:"parentOntologyCommit"`
}

// RunEvent is one event from the SSE Run stream. Kind names mirror the
// TypeScript SDK union; Payload carries the rest of the fields verbatim
// so callers can json.Unmarshal into their own shape.
type RunEvent struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"-"`
}

// RunOptions tunes Scenarios.Run.
type RunOptions struct {
	BufferSize int
}

// ScenarioRunRecord mirrors the GET /api/vertex/v1/scenarios/{rid}/runs/{runRid}
// payload. Checkpoint remains a generic map so SDK callers keep all server
// details without waiting for typed retry/checkpoint helpers.
type ScenarioRunRecord struct {
	RID         string         `json:"rid"`
	ScenarioRID string         `json:"scenarioRid"`
	Status      string         `json:"status"`
	Error       string         `json:"error,omitempty"`
	Checkpoint  map[string]any `json:"checkpoint,omitempty"`
	StartedAt   time.Time      `json:"startedAt,omitempty"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
	CreatedAt   time.Time      `json:"createdAt,omitempty"`
}

// WaitRunOptions tunes Scenarios.WaitRun.
type WaitRunOptions struct {
	PollInterval time.Duration
}

// Create POSTs to /api/vertex/v1/scenarios.
func (s *ScenariosService) Create(ctx context.Context, in ScenarioCreateInput) (*Scenario, error) {
	var out Scenario
	if err := s.client.postJSON(ctx, "/api/vertex/v1/scenarios", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyToMain POSTs to /api/vertex/v1/scenarios/{rid}/apply.
func (s *ScenariosService) ApplyToMain(ctx context.Context, scenarioRID string) (map[string]any, error) {
	out := map[string]any{}
	path := "/api/vertex/v1/scenarios/" + scenarioRID + "/apply"
	if err := s.client.postJSON(ctx, path, struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Run opens an SSE stream against /api/vertex/v1/scenarios/{rid}/runs and
// returns a channel that receives every parsed RunEvent. The channel
// closes when the stream ends (or ctx is canceled). Callers MUST drain
// the channel to avoid leaking the underlying goroutine.
func (s *ScenariosService) Run(ctx context.Context, scenarioRID string, opts *RunOptions) (<-chan RunEvent, error) {
	bufSize := 16
	if opts != nil && opts.BufferSize > 0 {
		bufSize = opts.BufferSize
	}
	url := s.client.BaseURL + "/api/vertex/v1/scenarios/" + scenarioRID + "/runs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	s.client.applyAuth(req)
	resp, err := s.client.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("vertex.scenarios.run: %d %s", resp.StatusCode, string(body))
	}
	ch := make(chan RunEvent, bufSize)
	go pumpSSE(resp.Body, ch)
	return ch, nil
}

// GetRun fetches a persisted scenario-run record from the canonical
// /api/vertex/v1/scenarios/{rid}/runs/{runRid} route.
func (s *ScenariosService) GetRun(ctx context.Context, scenarioRID string, runRID string) (*ScenarioRunRecord, error) {
	var out ScenarioRunRecord
	path := scenarioRunRecordPath(scenarioRID, runRID)
	if err := s.client.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitRun polls GetRun until the scenario run reaches a terminal state or ctx
// is canceled. Use context.WithTimeout to bound total wait time.
func (s *ScenariosService) WaitRun(ctx context.Context, scenarioRID string, runRID string, opts *WaitRunOptions) (*ScenarioRunRecord, error) {
	interval := time.Second
	if opts != nil && opts.PollInterval > 0 {
		interval = opts.PollInterval
	}
	for {
		run, err := s.GetRun(ctx, scenarioRID, runRID)
		if err != nil {
			return nil, err
		}
		if isTerminalScenarioRunStatus(run.Status) {
			return run, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func scenarioRunRecordPath(scenarioRID string, runRID string) string {
	return "/api/vertex/v1/scenarios/" + url.PathEscape(scenarioRID) + "/runs/" + url.PathEscape(runRID)
}

func isTerminalScenarioRunStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "canceled"
}

func pumpSSE(body io.ReadCloser, ch chan<- RunEvent) {
	defer close(ch)
	defer body.Close()

	reader := bufio.NewReader(body)
	var dataBuf bytes.Buffer
	flush := func() {
		if dataBuf.Len() == 0 {
			return
		}
		raw := append([]byte(nil), dataBuf.Bytes()...)
		dataBuf.Reset()
		var probe struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return
		}
		ch <- RunEvent{Kind: probe.Kind, Payload: raw}
	}
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				flush()
			} else if strings.HasPrefix(line, "data:") {
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.WriteString(strings.TrimLeft(line[len("data:"):], " "))
			}
		}
		if err != nil {
			flush()
			return
		}
	}
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("vertex SDK: %d %s", resp.StatusCode, string(respBody))
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	c.applyAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("vertex SDK: %d %s", resp.StatusCode, string(respBody))
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *Client) applyAuth(req *http.Request) {
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
}
