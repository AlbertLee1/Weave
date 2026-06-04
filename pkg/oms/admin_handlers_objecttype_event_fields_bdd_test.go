package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ObjectTypeEventFields covers VTX-077: the timeline "event" metadata
// on ObjectType (isEvent / eventStartProp / eventEndProp) must round-trip
// through the admin write path. The model has carried these fields since
// VTX-077, but the admin Create/Update handlers neither decoded nor persisted
// them, so the Vertex Timeline could never be configured through the API or UI.
//
// The scenarios assemble an in-memory repo behind the real chi router (no
// testcontainers — mirrors the existing ObjectType admin BDD/handler tests),
// drive POST/PUT, then GET the ObjectType back and assert the three fields
// survived the round-trip.
func TestBDD_ObjectTypeEventFields(t *testing.T) {
	newRouter := func(handler *oms.OMSHandler) *chi.Mux {
		r := chi.NewRouter()
		r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)
		r.Put("/api/admin/objectTypes/{objectTypeRid}", handler.UpdateObjectType)
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)
		return r
	}

	t.Run("Given a create request marking the ObjectType as an event When created Then the event fields persist and GET reflects them", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		r := newRouter(oms.NewOMSHandler(repo))

		body := `{
			"apiName":        "flightDelay",
			"displayName":    "Flight Delay",
			"primaryKey":     "id",
			"isEvent":        true,
			"eventStartProp": "delayStart",
			"eventEndProp":   "delayEnd"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Source-of-truth: the persisted row must carry the event metadata.
		if len(repo.objectTypes) != 1 {
			t.Fatalf("expected 1 object type persisted, got %d", len(repo.objectTypes))
		}
		stored := repo.objectTypes[0]
		if !stored.IsEvent {
			t.Errorf("stored IsEvent = false, want true")
		}
		if stored.EventStartProp != "delayStart" {
			t.Errorf("stored EventStartProp = %q, want delayStart", stored.EventStartProp)
		}
		if stored.EventEndProp != "delayEnd" {
			t.Errorf("stored EventEndProp = %q, want delayEnd", stored.EventEndProp)
		}

		// Externally observable: GET the ObjectType back and assert the wire
		// payload exposes the timeline metadata.
		getReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+ontRID+"/objectTypes/flightDelay", nil)
		getRec := httptest.NewRecorder()
		r.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
		}
		var got map[string]interface{}
		if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
			t.Fatalf("get: decode body: %v", err)
		}
		if got["isEvent"] != true {
			t.Errorf("GET isEvent = %v, want true; body=%s", got["isEvent"], getRec.Body.String())
		}
		if got["eventStartProp"] != "delayStart" {
			t.Errorf("GET eventStartProp = %v, want delayStart", got["eventStartProp"])
		}
		if got["eventEndProp"] != "delayEnd" {
			t.Errorf("GET eventEndProp = %v, want delayEnd", got["eventEndProp"])
		}
	})

	t.Run("Given an existing non-event ObjectType When updated to an event Then the event fields persist and GET reflects them", func(t *testing.T) {
		repo := &mockRepo{}
		ontRID := seedMockOntology(repo)
		repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
			RID:         "ri.ontology.main.object-type.maintenance",
			OntologyRID: ontRID,
			APIName:     "maintenance",
			DisplayName: "Maintenance",
			PrimaryKey:  "id",
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		})
		r := newRouter(oms.NewOMSHandler(repo))

		body := `{
			"displayName":    "Maintenance",
			"status":         "ACTIVE",
			"visibility":     "NORMAL",
			"isEvent":        true,
			"eventStartProp": "windowStart",
			"eventEndProp":   "windowEnd"
		}`
		req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		stored := repo.objectTypes[0]
		if !stored.IsEvent {
			t.Errorf("stored IsEvent = false, want true")
		}
		if stored.EventStartProp != "windowStart" {
			t.Errorf("stored EventStartProp = %q, want windowStart", stored.EventStartProp)
		}
		if stored.EventEndProp != "windowEnd" {
			t.Errorf("stored EventEndProp = %q, want windowEnd", stored.EventEndProp)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+ontRID+"/objectTypes/maintenance", nil)
		getRec := httptest.NewRecorder()
		r.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
		}
		var got map[string]interface{}
		if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
			t.Fatalf("get: decode body: %v", err)
		}
		if got["isEvent"] != true {
			t.Errorf("GET isEvent = %v, want true; body=%s", got["isEvent"], getRec.Body.String())
		}
		if got["eventStartProp"] != "windowStart" {
			t.Errorf("GET eventStartProp = %v, want windowStart", got["eventStartProp"])
		}
		if got["eventEndProp"] != "windowEnd" {
			t.Errorf("GET eventEndProp = %v, want windowEnd", got["eventEndProp"])
		}
	})
}
