package main

import (
	"sort"
	"strings"
	"testing"
)

// TestContract_CheckFamilyEndpoints locks the Foundry-parity probe
// endpoint family added across rounds 95-109. Sibling of round-93's
// TestContract_BatchByRidSymmetry — codifies the recipe as a
// CI-enforced regression guard so a future PR that accidentally
// drops or renames any of these probes fails with a clear message.
//
// Coverage map (10 pairs across rounds 91-110, backend half):
//   - r95: GET /api/v2/ontologies/{ontologyApiName}/me
//   - r97: POST /api/v2/me/permissions/check
//   - r99: GET /api/v2/me/ontologies
//   - r101: POST /api/v2/auth/sessions/revoke-others
//   - r103: GET /api/v2/ontologies/{ontologyApiName}/actions/{actionApiName}/check
//   - r105: GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/check
//   - r107: POST /api/v2/me/checks/objectTypes
//   - r109: POST /api/v2/me/checks/actionTypes
//
// The SDK contract sibling lives in
// sdk/python/tests/test_check_family_contract.py (round 112,
// pending). Together they form the same backend↔SDK contract guard
// the batch-by-RID family got in rounds 93+94.
func TestContract_CheckFamilyEndpoints(t *testing.T) {
	// (method, path) — entries are exact chi route templates so
	// extractChiRoutes can match them directly.
	expected := []specOperationKey{
		// /me family (rounds 95, 97, 99, 101)
		{Method: "GET", Path: "/api/v2/ontologies/{ontologyApiName}/me"},
		{Method: "POST", Path: "/api/v2/me/permissions/check"},
		{Method: "GET", Path: "/api/v2/me/ontologies"},
		{Method: "POST", Path: "/api/auth/sessions/revoke-others"},
		// Per-resource single check (rounds 103, 105, 113)
		{Method: "GET", Path: "/api/v2/ontologies/{ontologyApiName}/actions/{actionApiName}/check"},
		{Method: "GET", Path: "/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/check"},
		{Method: "GET", Path: "/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryTypeApiName}/check"},
		// Bulk check pair (rounds 107, 109)
		{Method: "POST", Path: "/api/v2/me/checks/objectTypes"},
		{Method: "POST", Path: "/api/v2/me/checks/actionTypes"},
	}

	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)

	var missing []specOperationKey
	for _, key := range expected {
		if !chiRoutes[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Path != missing[j].Path {
			return missing[i].Path < missing[j].Path
		}
		return missing[i].Method < missing[j].Method
	})
	var b strings.Builder
	b.WriteString("Foundry-parity check-family contract broken. ")
	b.WriteString("Missing routes:\n")
	for _, k := range missing {
		b.WriteString("  ")
		b.WriteString(k.Method)
		b.WriteString(" ")
		b.WriteString(k.Path)
		b.WriteString("\n")
	}
	b.WriteString("\nIf the removal was intentional, edit ")
	b.WriteString("expected in this test and document the ")
	b.WriteString("Foundry-parity rationale in the commit message.")
	t.Fatal(b.String())
}

// TestContract_CheckFamilyMethodDiscipline asserts each probe uses
// the right HTTP verb. The convention codifies what rounds 95-109
// settled into: GET for per-resource probes (path-only, cache-
// friendly), POST for batch probes (request body carries the array)
// and POST for action verbs like revoke-others (mutates server
// state, no idempotency guarantee).
//
// A PR that flips a verb without updating this test silently
// breaks SDK callers; the catch-this-early rationale matches
// round-93's TestContract_BatchByRidEndpointsAllPost.
func TestContract_CheckFamilyMethodDiscipline(t *testing.T) {
	cases := []struct {
		path       string
		wantMethod string
		// forbidden methods MUST NOT exist on this path — round-93's
		// "POST-only" assertion generalised.
		forbidden []string
	}{
		// /me family
		{
			path:       "/api/v2/ontologies/{ontologyApiName}/me",
			wantMethod: "GET",
			forbidden:  []string{"POST", "PUT", "DELETE", "PATCH"},
		},
		{
			path:       "/api/v2/me/permissions/check",
			wantMethod: "POST",
			forbidden:  []string{"GET", "PUT", "DELETE", "PATCH"},
		},
		{
			path:       "/api/v2/me/ontologies",
			wantMethod: "GET",
			forbidden:  []string{"POST", "PUT", "DELETE", "PATCH"},
		},
		{
			path:       "/api/auth/sessions/revoke-others",
			wantMethod: "POST",
			forbidden:  []string{"GET", "PUT", "DELETE", "PATCH"},
		},
		// Per-resource single checks — GET because path-only +
		// cacheable + fits row-render code.
		{
			path:       "/api/v2/ontologies/{ontologyApiName}/actions/{actionApiName}/check",
			wantMethod: "GET",
			forbidden:  []string{"POST", "PUT", "DELETE", "PATCH"},
		},
		{
			path:       "/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/check",
			wantMethod: "GET",
			forbidden:  []string{"POST", "PUT", "DELETE", "PATCH"},
		},
		{
			path:       "/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryTypeApiName}/check",
			wantMethod: "GET",
			forbidden:  []string{"POST", "PUT", "DELETE", "PATCH"},
		},
		// Bulk checks — POST because request body carries the array.
		{
			path:       "/api/v2/me/checks/objectTypes",
			wantMethod: "POST",
			forbidden:  []string{"GET", "PUT", "DELETE", "PATCH"},
		},
		{
			path:       "/api/v2/me/checks/actionTypes",
			wantMethod: "POST",
			forbidden:  []string{"GET", "PUT", "DELETE", "PATCH"},
		},
	}

	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)

	for _, c := range cases {
		// Required verb must exist.
		want := specOperationKey{Method: c.wantMethod, Path: c.path}
		if !chiRoutes[want] {
			t.Errorf("%s %s missing — check-family discipline broken", c.wantMethod, c.path)
		}
		// Forbidden verbs MUST NOT exist.
		for _, verb := range c.forbidden {
			if chiRoutes[specOperationKey{Method: verb, Path: c.path}] {
				t.Errorf("%s %s should not exist — check-family endpoint must be %s-only",
					verb, c.path, c.wantMethod)
			}
		}
	}
}
