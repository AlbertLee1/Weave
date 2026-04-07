// Package mcp implements a minimal Model Context Protocol (MCP) server
// over JSON-RPC 2.0 for the Weave ontology engine.
//
// MCP is the protocol Anthropic published for connecting AI agents to
// external tools and data sources. The wire format is JSON-RPC 2.0:
//
//	request : {"jsonrpc":"2.0","id":<id>,"method":"<method>","params":{...}}
//	response: {"jsonrpc":"2.0","id":<id>,"result":{...}}
//	error   : {"jsonrpc":"2.0","id":<id>,"error":{"code":<int>,"message":"..."}}
//
// The methods this package implements (the MVP subset) are:
//
//   - initialize     : handshake; returns protocolVersion + serverInfo + capabilities
//   - tools/list     : list registered tools and their input schemas
//   - tools/call     : invoke a tool by name with arguments
//   - prompts/list   : returns an empty list (Weave does not yet expose prompts)
//   - resources/list : returns an empty list (Weave does not yet expose resources)
//
// Notifications (requests with no id) such as notifications/initialized are
// accepted and dispatched but, per JSON-RPC 2.0, never receive a response.
//
// Tools are exposed via the Tool interface; the registry is transport-
// independent so the same Server can be served over HTTP (NewHTTPHandler)
// or stdio (StdioTransport — see stdio_transport.go).
//
// Auth is the responsibility of the surrounding HTTP middleware. The MCP
// server itself never inspects credentials; it simply executes the tool
// against the injected oss.Service / oms.Repository / actions.Executor
// using whatever context the caller has supplied.
package mcp
