package main

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

func TestGojaFunctionExecutorExecutesStoredSourceWithParameters(t *testing.T) {
	exec := newGojaFunctionExecutor()
	fn := &oms.Function{
		RID:        "ri.ontology.main.function.us444",
		Name:       "us444",
		Version:    "1.0.0",
		Runtime:    oms.FunctionRuntimeGoja,
		SourceCode: `function main(input) { return input.parameters.name + "-ok"; }`,
	}

	got, err := exec.Execute(context.Background(), fn, map[string]interface{}{
		"name": "weave",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got != "weave-ok" {
		t.Fatalf("Execute result = %#v, want weave-ok", got)
	}
}
