package functions

import (
	"context"
	"encoding/json"

	"github.com/dop251/goja"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
)

// OntologyClient is the narrow interface the JS shim uses to read Weave data.
type OntologyClient interface {
	GetObject(ctx context.Context, objectType, primaryKey string) (*oss.WireObject, error)
	SearchObjects(ctx context.Context, objectType string, w *where.WhereClause, pageSize int) (*oss.ObjectPage, error)
}

// SetOntologyClient configures the ontology JS shim for this runtime.
// When set, Execute will register a global `ontology` object with load/search.
func (r *Runtime) SetOntologyClient(client OntologyClient) {
	r.ontologyClient = client
}

// registerOntologyShim registers the global `ontology` object on the VM.
func (r *Runtime) registerOntologyShim(vm *goja.Runtime, ctx context.Context) {
	if r.ontologyClient == nil {
		return
	}

	ontology := vm.NewObject()

	// ontology.load(objectType, primaryKey) → JS object
	ontology.Set("load", func(call goja.FunctionCall) goja.Value {
		objectType := call.Argument(0).String()
		primaryKey := call.Argument(1).String()

		obj, err := r.ontologyClient.GetObject(ctx, objectType, primaryKey)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		return wireObjectToJS(vm, obj)
	})

	// ontology.search(objectType, where, opts?) → { data: [...], totalCount: string }
	ontology.Set("search", func(call goja.FunctionCall) goja.Value {
		objectType := call.Argument(0).String()

		// Parse where clause from JS object
		whereArg := call.Argument(1)
		var wc *where.WhereClause
		if whereArg != nil && !goja.IsUndefined(whereArg) && !goja.IsNull(whereArg) {
			raw, err := json.Marshal(whereArg.Export())
			if err != nil {
				panic(vm.NewGoError(err))
			}
			wc = &where.WhereClause{}
			if err := json.Unmarshal(raw, wc); err != nil {
				panic(vm.NewGoError(err))
			}
		}

		// Parse optional opts.pageSize
		pageSize := 100
		optsArg := call.Argument(2)
		if optsArg != nil && !goja.IsUndefined(optsArg) && !goja.IsNull(optsArg) {
			if opts, ok := optsArg.Export().(map[string]interface{}); ok {
				if ps, ok := opts["pageSize"]; ok {
					switch v := ps.(type) {
					case int64:
						pageSize = int(v)
					case float64:
						pageSize = int(v)
					}
				}
			}
		}

		page, err := r.ontologyClient.SearchObjects(ctx, objectType, wc, pageSize)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		return objectPageToJS(vm, page)
	})

	vm.Set("ontology", ontology)
}

// wireObjectToJS converts a WireObject to a flat JS object with properties
// at the top level plus __rid, __primaryKey, __apiName.
func wireObjectToJS(vm *goja.Runtime, obj *oss.WireObject) goja.Value {
	m := make(map[string]interface{}, len(obj.Properties)+3)
	for k, v := range obj.Properties {
		m[k] = v
	}
	m["__rid"] = obj.RID
	m["__primaryKey"] = obj.PrimaryKey
	m["__apiName"] = obj.APIName
	return vm.ToValue(m)
}

// objectPageToJS converts an ObjectPage to a JS object { data: [...], totalCount, nextPageToken }.
func objectPageToJS(vm *goja.Runtime, page *oss.ObjectPage) goja.Value {
	items := make([]interface{}, len(page.Data))
	for i, obj := range page.Data {
		m := make(map[string]interface{}, len(obj.Properties)+3)
		for k, v := range obj.Properties {
			m[k] = v
		}
		m["__rid"] = obj.RID
		m["__primaryKey"] = obj.PrimaryKey
		m["__apiName"] = obj.APIName
		items[i] = m
	}
	result := map[string]interface{}{
		"data":       items,
		"totalCount": page.TotalCount,
	}
	if page.NextPageToken != "" {
		result["nextPageToken"] = page.NextPageToken
	}
	return vm.ToValue(result)
}
