package objectset_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeBranchScopeProvider is the in-memory test double for the US-381
// BranchScopeProvider hook. data maps a branch name to the authoritative
// PK list visible on that branch — the handler treats the response as a
// full replacement for the executor's livePKs, so tests can model
// branch-only adds (return PKs absent from livePKs), branch deletions
// (return a subset), or arbitrary substitutions. err short-circuits every
// call so the BranchScopeFailed branch is exercisable.
type fakeBranchScopeProvider struct {
	mu          sync.Mutex
	data        map[string][]string
	err         error
	notFound    map[string]bool
	calls       []branchScopeCall
	lastContext string
}

type branchScopeCall struct {
	Branch     string
	Ontology   string
	ObjectType string
	LivePKs    []string
}

func newFakeBranchScopeProvider() *fakeBranchScopeProvider {
	return &fakeBranchScopeProvider{
		data:     map[string][]string{},
		notFound: map[string]bool{},
	}
}

func (f *fakeBranchScopeProvider) ScopeObjectSet(ctx context.Context, branch, ontologyAPIName, objectTypeAPIName string, livePKs []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastContext = objectset.BranchScopeFromContext(ctx)
	pks := append([]string(nil), livePKs...)
	f.calls = append(f.calls, branchScopeCall{
		Branch:     branch,
		Ontology:   ontologyAPIName,
		ObjectType: objectTypeAPIName,
		LivePKs:    pks,
	})
	if f.err != nil {
		return nil, f.err
	}
	if f.notFound[branch] {
		return nil, objectset.ErrBranchNotFound
	}
	if scoped, ok := f.data[branch]; ok {
		out := append([]string(nil), scoped...)
		return out, nil
	}
	return pks, nil
}

// TestBranchScope_DefaultsToMain verifies that a request without `?branch=`
// resolves to DefaultBranch ("main") and the BranchScopeProvider is NOT
// consulted — the live executor result flows through unchanged. This is
// the strict pre-US-381 backwards-compatibility gate.
func TestBranchScope_DefaultsToMain(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	handler.SetBranchScopeProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(prov.calls) != 0 {
		t.Errorf("provider must not be consulted on the default-main path; got %d calls", len(prov.calls))
	}
	resp := decodeJSON[struct {
		Data       []map[string]interface{} `json:"data"`
		TotalCount string                   `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if resp.TotalCount != "2" {
		t.Errorf("totalCount = %q, want 2 (live executor result)", resp.TotalCount)
	}
}

// TestBranchScope_ExplicitMainBypassesProvider documents the symmetric
// behaviour for callers that explicitly pass `?branch=main`: the provider
// is still skipped because there is no overlay to apply.
func TestBranchScope_ExplicitMainBypassesProvider(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	prov.data["main"] = []string{"only-this-pk"} // would replace if consulted
	handler.SetBranchScopeProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=main",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(prov.calls) != 0 {
		t.Errorf("provider must not be consulted on the explicit-main path; got %d calls", len(prov.calls))
	}
	resp := decodeJSON[struct {
		TotalCount string `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if resp.TotalCount != "2" {
		t.Errorf("totalCount = %q, want 2 (live executor result, provider skipped)", resp.TotalCount)
	}
}

// TestBranchScope_NonMainWithoutProviderReturnsLookupUnavailable is the
// degraded-mode contract: a non-default branch with no provider wired
// returns a documented 400 instead of silently falling through to main
// (which would let a UI mistakenly believe its branch view succeeded).
func TestBranchScope_NonMainWithoutProviderReturnsLookupUnavailable(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	// no SetBranchScopeProvider — provider is nil

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=feature-x",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "BranchLookupUnavailable" {
		t.Errorf("errorName = %q, want BranchLookupUnavailable", apiErr.ErrorName)
	}
	if apiErr.Parameters["branch"] != "feature-x" {
		t.Errorf("parameters.branch = %q, want feature-x", apiErr.Parameters["branch"])
	}
}

// TestBranchScope_NonMainWithProviderRoutesScopedPKs covers the happy path:
// the provider receives the live PKs, returns a scoped replacement, and
// the response reflects exactly the provider's verdict.
func TestBranchScope_NonMainWithProviderRoutesScopedPKs(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	// Branch hides e2 — live executor returns [e1, e2], branch sees only e1.
	prov.data["feature-x"] = []string{"e1"}
	handler.SetBranchScopeProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=feature-x",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(prov.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(prov.calls))
	}
	got := prov.calls[0]
	if got.Branch != "feature-x" || got.Ontology != "myOntology" || got.ObjectType != "employee" {
		t.Errorf("provider call = %+v, want {Branch:feature-x, Ontology:myOntology, ObjectType:employee}", got)
	}
	livePKs := append([]string(nil), got.LivePKs...)
	if !pkSetEqual(livePKs, []string{"e1", "e2"}) {
		t.Errorf("provider livePKs = %v, want [e1, e2] in any order", livePKs)
	}
	if prov.lastContext != "feature-x" {
		t.Errorf("BranchScopeFromContext(ctx) = %q, want feature-x", prov.lastContext)
	}
	resp := decodeJSON[struct {
		Data       []map[string]interface{} `json:"data"`
		TotalCount string                   `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if resp.TotalCount != "1" {
		t.Errorf("totalCount = %q, want 1 (branch hides e2)", resp.TotalCount)
	}
	if len(resp.Data) != 1 || resp.Data[0]["__primaryKey"] != "e1" {
		t.Errorf("data = %v, want one row with __primaryKey=e1", resp.Data)
	}
}

// TestBranchScope_BranchOnlyAdditionsAreSurfacedAsPKs proves the
// "在 branch 上写入对象" half of the PRD's E2E gate: when the provider
// returns PKs that are NOT in the live set (objects written on the
// branch but absent from main), the handler surfaces them downstream.
// Bleve will return zero hits for the branch-only PK so it does not
// appear in `data`, but the totalCount reflects the provider's
// authoritative set — proving the wiring routes to the branch overlay.
func TestBranchScope_BranchOnlyAdditionsAreSurfacedAsPKs(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	// Branch sees both main objects PLUS a branch-only e3 that has no
	// Bleve presence. The handler should not crash on the missing PK.
	prov.data["feature-x"] = []string{"e1", "e2", "e3-branch-only"}
	handler.SetBranchScopeProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=feature-x",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[struct {
		Data       []map[string]interface{} `json:"data"`
		TotalCount string                   `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if resp.TotalCount != "3" {
		t.Errorf("totalCount = %q, want 3 (provider verdict includes branch-only e3)", resp.TotalCount)
	}
	if len(resp.Data) != 2 {
		t.Errorf("data len = %d, want 2 (e3-branch-only has no Bleve hit and is silently dropped)", len(resp.Data))
	}
}

// TestBranchScope_E2E_BranchWritesAreInvisibleOnMain is the PRD-specified
// "在 branch 上写入对象→main 上 load 看不到" acceptance gate: a single
// router, two requests; the branch view shows the overlay's verdict, the
// main view shows only what is in the live executor. Asserts the flag of
// the wiring story — the provider acts ONLY on the non-main path.
func TestBranchScope_E2E_BranchWritesAreInvisibleOnMain(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	// The branch represents a "write" that adds an extra object on top of
	// the live e1, e2. Main never consults the provider so the extra row
	// stays invisible to it.
	prov.data["feature-x"] = []string{"e1", "e2", "branch-only"}
	handler.SetBranchScopeProvider(prov)
	router := newAsOfRouter(t, handler)

	loadOn := func(t *testing.T, branchParam string) string {
		t.Helper()
		body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
		path := "/api/v2/ontologies/myOntology/objectSets/loadObjects"
		if branchParam != "" {
			path += "?branch=" + branchParam
		}
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
		}
		resp := decodeJSON[struct {
			TotalCount string `json:"totalCount"`
		}](t, rr.Body.Bytes())
		return resp.TotalCount
	}

	if got := loadOn(t, "feature-x"); got != "3" {
		t.Errorf("branch=feature-x totalCount = %q, want 3 (overlay surfaces branch-only)", got)
	}
	if got := loadOn(t, ""); got != "2" {
		t.Errorf("default branch totalCount = %q, want 2 (main does NOT see branch-only)", got)
	}
	if got := loadOn(t, "main"); got != "2" {
		t.Errorf("explicit branch=main totalCount = %q, want 2 (main does NOT see branch-only)", got)
	}
	// Provider was consulted exactly once — only on the non-main request.
	if len(prov.calls) != 1 {
		t.Errorf("provider consulted %d times, want 1 (only on the non-main load)", len(prov.calls))
	}
}

// TestBranchScope_BranchNotFoundFromProvider verifies the
// ErrBranchNotFound sentinel maps to a clean BranchNotFound 400 envelope
// rather than the generic BranchScopeFailed shape.
func TestBranchScope_BranchNotFoundFromProvider(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	prov.notFound["ghost"] = true
	handler.SetBranchScopeProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=ghost",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "BranchNotFound" {
		t.Errorf("errorName = %q, want BranchNotFound", apiErr.ErrorName)
	}
	if apiErr.Parameters["branch"] != "ghost" {
		t.Errorf("parameters.branch = %q, want ghost", apiErr.Parameters["branch"])
	}
}

// TestBranchScope_PropagatesProviderError forces an unrecognised provider
// error and asserts it surfaces as the generic BranchScopeFailed envelope
// so configuration mistakes stay visible without leaking the 404 code.
func TestBranchScope_PropagatesProviderError(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	prov.err = errors.New("overlay store unreachable")
	handler.SetBranchScopeProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=feature-x",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "BranchScopeFailed" {
		t.Errorf("errorName = %q, want BranchScopeFailed", apiErr.ErrorName)
	}
	if !strings.Contains(apiErr.Parameters["error"], "overlay store unreachable") {
		t.Errorf("parameters.error = %q, want it to mention overlay store unreachable", apiErr.Parameters["error"])
	}
}

// TestBranchScope_RejectsWhitespaceBranch verifies the resolveBranch
// validator catches sloppy clients with a leading/trailing space rather
// than silently trimming. Branch identifiers are user-visible labels;
// whitespace is almost always a client bug.
func TestBranchScope_RejectsWhitespaceBranch(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	handler.SetBranchScopeProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=%20feature-x%20",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "InvalidBranch" {
		t.Errorf("errorName = %q, want InvalidBranch", apiErr.ErrorName)
	}
	if len(prov.calls) != 0 {
		t.Errorf("provider must not be consulted on a malformed branch; got %d calls", len(prov.calls))
	}
}

// TestBranchScope_RejectsOverlongBranch enforces the 128-char audit-log
// bound on the branch identifier so a runaway client cannot push huge
// labels into downstream telemetry.
func TestBranchScope_RejectsOverlongBranch(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	prov := newFakeBranchScopeProvider()
	handler.SetBranchScopeProvider(prov)

	huge := strings.Repeat("a", 200)
	body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch="+huge,
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "InvalidBranch" {
		t.Errorf("errorName = %q, want InvalidBranch", apiErr.ErrorName)
	}
}

// TestBranchScope_AsOf_BranchPostFiltersSnapshots covers the asOf+branch
// combination from the PRD wire example "?branch=feature-x&asOf=tx-123":
// the snapshot provider returns the full asOf history, then the branch
// scope provider is consulted as a post-filter. PKs the branch hides are
// removed from the response; PKs the branch adds but the snapshot lacks
// are silently dropped.
func TestBranchScope_AsOf_BranchPostFiltersSnapshots(t *testing.T) {
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	prov := newFakeSnapshotProvider()
	prov.asOfData["2026-01-15T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
		{PrimaryKey: "emp-2", Properties: map[string]interface{}{"name": "Bob"}},
		{PrimaryKey: "emp-3", Properties: map[string]interface{}{"name": "Carol"}},
	}
	h.SetHistorySnapshotProvider(prov)
	branchProv := newFakeBranchScopeProvider()
	branchProv.data["feature-x"] = []string{"emp-1", "emp-3"} // hides emp-2
	h.SetBranchScopeProvider(branchProv)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?branch=feature-x&asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(branchProv.calls) != 1 {
		t.Fatalf("branchProv calls = %d, want 1", len(branchProv.calls))
	}
	livePKs := append([]string(nil), branchProv.calls[0].LivePKs...)
	if !pkSetEqual(livePKs, []string{"emp-1", "emp-2", "emp-3"}) {
		t.Errorf("branchProv livePKs = %v, want [emp-1, emp-2, emp-3] in any order", livePKs)
	}
	resp := decodeJSON[struct {
		Data       []map[string]interface{} `json:"data"`
		TotalCount string                   `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if resp.TotalCount != "2" {
		t.Errorf("totalCount = %q, want 2 (branch hides emp-2)", resp.TotalCount)
	}
	gotPKs := make([]string, 0, len(resp.Data))
	for _, row := range resp.Data {
		if pk, ok := row["__primaryKey"].(string); ok {
			gotPKs = append(gotPKs, pk)
		}
	}
	if !pkSetEqual(gotPKs, []string{"emp-1", "emp-3"}) {
		t.Errorf("data PKs = %v, want [emp-1, emp-3] in any order", gotPKs)
	}
}

// TestBranchScope_AsOf_DefaultMainSkipsBranchProvider ensures the asOf
// path matches the live path's default-main contract: provider is not
// consulted when the request omits `?branch=` (or sets it to "main").
func TestBranchScope_AsOf_DefaultMainSkipsBranchProvider(t *testing.T) {
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	prov := newFakeSnapshotProvider()
	prov.asOfData["2026-01-15T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
		{PrimaryKey: "emp-2", Properties: map[string]interface{}{"name": "Bob"}},
	}
	h.SetHistorySnapshotProvider(prov)
	branchProv := newFakeBranchScopeProvider()
	branchProv.data["main"] = []string{"would-replace"} // would replace if consulted
	h.SetBranchScopeProvider(branchProv)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(branchProv.calls) != 0 {
		t.Errorf("branch provider must not be consulted on the default-main asOf path; got %d calls", len(branchProv.calls))
	}
	resp := decodeJSON[struct {
		TotalCount string `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if resp.TotalCount != "2" {
		t.Errorf("totalCount = %q, want 2 (snapshot returns 2; provider skipped)", resp.TotalCount)
	}
}

// TestBranchScope_AsOf_NonMainWithoutProviderReturnsLookupUnavailable
// closes the asOf-path symmetry: requesting a non-default branch with no
// provider wired must surface the same documented 400 as the live path
// rather than silently degrading to the main asOf snapshot.
func TestBranchScope_AsOf_NonMainWithoutProviderReturnsLookupUnavailable(t *testing.T) {
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	prov := newFakeSnapshotProvider()
	prov.asOfData["2026-01-15T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
	}
	h.SetHistorySnapshotProvider(prov)
	// no SetBranchScopeProvider — provider is nil

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?branch=feature-x&asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "BranchLookupUnavailable" {
		t.Errorf("errorName = %q, want BranchLookupUnavailable", apiErr.ErrorName)
	}
	if len(prov.calls) != 0 {
		t.Errorf("history snapshot provider must not be called when branch lookup fails upfront; got %d", len(prov.calls))
	}
}

// TestBranchScope_ContextHelpers covers the WithBranchScope /
// BranchScopeFromContext round-trip directly so independent callers can
// rely on the documented defaults without inspecting handler internals.
func TestBranchScope_ContextHelpers(t *testing.T) {
	ctx := context.Background()
	if got := objectset.BranchScopeFromContext(ctx); got != objectset.DefaultBranch {
		t.Errorf("BranchScopeFromContext(empty) = %q, want %q", got, objectset.DefaultBranch)
	}
	ctx = objectset.WithBranchScope(ctx, "main")
	if got := objectset.BranchScopeFromContext(ctx); got != objectset.DefaultBranch {
		t.Errorf("BranchScopeFromContext(main) = %q, want %q (no-op)", got, objectset.DefaultBranch)
	}
	ctx = objectset.WithBranchScope(ctx, "feature-x")
	if got := objectset.BranchScopeFromContext(ctx); got != "feature-x" {
		t.Errorf("BranchScopeFromContext(feature-x) = %q, want feature-x", got)
	}
	ctx = objectset.WithBranchScope(ctx, "")
	if got := objectset.BranchScopeFromContext(ctx); got != "feature-x" {
		t.Errorf("WithBranchScope(empty) must be a no-op; got %q after empty stamp", got)
	}
}

// pkSetEqual compares two PK slices independent of order.
func pkSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, pk := range a {
		set[pk]++
	}
	for _, pk := range b {
		set[pk]--
	}
	for _, n := range set {
		if n != 0 {
			return false
		}
	}
	return true
}
