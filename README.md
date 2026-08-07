# thailocate

Resolve a WGS84 latitude/longitude to the Thai administrative area that
contains it — province (จังหวัด), district (อำเภอ, or **เขต** inside Bangkok),
and subdistrict (ตำบล, or **แขวง** inside Bangkok).

- **Boundary data is embedded in the binary** (`go:embed`) — no network
  calls, no database, no external API, at runtime.
- **Pure standard library** — no third-party Go dependencies.
- Covers all 77 provinces / 928 districts / 7,425 subdistricts of Thailand.
- Typical lookup: **well under 1ms**.
- Usable two ways: as an **imported library function**, or via a small
  **HTTP server** you run yourself so other services can call it over a URL.

Boundary polygons are derived from the UN OCHA COD-AB administrative
boundaries for Thailand (Royal Thai Survey Department data, via
[HDX](https://data.humdata.org/dataset/cod-ab-tha)), simplified for size.
Because it's a static snapshot, double check district/subdistrict names
against an official source before using this for anything legally sensitive
(boundaries do occasionally get redrawn).

## 1. Use it as a library

```bash
go get github.com/siwakorne/thailocate   # after you push this to your own repo
```

```go
package main

import (
	"fmt"
	"github.com/siwakorne/thailocate"
)

func main() {
	// one-off, uses a shared lazily-initialized instance
	detail, err := thailocate.GetLocationDetail(13.7563, 100.5018)
	if err != nil {
		panic(err)
	}
	fmt.Println(detail.ProvinceEN, detail.DistrictEN, detail.SubdistrictEN)
	// Bangkok Phra Nakhon Bowon Niwet
}
```

If you're making many calls (e.g. from inside an HTTP handler), construct one
`Locator` at startup and reuse it — it's safe for concurrent use:

```go
loc, err := thailocate.New()
if err != nil {
	log.Fatal(err)
}

detail, _ := loc.GetLocationDetail(18.7883, 98.9853)
```

### Response shape

```go
type LocationDetail struct {
	Latitude, Longitude float64
	Found                bool

	CountryEN, CountryTH string

	ProvinceEN, ProvinceTH, ProvinceCode string

	DistrictEN, DistrictTH, DistrictCode string
	DistrictType string // "amphoe" or "khet" (Bangkok)

	SubdistrictEN, SubdistrictTH, SubdistrictCode string
	SubdistrictType string // "tambon" or "khwaeng" (Bangkok)

	MatchLevel string // "subdistrict" | "district" | "province" | "none"
}
```

`Found = false` / `MatchLevel = "none"` means the point isn't in Thailand —
that's a normal result, not an error. An error is only returned for
malformed input (e.g. lat/lng out of range).

## 2. Use it over a URL

A small HTTP server ships in `cmd/thailocate-server`:

```bash
go run ./cmd/thailocate-server
# boundary data loaded, starting server on :8080

curl "http://localhost:8080/v1/locate?lat=13.7563&lng=100.5018"
```

```json
{
  "latitude": 13.7563,
  "longitude": 100.5018,
  "found": true,
  "country_en": "Thailand",
  "country_th": "ประเทศไทย",
  "province_en": "Bangkok",
  "province_th": "กรุงเทพมหานคร",
  "province_code": "TH10",
  "district_en": "Phra Nakhon",
  "district_th": "พระนคร",
  "district_code": "TH1001",
  "district_type": "khet",
  "subdistrict_en": "Bowon Niwet",
  "subdistrict_th": "บวรนิเวศ",
  "subdistrict_code": "TH100107",
  "subdistrict_type": "khwaeng",
  "match_level": "subdistrict"
}
```

Change the listen address with `-addr`, e.g. `go run ./cmd/thailocate-server -addr :9090`.
Build a standalone binary with `go build -o thailocate-server ./cmd/thailocate-server` —
it's fully self-contained (data is embedded), so you can just copy the one
binary to a server and run it.

## How it works

1. Boundary polygons (province / district / subdistrict, `data/*.geojson`)
   are compiled into the binary with `go:embed` and parsed once at startup.
2. A lookup does a bounding-box pre-check followed by an even-odd
   ray-casting point-in-polygon test, starting at the subdistrict level and
   falling back to district, then province, if a point falls in a small gap
   between simplified polygons (rare — mostly right on a coastline).
3. Everything runs in memory; there's no per-request I/O.

## Project layout

```
thailocate/
├── go.mod
├── locator.go            # public API: Locator, LocationDetail, New(), GetLocationDetail()
├── geometry.go            # point-in-polygon + GeoJSON parsing (stdlib only)
├── data/                  # embedded boundary polygons
│   ├── province.geojson
│   ├── amphoe.geojson
│   └── tambon.geojson
├── cmd/thailocate-server/ # optional HTTP server
└── example/basic/         # minimal usage example
```

## Publishing so others can `go get` it

Go has no central "publish" step like npm — the module proxy discovers
packages straight from your git host. Steps:

1. **Create a public repo** on GitHub (or GitLab/etc.), e.g.
   `github.com/siwakorne/thailocate`.
2. **Update the module path** in `go.mod` to match that repo exactly, and
   update the two `import "github.com/siwakorne/..."` lines in
   `cmd/thailocate-server/main.go` and `example/basic/main.go` to match too.
3. Commit everything (code **and** the `data/*.geojson` files — they need to
   ship with the module so `go:embed` has something to embed) and push.
4. **Tag a version**, following semver:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```
5. That's it. Anyone can now run:
   ```bash
   go get github.com/siwakorne/thailocate@v0.1.0
   # or, to just run the server without cloning:
   go install github.com/siwakorne/thailocate/cmd/thailocate-server@latest
   ```
   The first time someone fetches it, Google's public module proxy
   (`proxy.golang.org`, the default `GOPROXY`) pulls it from GitHub, caches
   it, and serves it from then on — you don't run or pay for anything.

A few things worth doing before/soon after your first tag:

- **Keep the module small enough to be practical.** The embedded data is
  ~6MB; that's fine for `go get`, just don't be surprised the repo isn't tiny.
- **Add a `go.sum`**: not needed here since there are zero external
  dependencies, but if you add any later, commit `go.sum` too so consumers
  get reproducible, verified builds.
- **License the code** (a `LICENSE` file is included — MIT by default, change
  the name/year or pick a different license as you like) **and keep
  `DATA_ATTRIBUTION.md`** — the boundary data is CC BY-IGO, which permits
  redistribution and commercial use but requires attribution; that file
  gives proper credit to the source (Royal Thai Survey Department / HDX /
  ITOS / USAID). If you publish compiled binaries too, ship that file
  alongside them.
- **Semver matters**: once you tag `v1.0.0`+, any breaking change to the
  public API (`LocationDetail` fields, function signatures) needs a `v2`
  module path bump per Go's module rules — so it's worth living on `v0.x.y`
  until the API feels settled.
- Optional polish: add a `pkg.go.dev` badge (it indexes automatically once
  the module is fetched at least once) and a short `CHANGELOG.md`.

## Notes / limitations

- Update the module path in `go.mod` (currently `github.com/siwakorne/thailocate`)
  to your own repo before publishing, and update the import paths in
  `cmd/thailocate-server` and `example/basic` to match.
- Polygons are simplified for size (~6MB of embedded data total), so results
  right on a border are approximate to a few tens of meters at worst.
- No postal codes included. If you need them, join `SubdistrictCode`
  (`ADM3_PCODE`, e.g. `TH100101`) against a postal-code dataset such as
  [thailand-geography-data/thailand-geography-json](https://github.com/thailand-geography-data/thailand-geography-json).
