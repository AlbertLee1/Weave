package dashboards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// prometheusRuleFile is the shape that Prometheus's rule loader accepts: a
// top-level "groups" array, each group carrying a name and a list of rules
// (either recording rules — "record" — or alerting rules — "alert").
// https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/
type prometheusRuleFile struct {
	Groups []struct {
		Name     string `yaml:"name"`
		Interval string `yaml:"interval"`
		Rules    []struct {
			Alert       string            `yaml:"alert"`
			Record      string            `yaml:"record"`
			Expr        string            `yaml:"expr"`
			For         string            `yaml:"for"`
			Labels      map[string]string `yaml:"labels"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

// TestWeaveAlerts_FileExistsAndParses verifies the alerts YAML file exists,
// parses as a valid Prometheus rule file, and carries at least one rule
// group with a non-empty name.
func TestWeaveAlerts_FileExistsAndParses(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "alerts", "weave.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var f prometheusRuleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(f.Groups) == 0 {
		t.Fatalf("alerts file has no groups")
	}
	for _, g := range f.Groups {
		if g.Name == "" {
			t.Errorf("group has empty name")
		}
		if len(g.Rules) == 0 {
			t.Errorf("group %q has no rules", g.Name)
		}
	}
}

// TestWeaveAlerts_RequiredAlertsPresent verifies the five PRD-mandated
// alerts (HighErrorRate, HighLatency, NatsLag, DBConnExhaust, DiskFull) are
// all defined with non-empty expressions, for-durations, severity labels,
// and summary/description annotations. The matcher uses the "alert" name
// case-sensitively so a rename would require an explicit test update.
func TestWeaveAlerts_RequiredAlertsPresent(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "alerts", "weave.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var f prometheusRuleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required := []string{
		"HighErrorRate",
		"HighLatency",
		"NatsLag",
		"DBConnExhaust",
		"DiskFull",
	}
	found := make(map[string]bool, len(required))
	for _, g := range f.Groups {
		for _, r := range g.Rules {
			if r.Alert == "" {
				continue
			}
			found[r.Alert] = true
			if r.Expr == "" {
				t.Errorf("alert %q has empty expr", r.Alert)
			}
			if r.For == "" {
				t.Errorf("alert %q has no 'for' duration", r.Alert)
			}
			if r.Labels["severity"] == "" {
				t.Errorf("alert %q missing severity label", r.Alert)
			}
			if r.Annotations["summary"] == "" {
				t.Errorf("alert %q missing summary annotation", r.Alert)
			}
			if r.Annotations["description"] == "" {
				t.Errorf("alert %q missing description annotation", r.Alert)
			}
		}
	}
	for _, name := range required {
		if !found[name] {
			t.Errorf("alerts/weave.yml missing required alert %q", name)
		}
	}
}

// TestWeaveAlerts_TotalCountAtLeastFive guards the "at least 5 alerts"
// acceptance criterion without hard-coding the alert list. Future stories
// can append more alerts; removing one below five will fail this test.
func TestWeaveAlerts_TotalCountAtLeastFive(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "alerts", "weave.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var f prometheusRuleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var total int
	for _, g := range f.Groups {
		for _, r := range g.Rules {
			if r.Alert != "" {
				total++
			}
		}
	}
	if total < 5 {
		t.Errorf("alerts/weave.yml has %d alerts, want >= 5", total)
	}
}

// TestWeaveAlerts_ExpressionsReferenceWeaveMetrics verifies the
// service-level-objective alerts (HighErrorRate, HighLatency, NatsLag)
// reference the correct weave_* metric families. Infrastructure alerts
// (DBConnExhaust, DiskFull) are allowed to reference postgres_exporter /
// node_exporter metrics since Weave does not emit those itself.
func TestWeaveAlerts_ExpressionsReferenceWeaveMetrics(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "alerts", "weave.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var f prometheusRuleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expect := map[string]string{
		"HighErrorRate": "weave_http_requests_total",
		"HighLatency":   "weave_http_request_duration_seconds",
		"NatsLag":       "weave_nats_",
	}
	for _, g := range f.Groups {
		for _, r := range g.Rules {
			want, ok := expect[r.Alert]
			if !ok {
				continue
			}
			if !strings.Contains(r.Expr, want) {
				t.Errorf("alert %q expr does not reference %q: %s", r.Alert, want, r.Expr)
			}
		}
	}
}

// TestWeaveAlerts_WiredIntoPrometheus verifies the prometheus.yml scrape
// config references the alerts file via rule_files so `docker compose up`
// picks up new alerts without extra operator wiring.
func TestWeaveAlerts_WiredIntoPrometheus(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "grafana", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg struct {
		RuleFiles []string `yaml:"rule_files"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.RuleFiles) == 0 {
		t.Fatalf("grafana/prometheus.yml has no rule_files entries")
	}
	// Accept either an explicit reference to weave.yml or a glob that
	// would match it (e.g. /etc/prometheus/rules/*.yml). filepath.Match
	// understands Prometheus's POSIX-style globs — the loader uses the
	// same filepath.Glob under the hood.
	var found bool
	for _, rf := range cfg.RuleFiles {
		if strings.Contains(rf, "weave.yml") || strings.Contains(rf, "weave.yaml") {
			found = true
			break
		}
		for _, candidate := range []string{"weave.yml", "weave.yaml"} {
			base := filepath.Base(rf)
			if ok, _ := filepath.Match(base, candidate); ok {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("grafana/prometheus.yml rule_files does not reference weave alerts file: %v", cfg.RuleFiles)
	}
}

// TestWeaveAlerts_MountedInDockerCompose verifies docker-compose.yml mounts
// the alerts directory into the prometheus container so the rule_files
// path resolves at container boot.
func TestWeaveAlerts_MountedInDockerCompose(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "./alerts") {
		t.Errorf("docker-compose.yml does not mount ./alerts into prometheus")
	}
}
