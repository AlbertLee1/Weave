package main

import (
	"github.com/liyang/weave/pkg/mcp"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// objectSetCatalogAdapter bridges *objectset.Store to mcp.ObjectSetCatalog
// for the resources/list and resources/read methods. The Definition pointer
// is passed through unchanged so the MCP layer can encode it as JSON without
// any cross-package marshalling logic.
type objectSetCatalogAdapter struct {
	store *objectset.Store
}

func newObjectSetCatalogAdapter(store *objectset.Store) mcp.ObjectSetCatalog {
	return &objectSetCatalogAdapter{store: store}
}

func (a *objectSetCatalogAdapter) ListObjectSets() []mcp.ObjectSetEntry {
	entries := a.store.ListEntries()
	out := make([]mcp.ObjectSetEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, mcp.ObjectSetEntry{
			ID:         e.ID,
			Definition: e.Definition,
			CreatedAt:  e.CreatedAt,
		})
	}
	return out
}

func (a *objectSetCatalogAdapter) GetObjectSet(id string) (*mcp.ObjectSetEntry, error) {
	entry, err := a.store.GetEntry(id)
	if err != nil {
		return nil, mcp.ErrObjectSetNotFound
	}
	return &mcp.ObjectSetEntry{
		ID:         entry.ID,
		Definition: entry.Definition,
		CreatedAt:  entry.CreatedAt,
	}, nil
}
