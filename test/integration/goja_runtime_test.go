//go:build integration

package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/rid"
)

// ossOntologyClient adapts oss.Service to functions.OntologyClient by
// pre-binding the ontology RID. This is the integration bridge between
// the Goja JS shim (which only knows objectType + primaryKey) and the
// OSS layer (which requires ontologyRID on every call).
type ossOntologyClient struct {
	svc         oss.Service
	ontologyRID string
}

func (c *ossOntologyClient) GetObject(ctx context.Context, objectType, primaryKey string) (*oss.WireObject, error) {
	return c.svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: c.ontologyRID,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
	})
}

func (c *ossOntologyClient) SearchObjects(ctx context.Context, objectType string, w *where.WhereClause, pageSize int) (*oss.ObjectPage, error) {
	return c.svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: c.ontologyRID,
		ObjectType:  objectType,
		Where:       w,
		PageSize:    pageSize,
	})
}

// TestGojaRuntime_Integration is the US-090 acceptance test proving the Goja
// embedded runtime is usable and safe. It exercises the full stack: real
// PostgreSQL for OMS/function storage, real Bleve for object search, and the
// Goja VM with ontology JS shim.
//
// Scenarios:
//  1. hello world - basic function execution
//  2. findOrdersOver(1000) - ontology.search with where clause
//  3. sandbox escape - fetch/require blocked
//  4. while(true) timeout - 5s interrupt
func TestGojaRuntime_Integration(t *testing.T) {
	ctx := context.Background()

	// --- Infrastructure: real Postgres + Bleve ---
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "goja_e2e",
		DisplayName: "Goja E2E",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	orderOT := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "order",
		DisplayName: "Order",
		PrimaryKey:  "orderId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, orderOT); err != nil {
		t.Fatalf("create object type: %v", err)
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	props := []index.Property{
		{APIName: "orderId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "amount", BaseType: "double", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("order", props); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// Seed 5 orders: 3 over 1000, 2 under.
	orders := []struct {
		pk     string
		amount float64
	}{
		{"ORD-001", 500.0},
		{"ORD-002", 1500.0},
		{"ORD-003", 2000.0},
		{"ORD-004", 300.0},
		{"ORD-005", 3000.0},
	}
	for _, o := range orders {
		doc := map[string]interface{}{
			"orderId": o.pk,
			"amount":  o.amount,
		}
		if err := mgr.IndexDocument("order", o.pk, doc); err != nil {
			t.Fatalf("index %s: %v", o.pk, err)
		}
	}
	time.Sleep(200 * time.Millisecond) // settle for Bleve commit

	svc := oss.NewService(repo, mgr, nil)
	client := &ossOntologyClient{svc: svc, ontologyRID: ont.RID}

	// Store the findOrdersOver function in PG to prove CRUD round-trips.
	fnSource := `function main(input) {
  var result = ontology.search("order", {type: "gt", field: "amount", value: 1000});
  var ids = [];
  for (var i = 0; i < result.data.length; i++) {
    ids.push(result.data[i].orderId);
  }
  return ids;
}`
	fn := &oms.Function{
		RID:         rid.NewFunctionRID(),
		OntologyRID: ont.RID,
		Name:        "findOrdersOver",
		Version:     "1.0.0",
		SourceCode:  fnSource,
		CreatedBy:   "integration-test",
	}
	if err := repo.CreateFunction(ctx, fn); err != nil {
		t.Fatalf("create function: %v", err)
	}

	// Verify function round-trips from DB.
	storedFn, err := repo.GetFunctionByName(ctx, ont.RID, "findOrdersOver")
	if err != nil {
		t.Fatalf("get function by name: %v", err)
	}
	if storedFn.SourceCode != fnSource {
		t.Fatalf("stored source mismatch:\n  got:  %s\n  want: %s", storedFn.SourceCode, fnSource)
	}

	// --- Scenario 1: hello world function returns string ---
	t.Run("hello_world", func(t *testing.T) {
		rt := functions.NewRuntime(functions.DefaultConfig())
		result, err := rt.Execute(ctx, `function main() { return "Hello, World!"; }`, nil)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result != "Hello, World!" {
			t.Errorf("got %v, want Hello, World!", result)
		}
	})

	// --- Scenario 2: findOrdersOver(1000) calls ontology.search, returns IDs ---
	t.Run("findOrdersOver", func(t *testing.T) {
		rt := functions.NewRuntime(functions.DefaultConfig())
		rt.SetOntologyClient(client)

		// Execute the function source retrieved from the database.
		result, err := rt.Execute(ctx, storedFn.SourceCode, nil)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}

		ids, ok := result.([]interface{})
		if !ok {
			t.Fatalf("expected []interface{}, got %T: %v", result, result)
		}
		if len(ids) != 3 {
			t.Fatalf("expected 3 orders over 1000, got %d: %v", len(ids), ids)
		}

		expected := map[string]bool{"ORD-002": true, "ORD-003": true, "ORD-005": true}
		for _, id := range ids {
			s, ok := id.(string)
			if !ok {
				t.Errorf("non-string ID: %T %v", id, id)
				continue
			}
			if !expected[s] {
				t.Errorf("unexpected order ID %q in result", s)
			}
		}
	})

	// --- Scenario 3: sandbox escape via fetch / require fails ---
	t.Run("sandbox_escape", func(t *testing.T) {
		rt := functions.NewRuntime(functions.DefaultConfig())

		cases := []struct {
			name   string
			source string
		}{
			{"fetch", `function main() { return fetch("http://evil.com"); }`},
			{"require", `function main() { return require("fs"); }`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := rt.Execute(ctx, tc.source, nil)
				if err == nil {
					t.Fatal("expected error for sandbox escape, got nil")
				}
			})
		}
	})

	// --- Scenario 4: while(true) timeout after 5s ---
	t.Run("timeout_while_true", func(t *testing.T) {
		rt := functions.NewRuntime(functions.DefaultConfig()) // 5s timeout

		start := time.Now()
		_, err := rt.Execute(ctx, `function main() { while(true) {} }`, nil)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		errStr := strings.ToLower(err.Error())
		if !strings.Contains(errStr, "timeout") &&
			!strings.Contains(errStr, "interrupt") &&
			!strings.Contains(errStr, "cancel") {
			t.Errorf("expected timeout/interrupt/cancel in error, got: %v", err)
		}
		if elapsed > 10*time.Second {
			t.Errorf("timeout took too long: %v (expected ~5s)", elapsed)
		}
	})
}
