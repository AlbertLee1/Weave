package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RunHTTPBridge pumps newline-delimited JSON-RPC 2.0 requests from `in` to
// the cmd/server `POST /mcp` endpoint at `url` and writes each verbatim
// response back on `out` as a single line. This is what makes weave-mcp
// usable as a Claude Desktop / Cursor stdio MCP subprocess: instead of
// re-bootstrapping a PG/NATS-backed Weave inside the binary, we forward
// everything to the already-running cmd/server, so the stdio client sees
// the same tools/resources/prompts surface as the HTTP transport.
//
// Behaviour contract:
//   - One JSON-RPC request per line in, one JSON-RPC response per line out.
//   - Notifications (no id) are forwarded but produce no output line, matching
//     the JSON-RPC 2.0 spec and the in-process StdioTransport behaviour.
//   - Upstream-unreachable errors are NEVER surfaced to the caller (returning
//     nil) — instead a JSON-RPC error response is written to `out`, with the
//     original request id echoed back so the stdio peer can correlate.
//   - The loop exits cleanly on EOF or input scanner error.
//
// The bridge stays in `package main` of cmd/weave-mcp so it is not part of
// the public pkg/mcp API surface — it's a transport-edge concern.
func RunHTTPBridge(ctx context.Context, in io.Reader, out io.Writer, url string) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	client := &http.Client{Timeout: 30 * time.Second}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Sniff out id + notification status before forwarding so error
		// responses can echo the right id and notifications can skip the
		// output write per the JSON-RPC spec.
		var sniff struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(line, &sniff)
		isNotification := len(sniff.ID) == 0 || string(sniff.ID) == "null"

		respBytes, fwdErr := forwardOnce(ctx, client, url, line)
		if fwdErr != nil {
			if isNotification {
				// Notifications never produce a response per JSON-RPC 2.0,
				// even when the upstream is unreachable.
				continue
			}
			errLine := makeErrorLine(sniff.ID, fwdErr)
			if _, err := out.Write(errLine); err != nil {
				return err
			}
			continue
		}
		if isNotification {
			// Some MCP-over-HTTP servers reply with an empty body to
			// notifications; in that case we drop the line entirely.
			continue
		}
		// Normalise the upstream payload to a single line by trimming any
		// trailing whitespace and appending exactly one newline.
		trimmed := bytes.TrimRight(respBytes, " \r\n\t")
		if _, err := out.Write(trimmed); err != nil {
			return err
		}
		if _, err := out.Write([]byte("\n")); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("stdio scan: %w", err)
	}
	return nil
}

// forwardOnce POSTs a single JSON-RPC envelope to the upstream URL and
// returns the verbatim response bytes on success.
func forwardOnce(ctx context.Context, client *http.Client, url string, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream POST: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// makeErrorLine constructs a JSON-RPC error response line with the given
// echoed id and the upstream error rendered as the message. Code -32000 is
// the JSON-RPC server error range — concrete enough that MCP clients treat
// it as transport trouble rather than a "method not found".
func makeErrorLine(id json.RawMessage, fwdErr error) []byte {
	if len(id) == 0 {
		id = json.RawMessage(`null`)
	}
	type rpcError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type rpcResp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcError        `json:"error"`
	}
	buf, _ := json.Marshal(rpcResp{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcError{Code: -32000, Message: fwdErr.Error()},
	})
	return append(buf, '\n')
}
