package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// capturingServer is an httptest.Server that records every inbound request
// (method, path, body bytes, headers) and lets each test case stitch a custom
// response onto the next request. It is the OSV2-304 BDD harness for the new
// action / aggregate / objectset CLI subcommands.
type capturedRequest struct {
	Method string
	Path   string
	Body   []byte
	Header http.Header
}

type capturingServer struct {
	mu       sync.Mutex
	requests []capturedRequest
	router   func(r *http.Request) (status int, body string)
	srv      *httptest.Server
}

func newCapturingServer(t *testing.T, router func(r *http.Request) (status int, body string)) *capturingServer {
	t.Helper()
	c := &capturingServer{router: router}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.requests = append(c.requests, capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   body,
			Header: r.Header.Clone(),
		})
		c.mu.Unlock()
		status, respBody := c.router(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *capturingServer) Requests() []capturedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedRequest, len(c.requests))
	copy(out, c.requests)
	return out
}

// configuredCLI returns a tempdir prewired to talk to srv with a fake token.
func configuredCLI(t *testing.T, srv *capturingServer) string {
	t.Helper()
	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.srv.URL); exit != 0 {
		t.Fatalf("config set base_url failed")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "test-token"); exit != 0 {
		t.Fatalf("config set access_token failed")
	}
	return tmp
}

// --- action apply ----------------------------------------------------------

func TestActionApply_GivenParamsKVAndReturnEdits_When_Apply_Then_RequestBodyMatchesAndOutputContainsValid_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 200, `{"validation":{"result":"VALID"},"edits":{"type":"edits","addedObjectCount":1,"modifiedObjectsCount":0,"deletedObjectsCount":0}}`
	})
	tmp := configuredCLI(t, srv)

	stdout, stderr, exit := runCLIWith(t, tmp,
		"action", "apply",
		"--ontology", "northwind",
		"--action", "create-order",
		"--param", "customer=ALFKI",
		"--param", "qty=3",
		"--returnEdits", "ALL_V2",
	)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}

	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Method != http.MethodPost ||
		reqs[0].Path != "/api/v2/ontologies/northwind/actions/create-order/apply" {
		t.Fatalf("request shape: %s %s", reqs[0].Method, reqs[0].Path)
	}

	var got map[string]any
	if err := json.Unmarshal(reqs[0].Body, &got); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, reqs[0].Body)
	}
	params, _ := got["parameters"].(map[string]any)
	if params["customer"] != "ALFKI" {
		t.Errorf("parameters.customer = %v, want 'ALFKI'", params["customer"])
	}
	// --param values stay as strings (no auto-coercion).
	if params["qty"] != "3" {
		t.Errorf("parameters.qty = %v (type %T), want '3' (string)", params["qty"], params["qty"])
	}
	opts, _ := got["options"].(map[string]any)
	if opts["returnEdits"] != "ALL_V2_WITH_DELETIONS" {
		t.Errorf("options.returnEdits = %v, want 'ALL_V2_WITH_DELETIONS'", opts["returnEdits"])
	}
	if !strings.Contains(stdout, "VALID") {
		t.Errorf("stdout should contain 'VALID', got %q", stdout)
	}
}

func TestActionApply_GivenParamsFile_When_Apply_Then_NumericTypePreserved_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 200, `{"validation":{"result":"VALID"}}`
	})
	tmp := configuredCLI(t, srv)

	paramsFile := filepath.Join(t.TempDir(), "params.json")
	if err := os.WriteFile(paramsFile, []byte(`{"customer":"ALFKI","qty":3}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, exit := runCLIWith(t, tmp,
		"action", "apply",
		"--ontology", "northwind",
		"--action", "create-order",
		"--params", "@"+paramsFile,
	)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}

	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	var got map[string]any
	if err := json.Unmarshal(reqs[0].Body, &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	params, _ := got["parameters"].(map[string]any)
	if params["customer"] != "ALFKI" {
		t.Errorf("parameters.customer = %v", params["customer"])
	}
	// JSON numbers survive as float64 through the unmarshal.
	qty, ok := params["qty"].(float64)
	if !ok || qty != 3 {
		t.Errorf("parameters.qty = %v (type %T), want number 3", params["qty"], params["qty"])
	}
}

func TestActionApply_GivenMissingActionFlag_When_Apply_Then_ExitTwoNoRequest_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 500, `{"errorCode":"INTERNAL","errorName":"Internal"}`
	})
	tmp := configuredCLI(t, srv)

	_, stderr, exit := runCLIWith(t, tmp,
		"action", "apply",
		"--ontology", "northwind",
		"--param", "x=1",
	)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	low := strings.ToLower(stderr)
	if !strings.Contains(low, "required") || !(strings.Contains(low, "action") || strings.Contains(low, "usage")) {
		t.Errorf("stderr missing required-flag hint: %q", stderr)
	}
	if got := len(srv.Requests()); got != 0 {
		t.Errorf("expected 0 requests when validation fails, got %d", got)
	}
}

func TestActionApply_GivenServerError_When_Apply_Then_ExitOneStderrIncludesErrorCode_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 400, `{"errorCode":"InvalidArgument","errorName":"ParameterValidationFailed","errorInstanceId":"abc"}`
	})
	tmp := configuredCLI(t, srv)

	_, stderr, exit := runCLIWith(t, tmp,
		"action", "apply",
		"--ontology", "northwind",
		"--action", "create-order",
		"--param", "x=1",
	)
	if exit == 0 {
		t.Fatalf("expected non-zero exit on server error")
	}
	if !strings.Contains(stderr, "InvalidArgument") && !strings.Contains(stderr, "ParameterValidationFailed") {
		t.Errorf("stderr should surface error code/name: %q", stderr)
	}
}

// --- aggregate -------------------------------------------------------------

func TestAggregate_GivenBodyFileAndTableOutput_When_Aggregate_Then_RequestForwardsBodyAndTableRendered_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 200, `{"data":{"country":[{"key":"USA","value":42},{"key":"DE","value":9}]}}`
	})
	tmp := configuredCLI(t, srv)

	bodyFile := filepath.Join(t.TempDir(), "agg.json")
	bodyJSON := `{"aggregations":[{"metric":"count","name":"total","groupBy":[{"type":"exactValue","field":"country"}]}]}`
	if err := os.WriteFile(bodyFile, []byte(bodyJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exit := runCLIWith(t, tmp,
		"aggregate",
		"--ontology", "northwind",
		"--type", "Order",
		"--body", "@"+bodyFile,
		"--output", "table",
	)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Path != "/api/v2/ontologies/northwind/objects/Order/aggregate" {
		t.Errorf("path = %q", reqs[0].Path)
	}
	if !strings.Contains(string(reqs[0].Body), `"metric":"count"`) {
		t.Errorf("request body did not forward original JSON: %q", reqs[0].Body)
	}
	if !strings.Contains(stdout, "USA") || !strings.Contains(stdout, "42") ||
		!strings.Contains(stdout, "DE") || !strings.Contains(stdout, "9") {
		t.Errorf("table output missing rows: %q", stdout)
	}
}

func TestAggregate_GivenJSONOutput_When_Aggregate_Then_StdoutValidJSON_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 200, `{"data":{"total":[{"value":99}]}}`
	})
	tmp := configuredCLI(t, srv)

	bodyFile := filepath.Join(t.TempDir(), "agg.json")
	_ = os.WriteFile(bodyFile, []byte(`{"aggregations":[]}`), 0o644)

	stdout, stderr, exit := runCLIWith(t, tmp,
		"aggregate",
		"--ontology", "northwind",
		"--type", "Order",
		"--body", "@"+bodyFile,
	)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Errorf("stdout not valid JSON: %v (%q)", err, stdout)
	}
}

func TestAggregate_GivenMissingBody_When_Aggregate_Then_ExitTwoNoRequest_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 200, `{}`
	})
	tmp := configuredCLI(t, srv)

	_, stderr, exit := runCLIWith(t, tmp,
		"aggregate",
		"--ontology", "northwind",
		"--type", "Order",
	)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	if !strings.Contains(strings.ToLower(stderr), "body") {
		t.Errorf("stderr missing --body hint: %q", stderr)
	}
	if got := len(srv.Requests()); got != 0 {
		t.Errorf("expected 0 requests, got %d", got)
	}
}

// --- objectset -------------------------------------------------------------

func TestObjectSet_CreateTemporary_GivenBodyFile_When_Run_Then_ReturnsRid_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 200, `{"objectSetRid":"ri.oss.main.objectset.tmp-123"}`
	})
	tmp := configuredCLI(t, srv)

	bodyFile := filepath.Join(t.TempDir(), "def.json")
	defBody := `{"objectSet":{"type":"base","objectType":"Customer"}}`
	if err := os.WriteFile(bodyFile, []byte(defBody), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exit := runCLIWith(t, tmp,
		"objectset", "create-temporary",
		"--ontology", "northwind",
		"--body", "@"+bodyFile,
	)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Path != "/api/v2/ontologies/northwind/objectSets/createTemporary" {
		t.Errorf("path = %q", reqs[0].Path)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout not JSON: %v (%q)", err, stdout)
	}
	if got["objectSetRid"] != "ri.oss.main.objectset.tmp-123" {
		t.Errorf("objectSetRid = %v", got["objectSetRid"])
	}
}

func TestObjectSet_Load_GivenBodyFile_When_Run_Then_ForwardsAndReturnsData_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) {
		return 200, `{"data":[{"__primaryKey":"ALFKI"}],"totalCount":"1"}`
	})
	tmp := configuredCLI(t, srv)

	bodyFile := filepath.Join(t.TempDir(), "load.json")
	loadBody := `{"objectSet":{"type":"base","objectType":"Customer"},"select":["companyName"]}`
	_ = os.WriteFile(bodyFile, []byte(loadBody), 0o644)

	stdout, stderr, exit := runCLIWith(t, tmp,
		"objectset", "load",
		"--ontology", "northwind",
		"--body", "@"+bodyFile,
	)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/api/v2/ontologies/northwind/objectSets/load" {
		t.Fatalf("request shape: %+v", reqs)
	}
	if !strings.Contains(stdout, "ALFKI") {
		t.Errorf("stdout missing primary key: %q", stdout)
	}
}

func TestObjectSet_UnknownSub_When_Run_Then_ExitTwo_US304(t *testing.T) {
	srv := newCapturingServer(t, func(r *http.Request) (int, string) { return 200, `{}` })
	tmp := configuredCLI(t, srv)

	_, stderr, exit := runCLIWith(t, tmp,
		"objectset", "frob",
		"--ontology", "x",
	)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	if !strings.Contains(strings.ToLower(stderr), "unknown") {
		t.Errorf("stderr missing 'unknown': %q", stderr)
	}
}

// --- top-level dispatch ----------------------------------------------------

func TestDispatch_KnownTopLevelCommands_US304(t *testing.T) {
	// Verifies "weave action" / "weave aggregate" / "weave objectset" are
	// no longer "unknown command" — they hit subcommand-usage exits with no
	// HTTP request.
	srv := newCapturingServer(t, func(r *http.Request) (int, string) { return 200, `{}` })
	tmp := configuredCLI(t, srv)

	cases := []struct {
		name string
		args []string
	}{
		{"action no subcommand", []string{"action"}},
		{"aggregate no subcommand", []string{"aggregate"}},
		{"objectset no subcommand", []string{"objectset"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, exit := runCLIWith(t, tmp, tc.args...)
			if exit != 2 {
				t.Fatalf("exit = %d, want 2", exit)
			}
			if strings.Contains(stderr, "unknown command") {
				t.Errorf("dispatch fell through to unknown-command: %q", stderr)
			}
		})
	}
}

func TestRootUsage_ListsNewCommands_US304(t *testing.T) {
	tmp := t.TempDir()
	stdout, _, _ := runCLIWith(t, tmp)
	for _, want := range []string{"action", "aggregate", "objectset"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("root usage missing %q: %s", want, stdout)
		}
	}
}
