package modelfunctions_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/modelfunctions"
)

// TestBuildWrapperFunction_Given_LiveModelDeployment_When_Build_Then_HTTPRuntimeFunction
// covers VTX-050 BDD #1: a live model deployment (HTTP endpoint + I/O schema)
// turns into an oms.Function row with runtime="http", SourceCode pointing
// at the deployment endpoint, and a signature derived from the deployment's
// input/output schema.
func TestBuildWrapperFunction_Given_LiveModelDeployment_When_Build_Then_HTTPRuntimeFunction(t *testing.T) {
	dep := modelfunctions.Deployment{
		RID:         "ri.vertex.main.model-deployment.flight-delay",
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "flight-delay-predictor",
		EndpointURL: "https://models.example.com/flight-delay/predict",
		Inputs: []modelfunctions.SchemaParam{
			{Name: "distance_km", Type: "double", Required: true},
			{Name: "departure_hour", Type: "integer", Required: true},
		},
		Output: &modelfunctions.SchemaReturn{Type: "double"},
	}

	fn, err := modelfunctions.BuildWrapperFunction(dep, "creator@example.com")
	if err != nil {
		t.Fatalf("BuildWrapperFunction: %v", err)
	}

	if fn.OntologyRID != dep.OntologyRID {
		t.Errorf("OntologyRID: got %q, want %q", fn.OntologyRID, dep.OntologyRID)
	}
	if fn.Name != dep.Name {
		t.Errorf("Name: got %q, want %q", fn.Name, dep.Name)
	}
	if fn.Runtime != oms.FunctionRuntimeHTTP {
		t.Errorf("Runtime: got %q, want %q", fn.Runtime, oms.FunctionRuntimeHTTP)
	}
	if fn.SourceCode != dep.EndpointURL {
		t.Errorf("SourceCode: got %q, want endpoint URL %q", fn.SourceCode, dep.EndpointURL)
	}
	if fn.CreatedBy != "creator@example.com" {
		t.Errorf("CreatedBy: got %q, want %q", fn.CreatedBy, "creator@example.com")
	}
	if fn.RID == "" || !strings.HasPrefix(fn.RID, "ri.ontology.main.function.") {
		t.Errorf("RID: got %q, want canonical ri.ontology.main.function.<uuid>", fn.RID)
	}
	if fn.Version != oms.DefaultFunctionVersion {
		t.Errorf("Version: got %q, want default %q", fn.Version, oms.DefaultFunctionVersion)
	}

	parsed, err := oms.ParseFunctionSignature(fn.Signature)
	if err != nil {
		t.Fatalf("ParseFunctionSignature: %v", err)
	}
	if len(parsed.Params) != 2 {
		t.Fatalf("params: got %d, want 2", len(parsed.Params))
	}
	if parsed.Params[0].Name != "distance_km" || parsed.Params[0].Type != "double" || !parsed.Params[0].Required {
		t.Errorf("params[0]: got %+v, want distance_km/double/required", parsed.Params[0])
	}
	if parsed.Params[1].Name != "departure_hour" || parsed.Params[1].Type != "integer" || !parsed.Params[1].Required {
		t.Errorf("params[1]: got %+v, want departure_hour/integer/required", parsed.Params[1])
	}
	if parsed.Returns == nil || parsed.Returns.Type != "double" {
		t.Errorf("returns: got %+v, want type=double", parsed.Returns)
	}
}

// TestBuildWrapperFunction_Given_EmptyDeploymentName_When_Build_Then_Error
// guards the registration path so a deployment with no name can't sneak in
// an oms.Function whose Validate() would later reject it at write time.
func TestBuildWrapperFunction_Given_EmptyDeploymentName_When_Build_Then_Error(t *testing.T) {
	dep := modelfunctions.Deployment{
		OntologyRID: "ri.ontology.main.ontology.o1",
		EndpointURL: "https://models.example.com/predict",
		Output:      &modelfunctions.SchemaReturn{Type: "double"},
	}
	if _, err := modelfunctions.BuildWrapperFunction(dep, "u"); err == nil {
		t.Fatal("expected error for empty deployment name")
	}
}

// TestBuildWrapperFunction_Given_EmptyEndpointURL_When_Build_Then_Error
// ensures we never produce an http-runtime function whose SourceCode (the
// delegate URL) is the empty string — that would 404 at execution time.
func TestBuildWrapperFunction_Given_EmptyEndpointURL_When_Build_Then_Error(t *testing.T) {
	dep := modelfunctions.Deployment{
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "x",
		Output:      &modelfunctions.SchemaReturn{Type: "double"},
	}
	if _, err := modelfunctions.BuildWrapperFunction(dep, "u"); err == nil {
		t.Fatal("expected error for empty endpoint URL")
	}
}

// TestBuildWrapperFunction_Given_EmptyOntologyRID_When_Build_Then_Error
// keeps the wrapper from leaking into a "no ontology" row that the OMS
// migration would refuse to insert.
func TestBuildWrapperFunction_Given_EmptyOntologyRID_When_Build_Then_Error(t *testing.T) {
	dep := modelfunctions.Deployment{
		Name:        "x",
		EndpointURL: "https://e/predict",
		Output:      &modelfunctions.SchemaReturn{Type: "double"},
	}
	if _, err := modelfunctions.BuildWrapperFunction(dep, "u"); err == nil {
		t.Fatal("expected error for empty ontology RID")
	}
}

// TestBuildWrapperFunction_Given_UnsupportedInputType_When_Build_Then_Error
// confirms the param-type allowlist (funcregistry.ValidateParameterTypes)
// gates wrapper generation: a deployment claiming to take an "aggregation"
// input must be rejected at registration, not at first invocation.
func TestBuildWrapperFunction_Given_UnsupportedInputType_When_Build_Then_Error(t *testing.T) {
	dep := modelfunctions.Deployment{
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "bad-model",
		EndpointURL: "https://models.example.com/bad",
		Inputs:      []modelfunctions.SchemaParam{{Name: "agg", Type: "aggregation", Required: true}},
		Output:      &modelfunctions.SchemaReturn{Type: "double"},
	}
	_, err := modelfunctions.BuildWrapperFunction(dep, "u")
	if err == nil {
		t.Fatal("expected error for unsupported input type")
	}
	if !strings.Contains(err.Error(), "aggregation") {
		t.Errorf("error should mention offending type: %v", err)
	}
}

// TestBuildWrapperFunction_Given_UnsupportedOutputType_When_Build_Then_Error
// is the symmetric guard for the return field — an unsupported output
// type should also be rejected at registration time.
func TestBuildWrapperFunction_Given_UnsupportedOutputType_When_Build_Then_Error(t *testing.T) {
	dep := modelfunctions.Deployment{
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "bad-output",
		EndpointURL: "https://models.example.com/bad",
		Inputs:      []modelfunctions.SchemaParam{{Name: "x", Type: "double", Required: true}},
		Output:      &modelfunctions.SchemaReturn{Type: "notification"},
	}
	if _, err := modelfunctions.BuildWrapperFunction(dep, "u"); err == nil {
		t.Fatal("expected error for unsupported output type")
	}
}

// TestBuildWrapperFunction_Given_NoOutputSchema_When_Build_Then_NoReturnsInSignature
// permits a deployment that declares only inputs — the wrapper signature
// must not invent a fake returns clause (ValidateFunctionSignature would
// reject {"type":""}).
func TestBuildWrapperFunction_Given_NoOutputSchema_When_Build_Then_NoReturnsInSignature(t *testing.T) {
	dep := modelfunctions.Deployment{
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "fire-and-forget",
		EndpointURL: "https://models.example.com/fire",
		Inputs:      []modelfunctions.SchemaParam{{Name: "x", Type: "double", Required: true}},
	}
	fn, err := modelfunctions.BuildWrapperFunction(dep, "u")
	if err != nil {
		t.Fatalf("BuildWrapperFunction: %v", err)
	}
	parsed, _ := oms.ParseFunctionSignature(fn.Signature)
	if parsed.Returns != nil {
		t.Errorf("returns: got %+v, want nil", parsed.Returns)
	}
	if len(parsed.Params) != 1 {
		t.Errorf("params: got %d, want 1", len(parsed.Params))
	}
}

// TestBuildWrapperFunction_Given_GeneratedSignature_When_OMSValidate_Then_NoError
// is the integration assertion that the JSON we emit survives the OMS
// signature validator unchanged — i.e. the wrapper is directly persistable
// via Repository.CreateFunction with no shape massaging.
func TestBuildWrapperFunction_Given_GeneratedSignature_When_OMSValidate_Then_NoError(t *testing.T) {
	dep := modelfunctions.Deployment{
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "good",
		EndpointURL: "https://e/predict",
		Inputs:      []modelfunctions.SchemaParam{{Name: "x", Type: "double", Required: true}},
		Output:      &modelfunctions.SchemaReturn{Type: "double"},
	}
	fn, err := modelfunctions.BuildWrapperFunction(dep, "u")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := oms.ValidateFunctionSignature(fn.Signature); err != nil {
		t.Errorf("signature should pass OMS validator: %v", err)
	}
	if err := fn.Validate(); err != nil {
		t.Errorf("fn.Validate: %v", err)
	}

	// Sanity: emitted JSON has both keys when both are populated.
	var probe struct {
		Params  json.RawMessage `json:"params"`
		Returns json.RawMessage `json:"returns"`
	}
	if err := json.Unmarshal(fn.Signature, &probe); err != nil {
		t.Fatalf("unmarshal signature: %v", err)
	}
	if len(probe.Params) == 0 {
		t.Error("signature missing params")
	}
	if len(probe.Returns) == 0 {
		t.Error("signature missing returns")
	}
}
