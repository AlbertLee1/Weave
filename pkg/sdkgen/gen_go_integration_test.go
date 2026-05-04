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

// TestGoSDK_GoVet generates a Go SDK and runs `go vet ./...` against it to
// confirm the per-ontology layout (pkg/<ontology>/{client,objects,actions,
// functions}.go) compiles cleanly and passes static analysis. Skipped when
// no `go` binary is reachable on PATH.
func TestGoSDK_GoVet(t *testing.T) {
	goBin := findGo(t)
	if goBin == "" {
		t.Skip("no go binary available on PATH")
	}

	dir := writeGoSDK(t, goBin)

	cmd := exec.Command(goBin, "vet", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go vet failed: %v\noutput:\n%s", err, string(out))
	}
}

// TestGoSDK_GoTest runs `go test ./...` against the generated SDK to verify
// every emitted package loads cleanly under the test toolchain (US-420 AC:
// "go vet + go test 通过"). Packages without _test.go files report
// "[no test files]" — go test still exits 0, so the assertion is "every
// package compiles + the test binary loads", which catches subtler problems
// like cross-package symbol mismatches that go vet alone may miss.
func TestGoSDK_GoTest(t *testing.T) {
	goBin := findGo(t)
	if goBin == "" {
		t.Skip("no go binary available on PATH")
	}

	dir := writeGoSDK(t, goBin)

	cmd := exec.Command(goBin, "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\noutput:\n%s", err, string(out))
	}
}

// writeGoSDK generates a Go SDK from the shared test schema and writes it
// into a per-test temp directory. Returns the directory path so the caller
// can run go subcommands against it.
func writeGoSDK(t *testing.T, _ string) string {
	t.Helper()
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
	return dir
}

func findGo(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return ""
}
