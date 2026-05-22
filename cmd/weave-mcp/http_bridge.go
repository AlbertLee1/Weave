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

	"github.com/liyang/weave/pkg/mcp"
)

// BridgeOption mutates a bridgeOptions value before the bridge starts. It
// is the OSV2-305 extension point that lets the caller add per-request
// headers (Authorization, X-Weave-API-Key) without changing the bridge's
// existing four-argument signature — older callers like the OSV2-303 tests
// (RunHTTPBridge(ctx, in, out, url)) keep working unchanged.
type BridgeOption func(*bridgeOptions)

// bridgeOptions collects the auth header (if any) the bridge should attach
// to every upstream request. Token wins over API key when both are set.
type bridgeOptions struct {
	authHeader string // e.g. "Authorization" or "X-Weave-API-Key"
	authValue  string // e.g. "Bearer xyz..." or "wvk_..."
	timeout    time.Duration
}

const defaultBridgeHTTPTimeout = 30 * time.Second

func defaultBridgeOptions() bridgeOptions {
	return bridgeOptions{timeout: defaultBridgeHTTPTimeout}
}

// WithBearerToken makes the bridge send Authorization: Bearer <token> on
// every upstream request. An empty token is a no-op (the bridge sends no
// auth at all).
func WithBearerToken(token string) BridgeOption {
	return func(o *bridgeOptions) {
		if token == "" {
			return
		}
		o.authHeader = "Authorization"
		o.authValue = "Bearer " + token
	}
}

// WithAPIKey makes the bridge send X-Weave-API-Key: <key> on every upstream
// request. Ignored when a bearer token is already set (token wins).
func WithAPIKey(key string) BridgeOption {
	return func(o *bridgeOptions) {
		if key == "" || o.authHeader == "Authorization" {
			return
		}
		o.authHeader = "X-Weave-API-Key"
		o.authValue = key
	}
}

// WithHTTPTimeout bounds every upstream HTTP request. Non-positive durations
// are ignored so programmatic callers keep the default timeout.
func WithHTTPTimeout(timeout time.Duration) BridgeOption {
	return func(o *bridgeOptions) {
		if timeout <= 0 {
			return
		}
		o.timeout = timeout
	}
}

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
//   - When the operator supplies a BridgeOption that sets an auth header
//     (WithBearerToken / WithAPIKey), every upstream POST carries the
//     resolved header.
//   - Upstream HTTP stalls are bounded by the bridge timeout and converted
//     into JSON-RPC server errors instead of hanging the stdio peer.
//
// The bridge stays in `package main` of cmd/weave-mcp so it is not part of
// the public pkg/mcp API surface — it's a transport-edge concern.
func RunHTTPBridge(ctx context.Context, in io.Reader, out io.Writer, url string, opts ...BridgeOption) error {
	cfg := defaultBridgeOptions()
	for _, opt := range opts {
		opt(&cfg)
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	client := &http.Client{Timeout: cfg.timeout}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Classify the envelope before forwarding so upstream errors can echo a
		// useful id and notification-only envelopes can skip stdout.
		classification := classifyBridgeEnvelope(line)

		respBytes, fwdErr := forwardOnce(ctx, client, url, line, cfg)
		if fwdErr != nil {
			if !classification.expectsResponse {
				// Notifications never produce a response per JSON-RPC 2.0,
				// even when the upstream is unreachable.
				continue
			}
			errLine := makeErrorLine(classification.errorID, fwdErr)
			if _, err := out.Write(errLine); err != nil {
				return err
			}
			continue
		}
		if !classification.expectsResponse {
			// Some MCP-over-HTTP servers reply with an empty body to
			// notifications; in that case we drop the line entirely.
			continue
		}
		if len(bytes.TrimSpace(respBytes)) == 0 {
			errLine := makeErrorLine(classification.errorID, errors.New("upstream returned empty response body"))
			if _, err := out.Write(errLine); err != nil {
				return err
			}
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

type bridgeEnvelopeClassification struct {
	expectsResponse bool
	errorID         json.RawMessage
}

func classifyBridgeEnvelope(line []byte) bridgeEnvelopeClassification {
	envelope, err := mcp.ParseRequestEnvelope(line)
	if err != nil {
		return bridgeEnvelopeClassification{
			expectsResponse: true,
			errorID:         sniffBridgeRequestID(line),
		}
	}
	for _, item := range envelope.Items {
		if item.Error != nil {
			return bridgeEnvelopeClassification{
				expectsResponse: true,
				errorID:         item.Error.ID,
			}
		}
		if item.Request != nil && !item.Request.IsNotification() {
			return bridgeEnvelopeClassification{
				expectsResponse: true,
				errorID:         item.Request.ID,
			}
		}
	}
	return bridgeEnvelopeClassification{}
}

func sniffBridgeRequestID(data []byte) json.RawMessage {
	var sniff struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(data, &sniff)
	return sniff.ID
}

// forwardOnce POSTs a single JSON-RPC envelope to the upstream URL and
// returns the verbatim response bytes on success.
func forwardOnce(ctx context.Context, client *http.Client, url string, payload []byte, cfg bridgeOptions) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cfg.authHeader != "" && cfg.authValue != "" {
		req.Header.Set(cfg.authHeader, cfg.authValue)
	}
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
		return nil, fmt.Errorf("upstream HTTP status %d: %s", resp.StatusCode, string(body))
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
