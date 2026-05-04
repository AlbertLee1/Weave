//go:build integration
// +build integration

package sdkgen_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/sdkgen"
)

// TestJavaSDK_MvnCompile generates a Java SDK and runs `mvn -B -q compile`
// against it to confirm the Maven project structure compiles cleanly under
// JDK 11+. The acceptance criterion for US-421 is "mvn compile 通过".
//
// Skipped when no `mvn` binary is reachable on PATH so the unit-only
// `go test ./pkg/sdkgen/...` run on a developer laptop without a JVM stays
// green; CI environments that wire JDK + Maven exercise the real build.
func TestJavaSDK_MvnCompile(t *testing.T) {
	mvn := findMaven(t)
	if mvn == "" {
		t.Skip("no mvn binary available on PATH")
	}
	if !javaHomeValid(os.Getenv("JAVA_HOME")) {
		// JAVA_HOME unset or pointing at a stale path (Homebrew Cellar entries
		// disappear after upgrades). Try to discover a usable JDK; otherwise
		// skip — Maven cannot run without a JDK on the path.
		if home := discoverJavaHome(); home != "" {
			t.Setenv("JAVA_HOME", home)
		} else {
			t.Skip("JAVA_HOME not set and could not be discovered")
		}
	}

	dir := writeJavaSDK(t)

	cmd := exec.Command(mvn, "-B", "-q", "compile")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mvn compile failed: %v\noutput:\n%s", err, string(out))
	}
}

// javaHomeValid reports whether the given JAVA_HOME path looks like a usable
// JDK install — i.e. the directory exists and contains bin/javac.
func javaHomeValid(home string) bool {
	if home == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "javac")); err != nil {
		return false
	}
	return true
}

// discoverJavaHome returns a best-effort JAVA_HOME path. On macOS we use the
// system `java_home` helper; on Linux we resolve `javac` symlinks back to the
// JDK root. Empty string if nothing reasonable is found.
func discoverJavaHome() string {
	if out, err := exec.Command("/usr/libexec/java_home").Output(); err == nil {
		if home := strings.TrimSpace(string(out)); home != "" {
			return home
		}
	}
	javac, err := exec.LookPath("javac")
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(javac)
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(resolved))
}

// TestJavaSDK_Javac is a lighter-weight check that the generated .java files
// are at least syntactically valid: it invokes `javac` directly against the
// source tree without going through Maven. Skipped when no `javac` binary is
// reachable on PATH.
func TestJavaSDK_Javac(t *testing.T) {
	javac := findJavac(t)
	if javac == "" {
		t.Skip("no javac binary available on PATH")
	}

	dir := writeJavaSDK(t)

	classesDir := filepath.Join(dir, "target", "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		t.Fatalf("mkdir target/classes: %v", err)
	}

	srcDir := filepath.Join(dir, "src/main/java")
	var sources []string
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && filepath.Ext(path) == ".java" {
			sources = append(sources, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk src: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no .java sources found")
	}

	args := append([]string{"-d", classesDir, "-source", "11", "-target", "11"}, sources...)
	cmd := exec.Command(javac, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("javac failed: %v\noutput:\n%s", err, string(out))
	}
}

func writeJavaSDK(t *testing.T) string {
	t.Helper()
	g, err := sdkgen.GetGenerator("java")
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

func findMaven(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("mvn"); err == nil {
		return p
	}
	return ""
}

func findJavac(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("javac"); err == nil {
		return p
	}
	return ""
}
