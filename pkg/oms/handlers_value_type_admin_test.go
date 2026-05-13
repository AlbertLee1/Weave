package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestUS051_ValueType_Admin_CRUD_AND_Usages exercises the V2-mounted admin
// surface for ValueType management — the deleted /api/admin/value-types
// routes were re-mounted under /api/v2/ontologies/{ontologyApiName}/... in
// US-051 (PC-A05). The five sub-tests lock the per-route shape contract
// the frontend ValueTypeAdminPage relies on (Create, admin List without
// the preview gate, Update, Delete, and the new /usages reverse lookup).
func TestUS051_ValueType_Admin_CRUD_AND_Usages(t *testing.T) {
	t.Run("CreateValueType_V2_route_returns_201_with_constraints", func(t *testing.T) {
		repo := &mockRepo{}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/valueTypes", handler.CreateValueType)

		body := []byte(`{"apiName":"PhoneNumber","displayName":"Phone","baseType":"string","constraints":{"pattern":"^\\+?[0-9 ()-]+$"}}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/valueTypes", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var got oms.ValueType
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if got.APIName != "PhoneNumber" || got.BaseType != "string" {
			t.Errorf("unexpected created shape: %+v", got)
		}
		if string(got.Constraints) == "" {
			t.Error("expected constraints to round-trip on create response")
		}
		if len(repo.valueTypes) != 1 {
			t.Errorf("expected exactly one ValueType persisted, got %d", len(repo.valueTypes))
		}
	})

	t.Run("ListValueTypesAdmin_V2_route_returns_data_envelope_without_preview_gate", func(t *testing.T) {
		repo := &mockRepo{
			valueTypes: []oms.ValueType{
				{RID: "ri.ontology.main.value-type.email", APIName: "EmailAddress", DisplayName: "Email", BaseType: "string", Version: 1},
				{RID: "ri.ontology.main.value-type.currency", APIName: "Currency", DisplayName: "Currency", BaseType: "double", Version: 1},
			},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypesAdmin", handler.ListValueTypes)

		// NOTE: admin list does NOT require ?preview=true — runtime V2 list does.
		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypesAdmin", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var envelope struct {
			Data []oms.ValueType `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(envelope.Data) != 2 {
			t.Errorf("expected 2 ValueTypes, got %d", len(envelope.Data))
		}
	})

	t.Run("UpdateValueType_V2_route_persists_new_constraints", func(t *testing.T) {
		repo := &mockRepo{
			valueTypes: []oms.ValueType{
				{RID: "ri.test.vt.1", APIName: "EmailAddress", DisplayName: "Email", BaseType: "string", Version: 1, Constraints: json.RawMessage(`{"pattern":"^a$"}`)},
			},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Put("/api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}", handler.UpdateValueType)

		body := []byte(`{"displayName":"Email Address","baseType":"string","constraints":{"enum":["work","personal","system"]}}`)
		req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/valueTypes/byRid/ri.test.vt.1", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var got oms.ValueType
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if got.DisplayName != "Email Address" {
			t.Errorf("expected updated DisplayName, got %s", got.DisplayName)
		}
		var c map[string]interface{}
		if err := json.Unmarshal(got.Constraints, &c); err != nil {
			t.Fatalf("invalid constraints JSON: %v", err)
		}
		if _, ok := c["enum"]; !ok {
			t.Errorf("expected enum constraint, got %+v", c)
		}
	})

	t.Run("DeleteValueType_V2_route_returns_204_and_drops_row", func(t *testing.T) {
		repo := &mockRepo{
			valueTypes: []oms.ValueType{
				{RID: "ri.test.vt.delete", APIName: "Doomed", DisplayName: "Doomed", BaseType: "string", Version: 1},
			},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Delete("/api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}", handler.DeleteValueType)

		req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/northwind/valueTypes/byRid/ri.test.vt.delete", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
		if len(repo.valueTypes) != 0 {
			t.Errorf("expected ValueType to be deleted, still have %d", len(repo.valueTypes))
		}
	})

	t.Run("ListValueTypeUsages_V2_route_returns_reverse_references_by_base_type", func(t *testing.T) {
		// Seed: ValueType "EmailAddress" referenced by two properties on two
		// different ObjectTypes; another ValueType "Currency" is unreferenced.
		repo := &mockRepo{
			valueTypes: []oms.ValueType{
				{RID: "ri.test.vt.email", APIName: "EmailAddress", DisplayName: "Email", BaseType: "string", Version: 1},
				{RID: "ri.test.vt.currency", APIName: "Currency", DisplayName: "Currency", BaseType: "double", Version: 1},
			},
			objectTypes: []oms.ObjectType{
				{RID: "ri.test.ot.emp", APIName: "Employee"},
				{RID: "ri.test.ot.cust", APIName: "Customer"},
			},
			properties: []oms.Property{
				{RID: "ri.test.prop.emp.email", ObjectTypeRID: "ri.test.ot.emp", APIName: "email", BaseType: "EmailAddress"},
				{RID: "ri.test.prop.cust.email", ObjectTypeRID: "ri.test.ot.cust", APIName: "email", BaseType: "EmailAddress"},
				{RID: "ri.test.prop.emp.name", ObjectTypeRID: "ri.test.ot.emp", APIName: "name", BaseType: "string"},
			},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}/usages", handler.ListValueTypeUsages)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes/byRid/ri.test.vt.email/usages", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var envelope struct {
			Data []oms.PropertyUsage `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(envelope.Data) != 2 {
			t.Fatalf("expected 2 usages, got %d: %+v", len(envelope.Data), envelope.Data)
		}
		// Both rows must carry the joined ObjectType apiName, not a bare rid.
		seenOTs := map[string]bool{}
		for _, u := range envelope.Data {
			seenOTs[u.ObjectTypeAPIName] = true
			if u.PropertyAPIName != "email" {
				t.Errorf("unexpected propertyApiName: %s", u.PropertyAPIName)
			}
		}
		if !seenOTs["Employee"] || !seenOTs["Customer"] {
			t.Errorf("expected Employee + Customer in usages, got %+v", seenOTs)
		}

		// Currency is unreferenced — usages must come back as an empty array
		// (not nil) so the frontend doesn't have to discriminate the cases.
		req2 := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes/byRid/ri.test.vt.currency/usages", nil)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w2.Code)
		}
		var emptyEnvelope struct {
			Data []oms.PropertyUsage `json:"data"`
		}
		if err := json.Unmarshal(w2.Body.Bytes(), &emptyEnvelope); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if emptyEnvelope.Data == nil {
			t.Error("expected empty array, got null")
		}
		if len(emptyEnvelope.Data) != 0 {
			t.Errorf("expected 0 usages, got %d", len(emptyEnvelope.Data))
		}
	})

	t.Run("ListValueTypeUsages_V2_returns_404_for_unknown_value_type", func(t *testing.T) {
		repo := &mockRepo{}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}/usages", handler.ListValueTypeUsages)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes/byRid/ri.test.vt.missing/usages", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if body["errorName"] != "ValueTypeNotFound" {
			t.Errorf("expected errorName ValueTypeNotFound, got %v", body["errorName"])
		}
	})
}
