package main

import (
	"fmt"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// indexBootstrapAdapter satisfies oms.IndexBootstrapper by translating
// (ontologyAPIName, objectTypeAPIName, []oms.Property) calls into the
// scoped-key + []index.Property arguments the index.Manager understands.
//
// DOG-003: this adapter is the glue that lets the OMS create / import
// handlers materialise a Bleve index shell synchronously so a follow-up
// stream ingest cannot race against a missing index. Defining it in
// cmd/server keeps both packages free of the cross-import that a direct
// dependency would require.
type indexBootstrapAdapter struct {
	mgr *index.Manager
}

func newIndexBootstrapAdapter(mgr *index.Manager) *indexBootstrapAdapter {
	return &indexBootstrapAdapter{mgr: mgr}
}

func (a *indexBootstrapAdapter) EnsureObjectTypeIndex(ontologyAPIName, objectTypeAPIName string, props []oms.Property) error {
	if a == nil || a.mgr == nil {
		return nil
	}
	if ontologyAPIName == "" || objectTypeAPIName == "" {
		return fmt.Errorf("index bootstrap: ontology and objectType are required")
	}
	indexProps := make([]index.Property, 0, len(props))
	for _, p := range props {
		indexProps = append(indexProps, index.Property{
			APIName:      p.APIName,
			BaseType:     p.BaseType,
			IsSearchable: p.IsSearchable,
			IsArray:      p.IsArray,
			Analyzer:     index.AnalyzerFromTypeConfig(p.TypeConfig),
		})
	}
	scoped := index.ScopedKey(ontologyAPIName, objectTypeAPIName)
	if _, err := a.mgr.EnsureIndex(scoped, indexProps); err != nil {
		return fmt.Errorf("ensure index %q: %w", scoped, err)
	}
	return nil
}

func (a *indexBootstrapAdapter) DropObjectTypeIndex(ontologyAPIName, objectTypeAPIName string) error {
	if a == nil || a.mgr == nil {
		return nil
	}
	if ontologyAPIName == "" || objectTypeAPIName == "" {
		return nil
	}
	scoped := index.ScopedKey(ontologyAPIName, objectTypeAPIName)
	return a.mgr.DropIndex(scoped)
}

// indexReadinessAdapter satisfies oss.IndexReadinessChecker by probing the
// Manager's in-memory handle map. DOG-003: used by the stream ingest
// handler's fail-fast guard so the API returns 409 IndexNotReady instead
// of accepting a batch that the funnel consumer would silently drop.
type indexReadinessAdapter struct {
	mgr *index.Manager
}

func newIndexReadinessAdapter(mgr *index.Manager) *indexReadinessAdapter {
	return &indexReadinessAdapter{mgr: mgr}
}

func (a *indexReadinessAdapter) IndexReady(ontologyAPIName, objectType string) bool {
	if a == nil || a.mgr == nil {
		return true
	}
	scoped := index.ScopedKey(ontologyAPIName, objectType)
	return a.mgr.GetIndex(scoped) != nil
}

