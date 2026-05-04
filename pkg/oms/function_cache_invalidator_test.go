package oms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-425: Function cache invalidator drops cached results whose authors
// listed an upstream ObjectType in DependsOn after the object changes.

func TestFunctionCacheInvalidator_OnObjectChangeDropsMatchingFunctions(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:     "ri.ontology.main.ontology.o1",
			APIName: "northwind",
		}},
		functions: []oms.Function{
			{
				RID:         "ri.ontology.main.function.fn1",
				OntologyRID: "ri.ontology.main.ontology.o1",
				Name:        "fn1",
				Version:     "1.0.0",
				Pure:        true,
				DependsOn:   []string{"Customer", "Order"},
			},
			{
				RID:         "ri.ontology.main.function.fn2",
				OntologyRID: "ri.ontology.main.ontology.o1",
				Name:        "fn2",
				Version:     "1.0.0",
				Pure:        true,
				DependsOn:   []string{"Order"},
			},
			{
				RID:         "ri.ontology.main.function.fn3",
				OntologyRID: "ri.ontology.main.ontology.o1",
				Name:        "fn3",
				Version:     "1.0.0",
				Pure:        true,
				// no dependencies — must NEVER be invalidated by object change
			},
		},
	}
	cache := newRecordingCache()
	cache.Put("ri.ontology.main.function.fn1@1.0.0#aaa", "v1")
	cache.Put("ri.ontology.main.function.fn2@1.0.0#bbb", "v2")
	cache.Put("ri.ontology.main.function.fn3@1.0.0#ccc", "v3")

	inv := oms.NewFunctionCacheInvalidator(repo, cache)
	inv.OnObjectChange(context.Background(), "northwind", "Order")

	keys := cache.keys()
	if len(keys) != 1 {
		t.Fatalf("expected only fn3 entry to survive, got %d keys: %v", len(keys), keys)
	}
	if !strings.HasPrefix(keys[0], "ri.ontology.main.function.fn3@") {
		t.Errorf("expected fn3 entry to survive, got %q", keys[0])
	}
}

func TestFunctionCacheInvalidator_OnObjectChangeOnlyMatchesNamedType(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.o1", APIName: "northwind"}},
		functions: []oms.Function{{
			RID:         "ri.ontology.main.function.fn1",
			OntologyRID: "ri.ontology.main.ontology.o1",
			Name:        "fn1",
			Version:     "1.0.0",
			Pure:        true,
			DependsOn:   []string{"Customer"},
		}},
	}
	cache := newRecordingCache()
	cache.Put("ri.ontology.main.function.fn1@1.0.0#aaa", "v1")

	inv := oms.NewFunctionCacheInvalidator(repo, cache)
	// Touching an unrelated ObjectType must NOT drop the entry.
	inv.OnObjectChange(context.Background(), "northwind", "Product")

	if len(cache.keys()) != 1 {
		t.Errorf("Customer-only function must NOT invalidate on Product change")
	}

	// Touching the listed ObjectType drops the entry.
	inv.OnObjectChange(context.Background(), "northwind", "Customer")
	if len(cache.keys()) != 0 {
		t.Errorf("expected entry dropped after Customer change, keys=%v", cache.keys())
	}
}

func TestFunctionCacheInvalidator_NilSafe(t *testing.T) {
	var inv *oms.FunctionCacheInvalidator
	inv.OnObjectChange(context.Background(), "northwind", "Customer") // must not panic
	inv.Refresh("northwind")                                          // must not panic

	// nil cache → no-op
	repo := &mockRepo{}
	inv = oms.NewFunctionCacheInvalidator(repo, nil)
	inv.OnObjectChange(context.Background(), "northwind", "Customer")

	// nil repo → no-op
	cache := newRecordingCache()
	inv = oms.NewFunctionCacheInvalidator(nil, cache)
	cache.Put("ri.ontology.main.function.fn1@1.0.0#aaa", "v1")
	inv.OnObjectChange(context.Background(), "northwind", "Customer")
	if len(cache.keys()) != 1 {
		t.Errorf("nil repo invalidator must not touch cache, got keys=%v", cache.keys())
	}
}

func TestFunctionCacheInvalidator_RefreshClearsIndex(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.o1", APIName: "northwind"}},
		functions: []oms.Function{{
			RID:         "ri.ontology.main.function.fn1",
			OntologyRID: "ri.ontology.main.ontology.o1",
			Name:        "fn1",
			Version:     "1.0.0",
			Pure:        true,
			DependsOn:   []string{"Order"},
		}},
	}
	cache := newRecordingCache()
	inv := oms.NewFunctionCacheInvalidator(repo, cache)

	// Prime the reverse-index.
	cache.Put("ri.ontology.main.function.fn1@1.0.0#aaa", "v1")
	inv.OnObjectChange(context.Background(), "northwind", "Order")
	if len(cache.keys()) != 0 {
		t.Fatalf("expected entry dropped, got %v", cache.keys())
	}

	// Mutate the function to depend on a different type and refresh.
	repo.functions[0].DependsOn = []string{"Customer"}
	inv.Refresh("northwind")

	// After refresh, an Order change must NOT invalidate the new entry.
	cache.Put("ri.ontology.main.function.fn1@1.0.0#bbb", "v2")
	inv.OnObjectChange(context.Background(), "northwind", "Order")
	if len(cache.keys()) != 1 {
		t.Errorf("Order is no longer a dependency post-Refresh; expected entry to survive, got keys=%v", cache.keys())
	}

	// Customer is the new dependency — it should invalidate.
	inv.OnObjectChange(context.Background(), "northwind", "Customer")
	if len(cache.keys()) != 0 {
		t.Errorf("expected Customer change to drop entry, got keys=%v", cache.keys())
	}
}

func TestFunctionCacheInvalidator_EmptyArgsNoOp(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.o1", APIName: "northwind"}},
		functions: []oms.Function{{
			RID:         "ri.ontology.main.function.fn1",
			OntologyRID: "ri.ontology.main.ontology.o1",
			Pure:        true,
			DependsOn:   []string{"Order"},
		}},
	}
	cache := newRecordingCache()
	cache.Put("ri.ontology.main.function.fn1@1.0.0#aaa", "v1")
	inv := oms.NewFunctionCacheInvalidator(repo, cache)

	inv.OnObjectChange(context.Background(), "", "Order")
	inv.OnObjectChange(context.Background(), "northwind", "")
	if len(cache.keys()) != 1 {
		t.Errorf("empty ontology / objectType must not invalidate, got keys=%v", cache.keys())
	}
}
