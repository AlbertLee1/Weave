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

// TestBDD_CreateProperty_SharedPropertyTypeBinding covers the round-
// 55 fix for the second part of PRD-V2 OMS SharedProperty linkage
// gap. Before this round CreatePropertyRequest had no input field
// for SharedPropertyRID at all — the only way to bind a Property to
// a SharedProperty was via the import path or direct DB writes.
// Round 54 closed the dangling-reference delete; round 55 closes
// the inverse "you can't even create a binding" gap, and rejects
// the silent type-mismatch case (Property says baseType=integer
// but the referenced SharedProperty says baseType=string).
//
// Wire shape:
//
//	POST /api/admin/objectTypes/{otRID}/properties
//	{
//	  "apiName": "email",
//	  "baseType": "string",      // MUST match the SP's baseType
//	  "isArray": false,           // MUST match the SP's isArray
//	  "sharedPropertyTypeApiName": "email"   // round 55 — new field
//	}
//
//	201 + Property body          → ok; Property.sharedPropertyRid
//	                               set to the resolved SP's RID
//	400 SharedPropertyTypeNotFound  → api-name absent in this ontology
//	400 SharedPropertyTypeMismatch  → baseType / isArray differ; body
//	                                  carries both sides for the SDK
//
// Scenarios:
//   - Matched binding stores resolved RID and returns 201.
//   - Unknown SP api-name returns 400 with both ontology + api-name.
//   - baseType mismatch returns 400 with sharedPropertyBaseType +
//     propertyBaseType reported so SDKs can render a helpful diff.
//   - isArray mismatch returns 400 with sharedPropertyIsArray +
//     propertyIsArray reported.
//   - No sharedPropertyTypeApiName in the body keeps the existing
//     behavior exactly (Property created with empty SharedPropertyRID).
func TestBDD_CreateProperty_SharedPropertyTypeBinding(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"
	const otRID = "ri.ontology.main.object-type.user"
	const spRID = "ri.ontology.main.shared-property.email"

	newServer := func(t *testing.T) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
			RID:         otRID,
			OntologyRID: ontRID,
			APIName:     "user",
			DisplayName: "User",
			PrimaryKey:  "id",
		})
		repo.sharedProperties = append(repo.sharedProperties,
			oms.SharedProperty{
				RID:         spRID,
				OntologyRID: ontRID,
				APIName:     "email",
				DisplayName: "Email",
				BaseType:    "string",
				IsArray:     false,
			},
		)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/admin/objectTypes/{objectTypeRid}/properties",
			handler.CreateProperty)
		return r, repo
	}

	doPost := func(t *testing.T, r *chi.Mux, body map[string]interface{}) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost,
			"/api/admin/objectTypes/"+otRID+"/properties",
			bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Matched binding stores resolved RID and returns 201", func(t *testing.T) {
		r, repo := newServer(t)
		rec := doPost(t, r, map[string]interface{}{
			"apiName":                   "email",
			"baseType":                  "string",
			"isArray":                   false,
			"sharedPropertyTypeApiName": "email",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		// Repo received the stored RID, not the api-name.
		if len(repo.properties) != 1 {
			t.Fatalf("properties=%v, want one", repo.properties)
		}
		if repo.properties[0].SharedPropertyRID != spRID {
			t.Errorf("stored SharedPropertyRID=%q, want %q", repo.properties[0].SharedPropertyRID, spRID)
		}
		// Response body echoes the same.
		var resp oms.Property
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.SharedPropertyRID != spRID {
			t.Errorf("response.sharedPropertyRid=%q, want %q", resp.SharedPropertyRID, spRID)
		}
	})

	t.Run("Unknown api-name returns 400 SharedPropertyTypeNotFound", func(t *testing.T) {
		r, repo := newServer(t)
		rec := doPost(t, r, map[string]interface{}{
			"apiName":                   "phone",
			"baseType":                  "string",
			"isArray":                   false,
			"sharedPropertyTypeApiName": "ghost",
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "SharedPropertyTypeNotFound" {
			t.Errorf("errorName=%v, want SharedPropertyTypeNotFound", body["errorName"])
		}
		params, _ := body["parameters"].(map[string]interface{})
		if params["sharedPropertyType"] != "ghost" {
			t.Errorf("parameters.sharedPropertyType=%v, want ghost", params["sharedPropertyType"])
		}
		// Property must NOT be written when the binding is invalid.
		if len(repo.properties) != 0 {
			t.Errorf("properties=%v, want empty (no write on validation error)", repo.properties)
		}
	})

	t.Run("baseType mismatch returns 400 with both sides reported", func(t *testing.T) {
		r, repo := newServer(t)
		rec := doPost(t, r, map[string]interface{}{
			"apiName":                   "email",
			"baseType":                  "integer", // SP says "string"
			"isArray":                   false,
			"sharedPropertyTypeApiName": "email",
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "SharedPropertyTypeMismatch" {
			t.Errorf("errorName=%v, want SharedPropertyTypeMismatch", body["errorName"])
		}
		params, _ := body["parameters"].(map[string]interface{})
		if params["sharedPropertyBaseType"] != "string" {
			t.Errorf("parameters.sharedPropertyBaseType=%v, want string", params["sharedPropertyBaseType"])
		}
		if params["propertyBaseType"] != "integer" {
			t.Errorf("parameters.propertyBaseType=%v, want integer", params["propertyBaseType"])
		}
		if len(repo.properties) != 0 {
			t.Errorf("properties=%v, want empty", repo.properties)
		}
	})

	t.Run("isArray mismatch returns 400 with both sides reported", func(t *testing.T) {
		r, repo := newServer(t)
		rec := doPost(t, r, map[string]interface{}{
			"apiName":                   "email",
			"baseType":                  "string",
			"isArray":                   true, // SP says false
			"sharedPropertyTypeApiName": "email",
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "SharedPropertyTypeMismatch" {
			t.Errorf("errorName=%v, want SharedPropertyTypeMismatch", body["errorName"])
		}
		params, _ := body["parameters"].(map[string]interface{})
		if params["sharedPropertyIsArray"] != "false" {
			t.Errorf("parameters.sharedPropertyIsArray=%v, want \"false\"", params["sharedPropertyIsArray"])
		}
		if params["propertyIsArray"] != "true" {
			t.Errorf("parameters.propertyIsArray=%v, want \"true\"", params["propertyIsArray"])
		}
		if len(repo.properties) != 0 {
			t.Errorf("properties=%v, want empty", repo.properties)
		}
	})

	t.Run("No sharedPropertyTypeApiName preserves existing behavior", func(t *testing.T) {
		r, repo := newServer(t)
		rec := doPost(t, r, map[string]interface{}{
			"apiName":  "phone",
			"baseType": "string",
			"isArray":  false,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		if repo.properties[0].SharedPropertyRID != "" {
			t.Errorf("SharedPropertyRID=%q, want empty (no binding requested)", repo.properties[0].SharedPropertyRID)
		}
	})
}
