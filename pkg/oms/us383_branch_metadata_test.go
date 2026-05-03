package oms

import (
	"context"
	"errors"
	"testing"
)

// US-383 Ontology Branch metadata: parent_branch fallback, base_tx, status aliases.

func TestNormalizeBranchStatus(t *testing.T) {
	cases := map[string]string{
		"open":      "open",
		"merged":    "merged",
		"closed":    "closed",
		"ACTIVE":    "open",
		"MERGED":    "merged",
		"ABANDONED": "closed",
		"UNKNOWN":   "UNKNOWN", // pass-through; CHECK constraint surfaces violation
		"":          "",
	}
	for in, want := range cases {
		got := NormalizeBranchStatus(in)
		if got != want {
			t.Errorf("NormalizeBranchStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// chainRepo extends inMemoryRepo with branch lookup so
// NewBranchedRepositoryChain can resolve the parent chain.
type chainRepo struct {
	inMemoryRepo
	branches []OntologyBranch
}

func (r *chainRepo) GetBranch(_ context.Context, id string) (*OntologyBranch, error) {
	for i := range r.branches {
		if r.branches[i].ID == id {
			return &r.branches[i], nil
		}
	}
	return nil, ErrNotFound
}

func TestNewBranchedRepositoryChain_EmptyBranchID(t *testing.T) {
	base := &chainRepo{}
	repo, err := NewBranchedRepositoryChain(context.Background(), base, "")
	if err != nil {
		t.Fatal(err)
	}
	if repo != base {
		t.Error("expected empty branchID to return the unwrapped base")
	}
}

func TestNewBranchedRepositoryChain_LeafBranchMissing(t *testing.T) {
	base := &chainRepo{}
	repo, err := NewBranchedRepositoryChain(context.Background(), base, "br-missing")
	if err != nil {
		t.Fatal(err)
	}
	if repo != base {
		t.Error("expected missing leaf branch to fall through to base")
	}
}

func TestNewBranchedRepositoryChain_NoParent_AppliesOverlay(t *testing.T) {
	base := &chainRepo{
		inMemoryRepo: inMemoryRepo{
			ontologies: []Ontology{{RID: "ont-1", APIName: "test"}},
			objectTypes: []ObjectType{
				{RID: "ot-main", OntologyRID: "ont-1", APIName: "main_type"},
			},
			branchChanges: []BranchChange{
				makeBranchChange("br-leaf", "ADDED", "objectType", "ot-leaf",
					nil,
					ObjectType{RID: "ot-leaf", OntologyRID: "ont-1", APIName: "leaf_type"}),
			},
		},
		branches: []OntologyBranch{
			{ID: "br-leaf", OntologyRID: "ont-1", Name: "leaf", Status: "open"},
		},
	}

	repo, err := NewBranchedRepositoryChain(context.Background(), base, "br-leaf")
	if err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 (main + leaf-added), got %d", len(list))
	}
}

// TestNewBranchedRepositoryChain_ParentFallback verifies the core US-383
// requirement: a leaf branch sees its parent's overlay even when the leaf
// itself does not redeclare an entity.
func TestNewBranchedRepositoryChain_ParentFallback(t *testing.T) {
	base := &chainRepo{
		inMemoryRepo: inMemoryRepo{
			ontologies: []Ontology{{RID: "ont-1", APIName: "test"}},
			objectTypes: []ObjectType{
				{RID: "ot-main", OntologyRID: "ont-1", APIName: "main_type"},
			},
			branchChanges: []BranchChange{
				// Parent adds parent_type
				makeBranchChange("br-parent", "ADDED", "objectType", "ot-parent",
					nil,
					ObjectType{RID: "ot-parent", OntologyRID: "ont-1", APIName: "parent_type"}),
				// Leaf adds leaf_type but does not touch parent_type
				makeBranchChange("br-leaf", "ADDED", "objectType", "ot-leaf",
					nil,
					ObjectType{RID: "ot-leaf", OntologyRID: "ont-1", APIName: "leaf_type"}),
			},
		},
		branches: []OntologyBranch{
			{ID: "br-parent", OntologyRID: "ont-1", Name: "parent", Status: "open"},
			{ID: "br-leaf", OntologyRID: "ont-1", Name: "leaf", Status: "open", ParentBranchID: "br-parent"},
		},
	}

	repo, err := NewBranchedRepositoryChain(context.Background(), base, "br-leaf")
	if err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	apiNames := map[string]bool{}
	for _, ot := range list {
		apiNames[ot.APIName] = true
	}
	for _, want := range []string{"main_type", "parent_type", "leaf_type"} {
		if !apiNames[want] {
			t.Errorf("expected branch view to include %q (parent+leaf overlay), got %v", want, apiNames)
		}
	}
}

// TestNewBranchedRepositoryChain_LeafOverridesParent verifies that the leaf
// overlay wins over the parent's overlay when both touch the same entity.
func TestNewBranchedRepositoryChain_LeafOverridesParent(t *testing.T) {
	base := &chainRepo{
		inMemoryRepo: inMemoryRepo{
			ontologies: []Ontology{{RID: "ont-1", APIName: "test"}},
			objectTypes: []ObjectType{
				{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee"},
			},
			branchChanges: []BranchChange{
				makeBranchChange("br-parent", "MODIFIED", "objectType", "ot-1",
					ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Employee"},
					ObjectType{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Parent Edit"}),
				makeBranchChange("br-leaf", "MODIFIED", "objectType", "ot-1",
					ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Parent Edit"},
					ObjectType{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Leaf Edit"}),
			},
		},
		branches: []OntologyBranch{
			{ID: "br-parent", OntologyRID: "ont-1", Name: "parent", Status: "open"},
			{ID: "br-leaf", OntologyRID: "ont-1", Name: "leaf", Status: "open", ParentBranchID: "br-parent"},
		},
	}

	repo, err := NewBranchedRepositoryChain(context.Background(), base, "br-leaf")
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetObjectTypeByAPIName(context.Background(), "ont-1", "employee")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Leaf Edit" {
		t.Errorf("DisplayName = %q, want %q (leaf must win over parent)", got.DisplayName, "Leaf Edit")
	}
}

// TestNewBranchedRepositoryChain_ParentMissingTerminatesGracefully ensures the
// chain walker tolerates a parent_branch_id that no longer resolves (e.g. the
// parent branch row was hard-deleted) — the leaf overlay still applies.
func TestNewBranchedRepositoryChain_ParentMissingTerminatesGracefully(t *testing.T) {
	base := &chainRepo{
		inMemoryRepo: inMemoryRepo{
			ontologies:  []Ontology{{RID: "ont-1", APIName: "test"}},
			objectTypes: []ObjectType{},
			branchChanges: []BranchChange{
				makeBranchChange("br-leaf", "ADDED", "objectType", "ot-leaf",
					nil,
					ObjectType{RID: "ot-leaf", OntologyRID: "ont-1", APIName: "leaf_type"}),
			},
		},
		branches: []OntologyBranch{
			{ID: "br-leaf", OntologyRID: "ont-1", Name: "leaf", Status: "open", ParentBranchID: "br-deleted"},
		},
	}

	repo, err := NewBranchedRepositoryChain(context.Background(), base, "br-leaf")
	if err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].APIName != "leaf_type" {
		t.Errorf("expected leaf overlay despite missing parent, got %d entries", len(list))
	}
}

// TestNewBranchedRepositoryChain_CycleSafe ensures a cyclic parent chain (which
// the API forbids but the schema permits) does not loop forever.
func TestNewBranchedRepositoryChain_CycleSafe(t *testing.T) {
	base := &chainRepo{
		inMemoryRepo: inMemoryRepo{
			ontologies: []Ontology{{RID: "ont-1", APIName: "test"}},
		},
		branches: []OntologyBranch{
			{ID: "br-a", OntologyRID: "ont-1", Name: "a", Status: "open", ParentBranchID: "br-b"},
			{ID: "br-b", OntologyRID: "ont-1", Name: "b", Status: "open", ParentBranchID: "br-a"},
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := NewBranchedRepositoryChain(context.Background(), base, "br-a")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cycle handling produced error: %v", err)
		}
	}
}

// TestNewBranchedRepositoryChain_PropagatesGetError verifies that non-NotFound
// errors from GetBranch surface unchanged (rather than being collapsed).
func TestNewBranchedRepositoryChain_PropagatesGetError(t *testing.T) {
	want := errors.New("transient db error")
	base := &errBranchRepo{err: want}

	_, err := NewBranchedRepositoryChain(context.Background(), base, "br-leaf")
	if !errors.Is(err, want) {
		t.Fatalf("expected GetBranch error to propagate, got %v", err)
	}
}

type errBranchRepo struct {
	chainRepo
	err error
}

func (r *errBranchRepo) GetBranch(_ context.Context, _ string) (*OntologyBranch, error) {
	return nil, r.err
}
