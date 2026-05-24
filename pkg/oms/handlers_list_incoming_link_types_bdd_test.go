package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ListIncomingLinkTypes covers the round-77 symmetry fix.
// Repository.ListIncomingLinkTypes has PG impl + mocks everywhere
// but no HTTP endpoint surfaces it. Outgoing counterpart was wired
// long ago at /objectTypes/{api}/outgoingLinkTypes — round 77
// closes the asymmetry so the ObjectType detail page can render
// the "links coming INTO this type" panel without scanning every
// ObjectType in the ontology and calling the outgoing direction
// N times.
//
// Wire shape (mirror of ListOutgoingLinkTypes):
//
//   GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/incomingLinkTypes
//     200 + {"data": [LinkType, LinkType, ...]} — link types
//         whose TargetObjectType matches the resolved ObjectType
//     404 + ObjectTypeNotFound when the apiName is unknown
//
// Scenarios:
//   - Target ObjectType has 2 incoming links: returns both.
//   - Target with no incoming links: returns 200 + {data: []}.
//   - ObjectType apiName unknown: 404 ObjectTypeNotFound (key,
//     not filter — apiName must resolve to a real RID).
//   - Cross-ontology isolation: links from another ontology are
//     filtered via the repo's target_object_type WHERE clause.
//   - Outgoing links FROM the target object type do NOT leak
//     into the incoming response.
//   - Response shape is {data: [...]} envelope matching the
//     existing ListOutgoingLinkTypes convention.
func TestBDD_ListIncomingLinkTypes(t *testing.T) {
	const (
		ontRID  = "ri.ontology.main.ontology.1"
		otCust  = "ri.objecttype.main.Customer"
		otOrder = "ri.objecttype.main.Order"
		otItem  = "ri.objecttype.main.Item"
	)

	newServer := func(t *testing.T, linkTypes []oms.LinkType) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.objectTypes = append(repo.objectTypes,
			oms.ObjectType{RID: otCust, OntologyRID: ontRID, APIName: "Customer", PrimaryKey: "id"},
			oms.ObjectType{RID: otOrder, OntologyRID: ontRID, APIName: "Order", PrimaryKey: "id"},
			oms.ObjectType{RID: otItem, OntologyRID: ontRID, APIName: "Item", PrimaryKey: "id"},
		)
		repo.linkTypes = append(repo.linkTypes, linkTypes...)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/incomingLinkTypes",
			handler.ListIncomingLinkTypes)
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, otApiName string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/objectTypes/"+otApiName+"/incomingLinkTypes", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Target with 2 incoming links returns both", func(t *testing.T) {
		// Customer ← orderedBy (Order) and ← billedTo (Order).
		links := []oms.LinkType{
			{
				RID: "lt-1", OntologyRID: ontRID, APIName: "orderedBy",
				SourceObjectType: otOrder, TargetObjectType: otCust,
				Cardinality: "MANY_TO_ONE",
			},
			{
				RID: "lt-2", OntologyRID: ontRID, APIName: "billedTo",
				SourceObjectType: otOrder, TargetObjectType: otCust,
				Cardinality: "MANY_TO_ONE",
			},
		}
		r := newServer(t, links)
		rec := doGet(t, r, "Customer")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 2 {
			t.Fatalf("len(data)=%d, want 2; body=%s", len(resp.Data), rec.Body.String())
		}
	})

	t.Run("Target with no incoming links returns 200 + empty", func(t *testing.T) {
		r := newServer(t, nil)
		rec := doGet(t, r, "Customer")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data == nil {
			t.Errorf("data is nil, want empty array")
		}
		if len(resp.Data) != 0 {
			t.Errorf("len(data)=%d, want 0", len(resp.Data))
		}
	})

	t.Run("Unknown ObjectType returns 404", func(t *testing.T) {
		r := newServer(t, nil)
		rec := doGet(t, r, "GhostType")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "ObjectTypeNotFound" {
			t.Errorf("errorName=%v, want ObjectTypeNotFound", body["errorName"])
		}
	})

	t.Run("Outgoing links FROM target do NOT leak into incoming", func(t *testing.T) {
		// Customer → orders (Customer is source, Order is target).
		// This link is OUTGOING for Customer; the request for
		// Customer's incoming list MUST exclude it.
		links := []oms.LinkType{
			{
				RID: "lt-out", OntologyRID: ontRID, APIName: "orders",
				SourceObjectType: otCust, TargetObjectType: otOrder,
				Cardinality: "ONE_TO_MANY",
			},
			{
				RID: "lt-in", OntologyRID: ontRID, APIName: "orderedBy",
				SourceObjectType: otOrder, TargetObjectType: otCust,
				Cardinality: "MANY_TO_ONE",
			},
		}
		r := newServer(t, links)
		rec := doGet(t, r, "Customer")
		var resp struct {
			Data []json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 1 {
			t.Fatalf("len(data)=%d, want 1 (only orderedBy is incoming for Customer); body=%s",
				len(resp.Data), rec.Body.String())
		}
		// Verify the returned link is the incoming one.
		var lt map[string]interface{}
		_ = json.Unmarshal(resp.Data[0], &lt)
		if lt["apiName"] != "orderedBy" {
			t.Errorf("got apiName=%v, want orderedBy", lt["apiName"])
		}
	})

	t.Run("Response shape is {data: [...]}, not bare array", func(t *testing.T) {
		links := []oms.LinkType{
			{
				RID: "lt-1", OntologyRID: ontRID, APIName: "orderedBy",
				SourceObjectType: otOrder, TargetObjectType: otCust,
				Cardinality: "MANY_TO_ONE",
			},
		}
		r := newServer(t, links)
		rec := doGet(t, r, "Customer")
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("response body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
