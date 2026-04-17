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

// TestTypescriptSDK_TSCNoEmit generates a TypeScript SDK and runs `tsc --noEmit`
// against it to confirm the output is type-correct. Skipped when no `tsc`
// binary is reachable (locally via web/node_modules or globally via npx).
func TestTypescriptSDK_TSCNoEmit(t *testing.T) {
	tsc := findTSC(t)
	if tsc == "" {
		t.Skip("no tsc available (need web/node_modules/typescript or npx)")
	}

	g, err := sdkgen.GetGenerator("ts")
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

	cmd := exec.Command(tsc, "--noEmit", "--project", filepath.Join(dir, "tsconfig.json"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc --noEmit failed: %v\noutput:\n%s", err, string(out))
	}
}

// findTSC returns a path to a usable tsc binary, or "" if none can be found.
func findTSC(t *testing.T) string {
	t.Helper()

	// Prefer the locally installed tsc from web/node_modules to avoid network
	// fetches. Walk up from the test working dir to find the repo root.
	wd, err := os.Getwd()
	if err == nil {
		root := wd
		for i := 0; i < 6; i++ {
			candidate := filepath.Join(root, "web", "node_modules", "typescript", "bin", "tsc")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			root = filepath.Dir(root)
		}
	}

	// Fall back to PATH (npx-installed or globally installed).
	if p, err := exec.LookPath("tsc"); err == nil {
		return p
	}
	return ""
}
