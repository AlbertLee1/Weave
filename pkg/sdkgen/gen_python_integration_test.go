//go:build integration
// +build integration

package sdkgen_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liyang/weave/pkg/sdkgen"
)

// TestPythonSDK_Mypy generates a Python SDK and runs `mypy` against it to
// confirm the output is type-correct. Skipped when no `mypy` binary is
// reachable on PATH.
func TestPythonSDK_Mypy(t *testing.T) {
	mypy := findMypy(t)
	if mypy == "" {
		t.Skip("no mypy available on PATH")
	}

	g, err := sdkgen.GetGenerator("python")
	if err != nil {
		t.Fatalf("GetGenerator: %v", err)
	}
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	cmd := exec.Command(mypy, "--ignore-missing-imports", "weave_sdk")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mypy failed: %v\noutput:\n%s", err, string(out))
	}
}

// findMypy returns a path to a usable mypy binary, or "" if none can be found.
func findMypy(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("mypy"); err == nil {
		return p
	}
	return ""
}
