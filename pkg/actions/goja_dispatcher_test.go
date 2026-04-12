package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// Mock function lookup
// ---------------------------------------------------------------------------

type mockFunctionLookup struct {
	fn  *oms.Function
	err error
}

func (m *mockFunctionLookup) GetFunction(_ context.Context, rid string) (*oms.Function, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.fn != nil && m.fn.RID == rid {
		return m.fn, nil
	}
	return nil, oms.ErrNotFound
}

// ---------------------------------------------------------------------------
// GojaDispatcher tests
// ---------------------------------------------------------------------------

// TestActionGojaDispatch verifies that a function-backed action with a
// ri.ontology.* FunctionRID is dispatched through the Goja runtime, producing
// the correct funnel.Edit slice.
func TestActionGojaDispatch(t *testing.T) {
	fnRID := "ri.ontology.main.function.send-welcome-email"
	lookup := &mockFunctionLookup{
		fn: &oms.Function{
			RID:  fnRID,
			Name: "sendWelcomeEmail",
			SourceCode: `function main(input) {
				return {
					edits: [
						{
							type: "CREATE",
							objectType: "Email",
							primaryKey: "email-001",
							properties: { to: input.parameters.to, subject: "Welcome" }
						}
					]
				};
			}`,
		},
	}

	rt := functions.NewRuntime(functions.DefaultConfig())
	dispatcher := NewGojaDispatcher(rt, lookup)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.send-email",
		APIName:          "sendWelcomeEmail",
		FunctionRID:      fnRID,
		IsFunctionBacked: true,
	}

	edits, err := dispatcher.Dispatch(context.Background(), at, map[string]interface{}{
		"to": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeCreate {
		t.Errorf("expected CREATE, got %v", edits[0].Type)
	}
	if edits[0].ObjectType != "Email" {
		t.Errorf("expected objectType=Email, got %q", edits[0].ObjectType)
	}
	if edits[0].PrimaryKey != "email-001" {
		t.Errorf("expected primaryKey=email-001, got %q", edits[0].PrimaryKey)
	}
	if edits[0].Properties["to"] != "alice@example.com" {
		t.Errorf("expected to=alice@example.com, got %v", edits[0].Properties["to"])
	}
	if edits[0].Properties["subject"] != "Welcome" {
		t.Errorf("expected subject=Welcome, got %v", edits[0].Properties["subject"])
	}
}

// TestActionGojaDispatch_MultipleEdits verifies that a Goja function returning
// multiple edits (CREATE + MODIFY) produces the correct slice.
func TestActionGojaDispatch_MultipleEdits(t *testing.T) {
	fnRID := "ri.ontology.main.function.create-order"
	lookup := &mockFunctionLookup{
		fn: &oms.Function{
			RID:  fnRID,
			Name: "createOrderAndUpdateInventory",
			SourceCode: `function main(input) {
				return {
					edits: [
						{
							type: "CREATE",
							objectType: "Order",
							primaryKey: input.parameters.orderId,
							properties: { productId: input.parameters.productId, quantity: input.parameters.quantity }
						},
						{
							type: "MODIFY",
							objectType: "Product",
							primaryKey: input.parameters.productId,
							properties: { stock: input.parameters.currentStock - input.parameters.quantity }
						}
					]
				};
			}`,
		},
	}

	rt := functions.NewRuntime(functions.DefaultConfig())
	dispatcher := NewGojaDispatcher(rt, lookup)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.create-order",
		APIName:          "createOrderAndUpdateInventory",
		FunctionRID:      fnRID,
		IsFunctionBacked: true,
	}

	edits, err := dispatcher.Dispatch(context.Background(), at, map[string]interface{}{
		"orderId":      "order-001",
		"productId":    "prod-A",
		"quantity":     float64(5),
		"currentStock": float64(100),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}

	// First edit: CREATE Order
	if edits[0].Type != funnel.EditTypeCreate {
		t.Errorf("edit[0] expected CREATE, got %v", edits[0].Type)
	}
	if edits[0].ObjectType != "Order" {
		t.Errorf("edit[0] expected Order, got %q", edits[0].ObjectType)
	}

	// Second edit: MODIFY Product
	if edits[1].Type != funnel.EditTypeModify {
		t.Errorf("edit[1] expected MODIFY, got %v", edits[1].Type)
	}
	if edits[1].ObjectType != "Product" {
		t.Errorf("edit[1] expected Product, got %q", edits[1].ObjectType)
	}
}

// TestActionGojaDispatch_FunctionNotFound verifies proper error when function
// RID is not found in the lookup.
func TestActionGojaDispatch_FunctionNotFound(t *testing.T) {
	lookup := &mockFunctionLookup{} // no function registered
	rt := functions.NewRuntime(functions.DefaultConfig())
	dispatcher := NewGojaDispatcher(rt, lookup)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.missing",
		APIName:          "missingFn",
		FunctionRID:      "ri.ontology.main.function.does-not-exist",
		IsFunctionBacked: true,
	}

	_, err := dispatcher.Dispatch(context.Background(), at, nil)
	if err == nil {
		t.Fatal("expected error for missing function")
	}
	if !strings.Contains(err.Error(), "lookup function") {
		t.Errorf("expected lookup error, got: %v", err)
	}
}

// TestActionGojaDispatch_InvalidOutput verifies that a function returning
// a non-{edits: []} shape produces a clear error.
func TestActionGojaDispatch_InvalidOutput(t *testing.T) {
	fnRID := "ri.ontology.main.function.bad-output"
	lookup := &mockFunctionLookup{
		fn: &oms.Function{
			RID:        fnRID,
			Name:       "badOutput",
			SourceCode: `function main(input) { return "not an object"; }`,
		},
	}

	rt := functions.NewRuntime(functions.DefaultConfig())
	dispatcher := NewGojaDispatcher(rt, lookup)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.bad",
		APIName:          "badOutput",
		FunctionRID:      fnRID,
		IsFunctionBacked: true,
	}

	_, err := dispatcher.Dispatch(context.Background(), at, nil)
	if err == nil {
		t.Fatal("expected error for invalid function output")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T: %v", err, err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected StatusCode=400, got %d", apiErr.StatusCode)
	}
}

// TestActionGojaDispatch_FunctionError verifies that a function returning
// an error field propagates it correctly.
func TestActionGojaDispatch_FunctionError(t *testing.T) {
	fnRID := "ri.ontology.main.function.error-fn"
	lookup := &mockFunctionLookup{
		fn: &oms.Function{
			RID:        fnRID,
			Name:       "errorFn",
			SourceCode: `function main(input) { return { error: "insufficient stock", edits: [] }; }`,
		},
	}

	rt := functions.NewRuntime(functions.DefaultConfig())
	dispatcher := NewGojaDispatcher(rt, lookup)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.err",
		APIName:          "errorFn",
		FunctionRID:      fnRID,
		IsFunctionBacked: true,
	}

	_, err := dispatcher.Dispatch(context.Background(), at, nil)
	if err == nil {
		t.Fatal("expected error from function error field")
	}
	if !strings.Contains(err.Error(), "insufficient stock") {
		t.Errorf("expected error to contain 'insufficient stock', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RoutingDispatcher tests
// ---------------------------------------------------------------------------

// TestActionHTTPDispatch verifies that when FunctionRID is an HTTP URL, the
// RoutingDispatcher routes to the HTTPDispatcher.
func TestActionHTTPDispatch(t *testing.T) {
	// Spin up a test HTTP server that returns a valid FunctionResponse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req FunctionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		resp := FunctionResponse{
			Edits: []FunctionEdit{
				{
					Type:       "CREATE",
					ObjectType: "Notification",
					PrimaryKey: "notif-001",
					Properties: map[string]interface{}{"channel": req.Parameters["channel"]},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	httpDisp := NewHTTPDispatcher(srv.URL)
	gojaDisp := NewGojaDispatcher(
		functions.NewRuntime(functions.DefaultConfig()),
		&mockFunctionLookup{}, // no functions — should not be called
	)
	router := NewRoutingDispatcher(gojaDisp, httpDisp)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.notify",
		APIName:          "sendNotification",
		FunctionRID:      srv.URL + "/functions/notify",
		IsFunctionBacked: true,
	}

	edits, err := router.Dispatch(context.Background(), at, map[string]interface{}{
		"channel": "slack",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeCreate {
		t.Errorf("expected CREATE, got %v", edits[0].Type)
	}
	if edits[0].ObjectType != "Notification" {
		t.Errorf("expected Notification, got %q", edits[0].ObjectType)
	}
}

// TestRoutingFunctionDispatch_GojaRoute verifies that ri.* RIDs route to the
// GojaDispatcher.
func TestRoutingFunctionDispatch_GojaRoute(t *testing.T) {
	fnRID := "ri.ontology.main.function.hello"
	lookup := &mockFunctionLookup{
		fn: &oms.Function{
			RID:  fnRID,
			Name: "hello",
			SourceCode: `function main(input) {
				return { edits: [{ type: "CREATE", objectType: "Greeting", primaryKey: "g1", properties: {} }] };
			}`,
		},
	}

	gojaDisp := NewGojaDispatcher(
		functions.NewRuntime(functions.DefaultConfig()),
		lookup,
	)
	// HTTP dispatcher is nil — should not be reached.
	router := NewRoutingDispatcher(gojaDisp, nil)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.hello",
		APIName:          "hello",
		FunctionRID:      fnRID,
		IsFunctionBacked: true,
	}

	edits, err := router.Dispatch(context.Background(), at, nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].ObjectType != "Greeting" {
		t.Errorf("expected Greeting, got %q", edits[0].ObjectType)
	}
}

// TestRoutingFunctionDispatch_HTTPSRoute verifies that https:// URLs route to
// the HTTPDispatcher.
func TestRoutingFunctionDispatch_HTTPSRoute(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := FunctionResponse{
			Edits: []FunctionEdit{
				{Type: "MODIFY", ObjectType: "Config", PrimaryKey: "cfg-1"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	httpDisp := &HTTPDispatcher{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}
	router := NewRoutingDispatcher(nil, httpDisp)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.cfg",
		APIName:          "updateConfig",
		FunctionRID:      srv.URL + "/functions/update",
		IsFunctionBacked: true,
	}

	edits, err := router.Dispatch(context.Background(), at, nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeModify {
		t.Errorf("expected MODIFY, got %v", edits[0].Type)
	}
}

// TestRoutingFunctionDispatch_NoGojaDispatcher verifies error when Goja
// dispatcher is nil for ri.* RIDs.
func TestRoutingFunctionDispatch_NoGojaDispatcher(t *testing.T) {
	router := NewRoutingDispatcher(nil, nil)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.x",
		APIName:          "x",
		FunctionRID:      "ri.ontology.main.function.x",
		IsFunctionBacked: true,
	}

	_, err := router.Dispatch(context.Background(), at, nil)
	if err == nil {
		t.Fatal("expected error when goja dispatcher is nil")
	}
	if !strings.Contains(err.Error(), "Goja dispatcher not configured") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRoutingFunctionDispatch_NoHTTPDispatcher verifies error when HTTP
// dispatcher is nil for http:// URLs.
func TestRoutingFunctionDispatch_NoHTTPDispatcher(t *testing.T) {
	router := NewRoutingDispatcher(nil, nil)

	at := &oms.ActionType{
		RID:              "ri.ontology.main.actionType.y",
		APIName:          "y",
		FunctionRID:      "http://example.com/fn",
		IsFunctionBacked: true,
	}

	_, err := router.Dispatch(context.Background(), at, nil)
	if err == nil {
		t.Fatal("expected error when http dispatcher is nil")
	}
	if !strings.Contains(err.Error(), "HTTP dispatcher not configured") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Ensure fmt is used (suppress unused import).
var _ = fmt.Sprintf
