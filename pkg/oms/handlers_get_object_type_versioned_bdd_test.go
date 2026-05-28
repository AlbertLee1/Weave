package oms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_GetObjectType_VersionedRID covers round-117 Gap-T4 step-2.
// First production wiring of the round-91 RID @vN parser: the
// GetObjectType handler now recognizes versioned RIDs and returns
// 501 NotImplemented (errorName VersionedLookupNotSupported) instead
// of letting them fall through to GetObjectTypeByAPIName (which
// would return a misleading 404).
//
// Wire contract:
//
//	GET /api/v2/ontologies/{ont}/objectTypes/{otApiName-or-RID}
//	- apiName="Customer"              -> 200 (existing behavior)
//	- apiName="ri.x.y.z.{uuid}"       -> 200 (un-versioned RID — existing)
//	- apiName="ri.x.y.z.{uuid}@v3"    -> 501 VersionedLookupNotSupported
//
// The 501 carries the parsed version + the original input so the
// SPA/SDK can show "version 3 of Customer is pinned but snapshots
// aren't ready yet" rather than silently degrading to latest.
func TestBDD_GetObjectType_VersionedRID(t *testing.T) {
	const (
		ontRID  = "ri.ontology.main.ontology.northwind"
		otRID   = "ri.ontology.main.object-type.7c9e6679-7425-40de-944b-e07fc1f90ae7"
		otAPI   = "Customer"
		baseURL = "/api/v2/ontologies/" + ontRID + "/objectTypes/"
	)

	newServer := func(t *testing.T) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		_ = repo.CreateOntology(context.Background(), &oms.Ontology{
			RID: ontRID, APIName: "northwind", DisplayName: "Northwind",
		})
		_ = repo.CreateObjectType(context.Background(), &oms.ObjectType{
			RID:         otRID,
			OntologyRID: ontRID,
			APIName:     otAPI,
			DisplayName: "Customer",
			PrimaryKey:  "id",
			Status:      "ACTIVE",
		})
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}",
			handler.GetObjectType)
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, otID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, baseURL+otID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("API name lookup still works (existing behavior preserved)", func(t *testing.T) {
		r := newServer(t)
		rec := doGet(t, r, otAPI)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["apiName"] != otAPI {
			t.Errorf("apiName=%v, want %q", body["apiName"], otAPI)
		}
	})

	t.Run("Un-versioned RID lookup still works (Gap-T4 backwards-compat)", func(t *testing.T) {
		// The handler accepts both API name AND RID via the same path
		// parameter. A plain RID (no @vN) must continue to resolve
		// the latest version — round-117 must not break this path.
		r := newServer(t)
		// mockRepo's GetObjectTypeByAPIName returns by RID OR APIName,
		// so passing the RID directly tests the un-versioned-RID path.
		rec := doGet(t, r, otRID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["apiName"] != otAPI {
			t.Errorf("apiName=%v, want %q (lookup-by-RID should resolve)", body["apiName"], otAPI)
		}
	})

	t.Run("Versioned RID returns 501 VersionedLookupNotSupported", func(t *testing.T) {
		// The pivot: round-91 RID @vN parser is now first-wired
		// into a production handler. Versioned RIDs return 501 NOT
		// 404, distinguishing "feature pending" from "RID is wrong".
		r := newServer(t)
		versioned := otRID + "@v3"
		rec := doGet(t, r, versioned)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("status=%d, want 501; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "VersionedLookupNotSupported" {
			t.Errorf("errorName=%v, want VersionedLookupNotSupported", body["errorName"])
		}
		// errorCode should be UNIMPLEMENTED (gRPC-style mapping).
		if body["errorCode"] != "UNIMPLEMENTED" {
			t.Errorf("errorCode=%v, want UNIMPLEMENTED", body["errorCode"])
		}
		// Parameters echo the version + original input for client debugging.
		params, _ := body["parameters"].(map[string]any)
		if params == nil {
			t.Fatalf("parameters absent; body=%s", rec.Body.String())
		}
		if params["version"] != "3" {
			t.Errorf("parameters.version=%v, want \"3\"", params["version"])
		}
		if params["objectTypeApiName"] != versioned {
			t.Errorf("parameters.objectTypeApiName=%v, want %q",
				params["objectTypeApiName"], versioned)
		}
	})

	t.Run("Versioned RID never falls through to repo lookup", func(t *testing.T) {
		// Regression guard — the 501 must short-circuit BEFORE
		// GetObjectTypeByAPIName is called. If the version branch
		// were missing, the handler would still 404 (RID with @v3
		// doesn't match the stored RID), but the error name would
		// be ObjectTypeNotFound which is the wrong diagnostic for
		// the SPA. This subtest asserts the diagnostic, not the
		// repo-call count.
		r := newServer(t)
		rec := doGet(t, r, "ri.ontology.main.object-type.7c9e6679-7425-40de-944b-e07fc1f90ae7@v99")
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] == "ObjectTypeNotFound" {
			t.Errorf("versioned RID returned ObjectTypeNotFound — short-circuit broken; body=%s",
				rec.Body.String())
		}
		if body["errorName"] != "VersionedLookupNotSupported" {
			t.Errorf("errorName=%v, want VersionedLookupNotSupported", body["errorName"])
		}
	})
}
