package oss_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// US-039: GeotemporalSeriesProperty endpoints
//
// Foundry exposes 2 read endpoints under a distinct /geotemporalSeries/
// prefix (not /timeseries/):
//
//   GET  .../geotemporalSeries/{propertyName}/latestValue
//   POST .../geotemporalSeries/{propertyName}/streamHistoricValues
//
// Wire shape is GeotemporalSeriesValue {time, position}. Position is a
// GeoJSON Point {type, coordinates}.

func setupGeotemporalTest(t *testing.T) (http.Handler, geotemporal.Store) {
	t.Helper()

	svc, mgr, repo, _ := setupOSSTest(t)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.vehicle",
		OntologyRID: testOntologyRID,
		APIName:     "vehicle",
		DisplayName: "Vehicle",
		PrimaryKey:  "vehicleId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	if _, err := mgr.EnsureIndex("vehicle", []index.Property{
		{APIName: "vehicleId", BaseType: "string", IsSearchable: true},
		{APIName: "track", BaseType: "geotimeseries", IsSearchable: false},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("vehicle", "v1", map[string]interface{}{
		"vehicleId": "v1",
		"track":     "ri.geotimeseries.main.series.v1-track",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	store := geotemporal.NewMemoryStore()
	ctx := context.Background()
	key := geotemporal.SeriesKey{
		Ontology:   testOntologyRID,
		ObjectType: "vehicle",
		PrimaryKey: "v1",
		Property:   "track",
	}
	values := []geotemporal.Value{
		{
			Time: mustParseTime(t, "2026-04-01T00:00:00Z"),
			Position: map[string]interface{}{
				"type":        "Point",
				"coordinates": []interface{}{-122.41, 37.77},
			},
		},
		{
			Time: mustParseTime(t, "2026-04-02T00:00:00Z"),
			Position: map[string]interface{}{
				"type":        "Point",
				"coordinates": []interface{}{-122.42, 37.78},
			},
		},
		{
			Time: mustParseTime(t, "2026-04-03T00:00:00Z"),
			Position: map[string]interface{}{
				"type":        "Point",
				"coordinates": []interface{}{-122.43, 37.79},
			},
		},
	}
	for _, v := range values {
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue: %v", err)
		}
	}

	h := oss.NewHandler(svc)
	h.SetGeotemporalStore(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store
}

func TestGetGeotemporalLatestValue(t *testing.T) {
	r, _ := setupGeotemporalTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/vehicle/v1/geotemporalSeries/track/latestValue",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out geotemporal.Value
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := mustParseTime(t, "2026-04-03T00:00:00Z")
	if !out.Time.Equal(want) {
		t.Errorf("time = %v, want %v", out.Time, want)
	}
	pos, ok := out.Position.(map[string]interface{})
	if !ok {
		t.Fatalf("position type = %T, want map", out.Position)
	}
	if pos["type"] != "Point" {
		t.Errorf("position.type = %v, want Point", pos["type"])
	}
}

func TestGetGeotemporalLatestValue_EmptySeries(t *testing.T) {
	svc, mgr, repo, _ := setupOSSTest(t)
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.vehicle",
		OntologyRID: testOntologyRID,
		APIName:     "vehicle",
		DisplayName: "Vehicle",
		PrimaryKey:  "vehicleId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	if _, err := mgr.EnsureIndex("vehicle", []index.Property{
		{APIName: "vehicleId", BaseType: "string"},
		{APIName: "track", BaseType: "geotimeseries"},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("vehicle", "v1", map[string]interface{}{
		"vehicleId": "v1",
		"track":     "ri.geotimeseries.main.series.empty",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	h := oss.NewHandler(svc)
	h.SetGeotemporalStore(geotemporal.NewMemoryStore())
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/vehicle/v1/geotemporalSeries/track/latestValue",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostGeotemporalStreamHistoricValues(t *testing.T) {
	r, _ := setupGeotemporalTest(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/vehicle/v1/geotemporalSeries/track/streamHistoricValues",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []geotemporal.Value
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d values, want 3", len(out))
	}
	if !out[0].Time.Equal(mustParseTime(t, "2026-04-01T00:00:00Z")) {
		t.Errorf("out[0].Time = %v", out[0].Time)
	}
	if !out[2].Time.Equal(mustParseTime(t, "2026-04-03T00:00:00Z")) {
		t.Errorf("out[2].Time = %v", out[2].Time)
	}
}

func TestPostGeotemporalStreamHistoricValues_NotConfigured(t *testing.T) {
	svc, mgr, repo, _ := setupOSSTest(t)
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.vehicle",
		OntologyRID: testOntologyRID,
		APIName:     "vehicle",
		DisplayName: "Vehicle",
		PrimaryKey:  "vehicleId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	if _, err := mgr.EnsureIndex("vehicle", []index.Property{
		{APIName: "vehicleId", BaseType: "string"},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("vehicle", "v1", map[string]interface{}{
		"vehicleId": "v1",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// No SetGeotemporalStore → store is nil.
	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/vehicle/v1/geotemporalSeries/track/streamHistoricValues",
		bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
}
