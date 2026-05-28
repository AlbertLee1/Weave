package funcregistry_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_Functions_ListVersionsByName covers the round-73 fix
// for an internal-only dead-code gap in pkg/vertex/funcregistry.
// The FunctionLookup interface already exposes
// ListFunctionVersionsByName (called internally by the
// resolveFunction handler when a semver range filters the
// versions), but no HTTP endpoint surfaces it. Foundry SPA can't
// render a "version history" panel for a named function — the
// only way to discover sibling versions is to call resolve with
// a semver range and observe which one wins.
//
// Wire shape:
//
//	GET /api/vertex/v1/ontologies/{ontologyApiName}/functions/{name}/versions
//	  200 + {"versions": [Function, Function, ...]}
//	      sorted version DESC (newest first — same ordering as
//	      the registry's internal default).
//	  404 + OntologyNotFound when the ontology slug is unknown.
//
// An unknown function name returns 200 + {"versions": []} — name
// is a filter here, not a key. This matches round 68 (Scenario
// Runs list-by-scenario) and round 69 (ShareLinks list-by-graph)
// where the "filter-not-key" decision keeps the SPA's "Recent X"
// panel renderable against brand-new entities.
//
// Scenarios:
//   - Known name with 3 versions returns them sorted version DESC.
//   - Unknown name returns 200 + {versions: []}.
//   - Unknown ontology returns 404 (slug is a real lookup).
//   - Cross-ontology isolation: a function with the same name in
//     ontology B is NOT returned for an ontology-A request.
//   - Response shape is {versions: [...]} envelope, not a bare
//     array — future pagination would otherwise be a breaking
//     change.
func TestBDD_Functions_ListVersionsByName(t *testing.T) {
	const (
		ontologyA   = "ri.ontology.main.ontology.A"
		ontologyB   = "ri.ontology.main.ontology.B"
		apiNameA    = "alpha"
		apiNameB    = "beta"
		fnNameMatch = "transformOrder"
	)

	mkFn := func(rid, ontologyRID, name, version string) oms.Function {
		return oms.Function{
			RID: rid, OntologyRID: ontologyRID, Name: name, Version: version,
			SourceCode: "// noop", Runtime: "javascript",
		}
	}

	newServer := func(t *testing.T, fns []oms.Function) http.Handler {
		t.Helper()
		lookup := &stubLookup{functions: fns}
		resolver := &stubResolver{byAPIName: map[string]string{
			apiNameA: ontologyA,
			apiNameB: ontologyB,
		}}
		return newTestRouter(t, lookup, resolver)
	}

	doList := func(t *testing.T, r http.Handler, apiName, name string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/vertex/v1/ontologies/"+apiName+"/functions/"+name+"/versions", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Known name with 3 versions returns them sorted DESC", func(t *testing.T) {
		seed := []oms.Function{
			mkFn("fn-1.0.0", ontologyA, fnNameMatch, "1.0.0"),
			mkFn("fn-2.0.0", ontologyA, fnNameMatch, "2.0.0"),
			mkFn("fn-1.5.0", ontologyA, fnNameMatch, "1.5.0"),
		}
		r := newServer(t, seed)
		rec := doList(t, r, apiNameA, fnNameMatch)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Versions []oms.Function `json:"versions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Versions) != 3 {
			t.Fatalf("len(versions)=%d, want 3", len(resp.Versions))
		}
		// stubLookup.ListFunctionVersionsByName sorts by Version
		// string DESC; "2.0.0" > "1.5.0" > "1.0.0".
		want := []string{"2.0.0", "1.5.0", "1.0.0"}
		for i, w := range want {
			if resp.Versions[i].Version != w {
				t.Errorf("versions[%d].Version=%q, want %q (sort broken)",
					i, resp.Versions[i].Version, w)
			}
		}
	})

	t.Run("Unknown function name returns 200 + empty versions", func(t *testing.T) {
		r := newServer(t, []oms.Function{
			mkFn("fn-x", ontologyA, "someOtherName", "1.0.0"),
		})
		rec := doList(t, r, apiNameA, "neverExisted")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Versions []oms.Function `json:"versions"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Versions == nil {
			t.Errorf("versions is nil, want empty array")
		}
		if len(resp.Versions) != 0 {
			t.Errorf("len(versions)=%d, want 0", len(resp.Versions))
		}
	})

	t.Run("Unknown ontology slug returns 404", func(t *testing.T) {
		r := newServer(t, nil)
		rec := doList(t, r, "ghost-ontology", fnNameMatch)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "OntologyNotFound" {
			t.Errorf("errorName=%v, want OntologyNotFound", body["errorName"])
		}
	})

	t.Run("Cross-ontology isolation: name in B not returned for A", func(t *testing.T) {
		seed := []oms.Function{
			mkFn("fn-a-1", ontologyA, fnNameMatch, "1.0.0"),
			mkFn("fn-b-1", ontologyB, fnNameMatch, "1.0.0"),
			mkFn("fn-b-2", ontologyB, fnNameMatch, "2.0.0"),
		}
		r := newServer(t, seed)
		rec := doList(t, r, apiNameA, fnNameMatch)
		var resp struct {
			Versions []oms.Function `json:"versions"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Versions) != 1 {
			t.Fatalf("len(versions)=%d, want 1 (only ontology A's version); body=%s",
				len(resp.Versions), rec.Body.String())
		}
		if resp.Versions[0].RID != "fn-a-1" {
			t.Errorf("got %q, want only fn-a-1", resp.Versions[0].RID)
		}
	})

	t.Run("Response shape is {versions: []}, not bare array", func(t *testing.T) {
		// Future-proof for pagination fields (nextPageToken,
		// totalCount). A bare array would lock us out.
		r := newServer(t, []oms.Function{mkFn("fn", ontologyA, fnNameMatch, "1.0.0")})
		rec := doList(t, r, apiNameA, fnNameMatch)
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("response body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
