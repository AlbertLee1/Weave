package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBDD_WebEmbedDoesNotRequireGeneratedDist(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	serverDir := filepath.Dir(testFile)
	repoRoot := filepath.Clean(filepath.Join(serverDir, "..", ".."))

	embedGo, err := os.ReadFile(filepath.Join(serverDir, "embed.go"))
	if err != nil {
		t.Fatalf("read embed.go: %v", err)
	}
	for _, line := range strings.Split(string(embedGo), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//go:embed") && strings.Contains(line, "web/dist") {
			t.Fatalf("embed directive must not point directly at ignored generated dist: %s", line)
		}
	}

	if _, err := os.Stat(filepath.Join(serverDir, "web", "embed-placeholder.txt")); err != nil {
		t.Fatalf("fresh CI needs a tracked fallback file under cmd/server/web: %v", err)
	}

	t.Run("fresh Go build succeeds without generated SPA assets", func(t *testing.T) {
		moveGeneratedDistAside(t, filepath.Join(serverDir, "web", "dist"))

		cmd := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "weave"), "./cmd/server")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go build ./cmd/server without generated WebUI dist failed: %v\n%s", err, out)
		}
	})

	t.Run("local help describes no-UI and embedded-UI build paths", func(t *testing.T) {
		cmd := exec.Command("make", "help")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make help failed: %v\n%s", err, out)
		}

		help := string(out)
		for _, want := range []string{
			"build",
			"Build Go server without generating embedded WebUI assets",
			"build-with-ui",
			"Build production server with embedded WebUI assets",
		} {
			if !strings.Contains(help, want) {
				t.Fatalf("make help output missing %q:\n%s", want, help)
			}
		}
	})

	t.Run("production build target generates WebUI before embedding", func(t *testing.T) {
		makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
		if err != nil {
			t.Fatalf("read Makefile: %v", err)
		}
		text := string(makefile)
		if !strings.Contains(text, "\nbuild-with-ui: web-build build") {
			t.Fatalf("build-with-ui must run web-build before build so SPA assets are embedded")
		}
		if !strings.Contains(text, "cp -r web/dist cmd/server/web/dist") {
			t.Fatalf("web-build must copy web/dist into cmd/server/web/dist before the server build")
		}
	})
}

func moveGeneratedDistAside(t *testing.T, distDir string) {
	t.Helper()

	if _, err := os.Stat(distDir); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("stat generated WebUI dist: %v", err)
	}

	backup := filepath.Join(t.TempDir(), "dist")
	if err := os.Rename(distDir, backup); err != nil {
		t.Fatalf("move generated WebUI dist aside: %v", err)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(distDir); err == nil {
			t.Fatalf("generated WebUI dist was recreated before test cleanup; refusing to overwrite %s", distDir)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat generated WebUI dist before restore: %v", err)
		}
		if err := os.Rename(backup, distDir); err != nil {
			t.Fatalf("restore generated WebUI dist: %v", err)
		}
	})
}
