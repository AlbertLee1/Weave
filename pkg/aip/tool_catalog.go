package aip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"
)

// ErrToolRecordNotFound is returned by ToolCatalog.GetTool / UpdateTool /
// DeleteTool when no row matches the given name.
var ErrToolRecordNotFound = errors.New("aip: tool record not found")

// ErrToolAlreadyExists is returned by ToolCatalog.CreateTool when a row
// with the same name already exists.
var ErrToolAlreadyExists = errors.New("aip: tool record already exists")

// toolNameRE mirrors the aip_tools_name_format CHECK constraint: letter
// or underscore start, alphanumerics/underscores, 1..64 chars. Same
// shape as OpenAI / Anthropic tool-name conventions.
var toolNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// ValidateToolName returns an error when name does not match the SQL
// CHECK constraint on aip_tools.name.
func ValidateToolName(name string) error {
	if name == "" {
		return fmt.Errorf("tool name must not be empty")
	}
	if !toolNameRE.MatchString(name) {
		return fmt.Errorf("tool name %q is invalid: must match %s", name, toolNameRE.String())
	}
	return nil
}

// ToolRecord is one row in the aip_tools table — the persisted definition
// of a custom LLM-visible tool. The (Name, Description, Parameters)
// triple matches the OpenAI / Anthropic JSON-schema tool descriptor that
// rides on ChatRequest.Tools; HandlerFunctionRID is the optional handle
// of a stored Function (pkg/oms.Function) the runtime dispatches Execute
// calls to. When HandlerFunctionRID is empty the tool is "definition-
// only" — the LLM can see it but invocation surfaces an unconfigured
// error so the operator notices the gap.
type ToolRecord struct {
	Name               string          `json:"name"`
	Description        string          `json:"description,omitempty"`
	Parameters         json.RawMessage `json:"parameters,omitempty"`
	HandlerFunctionRID string          `json:"handlerFunctionRid,omitempty"`
	Enabled            bool            `json:"enabled"`
	CreatedBy          string          `json:"createdBy,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

// ToolUpdate is the partial-update payload for ToolCatalog.UpdateTool.
// Pointer fields carry the three-state "omit=preserve" semantic — same
// shape as ThreadUpdate / featureflags.FlagUpdate.
type ToolUpdate struct {
	Description        *string          `json:"description,omitempty"`
	Parameters         *json.RawMessage `json:"parameters,omitempty"`
	HandlerFunctionRID *string          `json:"handlerFunctionRid,omitempty"`
	Enabled            *bool            `json:"enabled,omitempty"`
}

// ToolCatalog is the narrow persistence surface for the aip_tools table.
// Kept off oms.Repository for the same reason aip.Store is — adding the
// catalog to the OMS interface would cascade into ~15 in-memory stubs
// scattered across the codebase. The PG impl lives in
// cmd/server/aip_tool_store.go.
type ToolCatalog interface {
	CreateTool(ctx context.Context, t *ToolRecord) error
	GetTool(ctx context.Context, name string) (*ToolRecord, error)
	ListTools(ctx context.Context) ([]*ToolRecord, error)
	UpdateTool(ctx context.Context, name string, upd ToolUpdate) error
	DeleteTool(ctx context.Context, name string) error
}

// MemoryToolCatalog is the in-memory ToolCatalog impl used in tests and
// degraded (no PG) deployments. Safe for concurrent use.
type MemoryToolCatalog struct {
	mu    sync.RWMutex
	tools map[string]*ToolRecord
}

// NewMemoryToolCatalog returns an empty MemoryToolCatalog.
func NewMemoryToolCatalog() *MemoryToolCatalog {
	return &MemoryToolCatalog{tools: map[string]*ToolRecord{}}
}

// CreateTool inserts t. Stamps CreatedAt / UpdatedAt when zero. Returns
// ErrToolAlreadyExists when the name is taken.
func (c *MemoryToolCatalog) CreateTool(_ context.Context, t *ToolRecord) error {
	if t == nil {
		return errors.New("aip: tool record is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tools[t.Name]; ok {
		return ErrToolAlreadyExists
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	cp := *t
	if len(t.Parameters) > 0 {
		cp.Parameters = append(json.RawMessage(nil), t.Parameters...)
	}
	c.tools[t.Name] = &cp
	return nil
}

// GetTool returns the named row or ErrToolRecordNotFound.
func (c *MemoryToolCatalog) GetTool(_ context.Context, name string) (*ToolRecord, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tools[name]
	if !ok {
		return nil, ErrToolRecordNotFound
	}
	return cloneToolRecord(t), nil
}

// ListTools returns every row sorted by name asc. Empty catalogue returns
// an empty (non-nil) slice.
func (c *MemoryToolCatalog) ListTools(_ context.Context) ([]*ToolRecord, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*ToolRecord, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, cloneToolRecord(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// UpdateTool applies a partial update; ErrToolRecordNotFound when missing.
func (c *MemoryToolCatalog) UpdateTool(_ context.Context, name string, upd ToolUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tools[name]
	if !ok {
		return ErrToolRecordNotFound
	}
	if upd.Description != nil {
		t.Description = *upd.Description
	}
	if upd.Parameters != nil {
		t.Parameters = append(json.RawMessage(nil), (*upd.Parameters)...)
	}
	if upd.HandlerFunctionRID != nil {
		t.HandlerFunctionRID = *upd.HandlerFunctionRID
	}
	if upd.Enabled != nil {
		t.Enabled = *upd.Enabled
	}
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// DeleteTool removes the row. ErrToolRecordNotFound when missing.
func (c *MemoryToolCatalog) DeleteTool(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tools[name]; !ok {
		return ErrToolRecordNotFound
	}
	delete(c.tools, name)
	return nil
}

func cloneToolRecord(t *ToolRecord) *ToolRecord {
	if t == nil {
		return nil
	}
	cp := *t
	if len(t.Parameters) > 0 {
		cp.Parameters = append(json.RawMessage(nil), t.Parameters...)
	}
	return &cp
}
