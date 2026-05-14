//go:build integration

package graphsvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// newRepo brings up a fresh PG container, runs migrations, seeds an ontology,
// and returns a wired graphsvc.Repo together with the ontology RID.
func newRepo(t *testing.T) (graphsvc.Repo, string, *testutil.PGContainer) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}
	ctx := context.Background()
	ontologyRID := "ri.ontology.main.ontology.vtxgraph"
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)`,
		ontologyRID, "vtxgraph", "Vertex Graph Test"); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}
	return graphsvc.NewPGRepo(pg.Pool), ontologyRID, pg
}

// TestSystemGraphRepo_Given_EmptyRepo_When_CreateGraph_Then_ReturnsVertexGraphRID
func TestSystemGraphRepo_Given_EmptyRepo_When_CreateGraph_Then_ReturnsVertexGraphRID(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	payload := json.RawMessage(`{"layers":[{"id":"L1"}],"edges":[]}`)
	g, err := repo.Create(ctx, ontologyRID, "JFK Map", "alice", payload, true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(g.RID, "ri.vertex.main.graph.") {
		t.Errorf("expected RID prefix ri.vertex.main.graph., got %q", g.RID)
	}
	if g.Version != 1 {
		t.Errorf("Version = %d, want 1 on fresh create", g.Version)
	}
	if !g.Versioned {
		t.Error("Versioned = false, want true")
	}
	if g.Name != "JFK Map" {
		t.Errorf("Name = %q, want JFK Map", g.Name)
	}
	if g.OntologyRID != ontologyRID {
		t.Errorf("OntologyRID = %q, want %q", g.OntologyRID, ontologyRID)
	}
}

// TestSystemGraphRepo_Given_VersionedGraph_When_Update_Then_VersionBumpsAndHistoryWritten
func TestSystemGraphRepo_Given_VersionedGraph_When_Update_Then_VersionBumpsAndHistoryWritten(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	g, _ := repo.Create(ctx, ontologyRID, "JFK Map", "alice",
		json.RawMessage(`{"layers":[],"edges":[]}`), true)

	updated, err := repo.Update(ctx, g.RID, json.RawMessage(`{"layers":[{"id":"L1"}],"edges":[]}`))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d after Update, want 2", updated.Version)
	}

	versions, err := repo.ListVersions(ctx, g.RID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("ListVersions returned %d, want 2 (v1 from Create + v2 from Update)", len(versions))
	}
}

// TestSystemGraphRepo_Given_Graph_When_PatchLayout_Then_NoNewVersion
func TestSystemGraphRepo_Given_Graph_When_PatchLayout_Then_NoNewVersion(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	g, _ := repo.Create(ctx, ontologyRID, "JFK Map", "alice",
		json.RawMessage(`{"layers":[],"edges":[],"positions":{}}`), true)

	positions := json.RawMessage(`{"node1":{"x":10,"y":20}}`)
	if err := repo.UpdateLayout(ctx, g.RID, positions); err != nil {
		t.Fatalf("UpdateLayout: %v", err)
	}

	got, err := repo.Get(ctx, g.RID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d after UpdateLayout, want 1 (no bump)", got.Version)
	}

	// payload.positions should reflect the layout patch (compare semantically
	// because PG's JSONB normalizes whitespace).
	var unmarshaled map[string]any
	if err := json.Unmarshal(got.Payload, &unmarshaled); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	posMap, ok := unmarshaled["positions"].(map[string]any)
	if !ok {
		t.Fatalf("positions not a map: %v", unmarshaled["positions"])
	}
	node1, ok := posMap["node1"].(map[string]any)
	if !ok {
		t.Fatalf("node1 not a map: %v", posMap["node1"])
	}
	if x, _ := node1["x"].(float64); x != 10 {
		t.Errorf("node1.x = %v, want 10", node1["x"])
	}
	if y, _ := node1["y"].(float64); y != 20 {
		t.Errorf("node1.y = %v, want 20", node1["y"])
	}

	versions, _ := repo.ListVersions(ctx, g.RID)
	if len(versions) != 1 {
		t.Errorf("ListVersions returned %d, want 1 (layout patch should not create version)", len(versions))
	}
}

// TestSystemGraphRepo_Given_Graph_When_Duplicate_Then_NewRIDDeepCopy
func TestSystemGraphRepo_Given_Graph_When_Duplicate_Then_NewRIDDeepCopy(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	originalPayload := json.RawMessage(`{"layers":[{"id":"L1","name":"Airports"}],"edges":[]}`)
	g, _ := repo.Create(ctx, ontologyRID, "Original", "alice", originalPayload, true)

	dup, err := repo.Duplicate(ctx, g.RID)
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if dup.RID == g.RID {
		t.Error("Duplicate returned same RID, expected new one")
	}
	if !strings.HasPrefix(dup.RID, "ri.vertex.main.graph.") {
		t.Errorf("expected duplicated RID prefix ri.vertex.main.graph., got %q", dup.RID)
	}
	if dup.Version != 1 {
		t.Errorf("duplicated Version = %d, want 1", dup.Version)
	}

	// Payload must be deep-copy: mutating duplicate must not affect original.
	mutated := json.RawMessage(`{"layers":[],"edges":[],"mutated":true}`)
	if _, err := repo.Update(ctx, dup.RID, mutated); err != nil {
		t.Fatalf("Update duplicate: %v", err)
	}

	origGot, _ := repo.Get(ctx, g.RID)
	if string(origGot.Payload) == string(mutated) {
		t.Error("mutating duplicate's payload affected original (not a deep copy)")
	}
}

// TestSystemGraphRepo_Given_GraphWith3Versions_When_GetVersion_Then_ReturnsCorrectSnapshot
func TestSystemGraphRepo_Given_GraphWith3Versions_When_GetVersion_Then_ReturnsCorrectSnapshot(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	g, _ := repo.Create(ctx, ontologyRID, "Map", "alice",
		json.RawMessage(`{"layers":[{"id":"L1"}],"edges":[]}`), true)
	if _, err := repo.Update(ctx, g.RID, json.RawMessage(`{"layers":[{"id":"L2"}],"edges":[]}`)); err != nil {
		t.Fatalf("Update v2: %v", err)
	}
	if _, err := repo.Update(ctx, g.RID, json.RawMessage(`{"layers":[{"id":"L3"}],"edges":[]}`)); err != nil {
		t.Fatalf("Update v3: %v", err)
	}

	v1, err := repo.GetVersion(ctx, g.RID, 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if !strings.Contains(string(v1.Payload), `"L1"`) {
		t.Errorf("v1 payload should contain L1, got %s", v1.Payload)
	}

	v3, err := repo.GetVersion(ctx, g.RID, 3)
	if err != nil {
		t.Fatalf("GetVersion(3): %v", err)
	}
	if !strings.Contains(string(v3.Payload), `"L3"`) {
		t.Errorf("v3 payload should contain L3, got %s", v3.Payload)
	}

	if _, err := repo.GetVersion(ctx, g.RID, 99); !errors.Is(err, graphsvc.ErrVersionNotFound) {
		t.Errorf("GetVersion(99) = %v, want ErrVersionNotFound", err)
	}
}

// TestSystemGraphRepo_Given_UnknownGraph_When_Get_Then_ErrGraphNotFound
func TestSystemGraphRepo_Given_UnknownGraph_When_Get_Then_ErrGraphNotFound(t *testing.T) {
	repo, _, _ := newRepo(t)
	ctx := context.Background()

	_, err := repo.Get(ctx, "ri.vertex.main.graph.00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, graphsvc.ErrGraphNotFound) {
		t.Errorf("expected ErrGraphNotFound, got %v", err)
	}
}

// TestSystemGraphRepo_Given_UnversionedGraph_When_Update_Then_NoHistoryRow
// verifies versioned=false bypasses history entirely.
func TestSystemGraphRepo_Given_UnversionedGraph_When_Update_Then_NoHistoryRow(t *testing.T) {
	repo, ontologyRID, _ := newRepo(t)
	ctx := context.Background()

	g, _ := repo.Create(ctx, ontologyRID, "Ephemeral", "alice",
		json.RawMessage(`{"layers":[],"edges":[]}`), false)

	if _, err := repo.Update(ctx, g.RID, json.RawMessage(`{"layers":[{"id":"L1"}],"edges":[]}`)); err != nil {
		t.Fatalf("Update: %v", err)
	}

	versions, _ := repo.ListVersions(ctx, g.RID)
	if len(versions) != 0 {
		t.Errorf("ListVersions returned %d, want 0 for unversioned graph", len(versions))
	}
}
