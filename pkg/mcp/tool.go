package mcp

import (
	"context"
	"fmt"
)

// PropertySchema is the minimal JSON-Schema-flavoured description we expose
// for a tool input field. We deliberately implement a small subset (type +
// description + items.type for arrays) so we don't have to vendor a full
// validator; tool calls perform field-presence and primitive-type checks
// only.
type PropertySchema struct {
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Items       *ItemsSchema   `json:"items,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// ItemsSchema describes the element type of an array property.
type ItemsSchema struct {
	Type string `json:"type"`
}

// InputSchema is the JSON Schema document attached to a tool definition. We
// keep the field set tiny so the wire format remains a strict subset of
// MCP-spec input schemas.
type InputSchema struct {
	Type       string                    `json:"type"` // always "object"
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// ToolDefinition is the descriptor returned by tools/list and embedded in
// every Tool implementation. The fields match the MCP spec one-for-one.
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
}

// Content is a single content block returned by a tool. The MVP only emits
// text blocks; image and resource blocks would slot into the same shape with
// additional fields.
type Content struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
}

// ToolResult is what a Tool.Call returns. It mirrors the MCP "tools/call"
// response shape so the server can pass it through to the response envelope
// without translation.
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Tool is the interface every MCP tool implements. Implementations are
// transport-agnostic: Call gets a context and a deserialized arguments map.
type Tool interface {
	Definition() ToolDefinition
	Call(ctx context.Context, args map[string]any) (*ToolResult, error)
}

// ValidateArgs runs the field-presence + primitive-type check that takes the
// place of a real JSON Schema validator. It is intentionally permissive:
// missing optional fields, extra fields, and nil values are tolerated; the
// only failures are missing required fields and clear primitive type
// mismatches (e.g. string field given an integer).
func (d *ToolDefinition) ValidateArgs(args map[string]any) error {
	for _, req := range d.InputSchema.Required {
		if _, ok := args[req]; !ok {
			return fmt.Errorf("missing required argument %q", req)
		}
	}
	for name, prop := range d.InputSchema.Properties {
		v, present := args[name]
		if !present || v == nil {
			continue
		}
		if err := checkPrimitiveType(name, prop.Type, v); err != nil {
			return err
		}
	}
	return nil
}

// checkPrimitiveType verifies a Go value matches the JSON Schema primitive
// type the tool declared. JSON numbers always arrive as float64 from
// encoding/json, so "integer" accepts both float64 (with no fractional part
// implied) and int variants — strict integer checking is not the
// responsibility of this MVP validator.
func checkPrimitiveType(name, want string, v any) error {
	switch want {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("argument %q must be a string, got %T", name, v)
		}
	case "integer", "number":
		switch v.(type) {
		case float64, float32, int, int32, int64:
			return nil
		default:
			return fmt.Errorf("argument %q must be a %s, got %T", name, want, v)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("argument %q must be a boolean, got %T", name, v)
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("argument %q must be an object, got %T", name, v)
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("argument %q must be an array, got %T", name, v)
		}
	}
	return nil
}
