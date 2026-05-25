package main

import (
	"sort"
	"strings"
	"testing"
)

// TestContract_BatchByRidSymmetry codifies the round 73-90 batch-get-by-RID
// recipe as a regression guard. Rounds 79/81/83/85/87/89 each added one
// missing /getByRidBatch endpoint until all 8 metadata kinds on the OMS
// surface shared identical wire contracts (POST {rids:[...]}, 200
// {data:[]}, missing-RID-silent-skip). This test locks that 8-of-8
// invariant: any future PR that accidentally drops a batch endpoint, or
// adds a new metadata kind without its batch sibling, must update both
// the route and this list.
//
// Round 93 PIVOTS away from "add another batch endpoint" (the 8-of-8 well
// ran dry after round 90) toward codifying the recipe as a contract. No
// new feature; no new handler. Just a TestContract sibling of the
// existing AllRoutesDocumented / NoOrphanedSpecPaths tests in
// contract_test.go that holds the line going forward.
//
// Failure mode: when this test fails, the developer who removed an
// endpoint sees exactly which one is missing and the 1:1 Foundry-parity
// justification ("SDK callers rendering N metadatas would need N
// round-trips to label them"). They can then either restore the route,
// remove the kind from expectedBatchByRidEndpoints below (with a
// commit-message justification), or extend the list when adding a new
// metadata kind.
func TestContract_BatchByRidSymmetry(t *testing.T) {
	expectedBatchByRidEndpoints := []string{
		// Original 4 — pre-existing before the round-79 closure wave:
		"/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch",
		// Round 79-89 closure wave (8-of-8 symmetry):
		"/api/v2/ontologies/{ontologyApiName}/linkTypes/getByRidBatch",          // round 79
		"/api/v2/ontologies/{ontologyApiName}/interfaceTypes/getByRidBatch",     // round 81
		"/api/v2/ontologies/{ontologyApiName}/valueTypes/getByRidBatch",         // round 83
		"/api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes/getByRidBatch", // round 85
		"/api/v2/ontologies/{ontologyApiName}/typeGroups/getByRidBatch",         // round 87
		"/api/v2/ontologies/{ontologyApiName}/queryTypes/getByRidBatch",         // round 89
	}

	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)

	var missing []string
	for _, path := range expectedBatchByRidEndpoints {
		key := specOperationKey{Method: "POST", Path: path}
		if !chiRoutes[key] {
			missing = append(missing, path)
		}
	}

	if len(missing) == 0 {
		return
	}

	sort.Strings(missing)
	var b strings.Builder
	b.WriteString("8-of-8 batch-get-by-RID symmetry broken. ")
	b.WriteString("Missing POST routes:\n")
	for _, p := range missing {
		b.WriteString("  ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\nIf the removal was intentional, edit ")
	b.WriteString("expectedBatchByRidEndpoints in this test and document ")
	b.WriteString("the Foundry-parity rationale in the commit message.")
	t.Fatal(b.String())
}

// TestContract_BatchByRidEndpointsAllPost is a sibling invariant: the
// 8-of-8 surfaces MUST be POST (never GET). The convention exists because
// (a) request bodies carry arbitrary-length RID arrays and URL length
// limits trip on >50 RIDs; (b) the established Foundry-parity verb for
// batch-resource-by-ID is POST across all four core kinds; (c) cache
// semantics — batch-by-ID returns a different shape per call, so GET
// caching would be misleading. This test holds the line by checking each
// expected endpoint resolves only to POST.
func TestContract_BatchByRidEndpointsAllPost(t *testing.T) {
	expectedBatchByRidEndpoints := []string{
		"/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/linkTypes/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/interfaceTypes/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/valueTypes/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/typeGroups/getByRidBatch",
		"/api/v2/ontologies/{ontologyApiName}/queryTypes/getByRidBatch",
	}
	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)

	for _, path := range expectedBatchByRidEndpoints {
		for _, wrongVerb := range []string{"GET", "PUT", "DELETE", "PATCH"} {
			key := specOperationKey{Method: wrongVerb, Path: path}
			if chiRoutes[key] {
				t.Errorf("%s %s should not exist — batch-by-RID endpoints must be POST only",
					wrongVerb, path)
			}
		}
		postKey := specOperationKey{Method: "POST", Path: path}
		if !chiRoutes[postKey] {
			t.Errorf("POST %s missing — every batch-by-RID surface MUST accept POST", path)
		}
	}
}
