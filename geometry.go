package thailocate

import "encoding/json"

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

// polygon is one exterior ring plus zero or more holes.
type polygon struct {
	rings []ring
	bbox  bbox
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
	p := polygon{rings: make([]ring, len(raw))}
	for i, ringRaw := range raw {
		p.rings[i] = parseRing(ringRaw)
	}
	b := boundsOf(p.rings[0])
	p.bbox = b
	return p
}
