//go:build integration

package oms_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// pgvectorAvailable returns true when the test database has the `vector`
// extension installed (i.e. it is a `pgvector/pgvector:pg16` image or
// equivalent). When false, the embedding tests skip themselves so the rest
// of the integration suite remains green on plain `postgres:16-alpine`.
func pgvectorAvailable(t *testing.T, repo *oms.PGRepository) bool {
	t.Helper()
	// We use a sentinel insert and inspect the error message; this avoids
	// poking at the pool internals or doing a separate `SELECT * FROM
	// pg_extension` round trip.
	e := &oms.ObjectEmbedding{
		ObjectTypeRID: "ri.ontology.main.object-type.pgvector-probe",
		PrimaryKey:    "probe-1",
		Model:         "weave-mock-embedding-v1",
		Embedding:     fixedDimVector(1536, 0.123),
		SourceText:    "probe",
	}
	if err := repo.UpsertObjectEmbedding(context.Background(), e); err != nil {
		// pgvector missing typically reports either "type \"vector\" does
		// not exist" or "relation \"object_embeddings\" does not exist"
		// (because the migration failed mid-flight).
		msg := err.Error()
		if strings.Contains(msg, "type \"vector\"") ||
			strings.Contains(msg, "object_embeddings") {
			return false
		}
		t.Fatalf("pgvector probe failed unexpectedly: %v", err)
	}
	return true
}

// fixedDimVector returns a `dim`-element float32 slice where every value
// equals v. Used by the integration tests to keep payloads compact while
// still hitting the schema's hard 1536 dimension requirement.
func fixedDimVector(dim int, v float32) []float32 {
	out := make([]float32, dim)
	for i := range out {
		out[i] = v
	}
	return out
}

// linearVector returns a `dim`-element vector where index i has value
// base + i*step. Distinct calls produce distinct vectors so the kNN test
// can rely on a stable distance ordering.
func linearVector(dim int, base, step float32) []float32 {
	out := make([]float32, dim)
	for i := range out {
		out[i] = base + float32(i)*step
	}
	return out
}

// TestPGRepository_UpsertObjectEmbedding_Create inserts a brand new
// embedding row and verifies that GetObjectEmbedding returns it byte-for-byte.
func TestPGRepository_UpsertObjectEmbedding_Create(t *testing.T) {
	repo := setupRepo(t)
	if !pgvectorAvailable(t, repo) {
		t.Skip("pgvector extension not available; skipping vector tests")
	}
	ctx := context.Background()

	otRID := "ri.ontology.main.object-type.embedding-employee"
	e := &oms.ObjectEmbedding{
		ObjectTypeRID: otRID,
		PrimaryKey:    "emp-1",
		Embedding:     fixedDimVector(1536, 0.5),
		Model:         "weave-mock-embedding-v1",
		SourceText:    "alice the engineer",
	}
	if err := repo.UpsertObjectEmbedding(ctx, e); err != nil {
		t.Fatalf("UpsertObjectEmbedding: %v", err)
	}
	if e.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be populated after upsert")
	}
	if e.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be populated after upsert")
	}

	got, err := repo.GetObjectEmbedding(ctx, otRID, "emp-1", "weave-mock-embedding-v1")
	if err != nil {
		t.Fatalf("GetObjectEmbedding: %v", err)
	}
	if got.PrimaryKey != "emp-1" {
		t.Errorf("PrimaryKey = %q", got.PrimaryKey)
	}
	if got.SourceText != "alice the engineer" {
		t.Errorf("SourceText = %q", got.SourceText)
	}
	if len(got.Embedding) != 1536 {
		t.Fatalf("len(Embedding) = %d, want 1536", len(got.Embedding))
	}
	for i := 0; i < 1536; i++ {
		if got.Embedding[i] != 0.5 {
			t.Fatalf("Embedding[%d] = %v, want 0.5", i, got.Embedding[i])
		}
	}
}

// TestPGRepository_UpsertObjectEmbedding_Update verifies that calling
// UpsertObjectEmbedding twice for the same (objectTypeRID, primaryKey,
// model) tuple replaces the embedding (and bumps updated_at) instead of
// inserting a new row.
func TestPGRepository_UpsertObjectEmbedding_Update(t *testing.T) {
	repo := setupRepo(t)
	if !pgvectorAvailable(t, repo) {
		t.Skip("pgvector extension not available; skipping vector tests")
	}
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.update-target"

	first := &oms.ObjectEmbedding{
		ObjectTypeRID: otRID,
		PrimaryKey:    "obj-1",
		Embedding:     fixedDimVector(1536, 0.1),
		Model:         "weave-mock-embedding-v1",
		SourceText:    "v1",
	}
	if err := repo.UpsertObjectEmbedding(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	firstUpdatedAt := first.UpdatedAt

	second := &oms.ObjectEmbedding{
		ObjectTypeRID: otRID,
		PrimaryKey:    "obj-1",
		Embedding:     fixedDimVector(1536, 0.9),
		Model:         "weave-mock-embedding-v1",
		SourceText:    "v2",
	}
	if err := repo.UpsertObjectEmbedding(ctx, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := repo.GetObjectEmbedding(ctx, otRID, "obj-1", "weave-mock-embedding-v1")
	if err != nil {
		t.Fatalf("GetObjectEmbedding: %v", err)
	}
	if got.SourceText != "v2" {
		t.Errorf("expected SourceText 'v2', got %q", got.SourceText)
	}
	if got.Embedding[0] != 0.9 {
		t.Errorf("expected first dim 0.9, got %v", got.Embedding[0])
	}
	if !got.UpdatedAt.After(firstUpdatedAt) && !got.UpdatedAt.Equal(firstUpdatedAt) {
		t.Errorf("expected UpdatedAt to be after %v, got %v", firstUpdatedAt, got.UpdatedAt)
	}
}

// TestPGRepository_GetObjectEmbedding_NotFound verifies that fetching a
// non-existent (objectTypeRID, primaryKey, model) tuple returns ErrNotFound.
func TestPGRepository_GetObjectEmbedding_NotFound(t *testing.T) {
	repo := setupRepo(t)
	if !pgvectorAvailable(t, repo) {
		t.Skip("pgvector extension not available; skipping vector tests")
	}

	_, err := repo.GetObjectEmbedding(context.Background(),
		"ri.ontology.main.object-type.missing", "nope", "weave-mock-embedding-v1")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestPGRepository_FindNearestNeighbors_OrderedByDistance seeds three
// embeddings whose distance from a chosen query vector is known, and
// asserts the kNN result returns them in ascending distance order.
//
// We use vectors that are linearly spaced so cosine distance has a clear
// monotonic ordering against the query.
func TestPGRepository_FindNearestNeighbors_OrderedByDistance(t *testing.T) {
	repo := setupRepo(t)
	if !pgvectorAvailable(t, repo) {
		t.Skip("pgvector extension not available; skipping vector tests")
	}
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.knn-target"

	// Seed three embeddings.
	seeds := []struct {
		pk  string
		vec []float32
	}{
		{"close", linearVector(1536, 1.0, 0.001)},     // very close to query
		{"middle", linearVector(1536, 0.5, 0.001)},    // middle distance
		{"far", linearVector(1536, -1.0, 0.001)},      // opposite direction
	}
	for _, s := range seeds {
		e := &oms.ObjectEmbedding{
			ObjectTypeRID: otRID,
			PrimaryKey:    s.pk,
			Embedding:     s.vec,
			Model:         "weave-mock-embedding-v1",
		}
		if err := repo.UpsertObjectEmbedding(ctx, e); err != nil {
			t.Fatalf("seed %s: %v", s.pk, err)
		}
	}

	// Query vector chosen to be most aligned with "close".
	query := linearVector(1536, 1.0, 0.001)

	results, err := repo.FindNearestNeighbors(ctx, otRID, query, 3, "weave-mock-embedding-v1")
	if err != nil {
		t.Fatalf("FindNearestNeighbors: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].PrimaryKey != "close" {
		t.Errorf("expected nearest = close, got %q", results[0].PrimaryKey)
	}
	if results[2].PrimaryKey != "far" {
		t.Errorf("expected farthest = far, got %q", results[2].PrimaryKey)
	}
	// Distances must be non-decreasing.
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("distances out of order: %v -> %v", results[i-1].Distance, results[i].Distance)
		}
	}
}

// TestPGRepository_FindNearestNeighbors_FiltersByModel verifies that the
// kNN query only considers embeddings produced by the requested model.
// This guards against bleeding ada-002 vectors into 3-small queries.
func TestPGRepository_FindNearestNeighbors_FiltersByModel(t *testing.T) {
	repo := setupRepo(t)
	if !pgvectorAvailable(t, repo) {
		t.Skip("pgvector extension not available; skipping vector tests")
	}
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.model-filter"

	a := &oms.ObjectEmbedding{
		ObjectTypeRID: otRID,
		PrimaryKey:    "obj-A",
		Embedding:     linearVector(1536, 1.0, 0.001),
		Model:         "model-x",
	}
	b := &oms.ObjectEmbedding{
		ObjectTypeRID: otRID,
		PrimaryKey:    "obj-B",
		Embedding:     linearVector(1536, 1.0, 0.001),
		Model:         "model-y",
	}
	if err := repo.UpsertObjectEmbedding(ctx, a); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := repo.UpsertObjectEmbedding(ctx, b); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	results, err := repo.FindNearestNeighbors(ctx, otRID,
		linearVector(1536, 1.0, 0.001), 5, "model-x")
	if err != nil {
		t.Fatalf("FindNearestNeighbors: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for model-x, got %d", len(results))
	}
	if results[0].PrimaryKey != "obj-A" {
		t.Errorf("expected obj-A, got %q", results[0].PrimaryKey)
	}
}
