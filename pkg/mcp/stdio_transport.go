package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// StdioTransport serves an MCP server over a pair of line-delimited JSON
// streams. This is the transport Claude Desktop, Cursor, and other local
// MCP clients use to spawn a server as a subprocess and exchange newline-
// terminated JSON-RPC messages over stdin/stdout.
//
// The transport reads one JSON object per line from `in` and writes one
// JSON object per line to `out`. Notifications produce no output line
// (matching the JSON-RPC 2.0 contract). The Run loop exits cleanly on EOF
// or when the input scanner returns a non-EOF error.
type StdioTransport struct {
	srv *Server
	in  io.Reader
	out io.Writer
}

// NewStdioTransport wires up an MCP server to the given input/output
// streams. Pass os.Stdin and os.Stdout for a "real" stdio server.
func NewStdioTransport(srv *Server, in io.Reader, out io.Writer) *StdioTransport {
	return &StdioTransport{srv: srv, in: in, out: out}
}

// maxStdioLineSize caps a single inbound JSON-RPC line at 1 MiB. Larger
// requests are rejected with a parse error to keep memory bounded.
const maxStdioLineSize = 1 << 20

// Run pumps requests from the input scanner through the server until the
// stream is closed. Returns the first non-EOF error encountered.
func (t *StdioTransport) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(t.in)
	// Larger initial + max buffer than the bufio default (64 KiB) so JSON
	// envelopes carrying tool results don't get truncated.
	scanner.Buffer(make([]byte, 0, 64*1024), maxStdioLineSize)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		envelope, parseErr := ParseRequestEnvelope(line)
		if parseErr != nil {
			// Sniff out any id from the raw bytes so the response can echo it.
			code, msg := rpcErrorCodeAndMessage(parseErr)
			if err := t.writeResponse(NewErrorResponse(sniffRequestID(line), code, msg, nil)); err != nil {
				return err
			}
			continue
		}

		responses := make([]*Response, 0, len(envelope.Items))
		for _, item := range envelope.Items {
			if item.Error != nil {
				responses = append(responses, item.Error)
				continue
			}
			resp := t.srv.Handle(ctx, item.Request)
			if resp != nil {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			// Notification-only envelopes produce no reply line.
			continue
		}
		payload := any(responses[0])
		if envelope.Batch {
			payload = responses
		}
		if err := t.writeJSONLine(payload); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("stdio scan: %w", err)
	}
	return nil
}

// writeResponse encodes a response to a single line on the output stream.
// Each response is followed by a newline so the peer can use a line
// scanner symmetric to ours.
func (t *StdioTransport) writeResponse(resp *Response) error {
	return t.writeJSONLine(resp)
}

func (t *StdioTransport) writeJSONLine(payload any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := t.out.Write(buf); err != nil {
		return err
	}
	if _, err := t.out.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}
