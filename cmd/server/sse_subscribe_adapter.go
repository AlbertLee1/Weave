package main

import (
	"encoding/json"

	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/oss/where"
)

// objectSetLookupAdapter resolves ObjectSet rids through the in-memory
// objectset.Store for the SSE subscribe handler. It lives in cmd/server/
// rather than pkg/oss/ because pkg/oss/objectset already imports pkg/oss
// (handler.go) — a direct dependency would create an import cycle, so the
// SSE handler takes a narrow oss.ObjectSetLookup interface and this adapter
// supplies it.
//
// Only the operators that preserve a single base ObjectType are unwrapped:
// filter / withProperties / searchAround / asType. union / intersect /
// subtract / interfaceBase / ... fall through to an empty ObjectType so the
// handler emits a clean 400 "SSE subscribe currently requires a base
// ObjectSet type" rather than silently yielding a wrong stream. The bounded
// for loop caps nesting depth at 8 hops so a pathological cycle cannot spin.
//
// US-056 adds Where extraction: every filter hop encountered along the walk
// contributes its Where JSON, and the adapter AND-collapses the collected
// clauses into a single tree so the SSE handler can evaluate it against
// each incoming BroadcastEvent without descending the Definition tree on
// the hot path.
type objectSetLookupAdapter struct {
	store *objectset.Store
}

func newObjectSetLookupAdapter(store *objectset.Store) *objectSetLookupAdapter {
	return &objectSetLookupAdapter{store: store}
}

func (a *objectSetLookupAdapter) ResolveSubscription(rid string) (oss.SubscriptionSpec, error) {
	if a == nil || a.store == nil {
		return oss.SubscriptionSpec{}, oss.ErrObjectSetNotFound
	}
	def, err := a.store.Get(rid)
	if err != nil {
		return oss.SubscriptionSpec{}, oss.ErrObjectSetNotFound
	}
	ot, clauses := extractBaseAndWhere(def)
	spec := oss.SubscriptionSpec{ObjectType: ot}
	if combined := combineClauses(clauses); combined != nil {
		spec.Where = combined
	}
	return spec, nil
}

// extractBaseAndWhere walks the Definition tree until it reaches a base
// ObjectType, collecting any filter hop Where JSON it encounters along the
// way. Returns an empty ObjectType when the tree cannot reduce to a single
// base (union / interfaceBase / subtract / ...).
func extractBaseAndWhere(def *objectset.Definition) (string, []json.RawMessage) {
	var wheres []json.RawMessage
	for i := 0; i < 8 && def != nil; i++ {
		switch def.Type {
		case "base", "static", "asType":
			return def.ObjectType, wheres
		case "filter":
			if len(def.Where) > 0 {
				wheres = append(wheres, def.Where)
			}
			def = def.ObjectSet
		case "withProperties", "searchAround":
			def = def.ObjectSet
		default:
			return "", nil
		}
	}
	return "", nil
}

// extractBaseObjectType preserves the US-055 entry point so other callers
// (contract test, potential future adapters) keep a narrow "just the base
// type" helper. It delegates to the richer walker and discards the Where.
func extractBaseObjectType(def *objectset.Definition) string {
	ot, _ := extractBaseAndWhere(def)
	return ot
}

// combineClauses AND-collapses N collected filter Where clauses into a
// single WhereClause tree. Zero clauses → nil (SSE handler streams
// unconditionally); one clause → the clause itself (no redundant wrapper);
// two or more → synthetic {"type":"and","value":[...]} envelope. Unparseable
// JSON is skipped to keep the SSE stream resilient — the main read path
// already rejected invalid filters at createTemporary time.
func combineClauses(raws []json.RawMessage) *where.WhereClause {
	var parsed []where.WhereClause
	for _, raw := range raws {
		if len(raw) == 0 {
			continue
		}
		var c where.WhereClause
		if err := json.Unmarshal(raw, &c); err != nil {
			continue
		}
		parsed = append(parsed, c)
	}
	switch len(parsed) {
	case 0:
		return nil
	case 1:
		c := parsed[0]
		return &c
	default:
		valueBytes, err := json.Marshal(parsed)
		if err != nil {
			return nil
		}
		return &where.WhereClause{Type: "and", Value: valueBytes}
	}
}
