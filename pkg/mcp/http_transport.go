package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// HTTPHandler serves the MCP server over a single JSON-RPC 2.0 POST endpoint.
//
// Important contract: per JSON-RPC 2.0, protocol-level errors (parse error,
// invalid request, method not found, invalid params, internal error, and
// any application error) are returned as a well-formed response envelope
// with HTTP 200. Non-200 status codes are reserved for transport-level
// failures such as the wrong HTTP method. Malformed JSON-RPC bodies, including
// oversized bodies stopped by the read cap, travel as JSON-RPC errors.
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
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, NewErrorResponse(nil, CodeInvalidRequest,
				"request body exceeds limit", map[string]int64{"limitBytes": maxBytesErr.Limit}))
			return
		}
		writeJSON(w, NewErrorResponse(nil, CodeInvalidRequest,
			"failed to read request body: "+err.Error(), nil))
		return
	}
	if len(body) == 0 {
		writeJSON(w, NewErrorResponse(nil, CodeInvalidRequest, "empty body", nil))
		return
	}

	envelope, parseErr := ParseRequestEnvelope(body)
	if parseErr != nil {
		// Best effort: try to recover the id from the raw body so the
		// response can echo it back. ParseRequestEnvelope already classified the
		// error code (parse vs invalid request).
		code, msg := rpcErrorCodeAndMessage(parseErr)
		writeJSON(w, NewErrorResponse(sniffRequestID(body), code, msg, nil))
		return
	}

	responses := make([]*Response, 0, len(envelope.Items))
	for _, item := range envelope.Items {
		if item.Error != nil {
			responses = append(responses, item.Error)
			continue
		}
		resp := h.srv.Handle(r.Context(), item.Request)
		if resp != nil {
			responses = append(responses, resp)
		}
	}
	if len(responses) == 0 {
		// Notification-only envelopes produce no response per JSON-RPC 2.0.
		// Use 204 so HTTP clients see an unambiguous "accepted with no body".
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if envelope.Batch {
		writeJSON(w, responses)
		return
	}
	writeJSON(w, responses[0])
}

// writeJSON encodes a JSON-RPC response payload and writes it with HTTP 200.
// Errors are surfaced via the JSON envelope, never via the status code.
func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}
