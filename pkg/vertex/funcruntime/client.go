// Package funcruntime is the Go-side client for the Vertex Python
// Function Runtime sidecar (VTX-049). The runtime lives in
// runtime/python/ — a FastAPI + pydantic + sklearn process that owns
// the actual function execution and sandbox boundary. This client
// forwards an invocation over HTTP and converts the runtime's standard
// error envelopes (FastAPI 422 / 403 / 5xx) into typed Go errors so
// callers can pattern-match on them without re-parsing JSON.
//
// The wire contract is intentionally narrow:
//
//	POST {BaseURL}/invoke
//	  → 200 {"output": <any JSON>}
//	  → 422 {"detail": [{"loc": [...], "msg": "...", "type": "..."}, ...]}  (pydantic)
//	  → 403 {"detail": "...", "code": "ForbiddenFileAccess"}                (sandbox)
//	  → 403 {"detail": "...", "code": "ForbiddenExternalCall"}              (allowlist; VTX-055)
//	  → 404 {"detail": "..."}                                                (unknown function)
//	  → 5xx {"detail": "...", "code": "..."}                                 (runtime panic)
//
// Wiring into cmd/server/main.go is intentionally deferred — same
// pattern VTX-046 (scenariodiff) and VTX-048 (funcregistry) use. The
// HTTP server doesn't depend on a live runtime to boot; callers
// construct a Client only after they've seen FUNCTION_RUNTIME_URL on
// the loaded *config.Config.
package funcruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout is the HTTP client timeout used when the caller does
// not supply their own *http.Client. Functions are expected to be
// short-lived (≤ 5 s end-to-end per VTX-043 acceptance); 30 s gives
// enough headroom for cold start + sklearn predict without letting a
// hung runtime block the calling goroutine forever.
const DefaultTimeout = 30 * time.Second

// invokePath is the runtime endpoint the client POSTs to. Exposed as a
// constant rather than baked into a sprintf so tests can confirm the
// path the client targets without duplicating the literal.
const invokePath = "/invoke"

// Client is the HTTP client for the Python function runtime. Zero
// value is not usable — call NewClient to construct.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient constructs a runtime client over baseURL. baseURL must
// parse as an absolute http(s) URL with a host segment; trailing
// slashes are tolerated and stripped. A nil httpClient is replaced
// with one configured with DefaultTimeout. Returns an error when
// baseURL is blank or malformed so callers can fail wiring loudly
// at boot rather than at first request.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return nil, errors.New("funcruntime: baseURL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("funcruntime: parse baseURL %q: %w", baseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("funcruntime: baseURL %q must use http or https scheme", baseURL)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("funcruntime: baseURL %q is missing host", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	// Strip trailing slash so callers passing "http://host/" don't
	// produce "/...//invoke" URLs that some servers (and the FastAPI
	// test client) treat as 307 redirects.
	canonical := strings.TrimRight(trimmed, "/")
	return &Client{baseURL: canonical, httpClient: httpClient}, nil
}

// BaseURL returns the canonicalised runtime URL the client targets.
// Useful for diagnostic logging and for tests that need to confirm
// the canonicalisation rules without exporting internal state.
func (c *Client) BaseURL() string { return c.baseURL }

// InvokeRequest is the wire body POSTed to {BaseURL}/invoke. Inputs
// is decoded by the Python side as **kwargs to the registered
// function — keys must match the function's pydantic input model.
type InvokeRequest struct {
	Function string                 `json:"function"`
	Inputs   map[string]interface{} `json:"inputs"`
}

// InvokeResponse mirrors the 200 OK body. Output is left as
// interface{} because functions are free to return primitives,
// records, or nested structures — the caller is responsible for
// asserting the concrete shape against the registered signature.
type InvokeResponse struct {
	Output interface{} `json:"output"`
}

// Invoke POSTs req to {BaseURL}/invoke and returns the parsed
// response. Wire-level failures (DNS, dial, TLS, timeout) surface as
// *TransportError; runtime-side failures surface as one of
// *ValidationError, *SandboxViolationError, *ExternalCallForbiddenError,
// *NotFoundError, or *RuntimeError depending on HTTP status + payload
// shape. Callers should errors.As on the specific type they want to
// handle.
func (c *Client) Invoke(ctx context.Context, req InvokeRequest) (*InvokeResponse, error) {
	if strings.TrimSpace(req.Function) == "" {
		return nil, errors.New("funcruntime: req.Function is required")
	}
	if req.Inputs == nil {
		// Encode {} rather than null so pydantic's required-field
		// errors fire predictably — null tries to coerce a None into
		// the input model and produces a less helpful 422 message.
		req.Inputs = map[string]interface{}{}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("funcruntime: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+invokePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("funcruntime: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &TransportError{Function: req.Function, Err: err}
	}
	defer resp.Body.Close()

	// Cap response read at 4 MiB so a runaway runtime can't OOM the
	// caller. Functions returning massive payloads should paginate.
	rawBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, &TransportError{Function: req.Function, Err: fmt.Errorf("read response: %w", readErr)}
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		var out InvokeResponse
		if err := json.Unmarshal(rawBody, &out); err != nil {
			return nil, &RuntimeError{
				Function:   req.Function,
				StatusCode: resp.StatusCode,
				Detail:     fmt.Sprintf("malformed 200 body: %s", err.Error()),
			}
		}
		return &out, nil
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return nil, parseValidationError(req.Function, resp.StatusCode, rawBody)
	case resp.StatusCode == http.StatusForbidden:
		return nil, parseForbiddenError(req.Function, resp.StatusCode, rawBody)
	case resp.StatusCode == http.StatusNotFound:
		return nil, parseNotFoundError(req.Function, resp.StatusCode, rawBody)
	default:
		return nil, parseRuntimeError(req.Function, resp.StatusCode, rawBody)
	}
}

// FieldError is one entry in the FastAPI / pydantic 422 detail array.
// Loc is the JSON pointer-like path (e.g. ["body", "inputs",
// "distance_km"]). The Go side keeps it as []string so callers can
// build user-facing messages by indexing without an extra parse.
type FieldError struct {
	Loc  []string `json:"loc"`
	Msg  string   `json:"msg"`
	Type string   `json:"type,omitempty"`
}

// ValidationError is returned when the runtime rejects the request
// payload at the pydantic boundary (HTTP 422). Details preserves the
// field-level errors verbatim so the caller can surface them in a
// form-validation UI.
type ValidationError struct {
	Function   string
	StatusCode int
	Details    []FieldError
}

func (e *ValidationError) Error() string {
	if len(e.Details) == 0 {
		return fmt.Sprintf("funcruntime: validation failed for function %q (status %d)", e.Function, e.StatusCode)
	}
	first := e.Details[0]
	loc := strings.Join(first.Loc, ".")
	return fmt.Sprintf("funcruntime: validation failed for function %q at %s: %s", e.Function, loc, first.Msg)
}

// SandboxViolationError is returned when the runtime aborts the
// function because it tried to escape its sandbox — currently the
// filesystem deny-list (e.g. /etc/passwd reads). HTTP 403 with code
// "ForbiddenFileAccess" maps here.
type SandboxViolationError struct {
	Function   string
	StatusCode int
	Code       string
	Detail     string
}

func (e *SandboxViolationError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("funcruntime: sandbox violation in function %q (%s): %s", e.Function, e.Code, e.Detail)
	}
	return fmt.Sprintf("funcruntime: sandbox violation in function %q (%s)", e.Function, e.Code)
}

// ExternalCallForbiddenError is returned when the runtime blocks an
// outbound HTTP call because the target host isn't on the active
// ``allowedExternalDomains`` list (VTX-055). Carried on the wire as
// HTTP 403 with code "ForbiddenExternalCall" — distinct type from
// SandboxViolationError so callers can decide which user-facing
// remediation to surface ("contact security to add the domain" vs.
// "function is misbehaving").
type ExternalCallForbiddenError struct {
	Function   string
	StatusCode int
	Code       string
	Detail     string
}

func (e *ExternalCallForbiddenError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("funcruntime: external call blocked in function %q (%s): %s", e.Function, e.Code, e.Detail)
	}
	return fmt.Sprintf("funcruntime: external call blocked in function %q (%s)", e.Function, e.Code)
}

// NotFoundError is returned when the runtime doesn't know the
// requested function (HTTP 404). Distinct from RuntimeError so
// callers can decide to fall back to a different runtime or treat it
// as a 4xx user error rather than a server crash.
type NotFoundError struct {
	Function   string
	StatusCode int
	Detail     string
}

func (e *NotFoundError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("funcruntime: function %q not found: %s", e.Function, e.Detail)
	}
	return fmt.Sprintf("funcruntime: function %q not found", e.Function)
}

// RuntimeError is the catch-all for unexpected runtime responses —
// 5xx, malformed 200 bodies, or any status we don't have a specialised
// type for. Detail carries whatever the runtime sent so the operator
// has something to grep server logs for.
type RuntimeError struct {
	Function   string
	StatusCode int
	Code       string
	Detail     string
}

func (e *RuntimeError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("funcruntime: runtime error for function %q (status %d, code %q): %s", e.Function, e.StatusCode, e.Code, e.Detail)
	}
	return fmt.Sprintf("funcruntime: runtime error for function %q (status %d)", e.Function, e.StatusCode)
}

// TransportError wraps wire-level failures (dial, TLS, timeout,
// truncated body). The runtime itself never produced a status code.
type TransportError struct {
	Function string
	Err      error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("funcruntime: transport error for function %q: %v", e.Function, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// errorEnvelope is the wire shape every non-2xx response (other than
// pydantic 422) carries. The runtime intentionally mirrors FastAPI's
// HTTPException shape so a caller using the runtime directly through
// curl sees consistent payloads.
type errorEnvelope struct {
	Detail interface{} `json:"detail"`
	Code   string      `json:"code,omitempty"`
}

func parseValidationError(function string, status int, body []byte) error {
	var env struct {
		Detail []FieldError `json:"detail"`
	}
	if err := json.Unmarshal(body, &env); err != nil || len(env.Detail) == 0 {
		// Some pydantic v1 stacks emit `loc` entries as mixed
		// string/int arrays; fall back to a single synthetic
		// FieldError so callers still see *ValidationError instead
		// of a generic *RuntimeError.
		return &ValidationError{
			Function:   function,
			StatusCode: status,
			Details: []FieldError{{
				Loc: []string{"body"},
				Msg: extractDetailString(body),
			}},
		}
	}
	return &ValidationError{Function: function, StatusCode: status, Details: env.Detail}
}

// parseForbiddenError branches on the envelope's ``code`` field so the
// VTX-055 outbound-HTTP rejection produces a distinct Go error type
// from the filesystem sandbox violation. Both share the 403 status
// code but the remediation (and the operator-facing log line) differ.
func parseForbiddenError(function string, status int, body []byte) error {
	var env errorEnvelope
	_ = json.Unmarshal(body, &env)
	code := env.Code
	if code == "ForbiddenExternalCall" {
		return &ExternalCallForbiddenError{
			Function:   function,
			StatusCode: status,
			Code:       code,
			Detail:     detailAsString(env.Detail),
		}
	}
	if code == "" {
		code = "Forbidden"
	}
	return &SandboxViolationError{
		Function:   function,
		StatusCode: status,
		Code:       code,
		Detail:     detailAsString(env.Detail),
	}
}

func parseNotFoundError(function string, status int, body []byte) error {
	var env errorEnvelope
	_ = json.Unmarshal(body, &env)
	return &NotFoundError{
		Function:   function,
		StatusCode: status,
		Detail:     detailAsString(env.Detail),
	}
}

func parseRuntimeError(function string, status int, body []byte) error {
	var env errorEnvelope
	_ = json.Unmarshal(body, &env)
	return &RuntimeError{
		Function:   function,
		StatusCode: status,
		Code:       env.Code,
		Detail:     detailAsString(env.Detail),
	}
}

// detailAsString flattens the FastAPI HTTPException "detail" field
// — which may be a string, an object, or an array — into a
// best-effort human-readable string. We deliberately don't try to
// preserve the structured shape on the Go side because all callers
// currently feed it into log lines or toasts.
func detailAsString(detail interface{}) string {
	switch v := detail.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(buf)
	}
}

// extractDetailString is the same shape as detailAsString but takes
// the raw response body — used when the 422 unmarshal failed entirely
// so we still salvage a useful message.
func extractDetailString(body []byte) string {
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil {
		s := detailAsString(env.Detail)
		if s != "" {
			return s
		}
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "validation failed"
	}
	return trimmed
}
