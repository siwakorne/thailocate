// Package thailocate resolves a WGS84 latitude/longitude coordinate to the
// Thai administrative area that contains it: province (จังหวัด), district
// (อำเภอ, or เขต inside Bangkok) and subdistrict (ตำบล, or แขวง inside Bangkok).
//
// Boundary data is embedded in the compiled binary, so lookups never touch
// the network or disk at runtime.
//
// Basic usage:
//
//	loc, err := thailocate.New()
//	if err != nil { ... }
//	detail, err := loc.GetLocationDetail(13.7563, 100.5018)
//
// A ready-to-use default instance is also available without constructing
// one yourself:
//
//	detail, err := thailocate.GetLocationDetail(13.7563, 100.5018)
package thailocate

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed data/province.geojson data/amphoe.geojson data/tambon.geojson
var dataFS embed.FS

// MatchLevel indicates the finest administrative level that was actually
// resolved for a query. Lookups almost always resolve at the subdistrict
// level; province/district-only results are a rare fallback for points that
// fall in tiny gaps between simplified polygons (e.g. right at a coastline).
type MatchLevel string

const (
	MatchSubdistrict MatchLevel = "subdistrict"
	MatchDistrict    MatchLevel = "district"
	MatchProvince    MatchLevel = "province"
	MatchNone        MatchLevel = "none"
)

// LocationDetail is everything thailocate knows about a coordinate.
type LocationDetail struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Found     bool    `json:"found"`

	CountryEN string `json:"country_en,omitempty"`
	CountryTH string `json:"country_th,omitempty"`

	ProvinceEN   string `json:"province_en,omitempty"`
	ProvinceTH   string `json:"province_th,omitempty"`
	ProvinceCode string `json:"province_code,omitempty"`

	// District is the อำเภอ (Amphoe) — or เขต (Khet) for the 50 districts
	// of Bangkok. DistrictType tells you which term applies.
	DistrictEN   string `json:"district_en,omitempty"`
	DistrictTH   string `json:"district_th,omitempty"`
	DistrictCode string `json:"district_code,omitempty"`
	DistrictType string `json:"district_type,omitempty"` // "amphoe" | "khet"

	// Subdistrict is the ตำบล (Tambon) — or แขวง (Khwaeng) for Bangkok's
	// subdistricts. SubdistrictType tells you which term applies.
	SubdistrictEN   string `json:"subdistrict_en,omitempty"`
	SubdistrictTH   string `json:"subdistrict_th,omitempty"`
	SubdistrictCode string `json:"subdistrict_code,omitempty"`
	SubdistrictType string `json:"subdistrict_type,omitempty"`

	MatchLevel MatchLevel `json:"match_level"`
}

type provinceFeature struct {
	mp     multiPolygon
	nameEN string
	nameTH string
	code   string
}

type districtFeature struct {
	mp           multiPolygon
	nameEN       string
	nameTH       string
	code         string
	provinceEN   string
	provinceTH   string
	provinceCode string
}

type subdistrictFeature struct {
	mp           multiPolygon
	nameEN       string
	nameTH       string
	code         string
	districtEN   string
	districtTH   string
	districtCode string
	provinceEN   string
	provinceTH   string
	provinceCode string
}

// Locator holds the parsed boundary polygons in memory and answers lookups.
// It is safe for concurrent use by multiple goroutines.
type Locator struct {
	provinces    []provinceFeature
	districts    []districtFeature
	subdistricts []subdistrictFeature
}

// New parses the embedded boundary data and returns a ready-to-use Locator.
// This does a few megabytes of JSON parsing, so construct one Locator and
// reuse it (it's safe for concurrent use) rather than calling New per request.
func New() (*Locator, error) {
	loc := &Locator{}

	provinceCollection, err := loadCollection("data/province.geojson")
	if err != nil {
		return nil, fmt.Errorf("thailocate: loading province data: %w", err)
	}
	for _, f := range provinceCollection.Features {
		mp, err := geometryToMultiPolygon(f)
		if err != nil {
			return nil, fmt.Errorf("thailocate: parsing province geometry: %w", err)
		}
		loc.provinces = append(loc.provinces, provinceFeature{
			mp:     mp,
			nameEN: f.Properties["ADM1_EN"],
			nameTH: f.Properties["ADM1_TH"],
			code:   f.Properties["ADM1_PCODE"],
		})
	}

	districtCollection, err := loadCollection("data/amphoe.geojson")
	if err != nil {
		return nil, fmt.Errorf("thailocate: loading district data: %w", err)
	}
	for _, f := range districtCollection.Features {
		mp, err := geometryToMultiPolygon(f)
		if err != nil {
			return nil, fmt.Errorf("thailocate: parsing district geometry: %w", err)
		}
		loc.districts = append(loc.districts, districtFeature{
			mp:           mp,
			nameEN:       f.Properties["ADM2_EN"],
			nameTH:       f.Properties["ADM2_TH"],
			code:         f.Properties["ADM2_PCODE"],
			provinceEN:   f.Properties["ADM1_EN"],
			provinceTH:   f.Properties["ADM1_TH"],
			provinceCode: f.Properties["ADM1_PCODE"],
		})
	}

	subdistrictCollection, err := loadCollection("data/tambon.geojson")
	if err != nil {
		return nil, fmt.Errorf("thailocate: loading subdistrict data: %w", err)
	}
	for _, f := range subdistrictCollection.Features {
		mp, err := geometryToMultiPolygon(f)
		if err != nil {
			return nil, fmt.Errorf("thailocate: parsing subdistrict geometry: %w", err)
		}
		loc.subdistricts = append(loc.subdistricts, subdistrictFeature{
			mp:           mp,
			nameEN:       f.Properties["ADM3_EN"],
			nameTH:       f.Properties["ADM3_TH"],
			code:         f.Properties["ADM3_PCODE"],
			districtEN:   f.Properties["ADM2_EN"],
			districtTH:   f.Properties["ADM2_TH"],
			districtCode: f.Properties["ADM2_PCODE"],
			provinceEN:   f.Properties["ADM1_EN"],
			provinceTH:   f.Properties["ADM1_TH"],
			provinceCode: f.Properties["ADM1_PCODE"],
		})
	}

	return loc, nil
}

func loadCollection(path string) (*geoJSONCollection, error) {
	raw, err := dataFS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c geoJSONCollection
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

const (
	bangkokEN = "Bangkok"
)

func districtTerm(provinceEN string) (district, subdistrict string) {
	if provinceEN == bangkokEN {
		return "khet", "khwaeng"
	}
	return "amphoe", "tambon"
}

// GetLocationDetail resolves lat/lng (WGS84 decimal degrees) to the
// Thai province / district / subdistrict that contains it.
//
// If the point falls outside Thailand, or in a gap in the simplified
// boundary data, Found is false and MatchLevel is "none" — this is not
// treated as an error, since "not in Thailand" is a perfectly valid answer.
// An error is only returned for malformed input.
func (l *Locator) GetLocationDetail(lat, lng float64) (*LocationDetail, error) {
	if lat < -90 || lat > 90 {
		return nil, fmt.Errorf("thailocate: latitude %v out of range [-90,90]", lat)
	}
	if lng < -180 || lng > 180 {
		return nil, fmt.Errorf("thailocate: longitude %v out of range [-180,180]", lng)
	}

	detail := &LocationDetail{
		Latitude:  lat,
		Longitude: lng,
		CountryEN: "Thailand",
		CountryTH: "ประเทศไทย",
	}

	// 1) Finest level first: subdistrict (tambon/khwaeng) polygons already
	// carry their parent district & province names, so a single match
	// gives us the full answer.
	for _, s := range l.subdistricts {
		if s.mp.contains(lng, lat) {
			dType, sType := districtTerm(s.provinceEN)
			detail.Found = true
			detail.MatchLevel = MatchSubdistrict
			detail.ProvinceEN, detail.ProvinceTH, detail.ProvinceCode = s.provinceEN, s.provinceTH, s.provinceCode
			detail.DistrictEN, detail.DistrictTH, detail.DistrictCode = s.districtEN, s.districtTH, s.districtCode
			detail.DistrictType = dType
			detail.SubdistrictEN, detail.SubdistrictTH, detail.SubdistrictCode = s.nameEN, s.nameTH, s.code
			detail.SubdistrictType = sType
			return detail, nil
		}
	}

	// 2) Fall back to district level (covers small gaps between the
	// simplified subdistrict polygons).
	for _, d := range l.districts {
		if d.mp.contains(lng, lat) {
			dType, _ := districtTerm(d.provinceEN)
			detail.Found = true
			detail.MatchLevel = MatchDistrict
			detail.ProvinceEN, detail.ProvinceTH, detail.ProvinceCode = d.provinceEN, d.provinceTH, d.provinceCode
			detail.DistrictEN, detail.DistrictTH, detail.DistrictCode = d.nameEN, d.nameTH, d.code
			detail.DistrictType = dType
			return detail, nil
		}
	}

	// 3) Fall back to province level.
	for _, p := range l.provinces {
		if p.mp.contains(lng, lat) {
			detail.Found = true
			detail.MatchLevel = MatchProvince
			detail.ProvinceEN, detail.ProvinceTH, detail.ProvinceCode = p.nameEN, p.nameTH, p.code
			return detail, nil
		}
	}

	// Outside Thailand (or in a data gap at province level too).
	detail.CountryEN, detail.CountryTH = "", ""
	detail.MatchLevel = MatchNone
	return detail, nil
}

// SubdistrictMatch is one administrative area (province/district/subdistrict)
// returned by FindSubdistrictsIntersecting.
type SubdistrictMatch struct {
	ProvinceEN   string `json:"province_en"`
	ProvinceTH   string `json:"province_th"`
	ProvinceCode string `json:"province_code"`

	// District is the อำเภอ (Amphoe) — or เขต (Khet) for the 50 districts
	// of Bangkok. DistrictType tells you which term applies.
	DistrictEN   string `json:"district_en"`
	DistrictTH   string `json:"district_th"`
	DistrictCode string `json:"district_code"`
	DistrictType string `json:"district_type"` // "amphoe" | "khet"

	// Subdistrict is the ตำบล (Tambon) — or แขวง (Khwaeng) for Bangkok's
	// subdistricts. SubdistrictType tells you which term applies.
	SubdistrictEN   string `json:"subdistrict_en"`
	SubdistrictTH   string `json:"subdistrict_th"`
	SubdistrictCode string `json:"subdistrict_code"`
	SubdistrictType string `json:"subdistrict_type"`
}

// FindSubdistrictsIntersecting resolves an arbitrary polygon — e.g. a rider
// shift zone or delivery area — to every subdistrict (tambon/khwaeng) whose
// boundary it touches or overlaps. It is the many-to-many counterpart of
// GetLocationDetail: a single point resolves to at most one subdistrict, but
// a polygon commonly spans several.
//
// The result is a slice in no particular order, and an empty slice (not an
// error) is a normal result if the polygon falls entirely outside Thailand
// or in a gap between simplified boundaries. An error is only returned for
// a malformed geometry (missing/unsupported Type, empty or non-numeric
// coordinates).
func (l *Locator) FindSubdistrictsIntersecting(geometry Geometry) ([]SubdistrictMatch, error) {
	query, err := geometryQueryToMultiPolygon(geometry)
	if err != nil {
		return nil, err
	}

	matches := make([]SubdistrictMatch, 0)
	for _, s := range l.subdistricts {
		if !query.intersects(s.mp) {
			continue
		}
		dType, sType := districtTerm(s.provinceEN)
		matches = append(matches, SubdistrictMatch{
			ProvinceEN:   s.provinceEN,
			ProvinceTH:   s.provinceTH,
			ProvinceCode: s.provinceCode,

			DistrictEN:   s.districtEN,
			DistrictTH:   s.districtTH,
			DistrictCode: s.districtCode,
			DistrictType: dType,

			SubdistrictEN:   s.nameEN,
			SubdistrictTH:   s.nameTH,
			SubdistrictCode: s.code,
			SubdistrictType: sType,
		})
	}
	return matches, nil
}

// --- convenience package-level API ------------------------------------------

var (
	defaultLocator     *Locator
	defaultLocatorOnce sync.Once
	defaultLocatorErr  error
)

func defaultInstance() (*Locator, error) {
	defaultLocatorOnce.Do(func() {
		defaultLocator, defaultLocatorErr = New()
	})
	return defaultLocator, defaultLocatorErr
}

// GetLocationDetail is a package-level convenience wrapper around a lazily
// initialized, shared Locator. Use this if you just want to call
// thailocate.GetLocationDetail(lat, lng) without managing a Locator yourself.
func GetLocationDetail(lat, lng float64) (*LocationDetail, error) {
	loc, err := defaultInstance()
	if err != nil {
		return nil, err
	}
	return loc.GetLocationDetail(lat, lng)
}

// FindSubdistrictsIntersecting is a package-level convenience wrapper around
// a lazily initialized, shared Locator. See Locator.FindSubdistrictsIntersecting.
func FindSubdistrictsIntersecting(geometry Geometry) ([]SubdistrictMatch, error) {
	loc, err := defaultInstance()
	if err != nil {
		return nil, err
	}
	return loc.FindSubdistrictsIntersecting(geometry)
}
