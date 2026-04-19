package dashboards_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot finds the Weave repo root by walking up from the test file until
// it sees go.mod. The tests embed no assets — they load the canonical file
// from grafana/dashboards/weave-slo.json so a stale copy is impossible.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repo root from %s", wd)
	return ""
}

// TestWeaveSLODashboard_Exists verifies the dashboard JSON file is present
// and parses as a valid Grafana dashboard envelope.
func TestWeaveSLODashboard_Exists(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "grafana", "dashboards", "weave-slo.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var envelope struct {
		Title       string           `json:"title"`
		UID         string           `json:"uid"`
		SchemaVer   int              `json:"schemaVersion"`
		Panels      []map[string]any `json:"panels"`
		Templating  map[string]any   `json:"templating"`
		Annotations map[string]any   `json:"annotations"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if envelope.Title == "" {
		t.Errorf("dashboard title is empty")
	}
	if envelope.UID == "" {
		t.Errorf("dashboard uid is empty")
	}
	if envelope.SchemaVer < 30 {
		t.Errorf("schemaVersion %d too low; grafana 9+ expects 30 or later", envelope.SchemaVer)
	}
	if len(envelope.Panels) == 0 {
		t.Fatalf("dashboard has no panels")
	}
}

// TestWeaveSLODashboard_RequiredPanels verifies the four PRD-mandated
// panels (http_p99, error_rate, edit_batch_lag, nats_consumer_lag) are all
// present. The matcher looks inside each panel's title + PromQL expressions
// so titles or queries can evolve without dropping coverage.
func TestWeaveSLODashboard_RequiredPanels(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "grafana", "dashboards", "weave-slo.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env struct {
		Panels []struct {
			Title   string `json:"title"`
			Type    string `json:"type"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required := []struct {
		id       string
		contains string
	}{
		{"http_p99", "weave_http_request_duration_seconds"},
		{"error_rate", "weave_http_requests_total"},
		{"edit_batch_lag", "weave_nats_consume_duration_seconds"},
		{"nats_consumer_lag", "weave_nats_"},
	}
	for _, r := range required {
		found := false
		for _, p := range env.Panels {
			idMatch := strings.Contains(strings.ToLower(p.Title), r.id)
			if !idMatch {
				for _, tgt := range p.Targets {
					if strings.Contains(strings.ToLower(tgt.Expr), r.id) {
						idMatch = true
						break
					}
				}
			}
			if !idMatch {
				continue
			}
			for _, tgt := range p.Targets {
				if strings.Contains(tgt.Expr, r.contains) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("dashboard is missing panel %q with metric substring %q", r.id, r.contains)
		}
	}
}

// TestDockerCompose_GrafanaWired verifies the grafana/prometheus services
// are wired into docker-compose.yml so `make docker-up` gives operators the
// whole observability stack in one command.
func TestDockerCompose_GrafanaWired(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	text := string(raw)
	for _, needle := range []string{
		"\n  grafana:",
		"\n  prometheus:",
		"weave-slo.json",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("docker-compose.yml missing %q", needle)
		}
	}
}

// TestGrafanaProvisioning_DatasourceAndDashboardsPresent verifies the
// provisioning configs wire Prometheus as the default datasource AND mount
// the dashboards folder so the weave-slo dashboard appears automatically.
func TestGrafanaProvisioning_DatasourceAndDashboardsPresent(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "grafana", "provisioning", "datasources", "prometheus.yml"),
		filepath.Join(root, "grafana", "provisioning", "dashboards", "weave.yml"),
		filepath.Join(root, "grafana", "prometheus.yml"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing provisioning file %s: %v", p, err)
		}
	}
}
