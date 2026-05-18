package ci_test

import (
	"fmt"
	"os"
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
