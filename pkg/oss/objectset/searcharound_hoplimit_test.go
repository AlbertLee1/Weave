package objectset_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// mkSearchAroundPath builds a searchAround Definition over a base employee set
// with n hops, each carrying a distinct non-empty link name.
func mkSearchAroundPath(n int) *objectset.Definition {
	steps := make([]objectset.PathStep, n)
	for i := range steps {
		steps[i] = objectset.PathStep{Link: fmt.Sprintf("link%d", i)}
	}
	return &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Path:      steps,
	}
}

// TestDefinition_ValidateSearchAround_HopLimit locks the Foundry-documented
// ceiling of 3 chained searchAround hops (docs/Palantir ObjectSet &
// OntologyAggregation 完整语法参考.md L97 / L226). A 3-hop path is valid; a
// 4-hop path is rejected at Definition.Validate so the request fails with a
// 400 instead of executing an over-deep chain.
func TestDefinition_ValidateSearchAround_HopLimit(t *testing.T) {
	for hops := 1; hops <= objectset.MaxSearchAroundHops; hops++ {
		if err := mkSearchAroundPath(hops).Validate(); err != nil {
			t.Errorf("%d-hop searchAround: unexpected error %v", hops, err)
		}
	}

	err := mkSearchAroundPath(objectset.MaxSearchAroundHops + 1).Validate()
	if err == nil {
		t.Fatalf("%d-hop searchAround: expected hop-limit rejection, got nil", objectset.MaxSearchAroundHops+1)
	}
	if !strings.Contains(err.Error(), "hop") {
		t.Errorf("error %q should mention the hop limit", err.Error())
	}
}

// TestBDD_SearchAround_FourHopPath_Rejected is the HTTP contract for the
// 3-hop searchAround ceiling.
//
//	Given a loadObjects request whose searchAround Path declares 4 hops
//	When  the handler runs
//	Then  it returns HTTP 400 with errorName "InvalidObjectSet" and a reason
//	      mentioning the hop limit — the over-deep chain never executes.
func TestBDD_SearchAround_FourHopPath_Rejected(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

	body := `{"objectSet":{"type":"searchAround","objectSet":{"type":"base","objectType":"employee"},` +
		`"path":[{"link":"a"},{"link":"b"},{"link":"c"},{"link":"d"}]},"select":["id"]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/myOntology/objectSets/loadObjects",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertExecutorSentinel(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT",
		"InvalidObjectSet", "hop")
}

// TestBDD_SearchAround_ThreeHopPath_Accepted guards the boundary: a 3-hop
// chain must still pass validation (it fails later only because the test
// fixture has no link resolver wired, which surfaces as a non-400 path).
func TestBDD_SearchAround_ThreeHopPath_Accepted(t *testing.T) {
	def := mkSearchAroundPath(3)
	if err := def.Validate(); err != nil {
		t.Fatalf("3-hop searchAround must validate, got %v", err)
	}
}
