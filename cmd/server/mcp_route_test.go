package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// OSV2-303 regression: POST /mcp must always return a JSON-RPC response,
// never the SPA HTML shell. The previous wiring registered /mcp only when
// both deps.OssSvc and deps.OmsRepo were non-nil; degraded boots (no PG, or
// PG unreachable) left the route unbound, so chi's NotFound handler — which
// in production serves the SPA index.html — caught POST /mcp and emitted
// `text/html` with a 200. MCP clients have no way to recover from that.
//
// The contract verified here:
//   - Content-Type starts with application/json (not text/html)
//   - The body parses as a JSON-RPC 2.0 envelope echoing the request id
//   - For `initialize`, the result advertises the `prompts` capability so
//     downstream clients that probe prompts/list don't get rejected.
//   - For `prompts/list`, the body is a valid {"prompts": []} envelope when
//     no ActionTypes are reachable (degraded mode), not HTML.
func TestMCP_RouteAlwaysReturnsJSON_DegradedDeps(t *testing.T) {
	// Empty deps simulates a boot where PG is unreachable: deps.OssSvc and
	// deps.OmsRepo are both nil.
	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	// Mirror production: NotFound serves the SPA shell for unknown paths.
	router.NotFound(spaHandler(testDistFS()))

	cases := []struct {
		name   string
		method string
	}{
		{"initialize", "initialize"},
		{"toolsList", "tools/list"},
		{"promptsList", "prompts/list"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"` + tc.method + `","params":{}}`
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("expected JSON content-type, got %q (body=%s)", ct, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "<!doctype html") ||
				strings.Contains(rec.Body.String(), "<html") {
				t.Fatalf("/mcp must never emit HTML, got: %s", rec.Body.String())
			}

			var env struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Result  map[string]any  `json:"result"`
				Error   map[string]any  `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("body is not valid JSON: %v (body=%s)", err, rec.Body.String())
			}
			if env.JSONRPC != "2.0" {
				t.Fatalf("expected jsonrpc=2.0, got %q", env.JSONRPC)
			}
			if string(env.ID) != "1" {
				t.Fatalf("expected id=1, got %s", string(env.ID))
			}
			if env.Error != nil {
				t.Fatalf("expected success, got error envelope: %v", env.Error)
			}

			switch tc.method {
			case "initialize":
				caps, ok := env.Result["capabilities"].(map[string]any)
				if !ok {
					t.Fatalf("initialize result missing capabilities: %v", env.Result)
				}
				if _, ok := caps["prompts"]; !ok {
					t.Fatalf("initialize must advertise prompts capability, got %v", caps)
				}
			case "prompts/list":
				prompts, ok := env.Result["prompts"]
				if !ok {
					t.Fatalf("prompts/list result missing prompts key: %v", env.Result)
				}
				// Must be a (possibly empty) JSON array, never null.
				arr, ok := prompts.([]any)
				if !ok {
					t.Fatalf("prompts/list prompts must be an array, got %T", prompts)
				}
				_ = arr // empty in degraded mode is fine
			case "tools/list":
				if _, ok := env.Result["tools"]; !ok {
					t.Fatalf("tools/list result missing tools key: %v", env.Result)
				}
			}
		})
	}
}

// OSV2-303 regression: GET /mcp must also be handled by the MCP transport
// (which returns 405 Method Not Allowed with the Allow header), not by the
// SPA fallback. Otherwise an MCP client probing the endpoint would receive
// HTML and conclude the server is not MCP-aware at all.
func TestMCP_RouteRejectsGET_WithoutSPAFallback(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)
	router.NotFound(spaHandler(testDistFS()))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET /mcp, got %d (body=%s)",
			rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<!doctype html") {
		t.Fatalf("GET /mcp must not fall through to the SPA, got: %s",
			rec.Body.String())
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("expected Allow: POST, got %q", allow)
	}
}
