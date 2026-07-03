package where

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

// TestBDD_GeoShapeV2_SpatialFilterModes is the behavioral contract for the
// Foundry GeoShapeV2Query operator (`geoShapeV2`). It runs the full
// where-clause → Bleve query → search pipeline against a REAL Bleve index
// (no mocks) whose `location` field is a GeoShape mapping, and asserts the
// externally-observable hit set for each SpatialFilterMode.
//
// The field is deliberately mapped with NewGeoShapeFieldMapping (not the
// geopoint mapping): Bleve's GeoShapeQuery — which backs every GeoShape
// operator in this package (withinPolygon / intersectsPolygon /
// doesNotIntersect* / geoShapeV2) — only matches geoshape-indexed fields.
// Wiring geoshape support into the production index mapping (pkg/index) is
// a separate, pre-existing gap shared by the sibling polygon operators and
// is out of scope for this change.
//
// Documents are built via json.Unmarshal so the indexed geometry has the
// exact generic ([]interface{} / float64) shape the real ingest path
// produces — a typed [][][]float64 in a Go map does NOT index as a usable
// geoshape.
func TestBDD_GeoShapeV2_SpatialFilterModes(t *testing.T) {
	idx := setupGeoShapeV2Index(t)

	// US East Coast polygon: contains the NYC point and the manhattanZone
	// polygon; excludes the LA and London points.
	eastCoast := `{"type":"Polygon","coordinates":[[[-80.0,35.0],[-70.0,35.0],[-70.0,45.0],[-80.0,45.0],[-80.0,35.0]]]}`
	geoJSONGeometry := func(shape string) json.RawMessage {
		return json.RawMessage(`{"type":"geoJson","geoJson":` + jsonString(shape) + `}`)
	}

	t.Run("INTERSECTS returns every object overlapping the query polygon", func(t *testing.T) {
		// Given a geoShapeV2 clause with INTERSECTS over the East Coast polygon,
		// When the search runs,
		// Then both the NYC point and the Manhattan zone polygon match.
		clause := &WhereClause{
			Type:              "geoShapeV2",
			Field:             "location",
			Geometry:          geoJSONGeometry(eastCoast),
			SpatialFilterMode: "INTERSECTS",
		}
		assertIDs(t, searchWithWhere(t, idx, clause), []string{"manhattanZone", "nyc"})
	})

	t.Run("WITHIN returns objects fully contained by the query polygon", func(t *testing.T) {
		clause := &WhereClause{
			Type:              "geoShapeV2",
			Field:             "location",
			Geometry:          geoJSONGeometry(eastCoast),
			SpatialFilterMode: "WITHIN",
		}
		assertIDs(t, searchWithWhere(t, idx, clause), []string{"manhattanZone", "nyc"})
	})

	t.Run("DISJOINT returns objects that do NOT intersect the query polygon", func(t *testing.T) {
		// DISJOINT must include far-away objects (la, london). Bleve's native
		// "disjoint" relation cannot express this (its candidate set is the
		// query shape's S2 cover), so the converter negates an intersects
		// query with MatchAll MUST / MUST_NOT — this scenario locks that in.
		clause := &WhereClause{
			Type:              "geoShapeV2",
			Field:             "location",
			Geometry:          geoJSONGeometry(eastCoast),
			SpatialFilterMode: "DISJOINT",
		}
		assertIDs(t, searchWithWhere(t, idx, clause), []string{"la", "london"})
	})

	t.Run("CONTAINS returns objects whose geometry contains the query point", func(t *testing.T) {
		// Given a query point that lies inside the Manhattan zone polygon,
		// When CONTAINS runs,
		// Then only the polygon (which contains the point) matches; the NYC
		// point does not contain a distinct point.
		insidePoint := `{"type":"Point","coordinates":[-73.98,40.72]}`
		clause := &WhereClause{
			Type:              "geoShapeV2",
			Field:             "location",
			Geometry:          geoJSONGeometry(insidePoint),
			SpatialFilterMode: "CONTAINS",
		}
		assertIDs(t, searchWithWhere(t, idx, clause), []string{"manhattanZone"})
	})

	t.Run("envelope geometry filters using a bounding box", func(t *testing.T) {
		// The GeoShapeV2Geometry union also accepts an "envelope" bounding
		// box (BoundingBoxValue). WITHIN over an East-Coast box matches the
		// NYC point and the Manhattan zone.
		clause := &WhereClause{
			Type:  "geoShapeV2",
			Field: "location",
			Geometry: json.RawMessage(`{
				"type": "envelope",
				"topLeft": {"latitude": 45.0, "longitude": -80.0},
				"bottomRight": {"latitude": 35.0, "longitude": -70.0}
			}`),
			SpatialFilterMode: "WITHIN",
		}
		assertIDs(t, searchWithWhere(t, idx, clause), []string{"manhattanZone", "nyc"})
	})

	t.Run("Pacific polygon matches nothing", func(t *testing.T) {
		pacific := `{"type":"Polygon","coordinates":[[[-170.0,10.0],[-160.0,10.0],[-160.0,20.0],[-170.0,20.0],[-170.0,10.0]]]}`
		clause := &WhereClause{
			Type:              "geoShapeV2",
			Field:             "location",
			Geometry:          geoJSONGeometry(pacific),
			SpatialFilterMode: "INTERSECTS",
		}
		assertIDs(t, searchWithWhere(t, idx, clause), []string{})
	})
}

// setupGeoShapeV2Index builds a real Bleve index whose `location` field is a
// GeoShape mapping and seeds three point objects plus one polygon object.
func setupGeoShapeV2Index(t *testing.T) bleve.Index {
	t.Helper()

	im := bleve.NewIndexMapping()
	dm := bleve.NewDocumentMapping()
	dm.AddFieldMappingsAt("location", bleve.NewGeoShapeFieldMapping())
	dm.AddFieldMappingsAt("name", bleve.NewTextFieldMapping())
	im.DefaultMapping = dm

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "geoshapev2"), im)
	if err != nil {
		t.Fatalf("create geoshape index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := map[string]string{
		"nyc":    `{"name":"nyc","location":{"type":"Point","coordinates":[-74.0060,40.7128]}}`,
		"la":     `{"name":"la","location":{"type":"Point","coordinates":[-118.2437,34.0522]}}`,
		"london": `{"name":"london","location":{"type":"Point","coordinates":[-0.1278,51.5074]}}`,
		// Small polygon around lower Manhattan — inside the East Coast query
		// polygon, and it contains the (-73.98, 40.72) query point.
		"manhattanZone": `{"name":"manhattanZone","location":{"type":"Polygon","coordinates":[[[-74.05,40.65],[-73.90,40.65],[-73.90,40.80],[-74.05,40.80],[-74.05,40.65]]]}}`,
	}
	for id, raw := range docs {
		var doc map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("unmarshal %s: %v", id, err)
		}
		if err := idx.Index(id, doc); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}
	return idx
}
