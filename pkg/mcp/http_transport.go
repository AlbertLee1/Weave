package mcp

import (
	"encoding/json"
	"io"
	"net/http"
)

// HTTPHandler serves the MCP server over a single JSON-RPC 2.0 POST endpoint.
//
// Important contract: per JSON-RPC 2.0, protocol-level errors (parse error,
// invalid request, method not found, invalid params, internal error, and
// any application error) are returned as a well-formed response envelope
// with HTTP 200. Non-200 status codes are reserved for transport-level
// failures (wrong HTTP method, missing body, content too large) so MCP
// clients can distinguish "the server returned an MCP error" from "the
// HTTP layer rejected the call".
type HTTPHandler struct {
	srv *Server
}

// NewHTTPHandler wraps a Server in an http.Handler.
func NewHTTPHandler(srv *Server) *HTTPHandler {
	return &HTTPHandler{srv: srv}
}

// maxBodySize caps incoming request bodies to 1 MiB. JSON-RPC envelopes are
// tiny; anything larger is almost certainly an attempt to abuse the server.
const maxBodySize = 1 << 20

// ServeHTTP implements the http.Handler interface.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		writeJSON(w, NewErrorResponse(nil, CodeInvalidRequest,
			"failed to read request body: "+err.Error(), nil))
		return
	}
	if len(body) == 0 {
		writeJSON(w, NewErrorResponse(nil, CodeInvalidRequest, "empty body", nil))
		return
	}

	req, parseErr := ParseRequest(body)
	if parseErr != nil {
		// Best effort: try to recover the id from the raw body so the
		// response can echo it back. ParseRequest already classified the
		// error code (parse vs invalid request).
		var sniff struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(body, &sniff)
		rpcErr, _ := parseErr.(*RPCError)
		code := CodeParseError
		msg := parseErr.Error()
		if rpcErr != nil {
			code = rpcErr.Code
			msg = rpcErr.Message
		}
		writeJSON(w, NewErrorResponse(sniff.ID, code, msg, nil))
		return
	}

	resp := h.srv.Handle(r.Context(), req)
	if resp == nil {
		// Notification: no response per JSON-RPC 2.0. Use 204 so HTTP
		// clients see an unambiguous "accepted with no body".
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, resp)
}

// writeJSON encodes a JSON-RPC response envelope and writes it with HTTP 200.
// Errors are surfaced via the JSON envelope, never via the status code.
func writeJSON(w http.ResponseWriter, resp *Response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
