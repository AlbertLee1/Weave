// Command weave-mcp is the stdio entry point local AI clients (Claude
// Desktop, Cursor, GPT desktop apps) spawn as a subprocess to talk to a
// running Weave server. It reads newline-delimited JSON-RPC 2.0 from
// stdin and writes responses to stdout, with two operating modes:
//
//  1. **Bridge mode (default when WEAVE_MCP_URL is set)** — forwards every
//     stdin request to the cmd/server `POST /mcp` HTTP transport and
//     writes the verbatim response back. This is the supported production
//     path: a single Weave server bootstraps PG/NATS/Bleve once, and any
//     number of stdio MCP clients connect to it through this thin proxy.
//     OSV2-302 prompts, OSV2-301 GeoTemporal data, and every other tool
//     are surfaced unchanged.
//
//  2. **In-memory demo mode (when WEAVE_MCP_URL is unset)** — falls back
//     to an in-process mcp.Server with no Weave dependencies. Suitable for
//     verifying stdio framing in isolation; do NOT use for real client
//     workflows because all Weave tools will return "not configured".
//
// Configure WEAVE_MCP_URL in the client's MCP launcher config, e.g. for
// Claude Desktop's claude_desktop_config.json:
//
//	"weave": {
//	    "command": "/usr/local/bin/weave-mcp",
//	    "env": {
//	        "WEAVE_MCP_URL": "http://127.0.0.1:9117/mcp",
//	        // OSV2-305 — set one of these when cmd/server runs with
//	        // AUTH_MODE=token. Token wins when both are set.
//	        "WEAVE_MCP_TOKEN":   "<jwt-or-bearer-access-token>",
//	        "WEAVE_MCP_API_KEY": "wvk_...",
//	        // Optional: bound stalled upstream HTTP requests. Default is 30s.
//	        "WEAVE_MCP_HTTP_TIMEOUT": "30s"
//	    }
//	}
//
// (`WEAVE_MCP_BEARER` is also accepted as an alias for WEAVE_MCP_TOKEN so
// operators don't have to remember which name the codebase preferred.)
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/liyang/weave/pkg/mcp"
)

func main() {
	ctx := context.Background()
	if url := os.Getenv("WEAVE_MCP_URL"); url != "" {
		opts, err := bridgeOptionsFromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "weave-mcp bridge: %v\n", err)
			os.Exit(1)
		}
		if err := RunHTTPBridge(ctx, os.Stdin, os.Stdout, url, opts...); err != nil {
			fmt.Fprintf(os.Stderr, "weave-mcp bridge: %v\n", err)
			os.Exit(1)
		}
		return
	}
	srv := mcp.NewServer(nil, nil, nil)
	transport := mcp.NewStdioTransport(srv, os.Stdin, os.Stdout)
	if err := transport.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "weave-mcp: %v\n", err)
		os.Exit(1)
	}
}

// bridgeOptionsFromEnv reads WEAVE_MCP_TOKEN / WEAVE_MCP_BEARER /
// WEAVE_MCP_API_KEY / WEAVE_MCP_HTTP_TIMEOUT and returns the matching
// BridgeOption list. Token wins over API key when both are set (see
// WithAPIKey's no-op guard).
func bridgeOptionsFromEnv() ([]BridgeOption, error) {
	var opts []BridgeOption
	token := os.Getenv("WEAVE_MCP_TOKEN")
	if token == "" {
		token = os.Getenv("WEAVE_MCP_BEARER")
	}
	opts = append(opts, WithBearerToken(token))
	opts = append(opts, WithAPIKey(os.Getenv("WEAVE_MCP_API_KEY")))
	if raw := os.Getenv("WEAVE_MCP_HTTP_TIMEOUT"); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("WEAVE_MCP_HTTP_TIMEOUT %q is not a valid duration: %w", raw, err)
		}
		if timeout <= 0 {
			return nil, fmt.Errorf("WEAVE_MCP_HTTP_TIMEOUT must be greater than zero, got %q", raw)
		}
		opts = append(opts, WithHTTPTimeout(timeout))
	}
	return opts, nil
}
