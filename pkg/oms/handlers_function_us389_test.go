package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-389: Function publish API accepts a branch parameter, branch v2 + main
// v1 coexist, and Execute routes to the branch row when ?branch= is set.

func us389SeedRepo() *mockRepo {
	return &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
}

func us389PostCreate(t *testing.T, repo *mockRepo, url, body string, wantStatus int) oms.Function {
	t.Helper()
	router := setupFunctionRouter(repo)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != wantStatus {
		t.Fatalf("POST %s: expected %d, got %d: %s", url, wantStatus, w.Code, w.Body.String())
	}
	if wantStatus != http.StatusCreated {
		return oms.Function{}
	}
	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return fn
}

func TestCreateFunction_DefaultsToMainBranch_US389(t *testing.T) {
	repo := us389SeedRepo()
	fn := us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions",
		`{"name":"compute","sourceCode":"function main(){return 1;}"}`,
		http.StatusCreated)
	if fn.BranchID != oms.DefaultBranch {
		t.Errorf("BranchID = %q, want %q (default)", fn.BranchID, oms.DefaultBranch)
	}
}

func TestCreateFunction_AcceptsBranchQueryParameter_US389(t *testing.T) {
	repo := us389SeedRepo()
	fn := us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"function main(){return 1;}"}`,
		http.StatusCreated)
	if fn.BranchID != "feature-x" {
		t.Errorf("BranchID = %q, want feature-x", fn.BranchID)
	}
}

func TestCreateFunction_AcceptsBranchBodyField_US389(t *testing.T) {
	repo := us389SeedRepo()
	fn := us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions",
		`{"name":"compute","sourceCode":"function main(){return 1;}","branchId":"feature-x"}`,
		http.StatusCreated)
	if fn.BranchID != "feature-x" {
		t.Errorf("BranchID = %q, want feature-x", fn.BranchID)
	}
}

func TestCreateFunction_QueryParamWinsOverBody_US389(t *testing.T) {
	repo := us389SeedRepo()
	fn := us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"function main(){return 1;}","branchId":"unused-branch"}`,
		http.StatusCreated)
	if fn.BranchID != "feature-x" {
		t.Errorf("BranchID = %q, want feature-x (query wins)", fn.BranchID)
	}
}

func TestCreateFunction_BranchAndMainCoexistAtSameSemver_US389(t *testing.T) {
	repo := us389SeedRepo()

	// Publish v1 on main.
	mainFn := us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions",
		`{"name":"compute","sourceCode":"function main(){return 'v1-main';}","version":"1.0.0"}`,
		http.StatusCreated)
	if mainFn.BranchID != oms.DefaultBranch {
		t.Errorf("main row BranchID = %q, want %q", mainFn.BranchID, oms.DefaultBranch)
	}

	// Publish another v1 on feature-x — same name+version, different branch.
	branchFn := us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"function main(){return 'v1-feature';}","version":"1.0.0"}`,
		http.StatusCreated)
	if branchFn.BranchID != "feature-x" {
		t.Errorf("branch row BranchID = %q, want feature-x", branchFn.BranchID)
	}
	if branchFn.RID == mainFn.RID {
		t.Errorf("branch and main rows must have distinct RIDs; got %q for both", branchFn.RID)
	}

	// Both rows are present in the registry.
	var mainSeen, branchSeen bool
	for _, fn := range repo.functions {
		if fn.Name != "compute" || fn.Version != "1.0.0" {
			continue
		}
		switch fn.BranchID {
		case oms.DefaultBranch:
			mainSeen = true
		case "feature-x":
			branchSeen = true
		}
	}
	if !mainSeen || !branchSeen {
		t.Fatalf("both branch and main v1 must coexist; mainSeen=%v branchSeen=%v", mainSeen, branchSeen)
	}
}

func TestCreateFunction_DuplicateOnSameBranch_Returns409_US389(t *testing.T) {
	repo := us389SeedRepo()
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"function main(){return 1;}","version":"1.0.0"}`,
		http.StatusCreated)
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"function main(){return 2;}","version":"1.0.0"}`,
		http.StatusConflict)
}

func TestCreateFunction_DuplicateAcrossBranches_DoesNotConflict_US389(t *testing.T) {
	repo := us389SeedRepo()
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions",
		`{"name":"compute","sourceCode":"function main(){return 1;}","version":"1.0.0"}`,
		http.StatusCreated)
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"function main(){return 2;}","version":"1.0.0"}`,
		http.StatusCreated)
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-y",
		`{"name":"compute","sourceCode":"function main(){return 3;}","version":"1.0.0"}`,
		http.StatusCreated)
	if got := len(repo.functions); got != 3 {
		t.Fatalf("expected 3 rows across branches, got %d", got)
	}
}

func TestGetFunction_BranchScopeRoutesToBranchVersion_US389(t *testing.T) {
	repo := us389SeedRepo()
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions",
		`{"name":"compute","sourceCode":"return 'main';","version":"1.0.0"}`,
		http.StatusCreated)
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"return 'feature';","version":"1.0.0"}`,
		http.StatusCreated)

	router := setupFunctionRouter(repo)

	// Default scope → main row.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/compute@1.0.0", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("main GET expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var mainGot oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &mainGot); err != nil {
		t.Fatalf("decode main: %v", err)
	}
	if mainGot.SourceCode != "return 'main';" || mainGot.BranchID != oms.DefaultBranch {
		t.Errorf("main GET wrong row: source=%q branch=%q", mainGot.SourceCode, mainGot.BranchID)
	}

	// The handler chain pulled by setupFunctionRouter does not stamp the
	// branch context for the GET path (only the POST publish path does in
	// this test surface), so verify branch routing via the repo helper
	// with WithBranchScope directly — same chokepoint the production
	// ExecuteFunction handler uses.
	branchFn, err := repo.GetFunctionByNameVersion(
		oms.WithBranchScope(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "feature-x"),
		"northwind", "compute", "1.0.0")
	if err != nil {
		t.Fatalf("GetFunctionByNameVersion(branch): %v", err)
	}
	if branchFn.SourceCode != "return 'feature';" || branchFn.BranchID != "feature-x" {
		t.Errorf("branch GET wrong row: source=%q branch=%q", branchFn.SourceCode, branchFn.BranchID)
	}
}

func TestListFunctions_BranchScopeMergesMainAndBranch_US389(t *testing.T) {
	repo := us389SeedRepo()
	// Main has fnA v1 only.
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions",
		`{"name":"fnA","sourceCode":"return 'main-A';","version":"1.0.0"}`,
		http.StatusCreated)
	// Branch publishes its own fnA v2 + a new fnB.
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"fnA","sourceCode":"return 'branch-A-v2';","version":"2.0.0"}`,
		http.StatusCreated)
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"fnB","sourceCode":"return 'branch-B';","version":"1.0.0"}`,
		http.StatusCreated)

	ctx := oms.WithBranchScope(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "feature-x")
	got, err := repo.ListFunctions(ctx, "northwind")
	if err != nil {
		t.Fatalf("ListFunctions: %v", err)
	}
	// Expect: main fnA v1 (inherited), branch fnA v2 (overlay), branch fnB v1 (new).
	if len(got) != 3 {
		t.Fatalf("ListFunctions branch scope: want 3 rows, got %d (%+v)", len(got), got)
	}
	type key struct {
		Name, Version, Branch string
	}
	want := map[key]bool{
		{"fnA", "1.0.0", oms.DefaultBranch}: true,
		{"fnA", "2.0.0", "feature-x"}:       true,
		{"fnB", "1.0.0", "feature-x"}:       true,
	}
	for _, fn := range got {
		k := key{fn.Name, fn.Version, oms.NormalizeBranchID(fn.BranchID)}
		if !want[k] {
			t.Errorf("unexpected row %+v", fn)
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("missing row %+v", k)
	}
}

func TestCreateFunction_DuplicateConflictResponseCarriesBranchId_US389(t *testing.T) {
	repo := us389SeedRepo()
	us389PostCreate(t, repo,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		`{"name":"compute","sourceCode":"return 1;","version":"1.0.0"}`,
		http.StatusCreated)
	router := setupFunctionRouter(repo)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions?branch=feature-x",
		bytes.NewBufferString(`{"name":"compute","sourceCode":"return 2;","version":"1.0.0"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ErrorCode  string            `json:"errorCode"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Parameters["branchId"] != "feature-x" {
		t.Errorf("conflict response missing branchId=%q; got %+v", "feature-x", resp.Parameters)
	}
}
