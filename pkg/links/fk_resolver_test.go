package links_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
)

// countingSearcher is a thin decorator around an index.Manager-shaped Searcher
// that counts how many Search calls have been made. Tests use it to assert
// that batch hydration uses a constant number of queries instead of N+1.
type countingSearcher struct {
	inner   links.Searcher
	count   int64
	objects []string // append-only log of (objectType) per call for diagnostics
}

func newCountingSearcher(inner links.Searcher) *countingSearcher {
	return &countingSearcher{inner: inner}
}

func (c *countingSearcher) Search(objectType string, req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	atomic.AddInt64(&c.count, 1)
	c.objects = append(c.objects, objectType)
	return c.inner.Search(objectType, req)
}

func (c *countingSearcher) SearchCount() int {
	return int(atomic.LoadInt64(&c.count))
}

func (c *countingSearcher) Reset() {
	atomic.StoreInt64(&c.count, 0)
	c.objects = c.objects[:0]
}

// fkSetup builds an in-memory employee/department fixture sized to N employees
// across two departments. The number of employees is chosen by the caller so
// that perf tests can prove the constant-query property at scale.
func fkSetup(t *testing.T, numEmployees int) (*links.Resolver, *countingSearcher, *index.Manager, []string) {
	t.Helper()

	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	empProps := []index.Property{
		{APIName: "employeeid", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", empProps); err != nil {
		t.Fatalf("ensure employee index: %v", err)
	}
	deptProps := []index.Property{
		{APIName: "deptid", BaseType: "string", IsSearchable: true},
		{APIName: "deptname", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("ensure department index: %v", err)
	}

	pks := make([]string, 0, numEmployees)
	for i := 0; i < numEmployees; i++ {
		id := fmt.Sprintf("emp%d", i)
		dept := "d1"
		if i%2 == 0 {
			dept = "d2"
		}
		if err := mgr.IndexDocument("employee", id, map[string]interface{}{
			"employeeid": id,
			"name":       fmt.Sprintf("name%d", i),
			"deptid":     dept,
		}); err != nil {
			t.Fatalf("index emp %s: %v", id, err)
		}
		pks = append(pks, id)
	}
	for _, d := range []string{"d1", "d2"} {
		if err := mgr.IndexDocument("department", d, map[string]interface{}{
			"deptid":   d,
			"deptname": d + "-name",
		}); err != nil {
			t.Fatalf("index dept %s: %v", d, err)
		}
	}

	repo := &mockRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.employee":   {RID: "ri.ot.employee", APIName: "employee", PrimaryKey: "employeeid"},
			"ri.ot.department": {RID: "ri.ot.department", APIName: "department", PrimaryKey: "deptid"},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.emp-dept": {
				RID:              "ri.lt.emp-dept",
				APIName:          "employeedepartment",
				SourceObjectType: "ri.ot.employee",
				TargetObjectType: "ri.ot.department",
				Cardinality:      "MANY_TO_ONE",
				ForeignKeyConfig: mustJSON(links.FKConfig{
					SourceProperty: "deptid",
					TargetProperty: "deptid",
				}),
			},
		},
		outgoing: map[string][]oms.LinkType{},
	}

	ctr := newCountingSearcher(mgr)
	resolver := links.NewResolverWithSearcher(repo, ctr)
	return resolver, ctr, mgr, pks
}

// TestFKResolver_BatchGetFKValues_SingleSearchCall asserts that resolving a
// forward FK link for N source PKs issues a constant (small) number of Bleve
// Search calls — specifically a single batch query against the source index
// and a second query against the target index. Without the batch fix this
// scales as O(N) per source PK.
func TestFKResolver_BatchGetFKValues_SingleSearchCall(t *testing.T) {
	resolver, ctr, _, pks := fkSetup(t, 50)
	ctx := context.Background()

	ctr.Reset()
	out, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", pks)
	if err != nil {
		t.Fatalf("ResolveLinkedObjects: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected at least one resolved department")
	}

	// Maximum 2 Search calls expected:
	//   1) batch read of FK values from the employee index (DocIDQuery over all source PKs)
	//   2) lookup into the department index for the matching FK values
	if got := ctr.SearchCount(); got > 2 {
		t.Fatalf("expected at most 2 Search calls (batch FK read + target lookup); got %d. log=%v",
			got, ctr.objects)
	}
}

// TestFKResolver_BatchGetFKValues_EmptySourceList ensures the batch path
// short-circuits on empty input and never touches the index.
func TestFKResolver_BatchGetFKValues_EmptySourceList(t *testing.T) {
	resolver, ctr, _, _ := fkSetup(t, 5)
	ctx := context.Background()

	ctr.Reset()
	out, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", []string{})
	if err != nil {
		t.Fatalf("ResolveLinkedObjects: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty result, got %v", out)
	}
	if got := ctr.SearchCount(); got != 0 {
		t.Fatalf("expected 0 Search calls for empty input, got %d", got)
	}
}

// TestFKResolver_BatchGetFKValues_MissingSourceDoc verifies that source PKs
// which have no document in the employee index are quietly excluded from the
// FK value set, and the result still resolves the present ones.
func TestFKResolver_BatchGetFKValues_MissingSourceDoc(t *testing.T) {
	resolver, ctr, _, _ := fkSetup(t, 4)
	ctx := context.Background()

	ctr.Reset()
	pks := []string{"emp0", "emp-missing", "emp1"}
	out, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", pks)
	if err != nil {
		t.Fatalf("ResolveLinkedObjects: %v", err)
	}
	// emp0 -> d2, emp1 -> d1; emp-missing has no doc and contributes nothing.
	if len(out) != 2 {
		t.Fatalf("expected 2 resolved departments, got %d: %v", len(out), out)
	}
	found := map[string]bool{}
	for _, p := range out {
		found[p] = true
	}
	if !found["d1"] || !found["d2"] {
		t.Fatalf("expected d1 and d2, got %v", out)
	}
	if got := ctr.SearchCount(); got > 2 {
		t.Fatalf("expected at most 2 Search calls; got %d", got)
	}
}

// TestFKResolver_BatchGetFKValues_PreservesMapping verifies that batch FK
// reads correctly walk multiple distinct FK values and return them all,
// not just the first one. Equivalent in spirit to deduplicated MANY_TO_ONE
// resolution.
func TestFKResolver_BatchGetFKValues_PreservesMapping(t *testing.T) {
	resolver, ctr, _, _ := fkSetup(t, 4)
	ctx := context.Background()

	ctr.Reset()
	// emp0(d2), emp1(d1), emp2(d2), emp3(d1) — both departments referenced.
	out, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", []string{"emp0", "emp1", "emp2", "emp3"})
	if err != nil {
		t.Fatalf("ResolveLinkedObjects: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 distinct departments, got %d: %v", len(out), out)
	}
	if got := ctr.SearchCount(); got > 2 {
		t.Fatalf("expected at most 2 Search calls; got %d", got)
	}
}
