// Verifies the SDK cookbook (US-364) ships every chapter the README
// promises and that each Python sample parses cleanly via py_compile.
// Mirrors examples/quickstart_test.go — structure check is unconditional,
// the Python toolchain check skips when no interpreter is on PATH so
// minimal CI containers stay green.
package cookbook_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// chapters is the canonical list of cookbook chapters. Update when adding
// or renaming a recipe — the README index must list every entry here.
var chapters = []string{
	"01-async",
	"02-retry",
	"03-batching",
	"04-subscription",
	"05-rag",
	"06-ws-subscription",
	"07-builders",
}

// TestCookbook_StructureMatches asserts every chapter ships both a markdown
// explanation and a runnable Python script under the same stem.
func TestCookbook_StructureMatches(t *testing.T) {
	must := []string{"README.md"}
	for _, c := range chapters {
		must = append(must, c+".md", c+".py")
	}
	for _, name := range must {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

// TestCookbook_PythonSamples_PyCompile parses every chapter's Python
// sample with py_compile to catch syntax breakage from a doc edit.
// Skipped when no python3 is on PATH — the SDK itself is exercised by
// sdk/python's own pytest suite, so missing the interpreter here only
// loses cookbook-specific gating.
func TestCookbook_PythonSamples_PyCompile(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		py, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("no python interpreter on PATH")
	}
	for _, c := range chapters {
		script := c + ".py"
		t.Run(script, func(t *testing.T) {
			cmd := exec.Command(py, "-m", "py_compile", script)
			cmd.Dir, _ = filepath.Abs(".")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("py_compile %s failed: %v\noutput:\n%s", script, err, string(out))
			}
		})
	}
}
