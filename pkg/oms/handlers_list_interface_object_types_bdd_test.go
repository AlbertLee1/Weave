package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ListInterfaceObjectTypes covers the round-75 symmetry
// fix. The Repository interface has both directions for the
// ObjectType↔Interface m:n relationship:
//
//   - ListObjectTypeInterfaces (which interfaces does
//     ObjectType X implement) — wired at
//     GET /api/v2/ontologies/{o}/objectTypes/byRid/{otRid}/interfaces
//   - ListInterfaceObjectTypes (which ObjectTypes implement
//     Interface Y) — no HTTP endpoint surfaces it despite the
//     repo method existing on every mock.
//
// Round 75 closes the symmetry gap: the Interface admin UI panel
// can now render the list of implementing ObjectTypes without
// scanning every ObjectType and calling the reverse direction
// N times.
//
// Wire shape:
//
//	GET /api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceRid}/objectTypes
//	  200 + {"data": [ObjectType, ObjectType, ...]}
//	      non-nil empty when interface has no implementors
//
// Scenarios:
//   - Known interface with 3 implementors returns all three.
//   - Unknown interface returns 200 + {data: []} — filter-not-key
//     per the round-68/69/73 pattern.
//   - The reverse-direction handler stays untouched (regression
//     guard so the new endpoint doesn't accidentally drift the
//     existing one's behavior).
//   - Object-types from other interfaces don't leak in.
//   - Response shape is {data: [...]}, not a bare array — matches
//     the existing OMS list-endpoint convention so SDK
//     deserialisation stays uniform.
func TestBDD_ListInterfaceObjectTypes(t *testing.T) {
	const (
		interfaceA = "ri.interface.main.HasOwner"
		interfaceB = "ri.interface.main.Searchable"
		otCust     = "ri.objectType.main.Customer"
		otSupplier = "ri.objectType.main.Supplier"
		otEmployee = "ri.objectType.main.Employee"
		otOrder    = "ri.objectType.main.Order"
	)

	newServer := func(t *testing.T, implementorsByInterface map[string][]oms.ObjectType) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		// Seed ObjectTypes the repo knows about so ListInterfaceObjectTypes
		// can return them. The mock filters via the interfaceAttachments
		// map; we set it up so the reverse lookup works.
		seen := map[string]bool{}
		repo.interfaceAttachments = nil
		for ifRID, ots := range implementorsByInterface {
			for _, ot := range ots {
				if !seen[ot.RID] {
					repo.objectTypes = append(repo.objectTypes, ot)
					seen[ot.RID] = true
				}
				repo.interfaceAttachments = append(repo.interfaceAttachments,
					oms.ObjectTypeInterface{ObjectTypeRID: ot.RID, InterfaceRID: ifRID})
			}
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceRid}/objectTypes",
			handler.ListInterfaceObjectTypesV2)
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, ifRID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/test/interfaces/"+ifRID+"/objectTypes", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Known interface with 3 implementors returns all three", func(t *testing.T) {
		r := newServer(t, map[string][]oms.ObjectType{
			interfaceA: {
				{RID: otCust, APIName: "Customer", DisplayName: "Customer"},
				{RID: otSupplier, APIName: "Supplier", DisplayName: "Supplier"},
				{RID: otEmployee, APIName: "Employee", DisplayName: "Employee"},
			},
		})
		rec := doGet(t, r, interfaceA)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []oms.ObjectType `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Data) != 3 {
			t.Fatalf("len(data)=%d, want 3; body=%s", len(resp.Data), rec.Body.String())
		}
		rids := map[string]bool{}
		for _, ot := range resp.Data {
			rids[ot.RID] = true
		}
		for _, want := range []string{otCust, otSupplier, otEmployee} {
			if !rids[want] {
				t.Errorf("missing implementor %s in response", want)
			}
		}
	})

	t.Run("Unknown interface returns 200 + empty data", func(t *testing.T) {
		r := newServer(t, nil)
		rec := doGet(t, r, "ri.interface.main.GHOST")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200 (filter-not-key)", rec.Code)
		}
		var resp struct {
			Data []oms.ObjectType `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data == nil {
			t.Errorf("data is nil, want empty array")
		}
		if len(resp.Data) != 0 {
			t.Errorf("len(data)=%d, want 0", len(resp.Data))
		}
	})

	t.Run("Cross-interface isolation: B's implementors not returned for A", func(t *testing.T) {
		r := newServer(t, map[string][]oms.ObjectType{
			interfaceA: {{RID: otCust, APIName: "Customer", DisplayName: "Customer"}},
			interfaceB: {
				{RID: otOrder, APIName: "Order", DisplayName: "Order"},
				{RID: otEmployee, APIName: "Employee", DisplayName: "Employee"},
			},
		})
		rec := doGet(t, r, interfaceA)
		var resp struct {
			Data []oms.ObjectType `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 1 || resp.Data[0].RID != otCust {
			t.Errorf("got %v, want only %s for interfaceA", resp.Data, otCust)
		}
	})

	t.Run("Response shape is {data: [...]}, not bare array", func(t *testing.T) {
		// Matches the existing OMS list-endpoint convention (every
		// other list-foo endpoint also uses {data: ...} so SDK
		// deserialisation stays uniform).
		r := newServer(t, map[string][]oms.ObjectType{
			interfaceA: {{RID: otCust, APIName: "Customer", DisplayName: "Customer"}},
		})
		rec := doGet(t, r, interfaceA)
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("response body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
