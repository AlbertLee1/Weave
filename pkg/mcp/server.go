package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// ProtocolVersion is the MCP protocol version this server implements. The
// value is the date-stamped revision Anthropic publishes alongside the spec;
// it is part of the initialize handshake response.
const ProtocolVersion = "2024-11-05"

// ServerName and ServerVersion identify Weave's MCP server in the
// initialize handshake. They are baked in (rather than read from build
// info) so the value is stable across build environments.
const (
	ServerName    = "weave-mcp"
	ServerVersion = "0.1.0"
)

// Server is the transport-independent MCP server. It owns a Registry of
// tools, plus the Weave deps the tools delegate to. Construct one per
// process; HTTP and stdio transports both wrap a single Server.
type Server struct {
	registry *Registry
	logger   *slog.Logger
	mu       sync.Mutex

	// Deps the built-in Weave tools call into.
	oss      oss.Service
	oms      oms.Repository
	executor *actions.Executor

	// semanticSearcher backs the AI tools introduced in US-046. Optional —
	// when nil, the semantic_search and ask_objectset tools return a clear
	// "not configured" error rather than empty data.
	semanticSearcher SemanticSearcher

	// vertexService backs the three Vertex MCP tools (VTX-112). Optional —
	// when nil, vertex_list_graphs / vertex_run_scenario / vertex_apply_scenario
	// return a "not configured" error rather than crashing.
	vertexService VertexService

	// objectSetCatalog backs the resources/list + resources/read methods
	// introduced in US-286. Optional — when nil, ObjectSet resources are
	// simply omitted from the catalogue.
	objectSetCatalog ObjectSetCatalog

	// resourceSubscriptions tracks MCP resources/subscribe state per URI.
	// Transports are request/response today, but keeping the registry here
	// gives future resource-updated notifications a precise recipient set.
	resourceSubscriptions map[string]struct{}

	// completionSource backs the completion/complete method (Gap-D4
	// round 46). Optional — when nil, completion/complete still works
	// and returns valid empty completion sets (per MCP spec). Wired
	// from cmd/server with the ontology-aware provider that resolves
	// objectType / actionType argument prefixes against the OMS repo.
	completionSource CompletionSource
}

// ServerOption configures a Server at construction time.
type ServerOption func(*Server)

// WithLogger overrides the default no-op logger.
func WithLogger(l *slog.Logger) ServerOption {
	return func(s *Server) { s.logger = l }
}

// NewServer wires up a fully-functional MCP server with the seven built-in
// Weave tools registered. The action executor may be nil — in that case
// weave_apply_action returns an InternalError when called.
func NewServer(ossSvc oss.Service, omsRepo oms.Repository, executor *actions.Executor, opts ...ServerOption) *Server {
	s := &Server{
		registry:              NewRegistry(),
		logger:                slog.Default(),
		oss:                   ossSvc,
		oms:                   omsRepo,
		executor:              executor,
		resourceSubscriptions: map[string]struct{}{},
	}
	for _, opt := range opts {
		opt(s)
	}
	registerWeaveTools(s)
	// AI tools that don't depend on the SemanticSearcher (explain_object,
	// draft_action) are always available; semantic_search and ask_objectset
	// register too but return "not configured" until SetSemanticSearcher is
	// called. Tool registration is idempotent.
	registerAITools(s)
	return s
}

// Registry exposes the underlying tool registry, primarily for tests that
// want to invoke a tool directly without round-tripping through Handle.
func (s *Server) Registry() *Registry { return s.registry }

// Handle dispatches a parsed JSON-RPC 2.0 request to the appropriate
// method handler. Returns nil for notifications (which never get a reply).
func (s *Server) Handle(ctx context.Context, req *Request) *Response {
	if req.IsNotification() {
		// Notifications are accepted (no error) but never produce a response.
		// Built-in MCP notifications include "notifications/initialized" sent
		// by the client after the initialize handshake completes.
		return nil
	}
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "prompts/list":
		result, err := s.handlePromptsList(ctx)
		if err != nil {
			return NewErrorResponse(req.ID, CodeInternalError, err.Error(), nil)
		}
		return NewSuccessResponse(req.ID, result)
	case "prompts/get":
		return s.handlePromptsGet(ctx, req)
	case "resources/list":
		return s.handleResourcesList(ctx, req)
	case "resources/read":
		return s.handleResourcesRead(ctx, req)
	case "resources/subscribe":
		return s.handleResourcesSubscribe(ctx, req)
	case "resources/unsubscribe":
		return s.handleResourcesUnsubscribe(req)
	case "completion/complete":
		return s.handleCompletionComplete(ctx, req)
	case "ping":
		return NewSuccessResponse(req.ID, map[string]any{})
	default:
		return NewErrorResponse(req.ID, CodeMethodNotFound,
			fmt.Sprintf("method %q not found", req.Method), nil)
	}
}

// handleInitialize implements the MCP initialize handshake. The response
// advertises tools, resources and prompts capabilities. Resources are
// advertised even when no ObjectSetCatalog is wired because ontologies
// are always enumerable as long as the OMS repo is present; prompts (added
// in OSV2-302) likewise default to an empty list when OMS is absent rather
// than dropping the capability entirely.
func (s *Server) handleInitialize(req *Request) *Response {
	result := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities": map[string]any{
			"tools":      map[string]any{"listChanged": false},
			"resources":  map[string]any{"listChanged": false, "subscribe": true},
			"prompts":    map[string]any{"listChanged": false},
			"completions": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    ServerName,
			"version": ServerVersion,
		},
	}
	return NewSuccessResponse(req.ID, result)
}

// handleToolsList implements tools/list, which returns the deterministic
// (sorted) list of registered tool definitions.
func (s *Server) handleToolsList(req *Request) *Response {
	defs := s.registry.List()
	return NewSuccessResponse(req.ID, map[string]any{"tools": defs})
}

// toolsCallParams is the params shape MCP defines for tools/call: a tool
// name plus a free-form arguments map.
type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// handleToolsCall implements tools/call. It validates that the requested
// tool exists, validates the arguments against the tool's input schema,
// invokes the tool, and packages the result (or error) into the JSON-RPC
// envelope.
func (s *Server) handleToolsCall(ctx context.Context, req *Request) *Response {
	var p toolsCallParams
	if len(req.Params) == 0 {
		return NewErrorResponse(req.ID, CodeInvalidParams, "params required", nil)
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return NewErrorResponse(req.ID, CodeInvalidParams, "decode params: "+err.Error(), nil)
	}
	if p.Name == "" {
		return NewErrorResponse(req.ID, CodeInvalidParams, "name is required", nil)
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}

	if _, ok := s.registry.Get(p.Name); !ok {
		return NewErrorResponse(req.ID, CodeMethodNotFound,
			fmt.Sprintf("tool %q not found", p.Name), nil)
	}

	result, err := s.registry.Call(ctx, p.Name, p.Arguments)
	if err != nil {
		// Distinguish validation errors (invalid params) from tool execution
		// errors so callers see the right JSON-RPC code. Tool-not-found at
		// this layer would have been caught above; validation failures wrap
		// fmt.Errorf("invalid params: ...") in registry.Call.
		switch {
		case errors.Is(err, ErrToolNotFound):
			return NewErrorResponse(req.ID, CodeMethodNotFound, err.Error(), nil)
		case isInvalidParamsError(err):
			return NewErrorResponse(req.ID, CodeInvalidParams, err.Error(), nil)
		default:
			return NewErrorResponse(req.ID, CodeToolError, err.Error(), nil)
		}
	}
	return NewSuccessResponse(req.ID, result)
}

// isInvalidParamsError reports whether an error string was produced by
// Registry.Call's "invalid params: ..." wrapper. The check is intentionally
// shallow because the validator does not yet expose typed errors.
func isInvalidParamsError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	const prefix = "invalid params:"
	if len(msg) < len(prefix) {
		return false
	}
	return msg[:len(prefix)] == prefix
}
