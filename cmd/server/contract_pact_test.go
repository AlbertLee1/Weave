package main

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/liyang/weave/pkg/contract"
)

// TestContract_SDKPactsVerify replays every consumer-authored pact JSON file
// under cmd/server/testdata/pacts against the fully-wired chi router. Each
// pact represents one SDK / web-client expectation about a request/response
// shape; this test fails the moment a server change drifts away from any
// declared interaction.
//
// Adding a new pact: drop a *.pact.json under testdata/pacts/. The discovery
// glob picks it up automatically. The pact format is documented in
// pkg/contract/pact.go and the expected schema is small enough that a junior
// SDK engineer can author one without reading any Go code.
func TestContract_SDKPactsVerify(t *testing.T) {
	router := newContractTestRouter(t)

	matches, err := filepath.Glob(filepath.Join("testdata", "pacts", "*.pact.json"))
	if err != nil {
		t.Fatalf("glob pacts: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no pact files under cmd/server/testdata/pacts/ — skipping consumer-driven verification")
	}
	sort.Strings(matches)

	for _, path := range matches {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			pact, err := contract.LoadPact(path)
			if err != nil {
				t.Fatalf("load %s: %v", path, err)
			}
			errs := contract.VerifyPact(router, pact, contract.VerifyOptions{})
			if len(errs) > 0 {
				t.Fatalf("pact %q (consumer=%s) has %d failing interactions:\n%s",
					filepath.Base(path), pact.Consumer.Name, len(errs), formatPactErrors(errs))
			}
		})
	}
}

func formatPactErrors(errs []error) string {
	var b []byte
	for _, e := range errs {
		b = append(b, "  • "...)
		b = append(b, e.Error()...)
		b = append(b, '\n')
	}
	return string(b)
}
