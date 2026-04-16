//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// --- OntologyBranch CRUD ---

func TestBranch_Create(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	b := &oms.OntologyBranch{
		ID:          "br-001",
		OntologyRID: ont.RID,
		Name:        "feature/add-buildings",
		BaseVersion: 1,
		Status:      "open",
		CreatedBy:   "user-1",
	}
	err := repo.CreateBranch(context.Background(), b)
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// Verify timestamps were set
	if b.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if b.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestBranch_Get(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	b := &oms.OntologyBranch{
		ID:          "br-002",
		OntologyRID: ont.RID,
		Name:        "feature/test-get",
		BaseVersion: 2,
		Status:      "open",
		CreatedBy:   "user-2",
	}
	if err := repo.CreateBranch(context.Background(), b); err != nil {
		t.Fatalf("seed branch: %v", err)
	}

	got, err := repo.GetBranch(context.Background(), "br-002")
	if err != nil {
		t.Fatalf("GetBranch failed: %v", err)
	}
	if got.ID != "br-002" {
		t.Errorf("ID = %q, want %q", got.ID, "br-002")
	}
	if got.OntologyRID != ont.RID {
		t.Errorf("OntologyRID = %q, want %q", got.OntologyRID, ont.RID)
	}
	if got.Name != "feature/test-get" {
		t.Errorf("Name = %q, want %q", got.Name, "feature/test-get")
	}
	if got.BaseVersion != 2 {
		t.Errorf("BaseVersion = %d, want 2", got.BaseVersion)
	}
	if got.Status != "open" {
		t.Errorf("Status = %q, want %q", got.Status, "open")
	}
	if got.CreatedBy != "user-2" {
		t.Errorf("CreatedBy = %q, want %q", got.CreatedBy, "user-2")
	}
}

func TestBranch_Get_NotFound(t *testing.T) {
	repo := setupRepo(t)

	_, err := repo.GetBranch(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent branch")
	}
	if err != oms.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBranch_ListBranches(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)
	ctx := context.Background()

	// Create two branches
	for _, name := range []string{"branch-a", "branch-b"} {
		b := &oms.OntologyBranch{
			ID:          "br-list-" + name,
			OntologyRID: ont.RID,
			Name:        name,
			BaseVersion: 1,
			Status:      "open",
			CreatedBy:   "user-1",
		}
		if err := repo.CreateBranch(ctx, b); err != nil {
			t.Fatalf("seed branch %s: %v", name, err)
		}
	}

	branches, err := repo.ListBranches(ctx, ont.RID)
	if err != nil {
		t.Fatalf("ListBranches failed: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches))
	}
}

func TestBranch_CloseBranch(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)
	ctx := context.Background()

	b := &oms.OntologyBranch{
		ID:          "br-close-1",
		OntologyRID: ont.RID,
		Name:        "to-close",
		BaseVersion: 1,
		Status:      "open",
		CreatedBy:   "user-1",
	}
	if err := repo.CreateBranch(ctx, b); err != nil {
		t.Fatalf("seed branch: %v", err)
	}

	if err := repo.CloseBranch(ctx, "br-close-1"); err != nil {
		t.Fatalf("CloseBranch failed: %v", err)
	}

	got, err := repo.GetBranch(ctx, "br-close-1")
	if err != nil {
		t.Fatalf("GetBranch after close: %v", err)
	}
	if got.Status != "closed" {
		t.Errorf("Status = %q, want %q", got.Status, "closed")
	}
}

func TestBranch_CloseBranch_NotFound(t *testing.T) {
	repo := setupRepo(t)

	err := repo.CloseBranch(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent branch")
	}
	if err != oms.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBranch_UniqueConstraint(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)
	ctx := context.Background()

	b1 := &oms.OntologyBranch{
		ID:          "br-dup-1",
		OntologyRID: ont.RID,
		Name:        "same-name",
		BaseVersion: 1,
		Status:      "open",
		CreatedBy:   "user-1",
	}
	if err := repo.CreateBranch(ctx, b1); err != nil {
		t.Fatalf("first create: %v", err)
	}

	b2 := &oms.OntologyBranch{
		ID:          "br-dup-2",
		OntologyRID: ont.RID,
		Name:        "same-name",
		BaseVersion: 1,
		Status:      "open",
		CreatedBy:   "user-1",
	}
	err := repo.CreateBranch(ctx, b2)
	if err == nil {
		t.Fatal("expected duplicate error for same (ontology_rid, name)")
	}
}

// --- BranchChange CRUD ---

func seedBranch(t *testing.T, repo *oms.PGRepository, ontologyRID string) *oms.OntologyBranch {
	t.Helper()
	b := &oms.OntologyBranch{
		ID:          "br-seed-1",
		OntologyRID: ontologyRID,
		Name:        "seed-branch",
		BaseVersion: 1,
		Status:      "open",
		CreatedBy:   "user-1",
	}
	if err := repo.CreateBranch(context.Background(), b); err != nil {
		t.Fatalf("seed branch: %v", err)
	}
	return b
}

func TestBranchChange_Create(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)
	br := seedBranch(t, repo, ont.RID)
	ctx := context.Background()

	c := &oms.BranchChange{
		ID:         "chg-001",
		BranchID:   br.ID,
		ChangeType: "ADDED",
		EntityType: "objectType",
		EntityRID:  "ri.ontology.main.object-type.new-1",
		AfterState: json.RawMessage(`{"apiName":"building","displayName":"Building"}`),
	}
	err := repo.CreateBranchChange(ctx, c)
	if err != nil {
		t.Fatalf("CreateBranchChange failed: %v", err)
	}
	if c.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestBranchChange_ListChanges(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)
	br := seedBranch(t, repo, ont.RID)
	ctx := context.Background()

	// Create three changes
	changes := []oms.BranchChange{
		{
			ID:         "chg-list-1",
			BranchID:   br.ID,
			ChangeType: "ADDED",
			EntityType: "objectType",
			EntityRID:  "ri.ontology.main.object-type.ot-1",
			AfterState: json.RawMessage(`{"apiName":"building"}`),
		},
		{
			ID:          "chg-list-2",
			BranchID:    br.ID,
			ChangeType:  "MODIFIED",
			EntityType:  "property",
			EntityRID:   "ri.ontology.main.property.p-1",
			BeforeState: json.RawMessage(`{"displayName":"Old Name"}`),
			AfterState:  json.RawMessage(`{"displayName":"New Name"}`),
		},
		{
			ID:          "chg-list-3",
			BranchID:    br.ID,
			ChangeType:  "DELETED",
			EntityType:  "linkType",
			EntityRID:   "ri.ontology.main.link-type.lt-1",
			BeforeState: json.RawMessage(`{"apiName":"employeeToManager"}`),
		},
	}
	for i := range changes {
		if err := repo.CreateBranchChange(ctx, &changes[i]); err != nil {
			t.Fatalf("create change %d: %v", i, err)
		}
	}

	got, err := repo.ListBranchChanges(ctx, br.ID)
	if err != nil {
		t.Fatalf("ListBranchChanges failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(got))
	}

	// Verify change types
	typeMap := map[string]bool{}
	for _, c := range got {
		typeMap[c.ChangeType] = true
	}
	for _, ct := range []string{"ADDED", "MODIFIED", "DELETED"} {
		if !typeMap[ct] {
			t.Errorf("missing change type %s", ct)
		}
	}
}

func TestBranchChange_BeforeAfterState(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)
	br := seedBranch(t, repo, ont.RID)
	ctx := context.Background()

	c := &oms.BranchChange{
		ID:          "chg-state-1",
		BranchID:    br.ID,
		ChangeType:  "MODIFIED",
		EntityType:  "objectType",
		EntityRID:   "ri.ontology.main.object-type.ot-mod",
		BeforeState: json.RawMessage(`{"displayName":"Before"}`),
		AfterState:  json.RawMessage(`{"displayName":"After"}`),
	}
	if err := repo.CreateBranchChange(ctx, c); err != nil {
		t.Fatalf("create change: %v", err)
	}

	got, err := repo.ListBranchChanges(ctx, br.ID)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %d", len(got))
	}

	// Verify JSONB round-trip
	var before map[string]string
	if err := json.Unmarshal(got[0].BeforeState, &before); err != nil {
		t.Fatalf("unmarshal before: %v", err)
	}
	if before["displayName"] != "Before" {
		t.Errorf("before displayName = %q, want %q", before["displayName"], "Before")
	}

	var after map[string]string
	if err := json.Unmarshal(got[0].AfterState, &after); err != nil {
		t.Fatalf("unmarshal after: %v", err)
	}
	if after["displayName"] != "After" {
		t.Errorf("after displayName = %q, want %q", after["displayName"], "After")
	}
}

func TestBranchChange_NullBeforeState_ForAdded(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)
	br := seedBranch(t, repo, ont.RID)
	ctx := context.Background()

	c := &oms.BranchChange{
		ID:         "chg-null-1",
		BranchID:   br.ID,
		ChangeType: "ADDED",
		EntityType: "objectType",
		EntityRID:  "ri.ontology.main.object-type.new-2",
		AfterState: json.RawMessage(`{"apiName":"sensor"}`),
	}
	if err := repo.CreateBranchChange(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.ListBranchChanges(ctx, br.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if got[0].BeforeState != nil {
		t.Errorf("expected nil BeforeState for ADDED, got %s", string(got[0].BeforeState))
	}
}
