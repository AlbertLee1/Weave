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

// TestGoSDK_GoVet generates a Go SDK and runs `go vet` against it to confirm
// the output compiles cleanly and passes static analysis. Skipped when no `go`
// binary is reachable on PATH.
func TestGoSDK_GoVet(t *testing.T) {
	goBin := findGo(t)
	if goBin == "" {
		t.Skip("no go binary available on PATH")
	}

	g, err := sdkgen.GetGenerator("go")
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

	cmd := exec.Command(goBin, "vet", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go vet failed: %v\noutput:\n%s", err, string(out))
	}
}

func findGo(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return ""
}
