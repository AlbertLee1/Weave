// Package modelfunctions hosts the Vertex Live Model Deployment wrapper
// surface (VTX-050): persisting deployment metadata for an external
// HTTP-served model and auto-generating an oms.Function row that wraps
// it. The generated wrapper carries Runtime="http", SourceCode pointing
// at the deployment endpoint, and a Signature derived from the
// deployment's declared input/output schema — so downstream Vertex
// Scenarios can reference the wrapper through the same Function APIs
// they use for hand-authored Functions (VTX-048).
//
// This package is HTTP-thin: BuildWrapperFunction is a pure-Go helper
// the handler delegates to, so the wrapper-generation rules are unit
// testable without touching chi or the database.
package modelfunctions

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
	"github.com/liyang/weave/pkg/vertex/funcregistry"
)

// SchemaParam is one input slot declared by a live model deployment.
// The fields mirror oms.FunctionParam exactly so wrapper generation is
// a one-to-one map — no impedance conversion at signature build time.
type SchemaParam struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Required bool            `json:"required,omitempty"`
	Default  json.RawMessage `json:"default,omitempty"`
}

// SchemaReturn is the optional output type the deployment declares.
// Modelled as a pointer so a deployment with no output schema (a
// fire-and-forget side effect) can be expressed as Output=nil rather
// than {Type: ""} — the OMS signature validator rejects the latter.
type SchemaReturn struct {
	Type string `json:"type"`
}

// Deployment is the row stored in model_deployments. It carries enough
// metadata for the wrapper to forward requests and for operators to
// distinguish multiple versions of the same logical model. The shape
// is also the JSON returned by the register endpoint.
type Deployment struct {
	RID          string        `json:"rid"`
	OntologyRID  string        `json:"ontologyRid"`
	Name         string        `json:"name"`
	EndpointURL  string        `json:"endpointUrl"`
	ModelVersion string        `json:"modelVersion,omitempty"`
	Inputs       []SchemaParam `json:"inputs,omitempty"`
	Output       *SchemaReturn `json:"output,omitempty"`
	FunctionRID  string        `json:"functionRid,omitempty"`
	CreatedBy    string        `json:"createdBy,omitempty"`
	CreatedAt    time.Time     `json:"createdAt,omitempty"`
}

// ErrEmptyDeploymentName is returned when BuildWrapperFunction is
// handed a deployment whose Name is blank. We surface a sentinel so
// the handler can distinguish "bad input" from "downstream failure"
// without grepping error strings.
var ErrEmptyDeploymentName = errors.New("modelfunctions: deployment name is required")

// ErrEmptyEndpointURL is returned when EndpointURL is blank. An empty
// EndpointURL would land in oms.Function.SourceCode and, for
// Runtime="http", would 404 the very first time the wrapper ran.
var ErrEmptyEndpointURL = errors.New("modelfunctions: deployment endpointUrl is required")

// ErrEmptyOntologyRID is returned when OntologyRID is blank. The OMS
// migration requires every function row to belong to an ontology, so
// catching the violation here gives a typed error rather than a
// pgx-level constraint failure later.
var ErrEmptyOntologyRID = errors.New("modelfunctions: deployment ontologyRid is required")

// BuildWrapperFunction turns a deployment into the oms.Function row a
// Repository.CreateFunction call can persist. The returned function:
//
//   - has a fresh RID
//   - declares Runtime="http"
//   - stores the deployment's EndpointURL in SourceCode (the runtime
//     dispatcher's convention for HTTP-runtime functions)
//   - carries a signature derived 1-1 from the deployment schema, with
//     the same param-type allowlist funcregistry enforces at human-
//     authored registration time (so a live model can't sneak in an
//     "aggregation" input the rest of the platform can't carry)
//
// The empty deployment is rejected up front. createdBy is passed
// through verbatim so audit log entries name the operator that ran
// the register call, not the platform.
func BuildWrapperFunction(dep Deployment, createdBy string) (*oms.Function, error) {
	if dep.Name == "" {
		return nil, ErrEmptyDeploymentName
	}
	if dep.EndpointURL == "" {
		return nil, ErrEmptyEndpointURL
	}
	if dep.OntologyRID == "" {
		return nil, ErrEmptyOntologyRID
	}

	sigBytes, err := buildSignature(dep)
	if err != nil {
		return nil, err
	}

	parsed, err := oms.ParseFunctionSignature(sigBytes)
	if err != nil {
		return nil, fmt.Errorf("modelfunctions: parse generated signature: %w", err)
	}
	if err := funcregistry.ValidateParameterTypes(parsed); err != nil {
		return nil, err
	}

	fn := &oms.Function{
		RID:         rid.NewFunctionRID(),
		OntologyRID: dep.OntologyRID,
		Name:        dep.Name,
		Version:     oms.DefaultFunctionVersion,
		SourceCode:  dep.EndpointURL,
		Runtime:     oms.FunctionRuntimeHTTP,
		Signature:   sigBytes,
		CreatedBy:   createdBy,
	}
	if err := fn.Validate(); err != nil {
		return nil, fmt.Errorf("modelfunctions: wrapper function failed validation: %w", err)
	}
	return fn, nil
}

// buildSignature renders the deployment's I/O schema as the canonical
// `{"params":[...],"returns":{...}}` shape oms.ParseFunctionSignature
// expects. An absent Output collapses to no `returns` key — emitting
// `{"type":""}` would trigger oms.ValidateFunctionSignature's
// "returns.type is required" rule.
func buildSignature(dep Deployment) (json.RawMessage, error) {
	out := map[string]interface{}{}
	if len(dep.Inputs) > 0 {
		params := make([]map[string]interface{}, 0, len(dep.Inputs))
		for _, p := range dep.Inputs {
			entry := map[string]interface{}{"name": p.Name}
			if p.Type != "" {
				entry["type"] = p.Type
			}
			if p.Required {
				entry["required"] = true
			}
			if len(p.Default) > 0 {
				entry["default"] = json.RawMessage(p.Default)
			}
			params = append(params, entry)
		}
		out["params"] = params
	}
	if dep.Output != nil && dep.Output.Type != "" {
		out["returns"] = map[string]interface{}{"type": dep.Output.Type}
	}
	// An output declared with an empty Type is not a no-op: it is an
	// invalid signature and must surface as such so the caller can
	// fix it rather than silently producing a {"returns":{"type":""}}
	// row that ValidateFunctionSignature would reject downstream.
	if dep.Output != nil && dep.Output.Type == "" {
		return nil, errors.New("modelfunctions: output.type is required when output is supplied")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("modelfunctions: marshal signature: %w", err)
	}
	return raw, nil
}

// NewDeploymentRID generates a fresh RID for a model deployment row.
// Kept out of pkg/rid because the resource type is Vertex-specific and
// adding it there would force every consumer of the rid package to
// re-test the deny-list.
func NewDeploymentRID() string {
	return rid.New("vertex", "main", "model-deployment")
}
