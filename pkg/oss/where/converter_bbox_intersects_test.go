package where

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// --- intersectsBoundingBox correctness unit tests ---
//
// Foundry distinguishes withinBoundingBox (the object must be fully CONTAINED
// by the box) from intersectsBoundingBox (the object need only OVERLAP the
// box). The two operators therefore MUST compile to different Bleve queries:
// intersectsBoundingBox uses a GeoShape "intersects" relation over the box's
// rectangular polygon (matching partial-overlap / boundary-crossing shapes),
// exactly like its own negation doesNotIntersectBoundingBox. withinBoundingBox
// keeps the point-oriented GeoBoundingBoxQuery.

// TestIntersectsBoundingBox_ConvertsToGeoShapeQuery locks the compiled query
// shape of each bounding-box operator. It is table-driven over the operator
// type and asserts the concrete Bleve query type produced.
func TestIntersectsBoundingBox_ConvertsToGeoShapeQuery(t *testing.T) {
	box := json.RawMessage(`{
		"topLeft": {"latitude": 45.0, "longitude": -80.0},
		"bottomRight": {"latitude": 35.0, "longitude": -70.0}
	}`)

	cases := []struct {
		name        string
		clauseType  string
		assertQuery func(t *testing.T, q query.Query)
	}{
		{
			name:       "intersectsBoundingBox compiles to a GeoShape intersects query",
			clauseType: "intersectsBoundingBox",
			assertQuery: func(t *testing.T, q query.Query) {
				gsq, ok := q.(*query.GeoShapeQuery)
				if !ok {
					t.Fatalf("intersectsBoundingBox: got %T, want *query.GeoShapeQuery", q)
				}
				if gsq.Geometry.Relation != "intersects" {
					t.Fatalf("intersectsBoundingBox relation = %q, want %q", gsq.Geometry.Relation, "intersects")
				}
			},
		},
		{
			name:       "withinBoundingBox stays a point GeoBoundingBox query",
			clauseType: "withinBoundingBox",
			assertQuery: func(t *testing.T, q query.Query) {
				if _, ok := q.(*query.GeoBoundingBoxQuery); !ok {
					t.Fatalf("withinBoundingBox: got %T, want *query.GeoBoundingBoxQuery", q)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clause := &WhereClause{Type: tc.clauseType, Field: "location", Value: box}
			q, err := ConvertToBleveQuery(clause)
			if err != nil {
				t.Fatalf("ConvertToBleveQuery(%s): %v", tc.clauseType, err)
			}
			if q == nil {
				t.Fatalf("ConvertToBleveQuery(%s): nil query", tc.clauseType)
			}
			tc.assertQuery(t, q)
		})
	}
}

// setupBboxGeoShapeIndex builds a real Bleve index whose `location` field is a
// GeoShape mapping and seeds four objects around an East-Coast box
// (lon [-80,-70], lat [35,45]):
//   - insidePoint     — a point fully inside the box
//   - insidePolygon   — a polygon fully inside the box
//   - boundaryPolygon — a polygon straddling the eastern edge (lon -70): part
//     inside, part outside — it INTERSECTS the box but is NOT within it
//   - outsidePolygon  — a polygon fully outside the box
//
// Documents are built via json.Unmarshal so the indexed geometry has the exact
// generic ([]interface{} / float64) shape the real ingest path produces.
func setupBboxGeoShapeIndex(t *testing.T) bleve.Index {
	t.Helper()

	im := bleve.NewIndexMapping()
	dm := bleve.NewDocumentMapping()
	dm.AddFieldMappingsAt("location", bleve.NewGeoShapeFieldMapping())
	dm.AddFieldMappingsAt("name", bleve.NewTextFieldMapping())
	im.DefaultMapping = dm

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "bbox_geoshape"), im)
	if err != nil {
		t.Fatalf("create bbox geoshape index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := map[string]string{
		"insidePoint":     `{"name":"insidePoint","location":{"type":"Point","coordinates":[-75.0,40.0]}}`,
		"insidePolygon":   `{"name":"insidePolygon","location":{"type":"Polygon","coordinates":[[[-76.0,39.0],[-74.0,39.0],[-74.0,41.0],[-76.0,41.0],[-76.0,39.0]]]}}`,
		"boundaryPolygon": `{"name":"boundaryPolygon","location":{"type":"Polygon","coordinates":[[[-72.0,39.0],[-68.0,39.0],[-68.0,41.0],[-72.0,41.0],[-72.0,39.0]]]}}`,
		"outsidePolygon":  `{"name":"outsidePolygon","location":{"type":"Polygon","coordinates":[[[-60.0,10.0],[-58.0,10.0],[-58.0,12.0],[-60.0,12.0],[-60.0,10.0]]]}}`,
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

var bboxEastCoast = json.RawMessage(`{
	"topLeft": {"latitude": 45.0, "longitude": -80.0},
	"bottomRight": {"latitude": 35.0, "longitude": -70.0}
}`)

// TestIntersectsBoundingBox_MatchesBoundaryCrossingShape asserts the
// externally-observable hit sets against a real geoshape index. The
// boundary-crossing polygon must match intersectsBoundingBox (it overlaps the
// box) but must NOT match withinBoundingBox (it is not fully contained) — the
// exact semantic that was collapsed when intersectsBoundingBox aliased the
// within converter.
func TestIntersectsBoundingBox_MatchesBoundaryCrossingShape(t *testing.T) {
	idx := setupBboxGeoShapeIndex(t)

	intersectsHits := searchWithWhere(t, idx, &WhereClause{
		Type: "intersectsBoundingBox", Field: "location", Value: bboxEastCoast,
	})
	// intersects overlaps the box: everything except the fully-outside polygon.
	assertIDs(t, intersectsHits, []string{"boundaryPolygon", "insidePoint", "insidePolygon"})
	assertContains(t, intersectsHits, "boundaryPolygon")

	withinHits := searchWithWhere(t, idx, &WhereClause{
		Type: "withinBoundingBox", Field: "location", Value: bboxEastCoast,
	})
	// The boundary-crossing polygon is NOT fully contained, so within excludes it.
	assertNotContains(t, withinHits, "boundaryPolygon")
}

// TestIntersectsBoundingBox_ComplementsDoesNotIntersect locks the core
// correctness invariant: intersectsBoundingBox and doesNotIntersectBoundingBox
// must partition the same universe. i.e. the intersects hit set must equal
// (all seeded objects − doesNotIntersect hit set). Before the fix,
// intersectsBoundingBox aliased the point-only within converter and returned
// the empty set on a geoshape field, contradicting its own negation.
func TestIntersectsBoundingBox_ComplementsDoesNotIntersect(t *testing.T) {
	idx := setupBboxGeoShapeIndex(t)

	universe := []string{"boundaryPolygon", "insidePoint", "insidePolygon", "outsidePolygon"}

	intersectsHits := searchWithWhere(t, idx, &WhereClause{
		Type: "intersectsBoundingBox", Field: "location", Value: bboxEastCoast,
	})
	doesNotIntersectHits := searchWithWhere(t, idx, &WhereClause{
		Type: "doesNotIntersectBoundingBox", Field: "location", Value: bboxEastCoast,
	})

	want := complement(universe, doesNotIntersectHits)
	assertIDs(t, intersectsHits, want)
}

// complement returns the members of universe not present in exclude, sorted.
func complement(universe, exclude []string) []string {
	excluded := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		excluded[e] = struct{}{}
	}
	var out []string
	for _, u := range universe {
		if _, ok := excluded[u]; !ok {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out
}

func assertContains(t *testing.T, got []string, want string) {
	t.Helper()
	for _, g := range got {
		if g == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, got)
}

func assertNotContains(t *testing.T, got []string, unwanted string) {
	t.Helper()
	for _, g := range got {
		if g == unwanted {
			t.Fatalf("did not expect %q in %v", unwanted, got)
		}
	}
}
