package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Prompt is the MCP prompts/list entry shape (MCP 2024-11-05). Weave
// synthesizes one Prompt per ActionType so an MCP client can surface
// existing Weave Actions as prompts without re-typing the schema.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument is one argument the user supplies when invoking a prompt.
// It mirrors the OMS ActionType parameter schema (id → name, plus required
// + description) so the same field that appears in weave_apply_action
// shows up here too.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one entry in the prompts/get messages array. MCP allows
// multiple content types; Weave's renderer emits a single user-role text
// message that names the ontology, action and supplied arguments so the
// downstream LLM can call weave_apply_action verbatim.
type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

// PromptContent is the typed payload of a PromptMessage.
type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// promptParameter mirrors the JSON shape used by other MCP AI tools
// (draftActionTool) so the decoder stays consistent across the package.
type promptParameter struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// promptNameSep separates the ontology and action segments of a prompt
// name. The double underscore is rare in API names so it makes the parse
// unambiguous even when the action contains a single underscore.
const promptNameSep = "__"

// handlePromptsList implements MCP prompts/list. When the OMS repo is wired
// the result is one Prompt per ActionType across all ontologies; when not
// wired (degraded test paths) the result is an empty list. The ordering is
// stable: ontology api name, then action api name.
func (s *Server) handlePromptsList(ctx context.Context) (map[string]any, error) {
	prompts := s.collectPrompts(ctx)
	return map[string]any{"prompts": prompts}, nil
}

// collectPrompts walks OMS metadata and synthesises one Prompt per
// ActionType. Returns a non-nil slice (possibly empty) so JSON output is
// always [{}...]/[] rather than null.
func (s *Server) collectPrompts(ctx context.Context) []Prompt {
	out := []Prompt{}
	if s.oms == nil {
		return out
	}
	ontologies, err := s.oms.ListOntologies(ctx)
	if err != nil {
		return out
	}
	// Stable ordering for both clients and tests.
	sort.SliceStable(ontologies, func(i, j int) bool {
		return ontologies[i].APIName < ontologies[j].APIName
	})
	for _, ont := range ontologies {
		actions, err := s.oms.ListActionTypes(ctx, ont.RID)
		if err != nil {
			continue
		}
		sort.SliceStable(actions, func(i, j int) bool {
			return actions[i].APIName < actions[j].APIName
		})
		for _, at := range actions {
			out = append(out, Prompt{
				Name:        promptNameFor(ont.APIName, at.APIName),
				Description: promptDescriptionFromActionType(at.DisplayName, at.Description),
				Arguments:   promptArgumentsFromRaw(at.Parameters),
			})
		}
	}
	return out
}

func promptNameFor(ontologyAPIName, actionAPIName string) string {
	return ontologyAPIName + promptNameSep + actionAPIName
}

// promptDescriptionFromActionType surfaces ActionType.Description when
// non-empty, falling back to DisplayName. Keeps prompts/list useful even
// when an ontology author left one of the two blank.
func promptDescriptionFromActionType(displayName, description string) string {
	if description != "" {
		return description
	}
	return displayName
}

// promptArgumentsFromRaw decodes ActionType.Parameters (json.RawMessage of a
// JSON array, declaration order) into the PromptArgument shape MCP expects.
// Decode failure yields nil — prompts/list still returns the prompt entry
// itself so the client at least sees the action exists.
func promptArgumentsFromRaw(raw json.RawMessage) []PromptArgument {
	if len(raw) == 0 {
		return nil
	}
	var params []promptParameter
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	args := make([]PromptArgument, 0, len(params))
	for _, p := range params {
		args = append(args, PromptArgument{
			Name:        p.ID,
			Description: p.Description,
			Required:    p.Required,
		})
	}
	return args
}

// promptsGetParams is the input of prompts/get per the MCP spec.
type promptsGetParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// handlePromptsGet implements MCP prompts/get. It parses the prompt name
// back into (ontologyApiName, actionApiName), looks up the ActionType, and
// renders a single user-role text message containing the verbatim instruction
// for the downstream LLM to call weave_apply_action with the right shape.
func (s *Server) handlePromptsGet(ctx context.Context, req *Request) *Response {
	if s.oms == nil {
		return NewErrorResponse(req.ID, CodeMethodNotFound, "prompts/get not available: oms not configured", nil)
	}
	if len(req.Params) == 0 {
		return NewErrorResponse(req.ID, CodeInvalidParams, "params required", nil)
	}
	var p promptsGetParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return NewErrorResponse(req.ID, CodeInvalidParams, "decode params: "+err.Error(), nil)
	}
	if p.Name == "" {
		return NewErrorResponse(req.ID, CodeInvalidParams, "name is required", nil)
	}
	ontology, action, ok := splitPromptName(p.Name)
	if !ok {
		return NewErrorResponse(req.ID, CodeInvalidParams, fmt.Sprintf("prompt name %q is not in 'ontology__action' form", p.Name), nil)
	}
	// Resolve ontology api-name → RID so both PG and in-memory test fakes
	// agree on the lookup key; the PG implementation accepts either form
	// natively but our shared fake is keyed strictly by RID.
	ontRID := ontology
	if ont, oerr := s.oms.GetOntology(ctx, ontology); oerr == nil && ont != nil {
		ontRID = ont.RID
	}
	at, err := s.oms.GetActionTypeByAPIName(ctx, ontRID, action)
	if err != nil || at == nil {
		return NewErrorResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("action type %q not found in ontology %q", action, ontology), nil)
	}
	desc := promptDescriptionFromActionType(at.DisplayName, at.Description)
	text := renderPromptText(ontology, action, p.Arguments)
	result := map[string]any{
		"description": desc,
		"messages": []PromptMessage{{
			Role:    "user",
			Content: PromptContent{Type: "text", Text: text},
		}},
	}
	return NewSuccessResponse(req.ID, result)
}

func splitPromptName(name string) (string, string, bool) {
	idx := strings.Index(name, promptNameSep)
	if idx <= 0 || idx+len(promptNameSep) >= len(name) {
		return "", "", false
	}
	return name[:idx], name[idx+len(promptNameSep):], true
}

// renderPromptText composes the user message body. The text is intentionally
// imperative so the LLM understands it as an action instruction rather than
// a question; it lists the supplied arguments verbatim and points the model
// at weave_apply_action as the follow-up tool to call.
func renderPromptText(ontology, action string, arguments map[string]interface{}) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Apply Weave action %q in ontology %q.\n", action, ontology)
	if len(arguments) > 0 {
		// Stable ordering for deterministic rendering / tests.
		keys := make([]string, 0, len(arguments))
		for k := range arguments {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("Parameters:\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "  - %s = %v\n", k, arguments[k])
		}
	}
	b.WriteString("\nWhen ready, invoke the weave_apply_action tool with the same ontology, actionType and parameters.")
	return b.String()
}
