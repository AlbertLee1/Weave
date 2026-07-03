package oss_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/pagination"
)

// seedListParityEmployees builds an isolated employee index seeded with n
// documents (emp0000..emp{n-1}) and returns a fully-wired service. It is the
// large-N fixture the list-default-page-size scenarios need — setupOSSTest only
// seeds 3 rows, which cannot distinguish a 100 default from a 1000 default.
func seedListParityEmployees(t *testing.T, n int) *oss.ServiceImpl {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "age", BaseType: "integer", IsSearchable: true},
		{APIName: "deptId", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// One atomic batch keeps scorch from churning a segment per document.
	ops := make([]index.BatchOp, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("emp%04d", i)
		ops = append(ops, index.BatchOp{
			Type:       index.BatchOpIndex,
			PrimaryKey: id,
			Document: map[string]interface{}{
				"employeeId": id,
				"name":       fmt.Sprintf("employee-%04d", i),
				"age":        float64(20 + i%40),
				"deptId":     fmt.Sprintf("d%d", i%5),
			},
		})
	}
	if err := mgr.ApplyBatch("employee", ops); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	// Bleve upserts are durable, but scorch DocCount can lag during segment
	// introduction — poll until every seeded doc is visible before returning.
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := mgr.DocCount("employee")
		if err == nil && int(c) >= n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("index did not settle to %d docs (last err=%v)", n, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.employee",
		OntologyRID: testOntologyRID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	linkResolver := &mockLinkResolver{results: make(map[string][]string)}
	return oss.NewService(repo, mgr, linkResolver)
}

// TestListObjects_SelectProjection is the unit-level contract for the Foundry
// `?select=` list projection on the service (mirrors the search-body select).
//
//   - Select=[name]   -> objects carry only name + the primary key field
//   - Select unset    -> objects carry the full property set (Foundry default)
func TestListObjects_SelectProjection(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	t.Run("select subset keeps only selected + primary key", func(t *testing.T) {
		page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
			Select:      []string{"name"},
		})
		if err != nil {
			t.Fatalf("ListObjects: %v", err)
		}
		if len(page.Data) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(page.Data))
		}
		for _, obj := range page.Data {
			if _, ok := obj.Properties["name"]; !ok {
				t.Errorf("selected property name missing; props=%v", obj.Properties)
			}
			// The primary-key field is always retained through a projection.
			if _, ok := obj.Properties["employeeId"]; !ok {
				t.Errorf("primary key employeeId must survive select; props=%v", obj.Properties)
			}
			for _, k := range []string{"age", "active", "deptId"} {
				if _, ok := obj.Properties[k]; ok {
					t.Errorf("unselected property %q must be stripped; props=%v", k, obj.Properties)
				}
			}
		}
	})

	t.Run("empty select returns the full property set", func(t *testing.T) {
		page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
		})
		if err != nil {
			t.Fatalf("ListObjects: %v", err)
		}
		for _, obj := range page.Data {
			for _, k := range []string{"employeeId", "name", "age", "active", "deptId"} {
				if _, ok := obj.Properties[k]; !ok {
					t.Errorf("full response missing %q; props=%v", k, obj.Properties)
				}
			}
		}
	})
}

// TestListObjects_ListDefaultPageSize locks the Foundry list default (1000)
// while proving the search path keeps its 100 default — the list-local override
// must not leak into other endpoints.
func TestListObjects_ListDefaultPageSize(t *testing.T) {
	const n = 150
	svc := seedListParityEmployees(t, n)
	ctx := context.Background()

	t.Run("list default page size returns up to 1000", func(t *testing.T) {
		page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
			PageSize:    0,
		})
		if err != nil {
			t.Fatalf("ListObjects: %v", err)
		}
		if len(page.Data) != n {
			t.Fatalf("list default page returned %d rows, want %d (list default should be 1000)", len(page.Data), n)
		}
		if page.NextPageToken != "" {
			t.Errorf("all %d rows fit under the 1000 default; want no nextPageToken, got %q", n, page.NextPageToken)
		}
	})

	t.Run("search default page size stays 100", func(t *testing.T) {
		page, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
			PageSize:    0,
		})
		if err != nil {
			t.Fatalf("SearchObjects: %v", err)
		}
		if len(page.Data) != 100 {
			t.Fatalf("search default page returned %d rows, want 100 (search default must stay 100)", len(page.Data))
		}
		if page.NextPageToken == "" {
			t.Errorf("100-of-%d must page; want a nextPageToken, got none", n)
		}
	})
}

// TestPaginationDefaults_ListLocalOverride guards the constant contract so a
// future edit cannot silently bump the shared default to 1000 (which would
// change search / linked-object / interface list behavior).
func TestPaginationDefaults_ListLocalOverride(t *testing.T) {
	if pagination.DefaultPageSize != 100 {
		t.Errorf("shared DefaultPageSize = %d, want 100 (must not change globally)", pagination.DefaultPageSize)
	}
	if pagination.ListDefaultPageSize != 1000 {
		t.Errorf("ListDefaultPageSize = %d, want 1000 (Foundry list default)", pagination.ListDefaultPageSize)
	}
	if pagination.MaxPageSize != 1000 {
		t.Errorf("MaxPageSize = %d, want 1000", pagination.MaxPageSize)
	}
}

// TestListObjects_OrderBy_PropertiesPrefix locks the Foundry list orderBy form
// `properties.{apiName}:{dir}` as equivalent to the legacy bare `{field}:{dir}`
// form, in both directions.
func TestListObjects_OrderBy_PropertiesPrefix(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	ctx := context.Background()

	order := func(t *testing.T, orderBy string) []string {
		t.Helper()
		page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
			OrderBy:     orderBy,
		})
		if err != nil {
			t.Fatalf("ListObjects(orderBy=%q): %v", orderBy, err)
		}
		pks := make([]string, 0, len(page.Data))
		for _, obj := range page.Data {
			pks = append(pks, fmt.Sprintf("%v", obj.PrimaryKey))
		}
		return pks
	}

	t.Run("properties.age:desc equals bare age:desc", func(t *testing.T) {
		want := []string{"emp3", "emp1", "emp2"} // 35, 30, 25
		bare := order(t, "age:desc")
		prefixed := order(t, "properties.age:desc")
		if !reflect.DeepEqual(bare, want) {
			t.Fatalf("bare age:desc = %v, want %v", bare, want)
		}
		if !reflect.DeepEqual(prefixed, want) {
			t.Fatalf("properties.age:desc = %v, want %v", prefixed, want)
		}
	})

	t.Run("properties.age:asc equals bare age:asc", func(t *testing.T) {
		want := []string{"emp2", "emp1", "emp3"} // 25, 30, 35
		bare := order(t, "age:asc")
		prefixed := order(t, "properties.age:asc")
		if !reflect.DeepEqual(bare, want) {
			t.Fatalf("bare age:asc = %v, want %v", bare, want)
		}
		if !reflect.DeepEqual(prefixed, want) {
			t.Fatalf("properties.age:asc = %v, want %v", prefixed, want)
		}
	})
}
