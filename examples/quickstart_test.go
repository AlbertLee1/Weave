// Verifies the three-language quickstart templates (US-363) stay in good
// shape. The "exists" test runs unconditionally; the per-language compile
// tests skip when their toolchain isn't reachable so the suite stays green
// in minimal CI containers.
//
// US-419 extends ts-quickstart from a single-file fetch demo into a full
// OSDK with Object / Action / Function / Subscribe + OpenAPI types. The
// build now emits .d.ts declarations and ships node:test unit tests; the
// Go suite verifies both alongside the legacy `tsc --noEmit` typecheck.
package examples_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestQuickstarts_StructureMatches asserts every quickstart ships the files
// the README promises. Cheap and unconditional so a missing file fails fast.
func TestQuickstarts_StructureMatches(t *testing.T) {
	cases := []struct {
		dir   string
		files []string
	}{
		{"py-quickstart", []string{"README.md", "main.py", "requirements.txt"}},
		{"ts-quickstart", []string{
			"README.md", "package.json", "tsconfig.json",
			"src/main.ts", "src/client.ts", "src/objects.ts",
			"src/actions.ts", "src/functions.ts", "src/subscribe.ts",
			"src/openapi.ts", "src/transport.ts", "src/index.ts",
		}},
		{"go-quickstart", []string{"README.md", "main.go", "go.mod"}},
	}

	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			for _, name := range tc.files {
				full := filepath.Join(tc.dir, name)
				if _, err := os.Stat(full); err != nil {
					t.Errorf("missing %s: %v", full, err)
				}
			}
		})
	}
}

// TestGoQuickstart_GoVet verifies the Go quickstart compiles and passes vet
// in its own module. Runs unconditionally — `go vet` is the test runner's
// own toolchain so it's always reachable.
func TestGoQuickstart_GoVet(t *testing.T) {
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = "go-quickstart"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet failed: %v\noutput:\n%s", err, string(out))
	}
}

// TestTypeScriptQuickstart_TSCNoEmit verifies the TS quickstart type-checks
// cleanly. Skipped when no tsc binary is available (locally via
// web/node_modules or globally via PATH) so devs without the web toolchain
// still see a green suite. CI installs typescript via the web/ workflow so
// this gate is enforced there.
func TestTypeScriptQuickstart_TSCNoEmit(t *testing.T) {
	tsc := findTSC(t)
	if tsc == "" {
		t.Skip("no tsc available (need web/node_modules/typescript or npx)")
	}
	dir, err := filepath.Abs("ts-quickstart")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	cmd := exec.Command(tsc, "--noEmit", "--project", filepath.Join(dir, "tsconfig.json"))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc --noEmit failed: %v\noutput:\n%s", err, string(out))
	}
}

// TestPythonQuickstart_PyCompile verifies the Python quickstart parses
// cleanly via py_compile. Skipped when no python3 binary is available —
// the SDK itself is exercised by sdk/python's own pytest suite.
func TestPythonQuickstart_PyCompile(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		py, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("no python interpreter on PATH")
	}
	cmd := exec.Command(py, "-m", "py_compile", "main.py")
	cmd.Dir = "py-quickstart"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("py_compile failed: %v\noutput:\n%s", err, string(out))
	}
}

// TestTypeScriptQuickstart_OSDKBuildEmitsDeclarations verifies the OSDK
// build emits .d.ts declaration files for every public module — the
// "类型安全（生成 .d.ts）" leg of US-419's acceptance criteria. Skipped
// when no tsc binary is available, same as the typecheck test.
func TestTypeScriptQuickstart_OSDKBuildEmitsDeclarations(t *testing.T) {
	tsc := findTSC(t)
	if tsc == "" {
		t.Skip("no tsc available (need web/node_modules/typescript or npx)")
	}
	dir, err := filepath.Abs("ts-quickstart")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	distDir := filepath.Join(dir, "dist")
	_ = os.RemoveAll(distDir)
	t.Cleanup(func() { _ = os.RemoveAll(distDir) })

	cmd := exec.Command(tsc, "--project", filepath.Join(dir, "tsconfig.json"))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc build failed: %v\noutput:\n%s", err, string(out))
	}

	// Every public OSDK module must have a sibling .d.ts.
	wantDeclarations := []string{
		"index.d.ts",
		"client.d.ts",
		"objects.d.ts",
		"actions.d.ts",
		"functions.d.ts",
		"subscribe.d.ts",
		"transport.d.ts",
		"openapi.d.ts",
	}
	for _, name := range wantDeclarations {
		p := filepath.Join(distDir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected .d.ts at %s: %v", p, err)
		}
	}
}

// TestTypeScriptQuickstart_OSDKUnitTests runs the OSDK's `node:test` unit
// suite from the freshly-emitted dist/ output. Skipped when either tsc or
// node is missing — Node's built-in test runner is needed (Node 20+).
func TestTypeScriptQuickstart_OSDKUnitTests(t *testing.T) {
	tsc := findTSC(t)
	if tsc == "" {
		t.Skip("no tsc available")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node binary on PATH")
	}
	dir, err := filepath.Abs("ts-quickstart")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	distDir := filepath.Join(dir, "dist")
	_ = os.RemoveAll(distDir)
	t.Cleanup(func() { _ = os.RemoveAll(distDir) })

	build := exec.Command(tsc, "--project", filepath.Join(dir, "tsconfig.json"))
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("tsc build failed: %v\noutput:\n%s", err, string(out))
	}

	testsDir := filepath.Join(distDir, "__tests__")
	entries, err := os.ReadDir(testsDir)
	if err != nil {
		t.Fatalf("read %s: %v", testsDir, err)
	}
	var jsTests []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".test.js") {
			jsTests = append(jsTests, filepath.Join(testsDir, e.Name()))
		}
	}
	if len(jsTests) == 0 {
		t.Fatalf("no compiled .test.js files under %s", testsDir)
	}

	args := append([]string{"--test"}, jsTests...)
	cmd := exec.Command(node, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test failed: %v\noutput:\n%s", err, string(out))
	}
}

// findTSC returns a path to a usable tsc binary, or "" if none can be found.
// Prefers web/node_modules to avoid network fetches; falls back to PATH.
// Mirrors pkg/sdkgen.gen_ts_integration_test.findTSC.
func findTSC(t *testing.T) string {
	t.Helper()

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
	if p, err := exec.LookPath("tsc"); err == nil {
		return p
	}
	return ""
}
