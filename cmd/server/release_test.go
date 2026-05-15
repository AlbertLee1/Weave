package main_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	releaseDoc    = "../../docs/RELEASE-vertex-v1.md"
	changelogPath = "../../CHANGELOG.md"
)

func readReleaseDoc(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(releaseDoc)
	if err != nil {
		t.Fatalf("failed to read %s: %v", releaseDoc, err)
	}
	return string(data)
}

func readChangelog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", changelogPath, err)
	}
	return string(data)
}

// TestRelease_VTX125_DocExists asserts the canonical release checklist file
// (docs/RELEASE-vertex-v1.md) lives at the path Ralph PRD VTX-125 specifies.
func TestRelease_VTX125_DocExists(t *testing.T) {
	if _, err := os.Stat(releaseDoc); err != nil {
		t.Fatalf("expected %s to exist: %v", releaseDoc, err)
	}
}

// TestRelease_VTX125_ListsAllStoryCheckboxes verifies the release doc contains a
// checklist with exactly 125 `- [x]` or `- [ ]` entries — one per planned VTX
// story (VTX-001 through VTX-125).
func TestRelease_VTX125_ListsAllStoryCheckboxes(t *testing.T) {
	body := readReleaseDoc(t)
	re := regexp.MustCompile(`(?m)^\s*-\s*\[[ xX]\]\s+VTX-(\d{3})\b`)
	matches := re.FindAllStringSubmatch(body, -1)
	if len(matches) != 125 {
		t.Fatalf("expected exactly 125 VTX story checkboxes, got %d", len(matches))
	}
	seen := make(map[string]bool, 125)
	for _, m := range matches {
		if seen[m[1]] {
			t.Errorf("duplicate VTX-%s entry in release checklist", m[1])
		}
		seen[m[1]] = true
	}
	for i := 1; i <= 125; i++ {
		id := ""
		switch {
		case i < 10:
			id = "00" + itoa(i)
		case i < 100:
			id = "0" + itoa(i)
		default:
			id = itoa(i)
		}
		if !seen[id] {
			t.Errorf("missing VTX-%s in release checklist", id)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestRelease_VTX125_MentionsReleaseTag verifies the doc references the planned
// release tag v1.0.0-vertex (BDD acceptance: tag v1.0.0-vertex).
func TestRelease_VTX125_MentionsReleaseTag(t *testing.T) {
	body := readReleaseDoc(t)
	if !strings.Contains(body, "v1.0.0-vertex") {
		t.Fatalf("expected release doc to reference tag v1.0.0-vertex")
	}
}

// TestRelease_VTX125_KnownIssuesSection enforces the "Known Issues" section
// (BDD: 含已知 issue 清单 — 调研报告附录 C 风险与未解项映射). The section must
// map all 9 technical risks (R1..R9) from docs/PRD-Weave-OSv2-深度复刻-V2.md §7.1
// and explicitly enumerate unshipped story ranges as known limitations.
func TestRelease_VTX125_KnownIssuesSection(t *testing.T) {
	body := readReleaseDoc(t)
	if !strings.Contains(body, "Known Issues") && !strings.Contains(body, "已知") {
		t.Fatalf("release doc missing Known Issues / 已知 issue 清单 section")
	}
	for i := 1; i <= 9; i++ {
		ref := "R" + itoa(i)
		if !regexp.MustCompile(`\b` + ref + `\b`).MatchString(body) {
			t.Errorf("release doc Known Issues section missing risk mapping for %s", ref)
		}
	}
}

// TestRelease_VTX125_ChangelogExists ensures CHANGELOG.md was created at repo
// root with the v1.0.0-vertex release section (BDD: CHANGELOG 更新).
func TestRelease_VTX125_ChangelogExists(t *testing.T) {
	if _, err := os.Stat(changelogPath); err != nil {
		t.Fatalf("expected %s to exist: %v", changelogPath, err)
	}
	body := readChangelog(t)
	if !strings.Contains(body, "v1.0.0-vertex") {
		t.Fatalf("CHANGELOG.md missing v1.0.0-vertex release header")
	}
	// Spot-check that the CHANGELOG calls out at least one feature from each
	// phase that actually shipped — Scenario Read Overlay (Phase 1) and
	// TimescaleDB time series (Phase 4) are the load-bearing milestones.
	for _, marker := range []string{"Scenario", "TimescaleDB", "Vertex"} {
		if !strings.Contains(body, marker) {
			t.Errorf("CHANGELOG.md missing expected marker %q", marker)
		}
	}
}
