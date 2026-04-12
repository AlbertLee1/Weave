//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// recordingPublisher captures published EditBatches so the test can assert on
// the wire-level output of the action executor without standing up real NATS.
type recordingPublisher struct {
	batches []funnel.EditBatch
}

func (p *recordingPublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	p.batches = append(p.batches, *batch)
	return uint64(len(p.batches)), nil
}

// TestFunctionAction_CreateOrderAndDecrementStock is the US-093 acceptance
// test proving that a function-backed action can produce multiple edits
// (CREATE an Order + MODIFY a Product to decrement stock) and that those
// edits flow through the executor → publisher → consumer → Bleve pipeline
// correctly.
//
// Full integration stack: real PostgreSQL (OMS metadata + function storage),
// real Bleve (object indexes), Goja VM (function execution), in-process
// funnel consumer (edit application).
func TestFunctionAction_CreateOrderAndDecrementStock(t *testing.T) {
	ctx := context.Background()

	// ── 1. Infrastructure: PostgreSQL + migrations ──
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)

	// ── 2. Ontology + ObjectTypes ──
	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "fn_action_e2e",
		DisplayName: "Function Action E2E",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	productOT := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "product",
		DisplayName: "Product",
		PrimaryKey:  "productId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, productOT); err != nil {
		t.Fatalf("create product object type: %v", err)
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
		t.Fatalf("create order object type: %v", err)
	}

	// ── 3. Bleve indexes ──
	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	productScopedKey := index.ScopedKey(ont.APIName, productOT.APIName)
	orderScopedKey := index.ScopedKey(ont.APIName, orderOT.APIName)

	productProps := []index.Property{
		{APIName: "productId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "stock", BaseType: "integer", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(productScopedKey, productProps); err != nil {
		t.Fatalf("ensure product index: %v", err)
	}

	orderProps := []index.Property{
		{APIName: "orderId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "productId", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "quantity", BaseType: "integer", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(orderScopedKey, orderProps); err != nil {
		t.Fatalf("ensure order index: %v", err)
	}

	// ── 4. Seed initial Product with stock=100 ──
	if err := mgr.IndexDocument(productScopedKey, "PROD-001", map[string]interface{}{
		"productId": "PROD-001",
		"name":      "Widget",
		"stock":     100,
	}); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // Bleve settle

	// ── 5. Create Goja function: createOrderAndUpdateInventory ──
	// The function creates an Order and decrements Product.stock by the
	// requested quantity. It returns {edits: [CREATE order, MODIFY product]}.
	fnSource := `function main(input) {
  var params = input.parameters;
  var orderId = "ORD-" + params.productId + "-" + params.quantity;
  return {
    edits: [
      {
        type: "CREATE",
        objectType: "order",
        primaryKey: orderId,
        properties: {
          orderId: orderId,
          productId: params.productId,
          quantity: params.quantity
        }
      },
      {
        type: "MODIFY",
        objectType: "product",
        primaryKey: params.productId,
        properties: {
          productId: params.productId,
          name: "Widget",
          stock: 100 - params.quantity
        }
      }
    ]
  };
}`
	fn := &oms.Function{
		RID:         rid.NewFunctionRID(),
		OntologyRID: ont.RID,
		Name:        "createOrderAndUpdateInventory",
		Version:     1,
		SourceCode:  fnSource,
		CreatedBy:   "integration-test",
	}
	if err := repo.CreateFunction(ctx, fn); err != nil {
		t.Fatalf("create function: %v", err)
	}

	// ── 6. Create function-backed ActionType ──
	paramsDef := json.RawMessage(`[
		{"id":"productId","type":"string","required":true},
		{"id":"quantity","type":"integer","required":true}
	]`)
	actionType := &oms.ActionType{
		RID:              rid.NewActionTypeRID(),
		OntologyRID:      ont.RID,
		APIName:          "createOrderAndUpdateInventory",
		DisplayName:      "Create Order & Update Inventory",
		Status:           "ACTIVE",
		Parameters:       paramsDef,
		Rules:            json.RawMessage(`[]`),
		FunctionRID:      fn.RID,
		IsFunctionBacked: true,
	}
	if err := repo.CreateActionType(ctx, actionType); err != nil {
		t.Fatalf("create action type: %v", err)
	}

	// ── 7. Wire executor: Goja runtime + GojaDispatcher + recording publisher ──
	rt := functions.NewRuntime(functions.DefaultConfig())
	dispatcher := actions.NewGojaDispatcher(rt, repo)

	pub := &recordingPublisher{}
	executor := actions.NewExecutor(repo, pub)
	executor.SetFunctionDispatcher(dispatcher)

	// ── 8. Apply the action ──
	result, err := executor.Apply(ctx, ont.APIName, &actions.ApplyRequest{
		ActionType: "createOrderAndUpdateInventory",
		Parameters: map[string]interface{}{
			"productId": "PROD-001",
			"quantity":  10,
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// ── 9. Assert executor produced 2 edits ──
	if len(result.Edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(result.Edits))
	}

	// Verify edit types
	if result.Edits[0].Type != funnel.EditTypeCreate {
		t.Errorf("edit[0] type: got %q, want CREATE", result.Edits[0].Type)
	}
	if result.Edits[0].ObjectType != "order" {
		t.Errorf("edit[0] objectType: got %q, want order", result.Edits[0].ObjectType)
	}
	if result.Edits[1].Type != funnel.EditTypeModify {
		t.Errorf("edit[1] type: got %q, want MODIFY", result.Edits[1].Type)
	}
	if result.Edits[1].ObjectType != "product" {
		t.Errorf("edit[1] objectType: got %q, want product", result.Edits[1].ObjectType)
	}

	// Verify edits are tagged as user source
	for i, e := range result.Edits {
		if e.Source != funnel.EditSourceUser {
			t.Errorf("edit[%d] source: got %q, want %q", i, e.Source, funnel.EditSourceUser)
		}
	}

	// ── 10. Assert publisher received the EditBatch ──
	if len(pub.batches) != 1 {
		t.Fatalf("expected 1 published batch, got %d", len(pub.batches))
	}
	publishedBatch := pub.batches[0]
	if len(publishedBatch.Edits) != 2 {
		t.Fatalf("published batch has %d edits, want 2", len(publishedBatch.Edits))
	}

	// ── 11. Apply edits to Bleve via consumer (simulating NATS consumer) ──
	consumer := funnel.NewConsumer(nil, mgr)
	if err := consumer.ApplyBatch(ctx, publishedBatch); err != nil {
		t.Fatalf("consumer ApplyBatch: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // Bleve settle

	// ── 12. Verify Order was created in Bleve ──
	expectedOrderPK := "ORD-PROD-001-10"
	orderQuery := bleve.NewDocIDQuery([]string{expectedOrderPK})
	orderReq := bleve.NewSearchRequest(orderQuery)
	orderReq.Fields = []string{"*"}
	orderResult, err := mgr.Search(orderScopedKey, orderReq)
	if err != nil {
		t.Fatalf("search order: %v", err)
	}
	if orderResult.Total != 1 {
		t.Fatalf("expected 1 order in Bleve, got %d", orderResult.Total)
	}
	orderDoc := orderResult.Hits[0].Fields
	if orderDoc["orderId"] != expectedOrderPK {
		t.Errorf("order.orderId: got %v, want %s", orderDoc["orderId"], expectedOrderPK)
	}
	if orderDoc["productId"] != "PROD-001" {
		t.Errorf("order.productId: got %v, want PROD-001", orderDoc["productId"])
	}
	// Bleve returns numerics as float64
	if qty, ok := orderDoc["quantity"].(float64); !ok || qty != 10 {
		t.Errorf("order.quantity: got %v (%T), want 10", orderDoc["quantity"], orderDoc["quantity"])
	}

	// ── 13. Verify Product.stock was decremented in Bleve ──
	productQuery := bleve.NewDocIDQuery([]string{"PROD-001"})
	productReq := bleve.NewSearchRequest(productQuery)
	productReq.Fields = []string{"*"}
	productResult, err := mgr.Search(productScopedKey, productReq)
	if err != nil {
		t.Fatalf("search product: %v", err)
	}
	if productResult.Total != 1 {
		t.Fatalf("expected 1 product in Bleve, got %d", productResult.Total)
	}
	productDoc := productResult.Hits[0].Fields
	stock, ok := productDoc["stock"].(float64)
	if !ok {
		t.Fatalf("product.stock not float64: %T %v", productDoc["stock"], productDoc["stock"])
	}
	if stock != 90 {
		t.Errorf("product.stock: got %v, want 90 (100 - 10)", stock)
	}
}
