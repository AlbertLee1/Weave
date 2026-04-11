package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/oss"
)

// SemanticSearchRequest is the input passed to a SemanticSearcher when an
// AI tool wants to find objects by similarity to a free-form text query.
type SemanticSearchRequest struct {
	Ontology   string
	ObjectType string
	QueryText  string
	K          int
}

// SemanticHit is a single result from a semantic search: the matching
// object's primary key plus the similarity distance the backend reported.
type SemanticHit struct {
	PrimaryKey string  `json:"primaryKey"`
	Distance   float32 `json:"distance"`
}

// SemanticSearchResult is the response shape returned to AI tools.
type SemanticSearchResult struct {
	Hits  []SemanticHit `json:"hits"`
	Model string        `json:"model,omitempty"`
}

// SemanticSearcher is the interface MCP AI tools call into for vector
// similarity search. The production implementation wraps the ObjectSet
// executor with a nearestNeighbors definition; tests inject a stub.
type SemanticSearcher interface {
	SemanticSearch(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResult, error)
}

// SetSemanticSearcher wires an optional SemanticSearcher into the server so
// the semantic_search and ask_objectset tools become functional. Without it,
// both tools return a clear "not configured" error rather than empty data.
// Safe to call before transport mounts the server.
func (s *Server) SetSemanticSearcher(ss SemanticSearcher) {
	s.semanticSearcher = ss
	registerAITools(s)
}

// registerAITools registers the four AI tools introduced in US-046. It is
// idempotent: tools that already exist in the registry are ignored so
// SetSemanticSearcher can be called more than once without panicking.
func registerAITools(s *Server) {
	tools := []Tool{
		&semanticSearchTool{s: s},
		&askObjectSetTool{s: s},
		&explainObjectTool{s: s},
		&draftActionTool{s: s},
	}
	for _, t := range tools {
		if _, ok := s.registry.Get(t.Definition().Name); ok {
			continue
		}
		if err := s.registry.Register(t); err != nil {
			panic(fmt.Sprintf("mcp: register AI tool: %v", err))
		}
	}
}

// --- weave_semantic_search ---

type semanticSearchTool struct{ s *Server }

func (t *semanticSearchTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_semantic_search",
		Description: "Find objects most similar to a free-form text query using vector embeddings.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology":   {Type: "string", Description: "Ontology API name or RID."},
				"objectType": {Type: "string", Description: "Object type API name to search."},
				"query":      {Type: "string", Description: "Natural-language query text."},
				"k":          {Type: "integer", Description: "Maximum number of neighbors to return (default 10)."},
			},
			Required: []string{"ontology", "objectType", "query"},
		},
	}
}

func (t *semanticSearchTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.semanticSearcher == nil {
		return nil, fmt.Errorf("semantic searcher not configured")
	}
	ontology, err := stringArg(args, "ontology")
	if err != nil {
		return nil, err
	}
	objectType, err := stringArg(args, "objectType")
	if err != nil {
		return nil, err
	}
	query, err := stringArg(args, "query")
	if err != nil {
		return nil, err
	}
	k := intArg(args, "k")
	if k <= 0 {
		k = 10
	}
	out, err := t.s.semanticSearcher.SemanticSearch(ctx, SemanticSearchRequest{
		Ontology:   ontology,
		ObjectType: objectType,
		QueryText:  query,
		K:          k,
	})
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	return jsonContent(out)
}

// --- weave_ask_objectset ---

type askObjectSetTool struct{ s *Server }

func (t *askObjectSetTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_ask_objectset",
		Description: "Ask a natural-language question over an object type. Performs a semantic search and hydrates the matching objects with their full property data.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology":   {Type: "string", Description: "Ontology API name or RID."},
				"objectType": {Type: "string", Description: "Object type API name to search."},
				"question":   {Type: "string", Description: "Natural-language question."},
				"k":          {Type: "integer", Description: "Maximum number of objects to return (default 5)."},
			},
			Required: []string{"ontology", "objectType", "question"},
		},
	}
}

func (t *askObjectSetTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.semanticSearcher == nil {
		return nil, fmt.Errorf("semantic searcher not configured")
	}
	if t.s.oss == nil {
		return nil, fmt.Errorf("oss service not configured")
	}
	ontology, err := stringArg(args, "ontology")
	if err != nil {
		return nil, err
	}
	objectType, err := stringArg(args, "objectType")
	if err != nil {
		return nil, err
	}
	question, err := stringArg(args, "question")
	if err != nil {
		return nil, err
	}
	k := intArg(args, "k")
	if k <= 0 {
		k = 5
	}
	hits, err := t.s.semanticSearcher.SemanticSearch(ctx, SemanticSearchRequest{
		Ontology:   ontology,
		ObjectType: objectType,
		QueryText:  question,
		K:          k,
	})
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	type answeredHit struct {
		PrimaryKey string         `json:"primaryKey"`
		Distance   float32        `json:"distance"`
		Object     *oss.WireObject `json:"object,omitempty"`
	}
	answered := make([]answeredHit, 0, len(hits.Hits))
	for _, h := range hits.Hits {
		obj, ferr := t.s.oss.GetObject(ctx, oss.GetObjectRequest{
			OntologyRID: ontology,
			ObjectType:  objectType,
			PrimaryKey:  h.PrimaryKey,
		})
		if ferr != nil {
			// Hydration failures are non-fatal — emit the hit without the
			// object payload so the caller can still see what matched.
			answered = append(answered, answeredHit{PrimaryKey: h.PrimaryKey, Distance: h.Distance})
			continue
		}
		answered = append(answered, answeredHit{
			PrimaryKey: h.PrimaryKey,
			Distance:   h.Distance,
			Object:     obj,
		})
	}
	return jsonContent(map[string]any{
		"question": question,
		"model":    hits.Model,
		"answers":  answered,
	})
}

// --- weave_explain_object ---

type explainObjectTool struct{ s *Server }

func (t *explainObjectTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_explain_object",
		Description: "Return a structured explanation of one object: its property values plus the object type's metadata (description, status, available link types).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology":   {Type: "string", Description: "Ontology API name or RID."},
				"objectType": {Type: "string", Description: "Object type API name."},
				"primaryKey": {Type: "string", Description: "Primary key value."},
			},
			Required: []string{"ontology", "objectType", "primaryKey"},
		},
	}
}

func (t *explainObjectTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.oss == nil {
		return nil, fmt.Errorf("oss service not configured")
	}
	if t.s.oms == nil {
		return nil, fmt.Errorf("oms repository not configured")
	}
	ontology, err := stringArg(args, "ontology")
	if err != nil {
		return nil, err
	}
	objectType, err := stringArg(args, "objectType")
	if err != nil {
		return nil, err
	}
	primaryKey, err := stringArg(args, "primaryKey")
	if err != nil {
		return nil, err
	}

	obj, err := t.s.oss.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: ontology,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
	})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}

	// Object-type metadata is optional — failures here drop the type-level
	// fields from the explanation but still return the live data.
	var typeSummary map[string]any
	if ot, lookupErr := t.s.oms.GetObjectTypeByAPIName(ctx, ontology, objectType); lookupErr == nil && ot != nil {
		typeSummary = map[string]any{
			"apiName":     ot.APIName,
			"displayName": ot.DisplayName,
			"description": ot.Description,
			"status":      ot.Status,
			"rid":         ot.RID,
		}
		if ot.PrimaryKey != "" {
			typeSummary["primaryKey"] = ot.PrimaryKey
		}
		// Outgoing links are nice-to-have context for the model.
		if links, lerr := t.s.oms.ListOutgoingLinkTypes(ctx, ot.RID); lerr == nil && len(links) > 0 {
			outgoing := make([]map[string]any, 0, len(links))
			for _, lt := range links {
				outgoing = append(outgoing, map[string]any{
					"apiName":     lt.APIName,
					"displayName": lt.DisplayName,
				})
			}
			typeSummary["outgoingLinkTypes"] = outgoing
		}
	}

	return jsonContent(map[string]any{
		"object": obj,
		"type":   typeSummary,
	})
}

// --- weave_draft_action ---

type draftActionTool struct{ s *Server }

func (t *draftActionTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_draft_action",
		Description: "Return a parameter template for an action type so callers can populate weave_apply_action without guessing the schema.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology":   {Type: "string", Description: "Ontology API name or RID."},
				"actionType": {Type: "string", Description: "Action type API name."},
			},
			Required: []string{"ontology", "actionType"},
		},
	}
}

// draftedParameter is the per-parameter template the tool emits. The
// "value" field is a placeholder appropriate for the parameter's type so
// the model can substitute a concrete value before calling weave_apply_action.
type draftedParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Value       any    `json:"value"`
}

// draftedAction is the response envelope for weave_draft_action.
type draftedAction struct {
	ActionType  string                 `json:"actionType"`
	DisplayName string                 `json:"displayName,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  []draftedParameter     `json:"parameters"`
	Template    map[string]interface{} `json:"template"`
}

func (t *draftActionTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.oms == nil {
		return nil, fmt.Errorf("oms repository not configured")
	}
	ontology, err := stringArg(args, "ontology")
	if err != nil {
		return nil, err
	}
	actionType, err := stringArg(args, "actionType")
	if err != nil {
		return nil, err
	}
	at, err := t.s.oms.GetActionTypeByAPIName(ctx, ontology, actionType)
	if err != nil {
		return nil, fmt.Errorf("get action type: %w", err)
	}
	if at == nil {
		return nil, fmt.Errorf("action type %q not found", actionType)
	}

	type storedParam struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Required    bool   `json:"required"`
		Description string `json:"description,omitempty"`
	}
	var defs []storedParam
	if len(at.Parameters) > 0 {
		_ = json.Unmarshal(at.Parameters, &defs)
	}

	params := make([]draftedParameter, 0, len(defs))
	template := make(map[string]interface{}, len(defs))
	for _, d := range defs {
		val := defaultValueForType(d.Type)
		params = append(params, draftedParameter{
			Name:        d.ID,
			Type:        d.Type,
			Required:    d.Required,
			Description: d.Description,
			Value:       val,
		})
		template[d.ID] = val
	}

	return jsonContent(draftedAction{
		ActionType:  at.APIName,
		DisplayName: at.DisplayName,
		Description: at.Description,
		Parameters:  params,
		Template:    template,
	})
}

// defaultValueForType returns a sensible placeholder for a parameter type so
// the model has a starting point. The values are deliberately benign — the
// caller is expected to overwrite them before applying the action.
func defaultValueForType(t string) any {
	switch t {
	case "string":
		return ""
	case "integer", "long":
		return 0
	case "double", "float":
		return 0.0
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object", "struct":
		return map[string]any{}
	default:
		return nil
	}
}

// Compile-time interface checks for the AI tools.
var (
	_ Tool = (*semanticSearchTool)(nil)
	_ Tool = (*askObjectSetTool)(nil)
	_ Tool = (*explainObjectTool)(nil)
	_ Tool = (*draftActionTool)(nil)
)
