package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const minGolangCILintForGo125 = "v2.4.0"

type workflowConfig struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

func TestBDD_GolangCILintWorkflowSupportsGo125(t *testing.T) {
	root := repoRoot(t)
	goMajor, goMinor := readGoVersion(t, filepath.Join(root, "go.mod"))
	lintVersion, onlyNewIssues := readGolangCILintActionConfig(t, filepath.Join(root, ".github", "workflows", "ci.yml"))

	if lintVersion == "latest" {
		t.Fatalf("golangci-lint CI version must be pinned to an explicit release, got %q", lintVersion)
	}
	if !onlyNewIssues {
		t.Fatal("golangci-lint CI must run with only-new-issues: true while the repository carries historical lint findings")
	}

	if goMajor > 1 || (goMajor == 1 && goMinor >= 25) {
		if compareVersions(lintVersion, minGolangCILintForGo125) < 0 {
			t.Fatalf(
				"go.mod targets Go %d.%d but CI pins golangci-lint %s; Go 1.25 targets require golangci-lint >= %s",
				goMajor,
				goMinor,
				lintVersion,
				minGolangCILintForGo125,
			)
		}
	}
}

func TestBDD_GovulncheckGateUsesFixedToolchainAndActionableScope(t *testing.T) {
	root := repoRoot(t)

	if got, want := readGoPatchVersion(t, filepath.Join(root, "go.mod")), [3]int{1, 26, 3}; compareVersionParts(got, want) < 0 {
		t.Errorf("go.mod must pin Go at or above %s for current standard-library govulncheck fixes, got %s", formatVersionParts(want), formatVersionParts(got))
	}

	if run := readGovulncheckWorkflowCommand(t, filepath.Join(root, ".github", "workflows", "ci.yml")); strings.TrimSpace(run) != "make vulncheck" {
		t.Errorf("CI govulncheck step must use local make vulncheck contract, got %q", run)
	}

	makefile := readFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "./scripts/ci/govulncheck.sh") {
		t.Error("Makefile vulncheck target must delegate to scripts/ci/govulncheck.sh so local and CI scans share one scope")
	}

	script := readFile(t, filepath.Join(root, "scripts", "ci", "govulncheck.sh"))
	for _, required := range []string{"./scripts/ci/go-packages.sh", "internal/testutil", "go env GOPATH", "\"$govulncheck_bin\" $packages"} {
		if !strings.Contains(script, required) {
			t.Errorf("scripts/ci/govulncheck.sh must contain %q", required)
		}
	}
}

func TestBDD_GoPackageListExcludesWebDependencyTrees(t *testing.T) {
	root := repoRoot(t)

	fixtureRoot := filepath.Join(root, "web", "node_modules", "ralph_go_package_fixture")
	fixturePkg := filepath.Join(fixtureRoot, "golang", "pkg", "foreign")
	if err := os.MkdirAll(fixturePkg, 0o755); err != nil {
		t.Fatalf("create node_modules Go fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(fixtureRoot); err != nil {
			t.Fatalf("remove node_modules Go fixture: %v", err)
		}
	})
	if err := os.WriteFile(filepath.Join(fixturePkg, "foreign.go"), []byte("package foreign\n"), 0o644); err != nil {
		t.Fatalf("write node_modules Go fixture: %v", err)
	}

	helper := filepath.Join(root, "scripts", "ci", "go-packages.sh")
	helperSource := readFile(t, helper)
	for _, required := range []string{"go list -e ./...", "node_modules"} {
		if !strings.Contains(helperSource, required) {
			t.Errorf("scripts/ci/go-packages.sh must contain %q", required)
		}
	}

	cmd := exec.Command(helper)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run Go package-list helper: %v\n%s", err, output)
	}
	packages := string(output)
	if strings.Contains(packages, "/web/node_modules/") || strings.Contains(packages, "ralph_go_package_fixture") {
		t.Fatalf("Go package-list helper must exclude web dependency trees, got:\n%s", packages)
	}
	if !strings.Contains(packages, "github.com/liyang/weave/cmd/server") {
		t.Fatalf("Go package-list helper must still include repository packages, got:\n%s", packages)
	}

	buildCmd := exec.Command(helper, "--build")
	buildCmd.Dir = root
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run buildable Go package-list helper: %v\n%s", err, buildOutput)
	}
	buildPackages := string(buildOutput)
	if strings.Contains(buildPackages, "/web/node_modules/") || strings.Contains(buildPackages, "ralph_go_package_fixture") {
		t.Fatalf("buildable Go package-list helper must exclude web dependency trees, got:\n%s", buildPackages)
	}
	if strings.Contains(buildPackages, "github.com/liyang/weave/scripts/ci") {
		t.Fatalf("buildable Go package-list helper must exclude test-only packages, got:\n%s", buildPackages)
	}
	if !strings.Contains(buildPackages, "github.com/liyang/weave/cmd/server") {
		t.Fatalf("buildable Go package-list helper must still include buildable repository packages, got:\n%s", buildPackages)
	}

	makefile := readFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "./scripts/ci/go-packages.sh") {
		t.Error("Makefile Go gates must use scripts/ci/go-packages.sh so local package discovery excludes web dependencies")
	}

	workflow := readFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"))
	if strings.Count(workflow, "./scripts/ci/go-packages.sh") < 3 {
		t.Error("CI build, vet, and test gates must use scripts/ci/go-packages.sh so fresh CI and local gates share package discovery")
	}
	if !strings.Contains(workflow, "./scripts/ci/go-packages.sh --build") {
		t.Error("CI build gate must use scripts/ci/go-packages.sh --build so explicit package arguments do not include test-only packages")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readGoVersion(t *testing.T, path string) (int, int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	m := regexp.MustCompile(`(?m)^go\s+([0-9]+)\.([0-9]+)(?:\.[0-9]+)?\s*$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("go.mod does not declare a parseable Go version")
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse Go major version: %v", err)
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("parse Go minor version: %v", err)
	}
	return major, minor
}

func readGoPatchVersion(t *testing.T, path string) [3]int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	m := regexp.MustCompile(`(?m)^go\s+([0-9]+)\.([0-9]+)\.([0-9]+)\s*$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("go.mod must declare a full major.minor.patch Go version")
	}
	var version [3]int
	for i := range version {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			t.Fatalf("parse Go version component %q: %v", m[i+1], err)
		}
		version[i] = n
	}
	return version
}

func readGolangCILintActionConfig(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	var workflow workflowConfig
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse CI workflow YAML: %v", err)
	}
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "golangci/golangci-lint-action@") {
				version, ok := step.With["version"].(string)
				if !ok || strings.TrimSpace(version) == "" {
					t.Fatalf("job %q golangci-lint action step %q must set with.version", jobName, step.Name)
				}
				return strings.TrimSpace(version), readBoolInput(step.With["only-new-issues"])
			}
		}
	}
	t.Fatal("CI workflow does not contain a golangci/golangci-lint-action step")
	return "", false
}

func readGovulncheckWorkflowCommand(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	var workflow workflowConfig
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse CI workflow YAML: %v", err)
	}
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.EqualFold(strings.TrimSpace(step.Name), "govulncheck") {
				if strings.TrimSpace(step.Run) == "" {
					t.Fatalf("job %q govulncheck step must use a run command", jobName)
				}
				return strings.TrimSpace(step.Run)
			}
		}
	}
	t.Fatal("CI workflow does not contain a govulncheck step")
	return ""
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readBoolInput(v any) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func compareVersions(left, right string) int {
	l := mustParseVersion(left)
	r := mustParseVersion(right)
	return compareVersionParts(l, r)
}

func compareVersionParts(l, r [3]int) int {
	for i := range l {
		if l[i] < r[i] {
			return -1
		}
		if l[i] > r[i] {
			return 1
		}
	}
	return 0
}

func formatVersionParts(v [3]int) string {
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

func mustParseVersion(v string) [3]int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		panic(fmt.Sprintf("version %q must have major.minor.patch format", v))
	}
	var parsed [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			panic(fmt.Sprintf("parse version %q: %v", v, err))
		}
		parsed[i] = n
	}
	return parsed
}
