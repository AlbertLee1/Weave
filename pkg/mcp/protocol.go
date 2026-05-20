package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 standard error codes (https://www.jsonrpc.org/specification#error_object).
const (
	CodeParseError     = -32700 // Invalid JSON received by the server.
	CodeInvalidRequest = -32600 // The JSON sent is not a valid Request object.
	CodeMethodNotFound = -32601 // The method does not exist or is not available.
	CodeInvalidParams  = -32602 // Invalid method parameters.
	CodeInternalError  = -32603 // Internal JSON-RPC error.

	// CodeToolError is a Weave-defined application error reserved per
	// JSON-RPC 2.0 §5.1 (-32000 to -32099 are server-implementation defined).
	CodeToolError = -32000
)

// Request is a JSON-RPC 2.0 request envelope. ID is held as a json.RawMessage
// because the spec allows it to be a string, number, or null; we round-trip
// the original bytes to the response so callers see exactly what they sent.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ParsedRequest is one item from a JSON-RPC request envelope. Exactly one of
// Request or Error is set; Error is used for malformed entries inside a batch
// so valid siblings can still be handled.
type ParsedRequest struct {
	Request *Request
	Error   *Response
}

// RequestEnvelope is either one request object or a JSON-RPC batch array.
type RequestEnvelope struct {
	Batch bool
	Items []ParsedRequest
}

// IsNotification reports whether the request is a JSON-RPC notification
// (no id field). Notifications must NOT receive a response.
func (r *Request) IsNotification() bool {
	return r == nil || len(r.ID) == 0
}

// Response is a JSON-RPC 2.0 response envelope. Exactly one of Result or
// Error is non-nil for a well-formed response. Marshaling enforces that
// invariant via custom JSON.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"-"`
	Error   *RPCError       `json:"-"`
}

// MarshalJSON enforces JSON-RPC 2.0's "exactly one of result/error" rule by
// hand-encoding the envelope. The Go zero value for ID (nil RawMessage) is
// emitted as null per spec when present, and omitted only for notifications
// (which never reach this path because notifications produce no response).
func (r Response) MarshalJSON() ([]byte, error) {
	out := map[string]any{"jsonrpc": "2.0"}
	if len(r.ID) > 0 {
		out["id"] = r.ID
	} else {
		out["id"] = nil
	}
	if r.Error != nil {
		out["error"] = r.Error
	} else {
		// Result may legitimately be nil for void responses; emit it as null
		// rather than omitting so the response is unambiguously a success.
		out["result"] = r.Result
	}
	return json.Marshal(out)
}

// UnmarshalJSON parses a JSON-RPC response envelope back into a Response.
// Result is held as a json.RawMessage so callers that round-trip the
// envelope (tests, the stdio transport's response sniffer) get back the
// same bytes the server emitted.
func (r *Response) UnmarshalJSON(data []byte) error {
	var raw struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.JSONRPC = raw.JSONRPC
	r.ID = raw.ID
	r.Error = raw.Error
	if len(raw.Result) > 0 && string(raw.Result) != "null" {
		r.Result = raw.Result
	}
	return nil
}

// RPCError is the JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface so RPCError values can be returned
// from functions like ParseRequest that report parse failures.
func (e *RPCError) Error() string {
	if e == nil {
		return "<nil RPCError>"
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// ParseRequest decodes a single JSON-RPC 2.0 request from raw bytes. On
// invalid JSON it returns an *RPCError with CodeParseError so callers can
// embed it directly into a response envelope.
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &RPCError{Code: CodeParseError, Message: "parse error: " + err.Error()}
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	if req.JSONRPC != "2.0" {
		return nil, &RPCError{Code: CodeInvalidRequest, Message: "jsonrpc must be 2.0"}
	}
	if req.Method == "" {
		return nil, &RPCError{Code: CodeInvalidRequest, Message: "method is required"}
	}
	return &req, nil
}

// ParseRequestEnvelope decodes either a single JSON-RPC request object or a
// JSON-RPC batch array. Empty batches are invalid per JSON-RPC 2.0.
func ParseRequestEnvelope(data []byte) (*RequestEnvelope, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var raws []json.RawMessage
		if err := json.Unmarshal(trimmed, &raws); err != nil {
			return nil, &RPCError{Code: CodeParseError, Message: "parse error: " + err.Error()}
		}
		if len(raws) == 0 {
			return nil, &RPCError{Code: CodeInvalidRequest, Message: "batch must contain at least one request"}
		}
		env := &RequestEnvelope{Batch: true, Items: make([]ParsedRequest, 0, len(raws))}
		for _, raw := range raws {
			req, err := ParseRequest(raw)
			if err != nil {
				code, msg := rpcErrorCodeAndMessage(err)
				env.Items = append(env.Items, ParsedRequest{
					Error: NewErrorResponse(sniffRequestID(raw), code, msg, nil),
				})
				continue
			}
			env.Items = append(env.Items, ParsedRequest{Request: req})
		}
		return env, nil
	}

	req, err := ParseRequest(trimmed)
	if err != nil {
		return nil, err
	}
	return &RequestEnvelope{Items: []ParsedRequest{{Request: req}}}, nil
}

func rpcErrorCodeAndMessage(err error) (int, string) {
	rpcErr, _ := err.(*RPCError)
	if rpcErr != nil {
		return rpcErr.Code, rpcErr.Message
	}
	return CodeParseError, err.Error()
}

func sniffRequestID(data []byte) json.RawMessage {
	var sniff struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(data, &sniff)
	return sniff.ID
}

// NewSuccessResponse builds a response envelope wrapping a successful result.
func NewSuccessResponse(id json.RawMessage, result any) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Result: result}
}

// NewErrorResponse builds a response envelope wrapping a JSON-RPC error.
func NewErrorResponse(id json.RawMessage, code int, message string, data any) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message, Data: data},
	}
}
