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
//	    "env": {"WEAVE_MCP_URL": "http://127.0.0.1:9117/mcp"}
//	}
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/liyang/weave/pkg/mcp"
)

func main() {
	ctx := context.Background()
	if url := os.Getenv("WEAVE_MCP_URL"); url != "" {
		if err := RunHTTPBridge(ctx, os.Stdin, os.Stdout, url); err != nil {
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
