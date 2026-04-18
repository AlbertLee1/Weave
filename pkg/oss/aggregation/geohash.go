package aggregation

import (
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/mmcloughlin/geohash"
)

// Geohash character-precision bounds.
// Precision 1 ≈ 5000km cell; precision 12 ≈ 3.7cm cell. The underlying
// mmcloughlin/geohash library encodes up to 12 characters (60 bits of
// interleaved lat/lng) — beyond that the extra characters carry no signal.
// The upstream PRD language ("precision 1-15") mirrors H3 resolution bounds;
// we keep the groupBy CGO-free by using character-geohash and clamp the
// validated range to [1, 12]. Callers that need finer buckets than precision
// 12 should switch to an H3-backed resolver in a future story.
const (
	MinGeohashPrecision     = 1
	MaxGeohashPrecision     = 12
	DefaultGeohashPrecision = 6
)

// getGeohashEntries groups documents by the geohash of their geopoint field
// at the requested precision. Each distinct hash becomes a bucket; the
// bucket value is the hash string and the scope query is a DocIDQuery over
// every hit that encoded to that hash. Documents whose field is missing or
// not decodable as a lat/lng pair are skipped (no null bucket — they simply
// don't participate, matching the existing duration-bucket behaviour).
//
// The fan-out walks the matching document set once in a single Bleve search
// capped at Engine.MaxDocScanSize; when the total matches exceed the cap
// the returned (truncated=true) flag bubbles the APPROXIMATE accuracy flag
// up through the normal aggregation plumbing.
func (e *Engine) getGeohashEntries(idx bleve.Index, baseQuery query.Query, gb GroupBySpec) ([]groupEntry, bool, error) {
	precision := DefaultGeohashPrecision
	if gb.Precision != nil {
		precision = *gb.Precision
	}
	if precision < MinGeohashPrecision || precision > MaxGeohashPrecision {
		return nil, false, fmt.Errorf("geohash groupBy: precision %d out of range [%d,%d]", precision, MinGeohashPrecision, MaxGeohashPrecision)
	}
	if gb.Field == "" {
		return nil, false, fmt.Errorf("geohash groupBy: field is required")
	}

	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = e.MaxDocScanSize
	searchReq.Fields = []string{gb.Field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, false, fmt.Errorf("geohash search: %w", err)
	}
	if len(result.Hits) == 0 {
		return nil, false, nil
	}

	truncated := result.Total > uint64(len(result.Hits))

	buckets := make(map[string][]string)
	for _, hit := range result.Hits {
		val, ok := hit.Fields[gb.Field]
		if !ok {
			continue
		}
		lat, lng, ok := decodeGeopoint(val)
		if !ok {
			continue
		}
		hash := geohash.EncodeWithPrecision(lat, lng, uint(precision))
		buckets[hash] = append(buckets[hash], hit.ID)
	}

	entries := make([]groupEntry, 0, len(buckets))
	for hash, docIDs := range buckets {
		docIDQ := bleve.NewDocIDQuery(docIDs)
		entries = append(entries, groupEntry{
			value:      hash,
			scopeQuery: bleve.NewConjunctionQuery(baseQuery, docIDQ),
		})
	}
	return entries, truncated, nil
}

// decodeGeopoint accepts every shape Bleve can surface for a geopoint field:
//   - []float64{lon, lat} (the canonical Bleve stored shape — 2-element array)
//   - []interface{}{lon, lat} (post-JSON-decoded fallback)
//   - map[string]interface{}{"lon"|"lng": ..., "lat": ...} (object form)
//
// Returns (lat, lng, true) on success. Unknown shapes / non-numeric members
// short-circuit with ok=false so the caller can skip the hit without aborting.
// Bleve stores geopoints as [lon, lat] pairs regardless of input shape, so
// the array branches swap the indices back to (lat, lng) for the caller.
func decodeGeopoint(v interface{}) (lat, lng float64, ok bool) {
	switch vv := v.(type) {
	case []float64:
		if len(vv) < 2 {
			return 0, 0, false
		}
		return vv[1], vv[0], true
	case []interface{}:
		if len(vv) < 2 {
			return 0, 0, false
		}
		lon, lonOK := toFloat64(vv[0])
		la, latOK := toFloat64(vv[1])
		if !lonOK || !latOK {
			return 0, 0, false
		}
		return la, lon, true
	case map[string]interface{}:
		la, latOK := toFloat64(vv["lat"])
		if !latOK {
			la, latOK = toFloat64(vv["latitude"])
		}
		lo, lonOK := toFloat64(vv["lon"])
		if !lonOK {
			lo, lonOK = toFloat64(vv["lng"])
		}
		if !lonOK {
			lo, lonOK = toFloat64(vv["longitude"])
		}
		if !latOK || !lonOK {
			return 0, 0, false
		}
		return la, lo, true
	}
	return 0, 0, false
}

func toFloat64(v interface{}) (float64, bool) {
	switch vv := v.(type) {
	case float64:
		return vv, true
	case float32:
		return float64(vv), true
	case int:
		return float64(vv), true
	case int32:
		return float64(vv), true
	case int64:
		return float64(vv), true
	}
	return 0, false
}
