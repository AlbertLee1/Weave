package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUS409Dashboard_ValidJSON ensures the materialize Grafana dashboard
// shipped under grafana/dashboards/weave-materialize.json (a) parses as
// JSON and (b) references all three US-409 metric names. The metric-name
// gate catches a future rename in pkg/metrics that forgets to land the
// dashboard side of the change.
func TestUS409Dashboard_ValidJSON(t *testing.T) {
	candidates := []string{
		filepath.Join("..", "..", "grafana", "dashboards", "weave-materialize.json"),
		filepath.Join("grafana", "dashboards", "weave-materialize.json"),
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
		"weave_materialize_lag_seconds",
		"weave_parquet_files_total",
		"weave_parquet_size_bytes",
	}
	body := string(data)
	for _, name := range required {
		if !strings.Contains(body, name) {
			t.Errorf("dashboard does not reference metric %q", name)
		}
	}

	if uid, _ := dash["uid"].(string); uid != "weave-materialize" {
		t.Errorf("dashboard uid = %q, want %q", uid, "weave-materialize")
	}
}
