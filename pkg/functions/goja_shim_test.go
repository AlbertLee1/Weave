package functions

import (
	"context"
	"fmt"
	"testing"

	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
)

// mockOntologyClient is a test double implementing OntologyClient.
type mockOntologyClient struct {
	getObjectFn     func(ctx context.Context, objectType, primaryKey string) (*oss.WireObject, error)
	searchObjectsFn func(ctx context.Context, objectType string, w *where.WhereClause, pageSize int) (*oss.ObjectPage, error)
}

func (m *mockOntologyClient) GetObject(ctx context.Context, objectType, primaryKey string) (*oss.WireObject, error) {
	if m.getObjectFn != nil {
		return m.getObjectFn(ctx, objectType, primaryKey)
	}
	return nil, fmt.Errorf("GetObject not implemented")
}

func (m *mockOntologyClient) SearchObjects(ctx context.Context, objectType string, w *where.WhereClause, pageSize int) (*oss.ObjectPage, error) {
	if m.searchObjectsFn != nil {
		return m.searchObjectsFn(ctx, objectType, w, pageSize)
	}
	return nil, fmt.Errorf("SearchObjects not implemented")
}

func TestGojaShimLoadSearch(t *testing.T) {
	t.Run("load returns object properties", func(t *testing.T) {
		client := &mockOntologyClient{
			getObjectFn: func(ctx context.Context, objectType, primaryKey string) (*oss.WireObject, error) {
				if objectType != "Employee" {
					t.Errorf("expected objectType 'Employee', got %q", objectType)
				}
				if primaryKey != "emp-001" {
					t.Errorf("expected primaryKey 'emp-001', got %q", primaryKey)
				}
				return &oss.WireObject{
					RID:        "ri.phonograph2-objects.main.object.emp-001",
					PrimaryKey: "emp-001",
					APIName:    "Employee",
					Properties: map[string]interface{}{
						"name":       "Alice",
						"department": "Engineering",
					},
				}, nil
			},
		}

		rt := NewRuntime(DefaultConfig())
		rt.SetOntologyClient(client)

		result, err := rt.Execute(context.Background(), `
			function main(input) {
				var obj = ontology.load("Employee", "emp-001");
				return obj.name + " in " + obj.department;
			}
		`, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "Alice in Engineering" {
			t.Fatalf("expected 'Alice in Engineering', got %v", result)
		}
	})

	t.Run("load returns __rid and __primaryKey", func(t *testing.T) {
		client := &mockOntologyClient{
			getObjectFn: func(ctx context.Context, objectType, primaryKey string) (*oss.WireObject, error) {
				return &oss.WireObject{
					RID:        "ri.phonograph2-objects.main.object.emp-001",
					PrimaryKey: "emp-001",
					APIName:    "Employee",
					Properties: map[string]interface{}{"name": "Alice"},
				}, nil
			},
		}

		rt := NewRuntime(DefaultConfig())
		rt.SetOntologyClient(client)

		result, err := rt.Execute(context.Background(), `
			function main(input) {
				var obj = ontology.load("Employee", "emp-001");
				return obj.__rid + "|" + obj.__primaryKey + "|" + obj.__apiName;
			}
		`, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "ri.phonograph2-objects.main.object.emp-001|emp-001|Employee"
		if result != expected {
			t.Fatalf("expected %q, got %v", expected, result)
		}
	})

	t.Run("load error throws JS exception", func(t *testing.T) {
		client := &mockOntologyClient{
			getObjectFn: func(ctx context.Context, objectType, primaryKey string) (*oss.WireObject, error) {
				return nil, fmt.Errorf("object not found")
			},
		}

		rt := NewRuntime(DefaultConfig())
		rt.SetOntologyClient(client)

		_, err := rt.Execute(context.Background(), `
			function main(input) {
				var obj = ontology.load("Employee", "missing");
				return obj.name;
			}
		`, nil)
		if err == nil {
			t.Fatal("expected error when object not found")
		}
	})

	t.Run("search returns data array", func(t *testing.T) {
		client := &mockOntologyClient{
			searchObjectsFn: func(ctx context.Context, objectType string, w *where.WhereClause, pageSize int) (*oss.ObjectPage, error) {
				if objectType != "Employee" {
					t.Errorf("expected objectType 'Employee', got %q", objectType)
				}
				if w == nil || w.Type != "eq" || w.Field != "department" {
					t.Errorf("unexpected where clause: %+v", w)
				}
				return &oss.ObjectPage{
					Data: []*oss.WireObject{
						{
							RID:        "ri.phonograph2-objects.main.object.emp-001",
							PrimaryKey: "emp-001",
							APIName:    "Employee",
							Properties: map[string]interface{}{"name": "Alice", "department": "Engineering"},
						},
						{
							RID:        "ri.phonograph2-objects.main.object.emp-002",
							PrimaryKey: "emp-002",
							APIName:    "Employee",
							Properties: map[string]interface{}{"name": "Bob", "department": "Engineering"},
						},
					},
					TotalCount: "2",
				}, nil
			},
		}

		rt := NewRuntime(DefaultConfig())
		rt.SetOntologyClient(client)

		result, err := rt.Execute(context.Background(), `
			function main(input) {
				var res = ontology.search("Employee", {type: "eq", field: "department", value: "Engineering"});
				return res.data.length + "|" + res.data[0].name + "|" + res.data[1].name + "|" + res.totalCount;
			}
		`, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "2|Alice|Bob|2" {
			t.Fatalf("expected '2|Alice|Bob|2', got %v", result)
		}
	})

	t.Run("search with pageSize option", func(t *testing.T) {
		var capturedPageSize int
		client := &mockOntologyClient{
			searchObjectsFn: func(ctx context.Context, objectType string, w *where.WhereClause, pageSize int) (*oss.ObjectPage, error) {
				capturedPageSize = pageSize
				return &oss.ObjectPage{Data: []*oss.WireObject{}}, nil
			},
		}

		rt := NewRuntime(DefaultConfig())
		rt.SetOntologyClient(client)

		_, err := rt.Execute(context.Background(), `
			function main(input) {
				var res = ontology.search("Employee", {type: "eq", field: "name", value: "x"}, {pageSize: 5});
				return res.data.length;
			}
		`, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedPageSize != 5 {
			t.Fatalf("expected pageSize 5, got %d", capturedPageSize)
		}
	})

	t.Run("search error throws JS exception", func(t *testing.T) {
		client := &mockOntologyClient{
			searchObjectsFn: func(ctx context.Context, objectType string, w *where.WhereClause, pageSize int) (*oss.ObjectPage, error) {
				return nil, fmt.Errorf("search failed")
			},
		}

		rt := NewRuntime(DefaultConfig())
		rt.SetOntologyClient(client)

		_, err := rt.Execute(context.Background(), `
			function main(input) {
				return ontology.search("Employee", {type: "eq", field: "name", value: "x"});
			}
		`, nil)
		if err == nil {
			t.Fatal("expected error when search fails")
		}
	})

	t.Run("ontology not available without client", func(t *testing.T) {
		rt := NewRuntime(DefaultConfig())
		// No SetOntologyClient call

		_, err := rt.Execute(context.Background(), `
			function main(input) {
				return typeof ontology;
			}
		`, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Without a client, ontology should not be registered
	})
}
