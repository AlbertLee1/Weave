// Command covercheck parses a Go cover profile, aggregates statement-level
// coverage by package, and gates the result against two checks:
//
//   1. Floor thresholds — each package listed in the thresholds config must
//      meet its declared minimum (oms 80% / oss/objectset 80% / actions 75%
//      / pagination 85% / apierror 90%).
//   2. Regression — when a baseline file is supplied, no package may drop
//      more than `defaultMaxDrop` percentage points (default 2.0%) versus
//      the baseline.
//
// Both signals are merged into a Markdown report (stdout + optional -md
// file) suitable for posting as a PR comment. The exit status is 0 when
// every gate passes, 1 when any package fails a gate, and 2 on usage /
// parse errors. Designed for the US-056 CI coverage gate.
//
// Usage:
//
//	go test ./pkg/... -coverprofile=coverage.out -covermode=atomic
//	go run ./cmd/covercheck \
//	  -profile coverage.out \
//	  -thresholds coverage/thresholds.json \
//	  -baseline coverage/baseline.json \
//	  -md coverage/report.md
//
// Update mode (recompute baseline from the current run; commit the result):
//
//	go test ./pkg/... -coverprofile=coverage.out -covermode=atomic
//	go run ./cmd/covercheck -profile coverage.out -update coverage/baseline.json
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultMaxDrop = 2.0

// CoverageMap aggregates statement counts per package import path.
type CoverageMap map[string]*PackageCoverage

// PackageCoverage tracks covered vs. total statements for one Go package.
type PackageCoverage struct {
	Covered int
	Total   int
}

// Percent returns the statement-level coverage percentage for the package.
// Zero-statement packages report 0% (caller should treat as "no data").
func (p *PackageCoverage) Percent() float64 {
	if p.Total == 0 {
		return 0
	}
	return float64(p.Covered) * 100.0 / float64(p.Total)
}

// Config is the on-disk thresholds file shape. DefaultMaxDrop is the
// percentage-point ceiling for baseline regressions; Floor lists per-package
// minimum coverage. ExcludeFiles is a list of source-file path *suffixes*
// (matched with strings.HasSuffix against the cover-line file path) whose
// lines are dropped before aggregating per-package totals — used to keep
// integration-only PG implementation files (`pg_*.go`) from dragging the
// unit-test floor down on packages that mix pure and PG-backed code.
type Config struct {
	DefaultMaxDrop float64            `json:"defaultMaxDrop"`
	Floor          map[string]float64 `json:"floor"`
	ExcludeFiles   []string           `json:"excludeFiles,omitempty"`
}

// Baseline is the on-disk baseline file shape — a snapshot of prior
// per-package percentages. Persist with -update; read with -baseline.
type Baseline struct {
	GeneratedAt string             `json:"generatedAt"`
	Coverage    map[string]float64 `json:"coverage"`
}

// PackageReport is the per-package gate verdict surfaced to humans + CI.
type PackageReport struct {
	Percent  float64 `json:"percent"`
	Baseline float64 `json:"baseline,omitempty"`
	Delta    float64 `json:"delta,omitempty"`
	Floor    float64 `json:"floor,omitempty"`
	Pass     bool    `json:"pass"`
	Note     string  `json:"note,omitempty"`
}

// Report is the aggregated gate output: per-package verdicts plus
// summary slices of failing packages and an overall OK flag.
type Report struct {
	GeneratedAt string                   `json:"generatedAt"`
	Packages    map[string]PackageReport `json:"packages"`
	FailedFloor []string                 `json:"failedFloor,omitempty"`
	FailedDrop  []string                 `json:"failedDrop,omitempty"`
	OK          bool                     `json:"ok"`
}

// coverLine matches one block in a `go test -coverprofile` output:
//
//	<package-path>/<file>.go:<startLine>.<startCol>,<endLine>.<endCol> <numStmt> <count>
//
// Header lines (`mode: set|count|atomic`) and stray comments are tolerated.
var coverLine = regexp.MustCompile(`^([^\s:]+):\d+\.\d+,\d+\.\d+\s+(\d+)\s+(\d+)$`)

func main() {
	profilePath := flag.String("profile", "coverage.out", "path to the Go cover profile")
	thresholdsPath := flag.String("thresholds", "coverage/thresholds.json", "path to the thresholds JSON config")
	baselinePath := flag.String("baseline", "coverage/baseline.json", "path to the baseline JSON (missing = skip drop check)")
	updatePath := flag.String("update", "", "if non-empty, write current measurements as the new baseline at this path and skip the gate")
	mdPath := flag.String("md", "", "if non-empty, write the markdown report to this path (in addition to stdout)")
	outputPath := flag.String("output", "", "if non-empty, write the report JSON to this path")
	flag.Parse()

	f, err := os.Open(*profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "covercheck: open profile: %v\n", err)
		os.Exit(2)
	}
	defer f.Close()

	cfg, err := loadConfig(*thresholdsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "covercheck: load thresholds: %v\n", err)
		os.Exit(2)
	}

	cov, err := parseProfileWithExcludes(f, cfg.ExcludeFiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "covercheck: parse profile: %v\n", err)
		os.Exit(2)
	}
	if len(cov) == 0 {
		fmt.Fprintln(os.Stderr, "covercheck: no coverage blocks parsed (empty profile?)")
		os.Exit(2)
	}

	if *updatePath != "" {
		if err := writeBaseline(*updatePath, cov); err != nil {
			fmt.Fprintf(os.Stderr, "covercheck: write baseline: %v\n", err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stdout, "baseline updated: %s (%d packages)\n", *updatePath, len(cov))
		return
	}
	baseline, err := loadBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "covercheck: load baseline: %v\n", err)
		os.Exit(2)
	}

	report := evaluate(cov, baseline, cfg)
	md := renderMarkdown(report)
	fmt.Fprint(os.Stdout, md)

	if *mdPath != "" {
		if err := os.WriteFile(*mdPath, []byte(md), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "covercheck: write markdown: %v\n", err)
			os.Exit(2)
		}
	}
	if *outputPath != "" {
		if err := writeJSON(*outputPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "covercheck: write output: %v\n", err)
			os.Exit(2)
		}
	}

	if !report.OK {
		fmt.Fprintf(os.Stderr, "covercheck: coverage gate failed (floor=%v drop=%v)\n",
			report.FailedFloor, report.FailedDrop)
		os.Exit(1)
	}
}

// parseProfile aggregates statement counts per Go package. The package key
// is derived by stripping the trailing /<filename>.go from the cover-line
// path, which matches the import path because go-test cover profiles emit
// fully-qualified paths.
func parseProfile(r io.Reader) (CoverageMap, error) {
	return parseProfileWithExcludes(r, nil)
}

// parseProfileWithExcludes is parseProfile with an opt-in suffix filter.
// Any cover-line whose source path ends with one of the supplied excludes
// is dropped before aggregation. Empty/nil excludes is a no-op (identical
// to parseProfile) so existing callers keep their semantics.
func parseProfileWithExcludes(r io.Reader, excludes []string) (CoverageMap, error) {
	out := CoverageMap{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") || strings.HasPrefix(line, "#") {
			continue
		}
		m := coverLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		srcPath := m[1]
		if isExcluded(srcPath, excludes) {
			continue
		}
		pkg := path.Dir(srcPath)
		if pkg == "" || pkg == "." {
			continue
		}
		stmts, err := strconv.Atoi(m[2])
		if err != nil {
			return nil, fmt.Errorf("parse stmt count %q: %w", line, err)
		}
		count, err := strconv.Atoi(m[3])
		if err != nil {
			return nil, fmt.Errorf("parse hit count %q: %w", line, err)
		}
		entry := out[pkg]
		if entry == nil {
			entry = &PackageCoverage{}
			out[pkg] = entry
		}
		entry.Total += stmts
		if count > 0 {
			entry.Covered += stmts
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func isExcluded(srcPath string, excludes []string) bool {
	for _, e := range excludes {
		if e == "" {
			continue
		}
		if strings.HasSuffix(srcPath, e) {
			return true
		}
	}
	return false
}

// evaluate walks every package and emits a per-package verdict + summary
// slices of failures. A package fails the floor gate when its percentage
// is strictly below the configured floor; it fails the drop gate when
// percent < baseline - cfg.DefaultMaxDrop. Both gates are independent and
// each contribute to a separate failed slice; either one trips OK=false.
func evaluate(cov CoverageMap, baseline map[string]float64, cfg Config) Report {
	maxDrop := cfg.DefaultMaxDrop
	if maxDrop <= 0 {
		maxDrop = defaultMaxDrop
	}
	report := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Packages:    map[string]PackageReport{},
		OK:          true,
	}
	for pkg, pc := range cov {
		pct := round1(pc.Percent())
		entry := PackageReport{Percent: pct, Pass: true}
		notes := []string{}

		if floor, ok := cfg.Floor[pkg]; ok {
			entry.Floor = floor
			if pct < floor {
				entry.Pass = false
				report.FailedFloor = append(report.FailedFloor, pkg)
				notes = append(notes, fmt.Sprintf("below floor (%.1f%% < %.1f%%)", pct, floor))
			}
		}
		if base, ok := baseline[pkg]; ok && base > 0 {
			entry.Baseline = base
			entry.Delta = round1(pct - base)
			if pct < base-maxDrop {
				entry.Pass = false
				report.FailedDrop = append(report.FailedDrop, pkg)
				notes = append(notes, fmt.Sprintf("dropped %.1fpp (> %.1fpp)", base-pct, maxDrop))
			}
		}
		if len(notes) > 0 {
			entry.Note = strings.Join(notes, "; ")
		}
		report.Packages[pkg] = entry
	}
	sort.Strings(report.FailedFloor)
	sort.Strings(report.FailedDrop)
	if len(report.FailedFloor) > 0 || len(report.FailedDrop) > 0 {
		report.OK = false
	}
	return report
}

// renderMarkdown emits the PR-comment-ready report. Failed packages float
// to the top, then a full per-package table sorted by import path for
// deterministic diffs.
func renderMarkdown(r Report) string {
	var b strings.Builder
	verdict := "PASS"
	if !r.OK {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "## Coverage gate: %s\n\n", verdict)
	fmt.Fprintf(&b, "_Generated: %s_\n\n", r.GeneratedAt)

	if len(r.FailedFloor) > 0 {
		fmt.Fprintln(&b, "### Failing the floor")
		for _, pkg := range r.FailedFloor {
			entry := r.Packages[pkg]
			fmt.Fprintf(&b, "- `%s`: %.1f%% (floor %.1f%%) — %s\n", pkg, entry.Percent, entry.Floor, entry.Note)
		}
		b.WriteString("\n")
	}
	if len(r.FailedDrop) > 0 {
		fmt.Fprintln(&b, "### Coverage regressions")
		for _, pkg := range r.FailedDrop {
			entry := r.Packages[pkg]
			fmt.Fprintf(&b, "- `%s`: %.1f%% (baseline %.1f%%, delta %+.1fpp) — %s\n",
				pkg, entry.Percent, entry.Baseline, entry.Delta, entry.Note)
		}
		b.WriteString("\n")
	}

	fmt.Fprintln(&b, "### Per-package coverage")
	fmt.Fprintln(&b, "| Package | Coverage | Baseline | Δ | Floor | Verdict |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|:---:|")

	pkgs := make([]string, 0, len(r.Packages))
	for pkg := range r.Packages {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		entry := r.Packages[pkg]
		baseStr, deltaStr, floorStr := "–", "–", "–"
		if entry.Baseline > 0 {
			baseStr = fmt.Sprintf("%.1f%%", entry.Baseline)
			deltaStr = fmt.Sprintf("%+.1f", entry.Delta)
		}
		if entry.Floor > 0 {
			floorStr = fmt.Sprintf("%.1f%%", entry.Floor)
		}
		v := "✓"
		if !entry.Pass {
			v = "✗"
		}
		fmt.Fprintf(&b, "| `%s` | %.1f%% | %s | %s | %s | %s |\n",
			pkg, entry.Percent, baseStr, deltaStr, floorStr, v)
	}
	return b.String()
}

func loadConfig(p string) (Config, error) {
	var cfg Config
	bs, err := os.ReadFile(p)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(bs, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", p, err)
	}
	if cfg.DefaultMaxDrop <= 0 {
		cfg.DefaultMaxDrop = defaultMaxDrop
	}
	if cfg.Floor == nil {
		cfg.Floor = map[string]float64{}
	}
	return cfg, nil
}

// loadBaseline returns an empty map when the file is missing — a missing
// baseline means "first run / no comparison available" and should not be a
// hard error. Parse failures still surface so callers notice corrupted
// JSON.
func loadBaseline(p string) (map[string]float64, error) {
	bs, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]float64{}, nil
		}
		return nil, err
	}
	if len(bs) == 0 {
		return map[string]float64{}, nil
	}
	var bl Baseline
	if err := json.Unmarshal(bs, &bl); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if bl.Coverage == nil {
		return map[string]float64{}, nil
	}
	return bl.Coverage, nil
}

func writeBaseline(p string, cov CoverageMap) error {
	out := Baseline{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Coverage:    map[string]float64{},
	}
	for pkg, pc := range cov {
		out.Coverage[pkg] = round1(pc.Percent())
	}
	return writeJSON(p, out)
}

func writeJSON(p string, v any) error {
	bs, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	bs = append(bs, '\n')
	return os.WriteFile(p, bs, 0o644)
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10.0
}
