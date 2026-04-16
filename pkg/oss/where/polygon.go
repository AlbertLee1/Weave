package where

// PointInPolygon determines whether point (x, y) lies inside the given polygon
// using the ray-casting algorithm. The polygon is a slice of [x, y] coordinate
// pairs; the last point should equal the first (closed ring) but the function
// works correctly even if it doesn't.
//
// This is used as the in-memory fallback for withinPolygon / intersectsPolygon
// operators when the indexed field is not a GeoShape.
func PointInPolygon(x, y float64, polygon [][]float64) bool {
	if len(polygon) < 3 {
		return false
	}

	inside := false
	n := len(polygon)
	j := n - 1
	for i := 0; i < n; i++ {
		if len(polygon[i]) < 2 || len(polygon[j]) < 2 {
			j = i
			continue
		}
		xi, yi := polygon[i][0], polygon[i][1]
		xj, yj := polygon[j][0], polygon[j][1]

		if ((yi > y) != (yj > y)) &&
			(x < (xj-xi)*(y-yi)/(yj-yi)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}
