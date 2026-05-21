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
//   - prompts/list   : list MCP prompts synthesised from OMS ActionType
//     metadata — one prompt per ActionType across all ontologies (OSV2-302)
//   - prompts/get    : render a user-role text message that names the
//     ontology, action and supplied arguments so the LLM can invoke
//     weave_apply_action with the same shape (OSV2-302)
//   - resources/list : list ontologies, ObjectTypes (one per ontology, OSV2-307),
//     and temporary ObjectSets as MCP resources, with optional cursor pagination
//   - resources/read : return the schema for an ontology, the schema for a
//     single ObjectType (weave://objecttype/<ontology>/<objectType>),
//     or the stored definition for an ObjectSet, given a `weave://<kind>/<id>` URI
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
