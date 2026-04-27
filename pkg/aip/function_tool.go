package aip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ErrToolHandlerNotConfigured is returned by FunctionToolHandler.Execute
// when the catalog row has no HandlerFunctionRID set OR no FunctionInvoker
// has been wired into the registry. The SendMessage handler maps this to
// a structured AIPToolHandlerNotConfigured 500 so the operator notices
// the misconfiguration (vs a silent no-op that the LLM might loop on).
var ErrToolHandlerNotConfigured = errors.New("aip: tool handler function not configured")

// FunctionInvoker is the narrow callback the FunctionToolHandler delegates
// Execute() to. The implementation lives in cmd/server (it ties together
// oms.Repository.GetFunction + oms.FunctionExecutor.Execute) so pkg/aip
// stays free of any oms / functions dep — same dep-direction trick as
// pgEdgePropertiesResolver / interface_method_dispatcher.
//
// rid is the Function's stored RID; params is the JSON-decoded argument
// blob the LLM produced for the tool call. The returned interface{}
// follows the same shape as oms.FunctionExecutor.Execute — the handler
// stringifies it before handing the result back to the LLM.
type FunctionInvoker interface {
	Invoke(ctx context.Context, rid string, params map[string]interface{}) (interface{}, error)
}

// FunctionToolHandler is the ToolHandler shim used for catalog-loaded
// tools (US-285). Each row in aip_tools is wrapped into one
// FunctionToolHandler: Definition() echoes the persisted (Name,
// Description, Parameters) triple back to the LLM and Execute() routes
// the model-produced argument blob through the FunctionInvoker, which
// dispatches to the named Function in the Function Registry.
type FunctionToolHandler struct {
	def     ToolDef
	fnRID   string
	invoker FunctionInvoker
}

// NewFunctionToolHandler constructs a FunctionToolHandler from a stored
// catalog row. invoker may be nil — in that case Execute returns
// ErrToolHandlerNotConfigured so the SendMessage loop surfaces a clean
// error instead of dispatching into a nil callback.
func NewFunctionToolHandler(rec *ToolRecord, invoker FunctionInvoker) *FunctionToolHandler {
	def := ToolDef{
		Name:        rec.Name,
		Description: rec.Description,
	}
	if len(rec.Parameters) > 0 {
		def.Parameters = append(json.RawMessage(nil), rec.Parameters...)
	}
	return &FunctionToolHandler{
		def:     def,
		fnRID:   rec.HandlerFunctionRID,
		invoker: invoker,
	}
}

// Definition returns the LLM-visible tool descriptor.
func (h *FunctionToolHandler) Definition() ToolDef { return h.def }

// Execute parses args into a map, dispatches through the invoker, and
// stringifies the result for the RoleTool message body. ErrToolHandlerNotConfigured
// is returned when either the catalog row lacks HandlerFunctionRID or
// the registry has no FunctionInvoker wired.
func (h *FunctionToolHandler) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if h.fnRID == "" || h.invoker == nil {
		return "", ErrToolHandlerNotConfigured
	}
	params := map[string]interface{}{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return "", fmt.Errorf("aip: invalid tool arguments: %w", err)
		}
	}
	result, err := h.invoker.Invoke(ctx, h.fnRID, params)
	if err != nil {
		return "", err
	}
	return stringifyToolResult(result), nil
}

// stringifyToolResult converts the FunctionExecutor return value into the
// string the LLM sees on the next iteration. Plain strings pass through;
// numeric / bool primitives use their canonical representation; maps and
// slices serialise to JSON so structured results round-trip faithfully.
func stringifyToolResult(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(buf)
}

// LoadCatalogIntoRegistry registers a FunctionToolHandler in reg for every
// enabled row in cat. Disabled rows are skipped so operators can hide
// tools from the LLM without deleting the row. Built-in tools registered
// before this call (e.g. EchoToolHandler) are preserved; a catalog row
// with the same name overwrites the prior entry, matching the
// ToolRegistry.Register "last-write-wins" contract.
func LoadCatalogIntoRegistry(ctx context.Context, reg *ToolRegistry, cat ToolCatalog, invoker FunctionInvoker) error {
	if reg == nil || cat == nil {
		return nil
	}
	rows, err := cat.ListTools(ctx)
	if err != nil {
		return err
	}
	for _, t := range rows {
		if !t.Enabled {
			continue
		}
		reg.Register(NewFunctionToolHandler(t, invoker))
	}
	return nil
}
