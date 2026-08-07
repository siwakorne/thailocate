package thailocate

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// point is a (longitude, latitude) pair — GeoJSON coordinate order is [lng, lat].
type point struct {
	Lng, Lat float64
}

// ring is a closed sequence of points. ring[0] of a polygon is the outer
// boundary; any further rings are holes cut out of it.
type ring []point

// bbox is an axis-aligned bounding box used to cheaply reject candidates
// before running the more expensive ray-casting test.
type bbox struct {
	minLng, minLat, maxLng, maxLat float64
}

func (b bbox) contains(lng, lat float64) bool {
	return lng >= b.minLng && lng <= b.maxLng && lat >= b.minLat && lat <= b.maxLat
}

func boundsOf(r ring) bbox {
	b := bbox{minLng: r[0].Lng, minLat: r[0].Lat, maxLng: r[0].Lng, maxLat: r[0].Lat}
	for _, p := range r {
		if p.Lng < b.minLng {
			b.minLng = p.Lng
		}
		if p.Lng > b.maxLng {
			b.maxLng = p.Lng
		}
		if p.Lat < b.minLat {
			b.minLat = p.Lat
		}
		if p.Lat > b.maxLat {
			b.maxLat = p.Lat
		}
	}
	return b
}

func mergeBounds(a, b bbox) bbox {
	return bbox{
		minLng: min(a.minLng, b.minLng),
		minLat: min(a.minLat, b.minLat),
		maxLng: max(a.maxLng, b.maxLng),
		maxLat: max(a.maxLat, b.maxLat),
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ringContains implements the standard even-odd ray casting test.
func ringContains(r ring, lng, lat float64) bool {
	inside := false
	n := len(r)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := r[i].Lng, r[i].Lat
		xj, yj := r[j].Lng, r[j].Lat
		if (yi > lat) != (yj > lat) {
			atX := (xj-xi)*(lat-yi)/(yj-yi) + xi
			if lng < atX {
				inside = !inside
			}
		}
	}
	return inside
}

// segIntersectCross and onSegment implement the standard orientation test
// for segment-segment intersection (used by ringsIntersect).
func segIntersectCross(a, b, c point) float64 {
	return (b.Lng-a.Lng)*(c.Lat-a.Lat) - (b.Lat-a.Lat)*(c.Lng-a.Lng)
}

func onSegment(p, q, r point) bool {
	return q.Lng <= max(p.Lng, r.Lng) && q.Lng >= min(p.Lng, r.Lng) &&
		q.Lat <= max(p.Lat, r.Lat) && q.Lat >= min(p.Lat, r.Lat)
}

func segmentsIntersect(p1, p2, p3, p4 point) bool {
	d1 := segIntersectCross(p3, p4, p1)
	d2 := segIntersectCross(p3, p4, p2)
	d3 := segIntersectCross(p1, p2, p3)
	d4 := segIntersectCross(p1, p2, p4)

	if ((d1 > 0) != (d2 > 0)) && ((d3 > 0) != (d4 > 0)) {
		return true
	}
	if d1 == 0 && onSegment(p3, p1, p4) {
		return true
	}
	if d2 == 0 && onSegment(p3, p2, p4) {
		return true
	}
	if d3 == 0 && onSegment(p1, p3, p2) {
		return true
	}
	if d4 == 0 && onSegment(p1, p4, p2) {
		return true
	}
	return false
}

// ringsIntersect reports whether two rings share any area or boundary: a
// vertex of one falls inside the other, or an edge of one crosses an edge of
// the other. It does not attempt exact overlap-area computation.
func ringsIntersect(a, b ring) bool {
	for _, p := range a {
		if ringContains(b, p.Lng, p.Lat) {
			return true
		}
	}
	for _, p := range b {
		if ringContains(a, p.Lng, p.Lat) {
			return true
		}
	}
	for i := range a {
		a1, a2 := a[i], a[(i+1)%len(a)]
		for j := range b {
			b1, b2 := b[j], b[(j+1)%len(b)]
			if segmentsIntersect(a1, a2, b1, b2) {
				return true
			}
		}
	}
	return false
}

func bboxesOverlap(a, b bbox) bool {
	return a.minLng <= b.maxLng && a.maxLng >= b.minLng && a.minLat <= b.maxLat && a.maxLat >= b.minLat
}

// polygon is one exterior ring plus zero or more holes.
type polygon struct {
	rings []ring
	bbox  bbox
}

// intersects reports whether p and other share any area or boundary. Only
// the outer boundary (rings[0]) of each polygon is tested — holes are not
// subtracted, the same simplification GetLocationDetail's boundary data
// already carries (see README "Notes / limitations"). In practice Thai
// subdistrict polygons rarely have holes, so this is a good approximation.
func (p polygon) intersects(other polygon) bool {
	if !bboxesOverlap(p.bbox, other.bbox) {
		return false
	}
	return ringsIntersect(p.rings[0], other.rings[0])
}

func (p polygon) contains(lng, lat float64) bool {
	if !p.bbox.contains(lng, lat) {
		return false
	}
	if !ringContains(p.rings[0], lng, lat) {
		return false
	}
	for _, hole := range p.rings[1:] {
		if ringContains(hole, lng, lat) {
			return false // point falls inside a hole cut out of the shape
		}
	}
	return true
}

// multiPolygon is a set of (possibly disjoint) polygons that together make
// up one administrative area, e.g. a coastal district with offshore islands.
type multiPolygon struct {
	polygons []polygon
	bbox     bbox
}

func (m multiPolygon) contains(lng, lat float64) bool {
	if !m.bbox.contains(lng, lat) {
		return false
	}
	for _, p := range m.polygons {
		if p.contains(lng, lat) {
			return true
		}
	}
	return false
}

// intersects reports whether m and other share any area or boundary.
func (m multiPolygon) intersects(other multiPolygon) bool {
	if !bboxesOverlap(m.bbox, other.bbox) {
		return false
	}
	for _, p := range m.polygons {
		for _, q := range other.polygons {
			if p.intersects(q) {
				return true
			}
		}
	}
	return false
}

// --- GeoJSON parsing -------------------------------------------------------

type geoJSONCollection struct {
	Type     string        `json:"type"`
	Features []geoJSONFeat `json:"features"`
}

type geoJSONFeat struct {
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
	Geometry   struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
}

func parseRing(raw [][2]float64) ring {
	r := make(ring, len(raw))
	for i, c := range raw {
		r[i] = point{Lng: c[0], Lat: c[1]}
	}
	return r
}

func geometryToMultiPolygon(f geoJSONFeat) (multiPolygon, error) {
	var mp multiPolygon

	switch f.Geometry.Type {
	case "Polygon":
		var raw [][][2]float64
		if err := json.Unmarshal(f.Geometry.Coordinates, &raw); err != nil {
			return mp, err
		}
		mp.polygons = []polygon{polygonFromRings(raw)}
	case "MultiPolygon":
		var raw [][][][2]float64
		if err := json.Unmarshal(f.Geometry.Coordinates, &raw); err != nil {
			return mp, err
		}
		mp.polygons = make([]polygon, 0, len(raw))
		for _, polyRaw := range raw {
			mp.polygons = append(mp.polygons, polygonFromRings(polyRaw))
		}
	}

	if len(mp.polygons) > 0 {
		b := mp.polygons[0].bbox
		for _, p := range mp.polygons[1:] {
			b = mergeBounds(b, p.bbox)
		}
		mp.bbox = b
	}
	return mp, nil
}

func polygonFromRings(raw [][][2]float64) polygon {
	rings := make([]ring, len(raw))
	for i, ringRaw := range raw {
		rings[i] = parseRing(ringRaw)
	}
	return newPolygon(rings)
}

func newPolygon(rings []ring) polygon {
	return polygon{rings: rings, bbox: boundsOf(rings[0])}
}

// --- flexible query-geometry parsing ---------------------------------------
//
// Unlike the embedded boundary data (parsed once from known-good JSON at
// startup), a caller-supplied query geometry may come from anywhere — a
// literal Go slice, or generic []any/map values decoded out of JSON
// or a database driver (e.g. MongoDB's bson package decoding a document
// field typed `any`). The helpers below accept either form and
// return a descriptive error for anything malformed, since (unlike a point
// that's simply outside Thailand) a broken query polygon isn't a valid input.

// Geometry is a GeoJSON-style Polygon or MultiPolygon describing an
// arbitrary area to query with FindSubdistrictsIntersecting — e.g. a rider
// shift zone, delivery area, or any other caller-defined boundary. It is
// intentionally not tied to the embedded administrative boundary data.
//
// Coordinates follows standard GeoJSON coordinate nesting for Type: rings of
// [lng, lat] pairs, with ring[0] the outer boundary and any further rings
// holes cut out of it.
//
//   - "Polygon":      [][][2]float64  (one ring set)
//   - "MultiPolygon":  [][][][2]float64 (one ring set per disjoint part)
//
// Coordinates also accepts the generic nesting ([]any of
// []any of ... numbers) produced by decoding arbitrary JSON/BSON
// into `any`, so callers can pass a value straight out of a database decode
// without re-typing it into the concrete Go shape above — including
// unmarshaling a standard GeoJSON geometry object straight off the wire,
// since json.Unmarshal populates Coordinates with exactly that nesting.
type Geometry struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}

func geometryQueryToMultiPolygon(g Geometry) (multiPolygon, error) {
	var mp multiPolygon

	switch g.Type {
	case "Polygon":
		rings, err := coordsToRings(g.Coordinates)
		if err != nil {
			return mp, fmt.Errorf("thailocate: parsing polygon coordinates: %w", err)
		}
		mp.polygons = []polygon{newPolygon(rings)}
	case "MultiPolygon":
		polys, err := coordsToPolygons(g.Coordinates)
		if err != nil {
			return mp, fmt.Errorf("thailocate: parsing multipolygon coordinates: %w", err)
		}
		mp.polygons = polys
	default:
		return mp, fmt.Errorf("thailocate: unsupported geometry type %q (want \"Polygon\" or \"MultiPolygon\")", g.Type)
	}

	b := mp.polygons[0].bbox
	for _, p := range mp.polygons[1:] {
		b = mergeBounds(b, p.bbox)
	}
	mp.bbox = b
	return mp, nil
}

// coordsToRings parses a Polygon's coordinate array (a ring set) from either
// the concrete [][][2]float64 nesting or the generic []any nesting.
func coordsToRings(coords any) ([]ring, error) {
	if raw, ok := coords.([][][2]float64); ok {
		if len(raw) == 0 {
			return nil, fmt.Errorf("polygon has no rings")
		}
		rings := make([]ring, len(raw))
		for i, r := range raw {
			if len(r) < 3 {
				return nil, fmt.Errorf("ring must have at least 3 points, got %d", len(r))
			}
			rings[i] = parseRing(r)
		}
		return rings, nil
	}

	raw, ok := toInterfaceSlice(coords)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("polygon coordinates must be a non-empty array of rings")
	}
	rings := make([]ring, 0, len(raw))
	for _, ringRaw := range raw {
		r, err := parseGenericRing(ringRaw)
		if err != nil {
			return nil, err
		}
		rings = append(rings, r)
	}
	return rings, nil
}

// coordsToPolygons parses a MultiPolygon's coordinate array (one ring set
// per disjoint part) from either the concrete [][][][2]float64 nesting or
// the generic []any nesting.
func coordsToPolygons(coords any) ([]polygon, error) {
	if raw, ok := coords.([][][][2]float64); ok {
		if len(raw) == 0 {
			return nil, fmt.Errorf("multipolygon has no parts")
		}
		polys := make([]polygon, len(raw))
		for i, polyRaw := range raw {
			if len(polyRaw) == 0 {
				return nil, fmt.Errorf("multipolygon part %d has no rings", i)
			}
			rings := make([]ring, len(polyRaw))
			for j, r := range polyRaw {
				if len(r) < 3 {
					return nil, fmt.Errorf("multipolygon part %d: ring must have at least 3 points, got %d", i, len(r))
				}
				rings[j] = parseRing(r)
			}
			polys[i] = newPolygon(rings)
		}
		return polys, nil
	}

	raw, ok := toInterfaceSlice(coords)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("multipolygon coordinates must be a non-empty array of polygons")
	}
	polys := make([]polygon, 0, len(raw))
	for _, polyRaw := range raw {
		rings, err := coordsToRings(polyRaw)
		if err != nil {
			return nil, err
		}
		polys = append(polys, newPolygon(rings))
	}
	return polys, nil
}

func parseGenericRing(ringRaw any) (ring, error) {
	ptsRaw, ok := toInterfaceSlice(ringRaw)
	if !ok {
		return nil, fmt.Errorf("ring must be an array of [lng,lat] pairs")
	}

	r := make(ring, 0, len(ptsRaw))
	for _, ptRaw := range ptsRaw {
		if pair, ok := ptRaw.([2]float64); ok {
			r = append(r, point{Lng: pair[0], Lat: pair[1]})
			continue
		}
		pairRaw, ok := toInterfaceSlice(ptRaw)
		if !ok || len(pairRaw) < 2 {
			return nil, fmt.Errorf("coordinate pair must have at least 2 numbers, got %v", ptRaw)
		}
		lng, ok1 := toFloat64(pairRaw[0])
		lat, ok2 := toFloat64(pairRaw[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("coordinate pair must be numeric, got %v", ptRaw)
		}
		r = append(r, point{Lng: lng, Lat: lat})
	}
	if len(r) < 3 {
		return nil, fmt.Errorf("ring must have at least 3 points, got %d", len(r))
	}
	return r, nil
}

// toInterfaceSlice normalizes any slice/array value (concrete or already
// []any) into []any via reflection, so the generic parsing
// path above doesn't need to special-case every possible decoded type.
func toInterfaceSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
