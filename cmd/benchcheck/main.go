// Command benchcheck parses Go benchmark output, compares it against a
// baseline JSON file, and exits non-zero when any benchmark regresses past
// the configured threshold (default 20%). Designed for the US-441
// performance regression CI gate.
//
// Usage:
//
//	go test -bench=. -run=^$ -benchtime=100ms ./bench/... \
//	  | go run ./cmd/benchcheck -baseline bench/baseline.json -output bench/results.json
//
// Update mode (recompute baseline from the current run):
//
//	go test -bench=. -run=^$ -benchtime=200ms ./bench/... \
//	  | go run ./cmd/benchcheck -update bench/baseline.json
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// BaselineFile is the JSON shape of bench/baseline.json. The threshold
// field is the maximum allowed ratio of measured/baseline before a bench
// is reported as a regression — 1.20 means "fail when measured ns/op is
// more than 120% of baseline".
type BaselineFile struct {
	GeneratedAt string                     `json:"generatedAt"`
	Threshold   float64                    `json:"thresholdRatio"`
	Benchmarks  map[string]BaselineEntry   `json:"benchmarks"`
}

type BaselineEntry struct {
	NsPerOp float64 `json:"nsPerOp"`
	Note    string  `json:"note,omitempty"`
}

// ResultFile is the JSON shape benchcheck writes to -output. It carries
// per-benchmark measurements plus per-row pass/fail verdicts so a CI
// dashboard can display the table directly.
type ResultFile struct {
	GeneratedAt    string                  `json:"generatedAt"`
	ThresholdRatio float64                 `json:"thresholdRatio"`
	Benchmarks     map[string]ResultEntry  `json:"benchmarks"`
	Regressions    []string                `json:"regressions"`
	MissingBench   []string                `json:"missingBenchmarks,omitempty"`
}

type ResultEntry struct {
	NsPerOp         float64 `json:"nsPerOp"`
	Iterations      int64   `json:"iterations"`
	BaselineNsPerOp float64 `json:"baselineNsPerOp,omitempty"`
	Ratio           float64 `json:"ratio,omitempty"`
	Pass            bool    `json:"pass"`
}

// benchLine matches the canonical `go test -bench` text output:
//   BenchmarkLoad-16   	     142	   1704387 ns/op
// The trailing fields (B/op, allocs/op) are tolerated but not parsed.
var benchLine = regexp.MustCompile(`^(Benchmark[\w/.]+?)(?:-\d+)?\s+(\d+)\s+([\d.]+)\s+ns/op`)

func main() {
	baselinePath := flag.String("baseline", "bench/baseline.json", "path to baseline JSON")
	outputPath := flag.String("output", "", "optional path for the results JSON; empty = no file written")
	updatePath := flag.String("update", "", "if non-empty, write current measurements as the new baseline at this path and skip the regression check")
	thresholdOverride := flag.Float64("threshold", 0, "override the baseline threshold ratio (0 = use file value)")
	flag.Parse()

	measured, err := parseBench(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchcheck: parse: %v\n", err)
		os.Exit(2)
	}
	if len(measured) == 0 {
		fmt.Fprintln(os.Stderr, "benchcheck: no benchmark lines parsed from stdin")
		os.Exit(2)
	}

	if *updatePath != "" {
		if err := writeBaseline(*updatePath, measured); err != nil {
			fmt.Fprintf(os.Stderr, "benchcheck: write baseline: %v\n", err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stdout, "baseline updated: %s (%d benchmarks)\n", *updatePath, len(measured))
		return
	}

	baseline, err := loadBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchcheck: load baseline: %v\n", err)
		os.Exit(2)
	}
	threshold := baseline.Threshold
	if *thresholdOverride > 0 {
		threshold = *thresholdOverride
	}
	if threshold <= 0 {
		threshold = 1.20
	}

	results, regressions, missing := evaluate(measured, baseline.Benchmarks, threshold)

	out := ResultFile{
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		ThresholdRatio: threshold,
		Benchmarks:     results,
		Regressions:    regressions,
		MissingBench:   missing,
	}
	if *outputPath != "" {
		if err := writeJSON(*outputPath, out); err != nil {
			fmt.Fprintf(os.Stderr, "benchcheck: write output: %v\n", err)
			os.Exit(2)
		}
	}
	report(os.Stdout, out)

	if len(regressions) > 0 {
		fmt.Fprintf(os.Stderr, "benchcheck: %d regression(s) detected (threshold=%.2fx)\n", len(regressions), threshold)
		os.Exit(1)
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "benchcheck: %d benchmark(s) declared in baseline but missing from measurements: %v\n", len(missing), missing)
		os.Exit(1)
	}
}

// parseBench scans benchmark output and returns one ResultEntry per
// benchmark name. When `-count=N>1` was passed and the same name appears
// multiple times, the SLOWEST sample wins — the baseline / regression gate
// reasons in worst-case terms so a lucky-fast measurement on the recording
// run can't lock the gate into a too-tight bound.
func parseBench(r io.Reader) (map[string]ResultEntry, error) {
	out := map[string]ResultEntry{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		match := benchLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		name := match[1]
		iter, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse iterations on line %q: %w", line, err)
		}
		ns, err := strconv.ParseFloat(match[3], 64)
		if err != nil {
			return nil, fmt.Errorf("parse ns/op on line %q: %w", line, err)
		}
		if existing, ok := out[name]; ok && existing.NsPerOp >= ns {
			continue // keep the slowest sample
		}
		out[name] = ResultEntry{
			NsPerOp:    ns,
			Iterations: iter,
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func loadBaseline(path string) (BaselineFile, error) {
	var bf BaselineFile
	bs, err := os.ReadFile(path)
	if err != nil {
		return bf, err
	}
	if err := json.Unmarshal(bs, &bf); err != nil {
		return bf, fmt.Errorf("parse %s: %w", path, err)
	}
	return bf, nil
}

func evaluate(measured map[string]ResultEntry, baseline map[string]BaselineEntry, threshold float64) (map[string]ResultEntry, []string, []string) {
	results := make(map[string]ResultEntry, len(measured))
	for name, m := range measured {
		entry := m
		if be, ok := baseline[name]; ok && be.NsPerOp > 0 {
			ratio := m.NsPerOp / be.NsPerOp
			entry.BaselineNsPerOp = be.NsPerOp
			entry.Ratio = ratio
			entry.Pass = ratio <= threshold
		} else {
			// No baseline → cannot fail; treat as pass but record the ratio as 0.
			entry.Pass = true
		}
		results[name] = entry
	}

	regressions := []string{}
	for name, e := range results {
		if !e.Pass {
			regressions = append(regressions, name)
		}
	}
	sort.Strings(regressions)

	missing := []string{}
	for name := range baseline {
		if _, ok := measured[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return results, regressions, missing
}

func writeBaseline(path string, measured map[string]ResultEntry) error {
	out := BaselineFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Threshold:   1.20,
		Benchmarks:  map[string]BaselineEntry{},
	}
	for name, m := range measured {
		out.Benchmarks[name] = BaselineEntry{NsPerOp: m.NsPerOp}
	}
	return writeJSON(path, out)
}

func writeJSON(path string, v interface{}) error {
	bs, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	bs = append(bs, '\n')
	return os.WriteFile(path, bs, 0o644)
}

func report(w io.Writer, out ResultFile) {
	names := make([]string, 0, len(out.Benchmarks))
	for n := range out.Benchmarks {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "benchcheck threshold=%.2fx\n", out.ThresholdRatio)
	fmt.Fprintf(w, "%-40s %12s %12s %8s %s\n", "benchmark", "ns/op", "baseline", "ratio", "verdict")
	for _, n := range names {
		e := out.Benchmarks[n]
		ratio := "n/a"
		if e.BaselineNsPerOp > 0 {
			ratio = fmt.Sprintf("%.2fx", e.Ratio)
		}
		verdict := "pass"
		if !e.Pass {
			verdict = "FAIL"
		}
		fmt.Fprintf(w, "%-40s %12.0f %12.0f %8s %s\n", n, e.NsPerOp, e.BaselineNsPerOp, ratio, verdict)
	}
	if len(out.MissingBench) > 0 {
		fmt.Fprintf(w, "missing-from-run: %v\n", out.MissingBench)
	}
}
