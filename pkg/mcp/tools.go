package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
)

// registerWeaveTools attaches the seven built-in Weave tools to the server's
// registry. Tools are pure structs holding a back-reference to the parent
// Server so they can read the injected oss/oms/executor.
func registerWeaveTools(s *Server) {
	for _, t := range []Tool{
		&listOntologiesTool{s: s},
		&listObjectTypesTool{s: s},
		&getObjectTool{s: s},
		&listObjectsTool{s: s},
		&searchObjectsTool{s: s},
		&listActionTypesTool{s: s},
		&applyActionTool{s: s},
	} {
		// Registration cannot fail at startup unless a name collision exists,
		// which is a programming error rather than a runtime condition.
		if err := s.registry.Register(t); err != nil {
			panic(fmt.Sprintf("mcp: register tool: %v", err))
		}
	}
}

// jsonContent serializes any value as a JSON string and packages it as a
// single text Content block. Tools use this so the model receives
// machine-readable JSON rather than ad-hoc prose.
func jsonContent(v any) (*ToolResult, error) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &ToolResult{
		Content: []Content{{Type: "text", Text: string(buf)}},
	}, nil
}

// stringArg pulls a required string argument from the args map.
func stringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string", key)
	}
	return s, nil
}

// intArg pulls an optional integer argument from the args map. JSON numbers
// arrive as float64 from encoding/json so we accept both float64 and int.
func intArg(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

// --- weave_list_ontologies ---

type listOntologiesTool struct{ s *Server }

func (t *listOntologiesTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_list_ontologies",
		Description: "List all ontologies registered in the Weave instance.",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]PropertySchema{},
		},
	}
}

func (t *listOntologiesTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.oms == nil {
		return nil, fmt.Errorf("oms repository not configured")
	}
	out, err := t.s.oms.ListOntologies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ontologies: %w", err)
	}
	return jsonContent(out)
}

// --- weave_list_object_types ---

type listObjectTypesTool struct{ s *Server }

func (t *listObjectTypesTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_list_object_types",
		Description: "List all object types defined in an ontology.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology": {Type: "string", Description: "Ontology API name or RID."},
			},
			Required: []string{"ontology"},
		},
	}
}

func (t *listObjectTypesTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.oms == nil {
		return nil, fmt.Errorf("oms repository not configured")
	}
	ontology, err := stringArg(args, "ontology")
	if err != nil {
		return nil, err
	}
	out, err := t.s.oms.ListObjectTypes(ctx, ontology)
	if err != nil {
		return nil, fmt.Errorf("list object types: %w", err)
	}
	return jsonContent(out)
}

// --- weave_get_object ---

type getObjectTool struct{ s *Server }

func (t *getObjectTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_get_object",
		Description: "Fetch a single ontology object by its primary key.",
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

func (t *getObjectTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
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
	return jsonContent(obj)
}

// --- weave_list_objects ---

type listObjectsTool struct{ s *Server }

func (t *listObjectsTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_list_objects",
		Description: "List ontology objects of a given type with cursor pagination.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology":   {Type: "string", Description: "Ontology API name or RID."},
				"objectType": {Type: "string", Description: "Object type API name."},
				"pageSize":   {Type: "integer", Description: "Number of objects per page (defaults to server default)."},
				"pageToken":  {Type: "string", Description: "Opaque next-page token from the previous call."},
				"orderBy":    {Type: "string", Description: "Sort field; prefix with - for descending."},
			},
			Required: []string{"ontology", "objectType"},
		},
	}
}

func (t *listObjectsTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
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
	pageToken, _ := args["pageToken"].(string)
	orderBy, _ := args["orderBy"].(string)
	page, err := t.s.oss.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: ontology,
		ObjectType:  objectType,
		PageSize:    intArg(args, "pageSize"),
		PageToken:   pageToken,
		OrderBy:     orderBy,
	})
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	return jsonContent(page)
}

// --- weave_search_objects ---

type searchObjectsTool struct{ s *Server }

func (t *searchObjectsTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_search_objects",
		Description: "Search ontology objects of a given type using a Palantir-style where clause.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology":   {Type: "string", Description: "Ontology API name or RID."},
				"objectType": {Type: "string", Description: "Object type API name."},
				"where":      {Type: "object", Description: "Where clause: {type, field, value}."},
				"pageSize":   {Type: "integer", Description: "Number of results per page."},
				"pageToken":  {Type: "string", Description: "Opaque next-page token from the previous call."},
			},
			Required: []string{"ontology", "objectType", "where"},
		},
	}
}

func (t *searchObjectsTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
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
	whereRaw, ok := args["where"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q must be an object", "where")
	}
	whereJSON, err := json.Marshal(whereRaw)
	if err != nil {
		return nil, fmt.Errorf("marshal where: %w", err)
	}
	var clause where.WhereClause
	if err := json.Unmarshal(whereJSON, &clause); err != nil {
		return nil, fmt.Errorf("decode where: %w", err)
	}
	pageToken, _ := args["pageToken"].(string)
	page, err := t.s.oss.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: ontology,
		ObjectType:  objectType,
		Where:       &clause,
		PageSize:    intArg(args, "pageSize"),
		PageToken:   pageToken,
	})
	if err != nil {
		return nil, fmt.Errorf("search objects: %w", err)
	}
	return jsonContent(page)
}

// --- weave_list_action_types ---

type listActionTypesTool struct{ s *Server }

func (t *listActionTypesTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_list_action_types",
		Description: "List all action types in an ontology.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology": {Type: "string", Description: "Ontology API name or RID."},
			},
			Required: []string{"ontology"},
		},
	}
}

func (t *listActionTypesTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.oms == nil {
		return nil, fmt.Errorf("oms repository not configured")
	}
	ontology, err := stringArg(args, "ontology")
	if err != nil {
		return nil, err
	}
	out, err := t.s.oms.ListActionTypes(ctx, ontology)
	if err != nil {
		return nil, fmt.Errorf("list action types: %w", err)
	}
	// Strip noisy raw JSON columns the model rarely benefits from.
	type actionTypeSummary struct {
		RID         string `json:"rid"`
		APIName     string `json:"apiName"`
		DisplayName string `json:"displayName"`
		Description string `json:"description,omitempty"`
		Status      string `json:"status,omitempty"`
	}
	summary := make([]actionTypeSummary, 0, len(out))
	for _, at := range out {
		summary = append(summary, actionTypeSummary{
			RID: at.RID, APIName: at.APIName, DisplayName: at.DisplayName,
			Description: at.Description, Status: at.Status,
		})
	}
	return jsonContent(summary)
}

// --- weave_apply_action ---

type applyActionTool struct{ s *Server }

func (t *applyActionTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "weave_apply_action",
		Description: "Apply (execute) an action against an ontology by API name with the supplied parameters.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontology":   {Type: "string", Description: "Ontology API name or RID."},
				"actionType": {Type: "string", Description: "Action type API name."},
				"parameters": {Type: "object", Description: "Map of parameter name to value."},
			},
			Required: []string{"ontology", "actionType", "parameters"},
		},
	}
}

func (t *applyActionTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.executor == nil {
		return nil, fmt.Errorf("action executor not configured")
	}
	ontology, err := stringArg(args, "ontology")
	if err != nil {
		return nil, err
	}
	actionType, err := stringArg(args, "actionType")
	if err != nil {
		return nil, err
	}
	params, ok := args["parameters"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q must be an object", "parameters")
	}
	result, err := t.s.executor.Apply(ctx, ontology, &actions.ApplyRequest{
		ActionType: actionType,
		Parameters: params,
	})
	if err != nil {
		return nil, fmt.Errorf("apply action: %w", err)
	}
	return jsonContent(result)
}

// Compile-time interface check: every built-in tool must implement Tool.
var (
	_ Tool = (*listOntologiesTool)(nil)
	_ Tool = (*listObjectTypesTool)(nil)
	_ Tool = (*getObjectTool)(nil)
	_ Tool = (*listObjectsTool)(nil)
	_ Tool = (*searchObjectsTool)(nil)
	_ Tool = (*listActionTypesTool)(nil)
	_ Tool = (*applyActionTool)(nil)
)

// Sanity check that our deps reference the upstream packages we plan to
// remove from imports if Weave drops them later. This avoids "imported and
// not used" if a future refactor strips one of the helpers above.
var (
	_ = oms.ErrNotFound
)
