package where

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// --- geoShapeV2 conversion unit tests (Foundry GeoShapeV2Query) ---
//
// Foundry's GeoShapeV2Query (foundry-platform-python
// docs/v2/Ontologies/models/GeoShapeV2Query.md) has the wire shape:
//
//	{"type":"geoShapeV2","field":"<prop>",
//	 "geometry":{...GeoShapeV2Geometry...},
//	 "spatialFilterMode":"INTERSECTS"|"DISJOINT"|"WITHIN"|"CONTAINS"}
//
// where GeoShapeV2Geometry is a discriminated union of:
//   - {"type":"envelope","topLeft":{lat,lon},"bottomRight":{lat,lon}}
//   - {"type":"geoJson","geoJson":"<GeoJSON geometry string>"}
//
// These tests exercise the converter → Bleve query compilation. The
// externally-observable spatial semantics (hit sets) are covered by the
// BDD integration test in converter_geoshape_bdd_test.go.

func TestGeoShapeV2_Convert_Valid(t *testing.T) {
	polygon := `{"type":"Polygon","coordinates":[[[-80.0,35.0],[-70.0,35.0],[-70.0,45.0],[-80.0,45.0],[-80.0,35.0]]]}`
	point := `{"type":"Point","coordinates":[-74.0,40.7]}`

	cases := []struct {
		name              string
		geometry          string
		spatialFilterMode string
	}{
		{"geoJson polygon INTERSECTS", `{"type":"geoJson","geoJson":` + jsonString(polygon) + `}`, "INTERSECTS"},
		{"geoJson polygon WITHIN", `{"type":"geoJson","geoJson":` + jsonString(polygon) + `}`, "WITHIN"},
		{"geoJson polygon DISJOINT", `{"type":"geoJson","geoJson":` + jsonString(polygon) + `}`, "DISJOINT"},
		{"geoJson polygon CONTAINS", `{"type":"geoJson","geoJson":` + jsonString(polygon) + `}`, "CONTAINS"},
		{"geoJson point INTERSECTS", `{"type":"geoJson","geoJson":` + jsonString(point) + `}`, "INTERSECTS"},
		// geoJson may also arrive as a raw JSON object (our JSON-native clients)
		// rather than the Foundry-canonical serialized string.
		{"geoJson raw-object polygon WITHIN", `{"type":"geoJson","geoJson":` + polygon + `}`, "WITHIN"},
		{"envelope INTERSECTS", `{"type":"envelope","topLeft":{"latitude":45.0,"longitude":-80.0},"bottomRight":{"latitude":35.0,"longitude":-70.0}}`, "INTERSECTS"},
		{"envelope WITHIN", `{"type":"envelope","topLeft":{"latitude":45.0,"longitude":-80.0},"bottomRight":{"latitude":35.0,"longitude":-70.0}}`, "WITHIN"},
		{"envelope DISJOINT", `{"type":"envelope","topLeft":{"latitude":45.0,"longitude":-80.0},"bottomRight":{"latitude":35.0,"longitude":-70.0}}`, "DISJOINT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clause := &WhereClause{
				Type:              "geoShapeV2",
				Field:             "location",
				Geometry:          json.RawMessage(tc.geometry),
				SpatialFilterMode: tc.spatialFilterMode,
			}
			q, err := ConvertToBleveQuery(clause)
			if err != nil {
				t.Fatalf("ConvertToBleveQuery: %v", err)
			}
			if q == nil {
				t.Fatal("expected non-nil query")
			}
		})
	}
}

func TestGeoShapeV2_Convert_Invalid(t *testing.T) {
	polygon := `{"type":"Polygon","coordinates":[[[-80.0,35.0],[-70.0,35.0],[-70.0,45.0],[-80.0,45.0],[-80.0,35.0]]]}`
	validGeoJSON := `{"type":"geoJson","geoJson":` + jsonString(polygon) + `}`

	cases := []struct {
		name              string
		geometry          string
		spatialFilterMode string
		wantSubstr        string
	}{
		{"missing spatialFilterMode", validGeoJSON, "", "spatialFilterMode"},
		{"unknown spatialFilterMode", validGeoJSON, "OVERLAPS", "spatialFilterMode"},
		{"missing geometry", "", "WITHIN", "geometry"},
		{"unknown geometry type", `{"type":"circleish"}`, "WITHIN", "geometry"},
		{"missing geometry type", `{"topLeft":{"latitude":45.0,"longitude":-80.0}}`, "WITHIN", "geometry"},
		{"invalid geoJson shape type", `{"type":"geoJson","geoJson":"{\"type\":\"Nonsense\",\"coordinates\":[0,0]}"}`, "WITHIN", "geoJson"},
		{"malformed geoJson", `{"type":"geoJson","geoJson":"not-json"}`, "WITHIN", "geoJson"},
		{"empty geoJson", `{"type":"geoJson","geoJson":""}`, "WITHIN", "geoJson"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clause := &WhereClause{
				Type:              "geoShapeV2",
				Field:             "location",
				SpatialFilterMode: tc.spatialFilterMode,
			}
			if tc.geometry != "" {
				clause.Geometry = json.RawMessage(tc.geometry)
			}
			_, err := ConvertToBleveQuery(clause)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !errors.Is(err, ErrInvalidWhereClause) {
				t.Fatalf("expected ErrInvalidWhereClause, got %v", err)
			}
			if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

// jsonString marshals s into a JSON string literal (with surrounding quotes
// and escaping) so it can be embedded as the value of the Foundry
// GeoJsonString.geoJson field, which is itself a serialized-GeoJSON string.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
