//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-384: branch_id column on action_types and functions enables an
// independent (apiName / name@version) row per branch. The integration
// tests below seed paired main-vs-branch rows then verify that the
// repository read methods route to the branch-scoped row when the
// request context carries a branch scope, and fall back to main
// otherwise.

func seedOntologyForUS384(t *testing.T, repo *oms.PGRepository) *oms.Ontology {
	t.Helper()
	o := &oms.Ontology{
		RID:         "ri.ontology.main.ontology.us384",
		APIName:     "us384-ontology",
		DisplayName: "US-384 Ontology",
	}
	if err := repo.CreateOntology(context.Background(), o); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}
	return o
}

func TestUS384_ActionType_BranchAndMainCoexist(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	ont := seedOntologyForUS384(t, repo)

	main := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.us384-main",
		OntologyRID: ont.RID,
		APIName:     "createOrder",
		DisplayName: "Create Order (main)",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[]`),
		Rules:       json.RawMessage(`[]`),
		BranchID:    "main",
	}
	if err := repo.CreateActionType(ctx, main); err != nil {
		t.Fatalf("create main: %v", err)
	}

	branch := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.us384-branch",
		OntologyRID: ont.RID,
		APIName:     "createOrder",
		DisplayName: "Create Order (feature-x)",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[]`),
		Rules:       json.RawMessage(`[]`),
		BranchID:    "feature-x",
	}
	if err := repo.CreateActionType(ctx, branch); err != nil {
		t.Fatalf("create feature row: %v", err)
	}

	mainCtx := ctx
	got, err := repo.GetActionTypeByAPIName(mainCtx, ont.RID, "createOrder")
	if err != nil {
		t.Fatalf("GetActionTypeByAPIName(main): %v", err)
	}
	if got.RID != main.RID {
		t.Errorf("main lookup picked %q, want %q", got.RID, main.RID)
	}
	if got.BranchID != "main" {
		t.Errorf("main row BranchID = %q, want main", got.BranchID)
	}

	branchCtx := oms.WithBranchScope(ctx, "feature-x")
	got, err = repo.GetActionTypeByAPIName(branchCtx, ont.RID, "createOrder")
	if err != nil {
		t.Fatalf("GetActionTypeByAPIName(branch): %v", err)
	}
	if got.RID != branch.RID {
		t.Errorf("branch lookup picked %q, want %q", got.RID, branch.RID)
	}
	if got.BranchID != "feature-x" {
		t.Errorf("branch row BranchID = %q, want feature-x", got.BranchID)
	}
}

func TestUS384_ActionType_BranchFallbackToMain(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	ont := seedOntologyForUS384(t, repo)

	mainOnly := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.us384-only-main",
		OntologyRID: ont.RID,
		APIName:     "shipOrder",
		DisplayName: "Ship Order",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[]`),
		Rules:       json.RawMessage(`[]`),
	}
	if err := repo.CreateActionType(ctx, mainOnly); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetActionTypeByAPIName(oms.WithBranchScope(ctx, "feature-x"), ont.RID, "shipOrder")
	if err != nil {
		t.Fatalf("GetActionTypeByAPIName(branch fallback): %v", err)
	}
	if got.RID != mainOnly.RID {
		t.Errorf("branch with no override should fall back to main; got %q", got.RID)
	}
}

func TestUS384_ListActionTypes_BranchOverlay(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	ont := seedOntologyForUS384(t, repo)

	mainA := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.us384-list-a-main",
		OntologyRID: ont.RID,
		APIName:     "approveLoan",
		DisplayName: "Approve Loan (main)",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[]`),
		Rules:       json.RawMessage(`[]`),
	}
	if err := repo.CreateActionType(ctx, mainA); err != nil {
		t.Fatalf("create main A: %v", err)
	}
	mainB := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.us384-list-b-main",
		OntologyRID: ont.RID,
		APIName:     "rejectLoan",
		DisplayName: "Reject Loan",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[]`),
		Rules:       json.RawMessage(`[]`),
	}
	if err := repo.CreateActionType(ctx, mainB); err != nil {
		t.Fatalf("create main B: %v", err)
	}
	branchA := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.us384-list-a-branch",
		OntologyRID: ont.RID,
		APIName:     "approveLoan",
		DisplayName: "Approve Loan (branch)",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[]`),
		Rules:       json.RawMessage(`[]`),
		BranchID:    "feature-x",
	}
	if err := repo.CreateActionType(ctx, branchA); err != nil {
		t.Fatalf("create branch A: %v", err)
	}

	mainList, err := repo.ListActionTypes(ctx, ont.RID)
	if err != nil {
		t.Fatalf("ListActionTypes(main): %v", err)
	}
	mainKeys := map[string]string{}
	for _, at := range mainList {
		mainKeys[at.APIName] = at.RID
	}
	if mainKeys["approveLoan"] != mainA.RID {
		t.Errorf("main list approveLoan = %q, want main RID %q", mainKeys["approveLoan"], mainA.RID)
	}
	if mainKeys["rejectLoan"] != mainB.RID {
		t.Errorf("main list rejectLoan = %q, want %q", mainKeys["rejectLoan"], mainB.RID)
	}

	branchList, err := repo.ListActionTypes(oms.WithBranchScope(ctx, "feature-x"), ont.RID)
	if err != nil {
		t.Fatalf("ListActionTypes(branch): %v", err)
	}
	branchKeys := map[string]string{}
	for _, at := range branchList {
		branchKeys[at.APIName] = at.RID
	}
	if branchKeys["approveLoan"] != branchA.RID {
		t.Errorf("branch list approveLoan = %q, want branch RID %q", branchKeys["approveLoan"], branchA.RID)
	}
	if branchKeys["rejectLoan"] != mainB.RID {
		t.Errorf("branch list rejectLoan = %q, want main RID %q (no override)", branchKeys["rejectLoan"], mainB.RID)
	}
}

func TestUS384_Function_BranchAndMainCoexist(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	ont := seedOntologyForUS384(t, repo)

	mainV1 := &oms.Function{
		RID:         "ri.functions.main.function.us384-main-v1",
		OntologyRID: ont.RID,
		Name:        "compute",
		Version:     "1.0.0",
		SourceCode:  "function compute(){ return 1 }",
	}
	if err := repo.CreateFunction(ctx, mainV1); err != nil {
		t.Fatalf("create main v1: %v", err)
	}
	branchV2 := &oms.Function{
		RID:         "ri.functions.main.function.us384-branch-v2",
		OntologyRID: ont.RID,
		Name:        "compute",
		Version:     "2.0.0",
		SourceCode:  "function compute(){ return 2 }",
		BranchID:    "feature-x",
	}
	if err := repo.CreateFunction(ctx, branchV2); err != nil {
		t.Fatalf("create branch v2: %v", err)
	}

	mainGot, err := repo.GetFunctionByName(ctx, ont.RID, "compute")
	if err != nil {
		t.Fatalf("GetFunctionByName(main): %v", err)
	}
	if mainGot.RID != mainV1.RID {
		t.Errorf("main GetFunctionByName picked %q, want %q", mainGot.RID, mainV1.RID)
	}
	if mainGot.Version != "1.0.0" {
		t.Errorf("main version = %q, want 1.0.0", mainGot.Version)
	}

	branchCtx := oms.WithBranchScope(ctx, "feature-x")
	branchGot, err := repo.GetFunctionByName(branchCtx, ont.RID, "compute")
	if err != nil {
		t.Fatalf("GetFunctionByName(branch): %v", err)
	}
	if branchGot.RID != branchV2.RID {
		t.Errorf("branch GetFunctionByName picked %q, want %q", branchGot.RID, branchV2.RID)
	}
	if branchGot.Version != "2.0.0" {
		t.Errorf("branch version = %q, want 2.0.0", branchGot.Version)
	}

	branchPinned, err := repo.GetFunctionByNameVersion(branchCtx, ont.RID, "compute", "2.0.0")
	if err != nil {
		t.Fatalf("GetFunctionByNameVersion(branch): %v", err)
	}
	if branchPinned.RID != branchV2.RID {
		t.Errorf("branch pinned RID = %q, want %q", branchPinned.RID, branchV2.RID)
	}
}

func TestUS384_Function_BranchFallback_KeepsMainVersionWhenNotOverridden(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	ont := seedOntologyForUS384(t, repo)

	mainV1 := &oms.Function{
		RID:         "ri.functions.main.function.us384-fallback-main",
		OntologyRID: ont.RID,
		Name:        "calc",
		Version:     "1.0.0",
		SourceCode:  "function calc(){ return 'main-1' }",
	}
	if err := repo.CreateFunction(ctx, mainV1); err != nil {
		t.Fatalf("create main v1: %v", err)
	}

	branchCtx := oms.WithBranchScope(ctx, "feature-x")
	got, err := repo.GetFunctionByName(branchCtx, ont.RID, "calc")
	if err != nil {
		t.Fatalf("GetFunctionByName(branch): %v", err)
	}
	if got.RID != mainV1.RID {
		t.Errorf("branch with no override should fall back to main; got %q", got.RID)
	}
}

func TestUS384_Function_ListFunctionVersions_PrefersBranchOnSemverCollision(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	ont := seedOntologyForUS384(t, repo)

	mainV1 := &oms.Function{
		RID:         "ri.functions.main.function.us384-list-main-1",
		OntologyRID: ont.RID,
		Name:        "rate",
		Version:     "1.0.0",
		SourceCode:  "function rate(){ return 1 }",
	}
	if err := repo.CreateFunction(ctx, mainV1); err != nil {
		t.Fatalf("create main v1: %v", err)
	}
	mainV2 := &oms.Function{
		RID:         "ri.functions.main.function.us384-list-main-2",
		OntologyRID: ont.RID,
		Name:        "rate",
		Version:     "2.0.0",
		SourceCode:  "function rate(){ return 2 }",
	}
	if err := repo.CreateFunction(ctx, mainV2); err != nil {
		t.Fatalf("create main v2: %v", err)
	}
	branchV2 := &oms.Function{
		RID:         "ri.functions.main.function.us384-list-branch-2",
		OntologyRID: ont.RID,
		Name:        "rate",
		Version:     "2.0.0",
		SourceCode:  "function rate(){ return 'branch-2' }",
		BranchID:    "feature-x",
	}
	if err := repo.CreateFunction(ctx, branchV2); err != nil {
		t.Fatalf("create branch v2: %v", err)
	}

	branchCtx := oms.WithBranchScope(ctx, "feature-x")
	versions, err := repo.ListFunctionVersionsByName(branchCtx, ont.RID, "rate")
	if err != nil {
		t.Fatalf("ListFunctionVersionsByName(branch): %v", err)
	}

	if len(versions) != 2 {
		t.Fatalf("expected 2 versions (1.0.0 main, 2.0.0 branch override), got %d: %#v", len(versions), versions)
	}
	rids := map[string]string{}
	for _, fn := range versions {
		rids[fn.Version] = fn.RID
	}
	if rids["1.0.0"] != mainV1.RID {
		t.Errorf("1.0.0 RID = %q, want main %q", rids["1.0.0"], mainV1.RID)
	}
	if rids["2.0.0"] != branchV2.RID {
		t.Errorf("2.0.0 RID = %q, want branch %q", rids["2.0.0"], branchV2.RID)
	}
}
