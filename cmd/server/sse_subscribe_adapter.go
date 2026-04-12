package main

import (
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// objectSetLookupAdapter resolves ObjectSet rids through the in-memory
// objectset.Store for the US-055 SSE subscribe scaffold. It lives in
// cmd/server/ rather than pkg/oss/ because pkg/oss/objectset already
// imports pkg/oss/handler.go — a direct dependency would create an import
// cycle, so the SSE handler takes a narrow oss.ObjectSetLookup interface
// and this adapter supplies it.
//
// Only the operators that preserve a single base ObjectType are unwrapped:
// filter / withProperties / searchAround / asType. union / intersect /
// subtract / interfaceBase / ... fall through to "" so the handler emits
// a clean 400 "SSE scaffold currently requires a base ObjectSet type"
// rather than silently yielding a wrong stream. The bounded for loop
// caps nesting depth at 8 hops so a pathological cycle cannot spin.
type objectSetLookupAdapter struct {
	store *objectset.Store
}

func newObjectSetLookupAdapter(store *objectset.Store) *objectSetLookupAdapter {
	return &objectSetLookupAdapter{store: store}
}

func (a *objectSetLookupAdapter) ResolveBaseObjectType(rid string) (string, error) {
	if a == nil || a.store == nil {
		return "", oss.ErrObjectSetNotFound
	}
	def, err := a.store.Get(rid)
	if err != nil {
		return "", oss.ErrObjectSetNotFound
	}
	return extractBaseObjectType(def), nil
}

func extractBaseObjectType(def *objectset.Definition) string {
	for i := 0; i < 8 && def != nil; i++ {
		switch def.Type {
		case "base", "static", "asType":
			return def.ObjectType
		case "filter", "withProperties", "searchAround":
			def = def.ObjectSet
		default:
			return ""
		}
	}
	return ""
}
