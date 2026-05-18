package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleProfile is a minimal `go test -coverprofile` output covering three
// packages: pkg/apierror (1/2 statements covered → 50%), pkg/pagination
// (3/3 → 100%), pkg/oms (0/4 → 0%). It exercises every branch the parser
// cares about: covered (count>0), uncovered (count=0), multi-file
// aggregation, and the leading "mode:" header.
const sampleProfile = `mode: atomic
github.com/liyang/weave/pkg/apierror/errors.go:10.40,15.2 1 7
github.com/liyang/weave/pkg/apierror/errors.go:18.40,23.2 1 0
github.com/liyang/weave/pkg/pagination/cursor.go:5.20,10.2 2 4
github.com/liyang/weave/pkg/pagination/parse.go:5.20,10.2 1 1
github.com/liyang/weave/pkg/oms/repo.go:5.20,10.2 2 0
github.com/liyang/weave/pkg/oms/repo.go:12.20,15.2 2 0
`

func writeTempFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "input")
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestParseProfile_AggregatesByPackage(t *testing.T) {
	cov, err := parseProfile(strings.NewReader(sampleProfile))
	if err != nil {
		t.Fatalf("parseProfile: %v", err)
	}

	got := map[string]float64{}
	for pkg, pc := range cov {
		got[pkg] = round1(pc.Percent())
	}
	want := map[string]float64{
		"github.com/liyang/weave/pkg/apierror":   50.0,
		"github.com/liyang/weave/pkg/pagination": 100.0,
		"github.com/liyang/weave/pkg/oms":        0.0,
	}
	for pkg, w := range want {
		if g := got[pkg]; g != w {
			t.Errorf("pkg %s: got %v, want %v", pkg, g, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected packages: got %d, want %d (%v)", len(got), len(want), got)
	}
}

func TestParseProfile_SkipsMalformedLines(t *testing.T) {
	in := "mode: set\nnot-a-cover-line\n# comment\ngithub.com/x/y/file.go:1.1,2.1 1 1\n"
	cov, err := parseProfile(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parseProfile: %v", err)
	}
	if _, ok := cov["github.com/x/y"]; !ok {
		t.Fatalf("expected pkg github.com/x/y in %v", cov)
	}
}

// VTX-122: parseProfile must support an exclude-files filter so packages
// that intentionally split unit-testable code from integration-only PG
// implementations can keep one package floor while ignoring the PG files.
// The filter matches by path suffix (so "pkg/scenarios/pg_repo.go" lands
// the right line regardless of the module-prefix in the cover output).
func TestParseProfile_ExcludeFilesDropsMatchingLines(t *testing.T) {
	in := `mode: atomic
github.com/liyang/weave/pkg/scenarios/archive.go:1.1,5.2 4 4
github.com/liyang/weave/pkg/scenarios/pg_repo.go:1.1,10.2 8 0
github.com/liyang/weave/pkg/scenarios/pg_repo.go:12.1,20.2 6 0
`
	cov, err := parseProfileWithExcludes(strings.NewReader(in), []string{"pkg/scenarios/pg_repo.go"})
	if err != nil {
		t.Fatalf("parseProfileWithExcludes: %v", err)
	}
	pc, ok := cov["github.com/liyang/weave/pkg/scenarios"]
	if !ok {
		t.Fatalf("expected pkg scenarios in %v", cov)
	}
	if pc.Total != 4 || pc.Covered != 4 {
		t.Fatalf("expected 4/4 statements after excluding pg_repo.go, got %d/%d", pc.Covered, pc.Total)
	}
	if pct := round1(pc.Percent()); pct != 100.0 {
		t.Errorf("expected 100%% after exclusion, got %v", pct)
	}
}

// Suffix matching means a short exclude like "pg_repo.go" applies across
// every package that shipped a file with that name — which is exactly the
// shape the PG-impl files take across pkg/scenarios and pkg/vertex/graphsvc.
func TestParseProfile_ExcludeFilesSuffixMatchesAcrossPackages(t *testing.T) {
	in := `mode: atomic
github.com/liyang/weave/pkg/scenarios/pg_repo.go:1.1,5.2 4 0
github.com/liyang/weave/pkg/vertex/graphsvc/pg_repo.go:1.1,5.2 4 0
github.com/liyang/weave/pkg/vertex/graphsvc/mem_repo.go:1.1,5.2 4 4
`
	cov, err := parseProfileWithExcludes(strings.NewReader(in), []string{"pg_repo.go"})
	if err != nil {
		t.Fatalf("parseProfileWithExcludes: %v", err)
	}
	if _, ok := cov["github.com/liyang/weave/pkg/scenarios"]; ok {
		t.Errorf("scenarios should have no remaining lines after excluding pg_repo.go")
	}
	pc := cov["github.com/liyang/weave/pkg/vertex/graphsvc"]
	if pc == nil || pc.Total != 4 || pc.Covered != 4 {
		t.Fatalf("graphsvc: expected 4/4 after exclusion, got %+v", pc)
	}
}

// An empty exclude list must behave identically to parseProfile — the
// filter is opt-in and must not silently drop lines for callers that
// don't configure it.
func TestParseProfile_EmptyExcludesIsIdentity(t *testing.T) {
	cov, err := parseProfileWithExcludes(strings.NewReader(sampleProfile), nil)
	if err != nil {
		t.Fatalf("parseProfileWithExcludes: %v", err)
	}
	base, err := parseProfile(strings.NewReader(sampleProfile))
	if err != nil {
		t.Fatalf("parseProfile: %v", err)
	}
	if len(cov) != len(base) {
		t.Fatalf("len mismatch: got %d, want %d", len(cov), len(base))
	}
	for k, v := range base {
		if got := cov[k]; got == nil || got.Covered != v.Covered || got.Total != v.Total {
			t.Errorf("pkg %s: got %+v, want %+v", k, got, v)
		}
	}
}

func TestLoadConfig_ExcludeFilesParsed(t *testing.T) {
	body := `{"floor":{"github.com/liyang/weave/pkg/scenarios":80.0},"excludeFiles":["pg_repo.go","pg_store.go"]}`
	path := writeTempFile(t, body)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.ExcludeFiles) != 2 || cfg.ExcludeFiles[0] != "pg_repo.go" {
		t.Errorf("expected ExcludeFiles=[pg_repo.go pg_store.go], got %v", cfg.ExcludeFiles)
	}
}

func TestLoadConfig_CICoverageFloorsTrackCurrentBroadPackageReality(t *testing.T) {
	cfg, err := loadConfig(filepath.Join("..", "..", "coverage", "thresholds.json"))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	cases := []struct {
		pkg         string
		ciMeasured  float64
		maxHeadroom float64
	}{
		{
			pkg:         "github.com/liyang/weave/pkg/oms",
			ciMeasured:  47.5,
			maxHeadroom: 5.0,
		},
		{
			pkg:         "github.com/liyang/weave/pkg/oss/objectset",
			ciMeasured:  68.6,
			maxHeadroom: 5.0,
		},
	}

	for _, tt := range cases {
		floor, ok := cfg.Floor[tt.pkg]
		if !ok {
			t.Fatalf("%s floor missing from coverage/thresholds.json", tt.pkg)
		}
		if floor <= 0 {
			t.Fatalf("%s floor = %.1f, want positive ratchet floor", tt.pkg, floor)
		}
		if floor > tt.ciMeasured {
			t.Fatalf("%s floor = %.1f, above current CI-measured %.1f", tt.pkg, floor, tt.ciMeasured)
		}
		if floor < tt.ciMeasured-tt.maxHeadroom {
			t.Fatalf("%s floor = %.1f, more than %.1fpp below current CI-measured %.1f", tt.pkg, floor, tt.maxHeadroom, tt.ciMeasured)
		}
	}
}

func TestEvaluate_FloorFailureFlagged(t *testing.T) {
	cov := CoverageMap{
		"github.com/liyang/weave/pkg/apierror":   {Covered: 1, Total: 10}, // 10% < 90%
		"github.com/liyang/weave/pkg/pagination": {Covered: 9, Total: 10}, // 90% ≥ 85%
		"github.com/liyang/weave/pkg/oms":        {Covered: 9, Total: 10}, // 90% ≥ 80%
	}
	cfg := Config{
		DefaultMaxDrop: 2.0,
		Floor: map[string]float64{
			"github.com/liyang/weave/pkg/apierror":   90.0,
			"github.com/liyang/weave/pkg/pagination": 85.0,
			"github.com/liyang/weave/pkg/oms":        80.0,
		},
	}

	report := evaluate(cov, nil, cfg)
	if !contains(report.FailedFloor, "github.com/liyang/weave/pkg/apierror") {
		t.Errorf("expected apierror in FailedFloor, got %v", report.FailedFloor)
	}
	if contains(report.FailedFloor, "github.com/liyang/weave/pkg/pagination") {
		t.Errorf("pagination should not be in FailedFloor")
	}
	if report.OK {
		t.Errorf("OK should be false when any floor fails")
	}
}

func TestEvaluate_BaselineDropFailureFlagged(t *testing.T) {
	cov := CoverageMap{
		"github.com/liyang/weave/pkg/apierror":   {Covered: 80, Total: 100}, // 80%
		"github.com/liyang/weave/pkg/pagination": {Covered: 90, Total: 100}, // 90%
	}
	baseline := map[string]float64{
		"github.com/liyang/weave/pkg/apierror":   95.0, // drop 15% — fails
		"github.com/liyang/weave/pkg/pagination": 91.0, // drop 1% — within 2% tolerance
	}
	cfg := Config{DefaultMaxDrop: 2.0}

	report := evaluate(cov, baseline, cfg)
	if !contains(report.FailedDrop, "github.com/liyang/weave/pkg/apierror") {
		t.Errorf("expected apierror in FailedDrop, got %v", report.FailedDrop)
	}
	if contains(report.FailedDrop, "github.com/liyang/weave/pkg/pagination") {
		t.Errorf("pagination drop is within tolerance, should not fail")
	}
	if report.OK {
		t.Errorf("OK should be false when any drop fails")
	}
}

func TestEvaluate_AllPass_ReportsOK(t *testing.T) {
	cov := CoverageMap{
		"github.com/liyang/weave/pkg/apierror": {Covered: 95, Total: 100},
	}
	baseline := map[string]float64{
		"github.com/liyang/weave/pkg/apierror": 94.0,
	}
	cfg := Config{
		DefaultMaxDrop: 2.0,
		Floor:          map[string]float64{"github.com/liyang/weave/pkg/apierror": 90.0},
	}
	report := evaluate(cov, baseline, cfg)
	if !report.OK {
		t.Errorf("expected OK=true, got report=%+v", report)
	}
	if len(report.FailedFloor) != 0 || len(report.FailedDrop) != 0 {
		t.Errorf("expected zero failures, got floor=%v drop=%v", report.FailedFloor, report.FailedDrop)
	}
}

func TestRenderMarkdown_LinesContainAllPackages(t *testing.T) {
	report := Report{
		GeneratedAt: "2026-05-13T00:00:00Z",
		Packages: map[string]PackageReport{
			"github.com/liyang/weave/pkg/apierror": {
				Percent:  80.0,
				Baseline: 95.0,
				Delta:    -15.0,
				Floor:    90.0,
				Pass:     false,
				Note:     "below floor; dropped > 2%",
			},
			"github.com/liyang/weave/pkg/pagination": {
				Percent:  91.0,
				Baseline: 90.0,
				Delta:    1.0,
				Floor:    85.0,
				Pass:     true,
			},
		},
		FailedFloor: []string{"github.com/liyang/weave/pkg/apierror"},
		FailedDrop:  []string{"github.com/liyang/weave/pkg/apierror"},
		OK:          false,
	}
	md := renderMarkdown(report)
	for _, want := range []string{
		"Coverage gate", "FAIL",
		"pkg/apierror", "80.0", "90.0", "below floor",
		"pkg/pagination", "91.0",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestRenderMarkdown_OKWhenNoFailures(t *testing.T) {
	report := Report{
		GeneratedAt: "2026-05-13T00:00:00Z",
		Packages: map[string]PackageReport{
			"github.com/liyang/weave/pkg/apierror": {Percent: 95.0, Pass: true},
		},
		OK: true,
	}
	md := renderMarkdown(report)
	if !strings.Contains(md, "PASS") {
		t.Errorf("expected PASS verdict, got:\n%s", md)
	}
}

func TestUpdateBaseline_WritesCurrentMeasurements(t *testing.T) {
	cov := CoverageMap{
		"github.com/liyang/weave/pkg/apierror":   {Covered: 95, Total: 100},
		"github.com/liyang/weave/pkg/pagination": {Covered: 90, Total: 100},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := writeBaseline(path, cov); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var got Baseline
	if err := json.Unmarshal(bs, &got); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	if got.Coverage["github.com/liyang/weave/pkg/apierror"] != 95.0 {
		t.Errorf("apierror baseline: got %v, want 95.0", got.Coverage["github.com/liyang/weave/pkg/apierror"])
	}
	if got.Coverage["github.com/liyang/weave/pkg/pagination"] != 90.0 {
		t.Errorf("pagination baseline: got %v, want 90.0", got.Coverage["github.com/liyang/weave/pkg/pagination"])
	}
	if got.GeneratedAt == "" {
		t.Errorf("expected GeneratedAt to be set")
	}
}

func TestLoadConfig_DefaultMaxDropFallback(t *testing.T) {
	body := `{"floor":{"github.com/liyang/weave/pkg/apierror":90.0}}`
	path := writeTempFile(t, body)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DefaultMaxDrop != defaultMaxDrop {
		t.Errorf("expected DefaultMaxDrop=%v fallback, got %v", defaultMaxDrop, cfg.DefaultMaxDrop)
	}
	if cfg.Floor["github.com/liyang/weave/pkg/apierror"] != 90.0 {
		t.Errorf("floor not loaded: %v", cfg.Floor)
	}
}

func TestLoadBaseline_MissingFileTreatedAsEmpty(t *testing.T) {
	bl, err := loadBaseline(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("loadBaseline should treat missing file as empty, got error: %v", err)
	}
	if len(bl) != 0 {
		t.Errorf("expected empty baseline, got %v", bl)
	}
}

func contains(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}
