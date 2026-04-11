package oss

import "github.com/liyang/weave/pkg/index"

// scopedBleveKey returns the per-ontology Bleve index key for an
// (ontology, objectType) pair, falling back to the bare object type when no
// scoped index exists. This shim preserves backwards compatibility for legacy
// callers that pre-create unscoped indexes (older test fixtures and the e2e
// suites) while routing production traffic — which always seeds the scoped
// index via the funnel consumer (US-044) — to the per-ontology key.
//
// Resolution order:
//  1. If ontologyRID is empty, return the bare objectType.
//  2. If a scoped index "{ontologyRID}__{objectType}" exists in the manager,
//     use it.
//  3. Otherwise fall back to the bare objectType.
func scopedBleveKey(mgr *index.Manager, ontologyRID, objectType string) string {
	if mgr == nil || ontologyRID == "" {
		return objectType
	}
	scoped := index.ScopedKey(ontologyRID, objectType)
	if mgr.GetIndex(scoped) != nil {
		return scoped
	}
	return objectType
}
