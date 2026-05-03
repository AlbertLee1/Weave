// Package pipeline implements the persistent Pipeline DSL service
// (US-287). A Pipeline describes a declarative data flow:
//
//	inputs     — named sources (e.g. an OMS ObjectSet, a JDBC table,
//	             an external CSV) that feed the pipeline
//	transforms — ordered list of transform nodes; each carries a free-
//	             form Config blob the runtime interprets per-Type
//	outputs    — named sinks the transformed rows land in
//	schedule   — optional CRON expression. Empty string means "on
//	             demand" (the future scheduler from US-289 ignores it).
//
// This package owns the descriptor + structural validation only. The
// DAG executor (US-288) and cron scheduler (US-289) will hang off the
// same Pipeline shape later — keeping schema concerns on this story
// keeps each user story narrowly scoped and independently testable.
package pipeline

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Mode classifies a pipeline's run shape (US-378).
//
//   - ModeFull (the default, also represented by an empty string for
//     pre-US-378 rows) re-scans the whole source on every run; offset
//     bookkeeping is ignored.
//   - ModeAppend opts into incremental runs: each run only processes
//     rows whose source-side offset is strictly greater than the prior
//     successful run's last_committed_offset. Schema-evolution rules
//     also kick in — new columns auto-add to the downstream index, but
//     dropped or type-conflicting columns abort the run with
//     WEAVE_PIPELINE_BREAKING_CHANGE.
const (
	ModeFull   = "FULL"
	ModeAppend = "APPEND"
)

// IsKnownMode reports whether m is one of the canonical mode values
// (the empty string is treated as ModeFull and accepted everywhere).
func IsKnownMode(m string) bool {
	switch m {
	case "", ModeFull, ModeAppend:
		return true
	}
	return false
}

// SchemaField is one column of a source schema as observed by the
// runtime. The shape is intentionally narrow — name + type — so it
// round-trips cleanly through pipelines.last_known_schema JSONB without
// pinning the persistence layer to the richer pkg/pipeline/schema.Field
// (which carries sample stats irrelevant to evolution).
type SchemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Pipeline is one persisted pipeline row.
type Pipeline struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Description     string        `json:"description,omitempty"`
	Inputs          []Input       `json:"inputs"`
	Transforms      []Transform   `json:"transforms"`
	Outputs         []Output      `json:"outputs"`
	Schedule        string        `json:"schedule,omitempty"`
	Enabled         bool          `json:"enabled"`
	Mode            string        `json:"mode,omitempty"`
	LastKnownSchema []SchemaField `json:"lastKnownSchema,omitempty"`
	CreatedBy       string        `json:"createdBy,omitempty"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`
}

// PipelineUpdate is the partial-update payload. Pointer fields preserve
// "omit=keep current" semantics; same shape as logic.FlowUpdate.
type PipelineUpdate struct {
	Name            *string        `json:"name,omitempty"`
	Description     *string        `json:"description,omitempty"`
	Inputs          *[]Input       `json:"inputs,omitempty"`
	Transforms      *[]Transform   `json:"transforms,omitempty"`
	Outputs         *[]Output      `json:"outputs,omitempty"`
	Schedule        *string        `json:"schedule,omitempty"`
	Enabled         *bool          `json:"enabled,omitempty"`
	Mode            *string        `json:"mode,omitempty"`
	LastKnownSchema *[]SchemaField `json:"lastKnownSchema,omitempty"`
}

// Input is one source descriptor. Type identifies the connector (e.g.
// "objectset", "jdbc", "csv"); Config is a free-form JSON object whose
// schema is validated per-type at execute time by the runtime.
type Input struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// Transform is one transformation step. Inputs lists the names of
// upstream nodes (Input entries OR earlier Transform entries) feeding
// this transform. Empty Inputs is allowed for transforms that derive
// their data purely from Config (e.g. a constant-table generator).
type Transform struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Inputs []string       `json:"inputs,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}

// Output is one sink descriptor. Input is the name of the upstream node
// (Input or Transform) whose rows land in this sink.
type Output struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Input  string         `json:"input,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}

// pipelineIDRE matches the canonical pipeline identifier shape. Mirrors
// the SQL CHECK constraint and the allowlist used by aip_logic_flows.
var pipelineIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// nodeNameRE matches input/transform/output names: lowercase letter or
// underscore start, alphanumerics/underscores. Names ride on the wire
// as user-facing identifiers, so we keep the rule strict to avoid
// quoting headaches in downstream YAML / SQL renderings.
var nodeNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// ValidatePipelineID rejects empty / over-long / disallowed-character
// ids. Mirrors logic.ValidateFlowID for cross-feature consistency.
func ValidatePipelineID(id string) error {
	if id == "" {
		return errors.New("pipeline id must not be empty")
	}
	if !pipelineIDRE.MatchString(id) {
		return fmt.Errorf("pipeline id %q is invalid: allowed characters are [A-Za-z0-9._-] and length must be 1..128", id)
	}
	return nil
}

// ValidateNodeName rejects empty / over-long / disallowed-character
// node names (Input.Name, Transform.Name, Output.Name).
func ValidateNodeName(name string) error {
	if name == "" {
		return errors.New("node name must not be empty")
	}
	if !nodeNameRE.MatchString(name) {
		return fmt.Errorf("node name %q is invalid: must match %s", name, nodeNameRE.String())
	}
	return nil
}

// ValidateSchedule applies a minimal sanity check on the schedule
// string. Empty schedule = run-on-demand, accepted unconditionally.
// Non-empty schedules must split into 5 or 6 whitespace-separated
// fields (standard cron / extended cron with seconds). The full cron
// syntax check happens in US-289 when robfig/cron parses it.
func ValidateSchedule(schedule string) error {
	if schedule == "" {
		return nil
	}
	fields := splitFields(schedule)
	if len(fields) != 5 && len(fields) != 6 {
		return fmt.Errorf("schedule %q must be a cron expression with 5 or 6 whitespace-separated fields, got %d", schedule, len(fields))
	}
	return nil
}

// Validate checks structural invariants on the pipeline:
//   - id matches pipelineIDRE
//   - at least one input AND one output (a no-op pipeline is rejected
//     so a typo doesn't silently land an empty descriptor)
//   - every Input/Transform/Output Name is non-empty + valid + unique
//     across the whole pipeline (one global namespace for downstream
//     references)
//   - every Type is non-empty
//   - every Transform.Inputs and Output.Input references a known
//     upstream node (Input or earlier Transform — Transforms see
//     Inputs and any earlier-declared Transform; Outputs see anything)
//   - schedule passes ValidateSchedule
func (p *Pipeline) Validate() error {
	if p == nil {
		return errors.New("pipeline is nil")
	}
	if err := ValidatePipelineID(p.ID); err != nil {
		return err
	}
	if len(p.Inputs) == 0 {
		return errors.New("pipeline must declare at least one input")
	}
	if len(p.Outputs) == 0 {
		return errors.New("pipeline must declare at least one output")
	}
	names := make(map[string]string, len(p.Inputs)+len(p.Transforms)+len(p.Outputs))
	for i, in := range p.Inputs {
		if err := ValidateNodeName(in.Name); err != nil {
			return fmt.Errorf("inputs[%d]: %w", i, err)
		}
		if _, dup := names[in.Name]; dup {
			return fmt.Errorf("duplicate node name %q (inputs[%d])", in.Name, i)
		}
		if in.Type == "" {
			return fmt.Errorf("inputs[%d] (%q): type must not be empty", i, in.Name)
		}
		names[in.Name] = "input"
	}
	for i, t := range p.Transforms {
		if err := ValidateNodeName(t.Name); err != nil {
			return fmt.Errorf("transforms[%d]: %w", i, err)
		}
		if _, dup := names[t.Name]; dup {
			return fmt.Errorf("duplicate node name %q (transforms[%d])", t.Name, i)
		}
		if t.Type == "" {
			return fmt.Errorf("transforms[%d] (%q): type must not be empty", i, t.Name)
		}
		for j, ref := range t.Inputs {
			if ref == t.Name {
				return fmt.Errorf("transforms[%d] (%q): inputs[%d] is a self-reference", i, t.Name, j)
			}
			if _, ok := names[ref]; !ok {
				return fmt.Errorf("transforms[%d] (%q): inputs[%d] %q is not a known upstream node", i, t.Name, j, ref)
			}
		}
		names[t.Name] = "transform"
	}
	for i, o := range p.Outputs {
		if err := ValidateNodeName(o.Name); err != nil {
			return fmt.Errorf("outputs[%d]: %w", i, err)
		}
		if _, dup := names[o.Name]; dup {
			return fmt.Errorf("duplicate node name %q (outputs[%d])", o.Name, i)
		}
		if o.Type == "" {
			return fmt.Errorf("outputs[%d] (%q): type must not be empty", i, o.Name)
		}
		if o.Input != "" {
			if _, ok := names[o.Input]; !ok {
				return fmt.Errorf("outputs[%d] (%q): input %q is not a known upstream node", i, o.Name, o.Input)
			}
		}
		names[o.Name] = "output"
	}
	if err := ValidateSchedule(p.Schedule); err != nil {
		return err
	}
	if !IsKnownMode(p.Mode) {
		return fmt.Errorf("pipeline mode %q is invalid: allowed values are '', %q, %q", p.Mode, ModeFull, ModeAppend)
	}
	return nil
}

// splitFields whitespace-splits s the same way the cron specs treat
// it. Avoids importing strings from the type file just to call Fields
// on the schedule string.
func splitFields(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// ClonePipeline returns a deep copy of p. Slices and maps are copied
// so callers can mutate the result without aliasing back into the
// store's row.
func ClonePipeline(p *Pipeline) *Pipeline {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Inputs = cloneInputs(p.Inputs)
	cp.Transforms = cloneTransforms(p.Transforms)
	cp.Outputs = cloneOutputs(p.Outputs)
	cp.LastKnownSchema = cloneSchemaFields(p.LastKnownSchema)
	return &cp
}

// cloneSchemaFields returns a deep copy of in.
func cloneSchemaFields(in []SchemaField) []SchemaField {
	if in == nil {
		return nil
	}
	out := make([]SchemaField, len(in))
	copy(out, in)
	return out
}

func cloneInputs(in []Input) []Input {
	if in == nil {
		return nil
	}
	out := make([]Input, len(in))
	for i, v := range in {
		c := v
		c.Config = cloneConfig(v.Config)
		out[i] = c
	}
	return out
}

func cloneTransforms(in []Transform) []Transform {
	if in == nil {
		return nil
	}
	out := make([]Transform, len(in))
	for i, v := range in {
		c := v
		c.Inputs = append([]string(nil), v.Inputs...)
		c.Config = cloneConfig(v.Config)
		out[i] = c
	}
	return out
}

func cloneOutputs(in []Output) []Output {
	if in == nil {
		return nil
	}
	out := make([]Output, len(in))
	for i, v := range in {
		c := v
		c.Config = cloneConfig(v.Config)
		out[i] = c
	}
	return out
}

func cloneConfig(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
