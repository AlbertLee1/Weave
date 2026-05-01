package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureMetaServer returns an httptest.Server that serves a single
// fullMetadata payload at the expected path.
func fixtureMetaServer(t *testing.T, ontology, objType, body string) *httptest.Server {
	t.Helper()
	want := "/api/v2/ontologies/" + ontology + "/objectTypes/" + objType + "/fullMetadata"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == want {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const customerMeta = `{
  "apiName": "Customer",
  "displayName": "Customer",
  "primaryKey": "customerId",
  "primaryKeys": ["customerId"],
  "properties": {
    "customerId": {
      "dataType": {"type":"string","minLength":5,"maxLength":5,"regex":"^[A-Z]{5}$"}
    },
    "companyName": {
      "dataType": {"type":"string","minLength":2,"maxLength":40}
    },
    "rating": {
      "dataType": {"type":"integer","min":1,"max":5}
    },
    "active": {
      "dataType": {"type":"boolean"}
    }
  }
}`

func TestFixturesGenerate_RespectsCount(t *testing.T) {
	srv := fixtureMetaServer(t, "northwind", "Customer", customerMeta)

	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config base_url exit")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "tok"); exit != 0 {
		t.Fatalf("config token exit")
	}

	stdout, stderr, exit := runCLIWith(t, tmp,
		"fixtures", "generate",
		"--ontology", "northwind",
		"--type", "Customer",
		"--count", "10",
		"--seed", "42",
	)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 10 {
		t.Fatalf("got %d ndjson rows, want 10\n%s", len(lines), stdout)
	}
	for i, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row %d not valid JSON: %v\n%s", i, err, line)
		}
		if _, ok := row["customerId"]; !ok {
			t.Fatalf("row %d missing customerId: %v", i, row)
		}
		if _, ok := row["__apiName"]; !ok {
			t.Fatalf("row %d missing __apiName envelope", i)
		}
		if _, ok := row["__primaryKey"].(string); !ok {
			t.Fatalf("row %d missing __primaryKey envelope", i)
		}
	}
}

func TestFixturesGenerate_DeterministicWithSeed(t *testing.T) {
	srv := fixtureMetaServer(t, "northwind", "Customer", customerMeta)
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	a, _, _ := runCLIWith(t, tmp,
		"fixtures", "generate", "--ontology", "northwind", "--type", "Customer",
		"--count", "5", "--seed", "9001")
	b, _, _ := runCLIWith(t, tmp,
		"fixtures", "generate", "--ontology", "northwind", "--type", "Customer",
		"--count", "5", "--seed", "9001")
	if a == "" || a != b {
		t.Fatalf("same seed should produce same output:\nA=%q\nB=%q", a, b)
	}
}

func TestFixturesGenerate_RegexConstraintRespected(t *testing.T) {
	srv := fixtureMetaServer(t, "northwind", "Customer", customerMeta)
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")
	stdout, stderr, exit := runCLIWith(t, tmp,
		"fixtures", "generate", "--ontology", "northwind", "--type", "Customer",
		"--count", "20", "--seed", "777")
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%q", exit, stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		id, _ := row["customerId"].(string)
		if len(id) != 5 {
			t.Errorf("customerId %q wrong length", id)
		}
		for _, c := range id {
			if c < 'A' || c > 'Z' {
				t.Errorf("customerId %q has non-A-Z char %q", id, c)
				break
			}
		}
		// rating bounds
		switch r := row["rating"].(type) {
		case float64:
			if r < 1 || r > 5 {
				t.Errorf("rating %v out of [1,5]", r)
			}
		default:
			t.Errorf("rating type: %T", row["rating"])
		}
	}
}

func TestFixturesGenerate_OutputFile(t *testing.T) {
	srv := fixtureMetaServer(t, "northwind", "Customer", customerMeta)
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	out := filepath.Join(tmp, "fixtures.ndjson")
	_, stderr, exit := runCLIWith(t, tmp,
		"fixtures", "generate", "--ontology", "northwind", "--type", "Customer",
		"--count", "3", "--seed", "1", "--output", out)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr=%q", exit, stderr)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read fixtures file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 3 {
		t.Fatalf("file contains %d lines, want 3:\n%s", len(lines), body)
	}
}

func TestFixturesGenerate_JSONFormat(t *testing.T) {
	srv := fixtureMetaServer(t, "northwind", "Customer", customerMeta)
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, stderr, exit := runCLIWith(t, tmp,
		"fixtures", "generate", "--ontology", "northwind", "--type", "Customer",
		"--count", "4", "--seed", "1", "--format", "json")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr=%q", exit, stderr)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("output not valid JSON array: %v\n%s", err, stdout)
	}
	if len(arr) != 4 {
		t.Fatalf("array length = %d, want 4", len(arr))
	}
}

func TestFixturesGenerate_MissingFlagsErrors(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "fixtures", "generate", "--ontology", "x")
	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "type") && !strings.Contains(stderr, "Usage") &&
		!strings.Contains(stderr, "usage") {
		t.Fatalf("stderr should hint at missing --type: %q", stderr)
	}
}

func TestFixturesUnknownSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "fixtures", "ghost")
	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown") {
		t.Fatalf("stderr: %q", stderr)
	}
}
