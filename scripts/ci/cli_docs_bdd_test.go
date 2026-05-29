package ci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBDD_CLIDocsCoverActionAggregateObjectset is the round-48 doc
// contract for PRD-V2 Gap-D3 ("CLI action / aggregate / objectset
// 深度"). The CLI binaries grew action / aggregate / objectset
// subcommands long ago but docs/cli.md never described them — SDK
// users had to read the source to discover the flag surface.
//
// This test asserts that:
//
//   - Every subcommand the runner dispatches has a corresponding
//     `### \`weave <cmd>` section in docs/cli.md.
//   - Every required flag the command parses (e.g. --ontology,
//     --action, --body, --params) is mentioned in the doc section
//     so users see the contract before invoking.
//   - At least one runnable example for each major subcommand
//     (action apply, aggregate, objectset load) is present.
//
// The test is intentionally string-grep based: a richer parser
// would over-constrain how docs author future examples. A grep
// drifts only when the doc actually loses a section.
func TestBDD_CLIDocsCoverActionAggregateObjectset(t *testing.T) {
	docPath := filepath.Join(repoRoot(t), "docs", "cli.md")
	bytes, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read docs/cli.md: %v", err)
	}
	doc := string(bytes)

	// One section header per CLI command family — anchors the
	// rest of the assertions to a stable headline structure.
	requiredSections := []string{
		"### `weave action apply`",
		"### `weave aggregate`",
		"### `weave objectset <load|create-temporary>`",
	}
	for _, s := range requiredSections {
		if !strings.Contains(doc, s) {
			t.Errorf("docs/cli.md must contain section header %q", s)
		}
	}

	// Required flags each section MUST mention so users learn the
	// contract from the doc rather than from --help.
	requiredFlags := []string{
		"`--ontology <name>`",  // shared across all three
		"`--action <api-name>`", // action apply
		"`--params",            // action apply parameter input
		"`--param key=value`",  // action apply key=value
		"`--mode VALIDATE_ONLY", // action apply mode toggle
		"`--returnEdits ALL",   // action apply edits policy
		"`--type <api-name>`",  // aggregate
		"`--body",              // aggregate + objectset bodies
		"`--output json \\| table`", // aggregate output format
	}
	for _, f := range requiredFlags {
		if !strings.Contains(doc, f) {
			t.Errorf("docs/cli.md must document flag %q", f)
		}
	}

	// Each major subcommand needs at least one runnable example.
	// Grep for the command verbatim — examples typically appear
	// inside ```bash blocks below the section header.
	requiredExamples := []string{
		"weave action apply",
		"weave aggregate",
		"weave objectset load",
		"weave objectset create-temporary",
	}
	for _, e := range requiredExamples {
		if !strings.Contains(doc, e) {
			t.Errorf("docs/cli.md must contain at least one runnable example for %q", e)
		}
	}

	// Body templates section — copy-paste-ready JSON shapes for
	// the common cases. Helps users skip the "what shape does the
	// server expect?" round-trip.
	requiredTemplates := []string{
		`"objectSet"`,
		`"aggregation"`,
		`"groupBy"`,
		`"searchAround"`,
	}
	for _, t_ := range requiredTemplates {
		if !strings.Contains(doc, t_) {
			t.Errorf("docs/cli.md must contain a body-template fragment matching %q", t_)
		}
	}
}

// (repoRoot helper lives in ci_workflow_bdd_test.go in this package
// and is reused here — same go.mod walk-up.)
