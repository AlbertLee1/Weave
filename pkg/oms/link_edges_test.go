//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// seedLinkTypeM2M creates an M2M link type and returns it. It is the
// link_edges write-side counterpart to seedTwoObjectTypes for use by the
// new UpsertLinkEdge / DeleteLinkEdge tests.
func seedLinkTypeM2M(t *testing.T, repo *oms.PGRepository, ontologyRID, srcRID, tgtRID string) *oms.LinkType {
	t.Helper()
	lt := &oms.LinkType{
		RID:              "ri.ontology.main.link-type.m2m1",
		OntologyRID:      ontologyRID,
		APIName:          "employeeProject",
		DisplayName:      "Employee Project",
		SourceObjectType: srcRID,
		TargetObjectType: tgtRID,
		Cardinality:      "MANY_TO_MANY",
		JoinTableConfig:  json.RawMessage(`{"datasetRid":"ds1","sourceColumn":"empId","targetColumn":"projId"}`),
	}
	if err := repo.CreateLinkType(context.Background(), lt); err != nil {
		t.Fatalf("seed link type failed: %v", err)
	}
	return lt
}

// --- UpsertLinkEdge tests ---

func TestPGRepository_UpsertLinkEdge_Create(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)
	lt := seedLinkTypeM2M(t, repo, o.RID, src.RID, tgt.RID)

	edge := oms.LinkEdge{
		LinkTypeRID:    lt.RID,
		SourceObjectPK: "emp-1",
		TargetObjectPK: "proj-7",
		EdgeProperties: json.RawMessage(`{"role":"lead"}`),
	}
	err := repo.UpsertLinkEdge(context.Background(), edge)
	if err != nil {
		t.Fatalf("upsert (create) failed: %v", err)
	}

	targets, err := repo.ListEdgeTargets(context.Background(), lt.RID, []string{"emp-1"})
	if err != nil {
		t.Fatalf("ListEdgeTargets failed: %v", err)
	}
	if len(targets) != 1 || targets[0] != "proj-7" {
		t.Errorf("expected [proj-7], got %v", targets)
	}
}

func TestPGRepository_UpsertLinkEdge_Update(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)
	lt := seedLinkTypeM2M(t, repo, o.RID, src.RID, tgt.RID)

	edge1 := oms.LinkEdge{
		LinkTypeRID:    lt.RID,
		SourceObjectPK: "emp-1",
		TargetObjectPK: "proj-7",
		EdgeProperties: json.RawMessage(`{"role":"lead"}`),
	}
	if err := repo.UpsertLinkEdge(context.Background(), edge1); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Upsert with new edge properties.
	edge2 := edge1
	edge2.EdgeProperties = json.RawMessage(`{"role":"contributor"}`)
	if err := repo.UpsertLinkEdge(context.Background(), edge2); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Still exactly one row for this (source, target) pair.
	targets, err := repo.ListEdgeTargets(context.Background(), lt.RID, []string{"emp-1"})
	if err != nil {
		t.Fatalf("ListEdgeTargets failed: %v", err)
	}
	if len(targets) != 1 {
		t.Errorf("expected exactly 1 edge after upsert, got %d (%v)", len(targets), targets)
	}
}

func TestPGRepository_UpsertLinkEdge_NilProperties(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)
	lt := seedLinkTypeM2M(t, repo, o.RID, src.RID, tgt.RID)

	edge := oms.LinkEdge{
		LinkTypeRID:    lt.RID,
		SourceObjectPK: "emp-2",
		TargetObjectPK: "proj-3",
		EdgeProperties: nil,
	}
	if err := repo.UpsertLinkEdge(context.Background(), edge); err != nil {
		t.Fatalf("upsert with nil properties failed: %v", err)
	}

	targets, err := repo.ListEdgeTargets(context.Background(), lt.RID, []string{"emp-2"})
	if err != nil {
		t.Fatalf("ListEdgeTargets failed: %v", err)
	}
	if len(targets) != 1 || targets[0] != "proj-3" {
		t.Errorf("expected [proj-3], got %v", targets)
	}
}

// --- DeleteLinkEdge tests ---

func TestPGRepository_DeleteLinkEdge_Existing(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)
	lt := seedLinkTypeM2M(t, repo, o.RID, src.RID, tgt.RID)

	edge := oms.LinkEdge{
		LinkTypeRID:    lt.RID,
		SourceObjectPK: "emp-9",
		TargetObjectPK: "proj-9",
	}
	if err := repo.UpsertLinkEdge(context.Background(), edge); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	if err := repo.DeleteLinkEdge(context.Background(), lt.RID, "emp-9", "proj-9"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	targets, err := repo.ListEdgeTargets(context.Background(), lt.RID, []string{"emp-9"})
	if err != nil {
		t.Fatalf("ListEdgeTargets failed: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected no edges after delete, got %v", targets)
	}
}

func TestPGRepository_DeleteLinkEdge_NonExistent(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)
	lt := seedLinkTypeM2M(t, repo, o.RID, src.RID, tgt.RID)

	// Deleting a non-existent edge should be idempotent.
	if err := repo.DeleteLinkEdge(context.Background(), lt.RID, "ghost", "phantom"); err != nil {
		t.Errorf("delete of non-existent edge should be a no-op, got %v", err)
	}
}

func TestPGRepository_DeleteAllLinkEdgesForSource(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)
	lt := seedLinkTypeM2M(t, repo, o.RID, src.RID, tgt.RID)

	for _, target := range []string{"proj-a", "proj-b", "proj-c"} {
		edge := oms.LinkEdge{LinkTypeRID: lt.RID, SourceObjectPK: "emp-bulk", TargetObjectPK: target}
		if err := repo.UpsertLinkEdge(context.Background(), edge); err != nil {
			t.Fatalf("seed upsert failed: %v", err)
		}
	}

	// Edge belonging to a different source — must be left alone.
	otherEdge := oms.LinkEdge{LinkTypeRID: lt.RID, SourceObjectPK: "emp-other", TargetObjectPK: "proj-z"}
	if err := repo.UpsertLinkEdge(context.Background(), otherEdge); err != nil {
		t.Fatalf("other-source upsert failed: %v", err)
	}

	if err := repo.DeleteAllLinkEdgesForSource(context.Background(), lt.RID, "emp-bulk"); err != nil {
		t.Fatalf("DeleteAllLinkEdgesForSource failed: %v", err)
	}

	bulkLeft, err := repo.ListEdgeTargets(context.Background(), lt.RID, []string{"emp-bulk"})
	if err != nil {
		t.Fatalf("ListEdgeTargets failed: %v", err)
	}
	if len(bulkLeft) != 0 {
		t.Errorf("expected emp-bulk edges to be removed, got %v", bulkLeft)
	}

	otherLeft, err := repo.ListEdgeTargets(context.Background(), lt.RID, []string{"emp-other"})
	if err != nil {
		t.Fatalf("ListEdgeTargets failed: %v", err)
	}
	if len(otherLeft) != 1 || otherLeft[0] != "proj-z" {
		t.Errorf("expected emp-other edges intact, got %v", otherLeft)
	}
}

func TestPGRepository_DeleteAllLinkEdgesForSource_NoMatch(t *testing.T) {
	repo := setupRepo(t)
	o := seedOntology(t, repo)
	src, tgt := seedTwoObjectTypes(t, repo, o.RID)
	lt := seedLinkTypeM2M(t, repo, o.RID, src.RID, tgt.RID)

	if err := repo.DeleteAllLinkEdgesForSource(context.Background(), lt.RID, "ghost"); err != nil {
		t.Errorf("expected no error for unmatched source, got %v", err)
	}
}
