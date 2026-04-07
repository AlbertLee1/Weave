// Command weave-mcp is a thin wrapper that runs Weave's MCP server over
// stdio so local AI clients (Claude Desktop, Cursor, GPT desktop apps) can
// spawn it as a subprocess.
//
// The current binary is a STUB: it speaks the MCP JSON-RPC 2.0 envelope
// (initialize, tools/list, etc.) but does NOT yet wire up a live
// PostgreSQL/NATS-backed Weave instance. Building the full subprocess
// requires deciding how to bootstrap the same dependency graph cmd/server
// uses (PG pool, NATS, Bleve dir) without bringing the entire HTTP server
// along; that work is tracked separately.
//
// The HTTP transport at POST /mcp on the main server is the supported
// MVP path — point Claude Desktop's HTTP MCP client at the running Weave
// server instead of running this binary directly.
//
// To verify the stdio framing in isolation, this stub registers an
// in-memory dummy ontology and responds to initialize/tools/list/ping so
// you can pipe JSON-RPC into it via:
//
//	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | weave-mcp
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/liyang/weave/pkg/mcp"
)

func main() {
	srv := mcp.NewServer(nil, nil, nil)
	transport := mcp.NewStdioTransport(srv, os.Stdin, os.Stdout)
	if err := transport.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "weave-mcp: %v\n", err)
		os.Exit(1)
	}
}
