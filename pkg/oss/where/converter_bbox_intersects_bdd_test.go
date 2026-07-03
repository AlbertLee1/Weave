package where

import (
	"encoding/json"
	"testing"
)

// TestBDD_IntersectsBoundingBox_GeoShapeSemantics is the behavioral contract
// for the Foundry `intersectsBoundingBox` operator. Like the sibling
// GeoShapeV2 BDD (converter_geoshape_bdd_test.go, #308), it runs the full
// where-clause → Bleve query → search pipeline against a REAL Bleve index
// (no mocks) whose `location` field is a GeoShape mapping, and asserts the
// externally-observable hit set.
//
// Foundry semantics (foundry-platform-python
// docs/v2/Ontologies/models/{IntersectsBoundingBoxQuery,WithinBoundingBoxQuery}.md):
//   - intersectsBoundingBox matches any object that OVERLAPS the box, including
//     shapes that straddle its boundary.
//   - withinBoundingBox matches only objects fully CONTAINED by the box.
//   - doesNotIntersectBoundingBox is the exact negation of intersectsBoundingBox.
//
// This scenario locks in the corrected wiring: before the fix,
// intersectsBoundingBox aliased the point-only within converter, so it (a) missed
// every geoshape object and (b) contradicted its own negation
// doesNotIntersectBoundingBox — an impossible partition.
//
// Note: as with every GeoShapeQuery-backed operator in this package, geoshape
// support is not yet wired into the production index mapping (pkg/index) — a
// pre-existing gap shared by the polygon / geoShapeV2 operators and out of
// scope here. The contract is therefore asserted at the where → Bleve boundary,
// exactly as the #308 GeoShapeV2 BDD does.
func TestBDD_IntersectsBoundingBox_GeoShapeSemantics(t *testing.T) {
	idx := setupBboxGeoShapeIndex(t)

	// East-Coast bounding box: lon [-80,-70], lat [35,45]. It fully contains
	// insidePoint and insidePolygon, partially overlaps boundaryPolygon (which
	// crosses the eastern edge), and misses outsidePolygon entirely.
	box := json.RawMessage(`{
		"topLeft": {"latitude": 45.0, "longitude": -80.0},
		"bottomRight": {"latitude": 35.0, "longitude": -70.0}
	}`)

	t.Run("intersectsBoundingBox returns every object overlapping the box", func(t *testing.T) {
		// Given a boundary-crossing polygon and objects inside/outside the box,
		// When intersectsBoundingBox runs,
		// Then it returns every overlapping object — including the boundary-crossing
		// polygon — and excludes only the fully-outside polygon.
		clause := &WhereClause{Type: "intersectsBoundingBox", Field: "location", Value: box}
		assertIDs(t, searchWithWhere(t, idx, clause), []string{"boundaryPolygon", "insidePoint", "insidePolygon"})
	})

	t.Run("withinBoundingBox excludes the boundary-crossing object", func(t *testing.T) {
		// Given the same boundary-crossing polygon,
		// When withinBoundingBox runs,
		// Then it does NOT return the polygon, because it is not fully contained —
		// the semantic distinction from intersectsBoundingBox.
		clause := &WhereClause{Type: "withinBoundingBox", Field: "location", Value: box}
		assertNotContains(t, searchWithWhere(t, idx, clause), "boundaryPolygon")
	})

	t.Run("intersectsBoundingBox and doesNotIntersectBoundingBox partition the universe", func(t *testing.T) {
		// Given the box, When both an operator and its negation run,
		// Then their hit sets are exactly complementary over the seeded objects —
		// the invariant the aliasing bug violated.
		universe := []string{"boundaryPolygon", "insidePoint", "insidePolygon", "outsidePolygon"}
		intersectsHits := searchWithWhere(t, idx, &WhereClause{Type: "intersectsBoundingBox", Field: "location", Value: box})
		doesNotIntersectHits := searchWithWhere(t, idx, &WhereClause{Type: "doesNotIntersectBoundingBox", Field: "location", Value: box})
		assertIDs(t, intersectsHits, complement(universe, doesNotIntersectHits))
	})
}
