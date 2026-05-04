package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/materialize"
)

func writeMaterializeBatch(t *testing.T, m *materialize.Materializer, ts time.Time, edits ...funnel.Edit) {
	t.Helper()
	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-" + ts.Format("150405"),
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits:           edits,
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
}

func TestMaterializeRebuildRequiresOntology(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "materialize", "rebuild", "--object-type", "Customer", "--data-dir", tmp)
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "--ontology is required") {
		t.Fatalf("stderr should mention missing ontology: %q", stderr)
	}
}

func TestMaterializeRebuildRequiresObjectType(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "materialize", "rebuild", "--ontology", "northwind", "--data-dir", tmp)
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "--object-type is required") {
		t.Fatalf("stderr should mention missing object-type: %q", stderr)
	}
}

func TestMaterializeRebuildEmptyDataDirSucceeds(t *testing.T) {
	tmp := t.TempDir()
	stdout, stderr, exit := runCLIWith(t, tmp, "materialize", "rebuild",
		"--ontology", "northwind", "--object-type", "Customer", "--data-dir", tmp)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "rows=0") {
		t.Fatalf("expected rows=0 line in stdout, got %q", stdout)
	}
}

func TestMaterializeRebuildPrintsSnapshotRows(t *testing.T) {
	tmp := t.TempDir()
	m := materialize.NewMaterializer(tmp)
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	t3 := t1.Add(2 * time.Minute)

	writeMaterializeBatch(t, m, t1,
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice"}},
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-2", Properties: map[string]interface{}{"name": "Bob"}},
	)
	writeMaterializeBatch(t, m, t2,
		funnel.Edit{Type: funnel.EditTypeDelete, ObjectType: "Customer", PrimaryKey: "C-2"},
	)
	writeMaterializeBatch(t, m, t3,
		funnel.Edit{Type: funnel.EditTypeModify, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice II"}},
	)

	cfg := t.TempDir()
	stdout, stderr, exit := runCLIWith(t, cfg, "materialize", "rebuild",
		"--ontology", "northwind", "--object-type", "Customer", "--data-dir", tmp)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "rows=1") {
		t.Fatalf("expected rows=1 (C-2 deleted), got %q", stdout)
	}
	if !strings.Contains(stdout, "C-1") {
		t.Fatalf("expected C-1 in output, got %q", stdout)
	}
	if strings.Contains(stdout, "C-2") {
		t.Fatalf("expected C-2 to be filtered out, got %q", stdout)
	}
	if !strings.Contains(stdout, "Alice II") {
		t.Fatalf("expected latest name 'Alice II' in output, got %q", stdout)
	}
}

func TestMaterializeRebuildJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	m := materialize.NewMaterializer(tmp)
	ts := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	writeMaterializeBatch(t, m, ts,
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice"}},
	)

	cfg := t.TempDir()
	stdout, stderr, exit := runCLIWith(t, cfg, "materialize", "rebuild",
		"--ontology", "northwind", "--object-type", "Customer", "--data-dir", tmp, "--json")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	var resp struct {
		Ontology   string                    `json:"ontology"`
		ObjectType string                    `json:"objectType"`
		Rows       []materialize.SnapshotRow `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("decode JSON output %q: %v", stdout, err)
	}
	if resp.Ontology != "northwind" || resp.ObjectType != "Customer" {
		t.Fatalf("unexpected envelope: %+v", resp)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].PrimaryKey != "C-1" {
		t.Fatalf("unexpected rows: %+v", resp.Rows)
	}
}

func TestMaterializeRebuildAsOfFlag(t *testing.T) {
	tmp := t.TempDir()
	m := materialize.NewMaterializer(tmp)
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)

	writeMaterializeBatch(t, m, t1,
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice"}},
	)
	writeMaterializeBatch(t, m, t2,
		funnel.Edit{Type: funnel.EditTypeDelete, ObjectType: "Customer", PrimaryKey: "C-1"},
	)

	cfg := t.TempDir()
	// Cutoff before the delete: row should still be alive.
	stdout, stderr, exit := runCLIWith(t, cfg, "materialize", "rebuild",
		"--ontology", "northwind", "--object-type", "Customer",
		"--data-dir", tmp, "--as-of", t1.Format(time.RFC3339))
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "rows=1") {
		t.Fatalf("expected rows=1 at t1, got %q", stdout)
	}

	// Cutoff at or after the delete: no rows.
	stdout, stderr, exit = runCLIWith(t, cfg, "materialize", "rebuild",
		"--ontology", "northwind", "--object-type", "Customer",
		"--data-dir", tmp, "--as-of", t2.Format(time.RFC3339))
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "rows=0") {
		t.Fatalf("expected rows=0 at t2, got %q", stdout)
	}
}

func TestMaterializeRebuildRejectsBadAsOf(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "materialize", "rebuild",
		"--ontology", "northwind", "--object-type", "Customer",
		"--data-dir", tmp, "--as-of", "not-a-date")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "--as-of must be RFC3339") {
		t.Fatalf("stderr should mention RFC3339: %q", stderr)
	}
}

func TestMaterializeUnknownSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "materialize", "huh")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Fatalf("stderr should mention unknown: %q", stderr)
	}
}

func TestMaterializeNoSubcommandShowsUsage(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "materialize")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "usage:") {
		t.Fatalf("stderr should mention usage: %q", stderr)
	}
}
