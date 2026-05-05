package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUS447Dashboard_ValidJSON ensures the cost-tracking Grafana dashboard
// shipped under grafana/dashboards/weave-cost-tracking.json (a) parses as
// JSON and (b) references all four US-447 metric names. The metric-name
// gate catches a future rename in pkg/metrics that forgets to land the
// dashboard side of the change. Mirrors the US-409 dashboard test shape
// (path-robust against being run from either repo root or pkg/metrics).
func TestUS447Dashboard_ValidJSON(t *testing.T) {
	candidates := []string{
		filepath.Join("..", "..", "grafana", "dashboards", "weave-cost-tracking.json"),
		filepath.Join("grafana", "dashboards", "weave-cost-tracking.json"),
	}
	var path string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Skip("dashboard file not found from any candidate path; skipping (test is path-robust by design)")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dashboard %s: %v", path, err)
	}

	var dash map[string]interface{}
	if err := json.Unmarshal(data, &dash); err != nil {
		t.Fatalf("dashboard JSON parse failed: %v", err)
	}

	required := []string{
		"weave_cost_storage_bytes_total",
		"weave_cost_cpu_seconds_total",
		"weave_cost_nats_messages_total",
		"weave_cost_pg_rows",
	}
	body := string(data)
	for _, name := range required {
		if !strings.Contains(body, name) {
			t.Errorf("dashboard does not reference metric %q", name)
		}
	}

	if uid, _ := dash["uid"].(string); uid != "weave-cost-tracking" {
		t.Errorf("dashboard uid = %q, want %q", uid, "weave-cost-tracking")
	}

	// The ontology label must appear in PromQL expressions to honour the
	// AC: "metrics 按 ontology label". Grep for the label-selector form
	// rather than the raw word so a description string with "ontology"
	// elsewhere doesn't masquerade as a PromQL match.
	if !strings.Contains(body, "ontology=~") {
		t.Errorf("dashboard PromQL does not include the {ontology=~\"...\"} selector")
	}

	// Ontology templating variable must drive every panel; check the
	// template list exposes it.
	tmpl, _ := dash["templating"].(map[string]interface{})
	listAny, _ := tmpl["list"].([]interface{})
	foundOntology := false
	for _, v := range listAny {
		entry, _ := v.(map[string]interface{})
		if name, _ := entry["name"].(string); name == "ontology" {
			foundOntology = true
			break
		}
	}
	if !foundOntology {
		t.Errorf("dashboard templating does not declare an `ontology` variable")
	}
}
