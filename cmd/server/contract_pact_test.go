package main

import (
	"path/filepath"
	"sort"
	"strings"
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

// TestContract_AllFourSDKsHaveAPactFile is the US-445 presence gate: every
// language SDK in the multi-language fan-out (py / ts / go / java) MUST ship
// at least one consumer-driven pact file under cmd/server/testdata/pacts/.
// The file naming convention is `<consumer>-<topic>.pact.json` where
// `<consumer>` starts with the canonical `<lang>-sdk` prefix; this lets the
// gate match by filename without parsing every JSON to inspect the
// `consumer.name` field.
func TestContract_AllFourSDKsHaveAPactFile(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("testdata", "pacts", "*.pact.json"))
	if err != nil {
		t.Fatalf("glob pacts: %v", err)
	}
	wantPrefixes := []string{"python-sdk", "ts-sdk", "go-sdk", "java-sdk"}
	seen := map[string]bool{}
	for _, path := range matches {
		base := filepath.Base(path)
		for _, prefix := range wantPrefixes {
			if strings.HasPrefix(base, prefix+"-") {
				seen[prefix] = true
			}
		}
	}
	for _, prefix := range wantPrefixes {
		if !seen[prefix] {
			t.Errorf("missing pact file for %s — expected at least one cmd/server/testdata/pacts/%s-*.pact.json",
				prefix, prefix)
		}
	}
}

// TestContract_PactConsumerNameMatchesFilename guards the same convention
// from the inside: every pact file's `consumer.name` MUST start with the
// `weave-<lang>-sdk` form (or `weave-web-client` for the SPA pact). A
// mistyped consumer name would silently fail the broker-side dedup contract
// (re-publishes for "weave-pyhton-sdk" become a new consumer rather than a
// new version of the existing one).
func TestContract_PactConsumerNameMatchesFilename(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("testdata", "pacts", "*.pact.json"))
	if err != nil {
		t.Fatalf("glob pacts: %v", err)
	}
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			pact, err := contract.LoadPact(path)
			if err != nil {
				t.Fatalf("load %s: %v", path, err)
			}
			name := pact.Consumer.Name
			if !strings.HasPrefix(name, "weave-") {
				t.Errorf("pact %s consumer.name=%q must start with 'weave-'", path, name)
			}
			if pact.Provider.Name != "weave-server" {
				t.Errorf("pact %s provider.name=%q must be 'weave-server'", path, pact.Provider.Name)
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
