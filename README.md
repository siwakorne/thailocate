# thailocate

Resolve a WGS84 latitude/longitude to the Thai administrative area that
contains it — province (จังหวัด), district (อำเภอ, or **เขต** inside Bangkok),
and subdistrict (ตำบล, or **แขวง** inside Bangkok).

**Live demo:** [thailocate.onrender.com](https://thailocate.onrender.com) —
paste a Polygon/MultiPolygon GeoJSON geometry and see which subdistricts it
covers, drawn on a map. (Free-tier hosting — the first load after a while
may take 30-60s to wake up.)

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
go get github.com/siwakorne/thailocate
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

### Polygon → many subdistricts

`GetLocationDetail` answers "which one subdistrict contains this point?".
`FindSubdistrictsIntersecting` answers the inverse, many-to-many question:
"which subdistricts does this polygon touch or overlap?" — useful for
things like a delivery/shift zone that spans several tambon/khwaeng.

```go
matches, err := loc.FindSubdistrictsIntersecting(thailocate.Geometry{
	Type: "Polygon",
	Coordinates: [][][2]float64{{ // GeoJSON ring: [lng, lat] pairs
		{100.490, 13.720},
		{100.540, 13.720},
		{100.540, 13.755},
		{100.490, 13.755},
		{100.490, 13.720},
	}},
})
if err != nil {
	log.Fatal(err)
}
for _, m := range matches {
	fmt.Println(m.DistrictEN, m.SubdistrictEN)
}
```

`Coordinates` also accepts `"MultiPolygon"` (`[][][][2]float64`), and either
form also accepts the generic `[]any` nesting you get back from decoding
arbitrary JSON/BSON into `any` (e.g. straight out of a MongoDB driver
decode) — no need to re-type already-decoded geometry.

The result is a `[]SubdistrictMatch` (same province/district/subdistrict
fields as `LocationDetail`, minus the point-specific ones) and may be empty
— not an error — if the polygon falls entirely outside Thailand. An error is
only returned for malformed input (bad `Type`, empty or non-numeric
coordinates). Intersection is tested against each subdistrict's outer
boundary only; holes are not subtracted (subdistrict polygons in this
dataset rarely have any).

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

The same polygon → many-subdistricts query is available as `POST
/v1/subdistricts-intersecting`, body is a raw GeoJSON geometry object:

```bash
curl -X POST http://localhost:8080/v1/subdistricts-intersecting \
  -d '{
        "type": "Polygon",
        "coordinates": [[[100.490,13.720],[100.540,13.720],[100.540,13.755],[100.490,13.755],[100.490,13.720]]]
      }'
```

```json
[
  {
    "province_en": "Bangkok", "province_th": "กรุงเทพมหานคร", "province_code": "TH10",
    "district_en": "Bang Rak", "district_th": "บางรัก", "district_code": "TH1004", "district_type": "khet",
    "subdistrict_en": "Si Lom", "subdistrict_th": "สีลม", "subdistrict_code": "TH100402", "subdistrict_type": "khwaeng"
  },
  { "...": "one entry per intersecting subdistrict" }
]
```

`"type"` may also be `"MultiPolygon"`. An empty `[]` response means the
polygon doesn't intersect any subdistrict (not an error); a `400` means the
geometry itself was malformed.

Change the listen address with `-addr`, e.g. `go run ./cmd/thailocate-server -addr :9090`.
Build a standalone binary with `go build -o thailocate-server ./cmd/thailocate-server` —
it's fully self-contained (data is embedded), so you can just copy the one
binary to a server and run it.

### Browser demo

Try it live at [thailocate.onrender.com](https://thailocate.onrender.com), or
run it yourself — the server also serves a small demo page at `/` (e.g.
`http://localhost:8080/`) — paste a Polygon/MultiPolygon GeoJSON geometry,
click ค้นหา, and it calls `/v1/subdistricts-intersecting` for you, lists the
matching subdistricts, and draws the shape on a Leaflet/OpenStreetMap map.
The page is embedded in the binary (`cmd/thailocate-server/web/index.html`)
same as the boundary data, so no separate deploy step is needed — it only
needs the tile/JS CDN reachable in the browser, not the server itself.

### Deploying for free (e.g. so QA can test without running it locally)

A [`render.yaml`](render.yaml) blueprint is included for [Render](https://render.com)'s
free tier:

1. Push this repo to GitHub (already done if you're reading this from there).
2. On Render, **New → Blueprint**, point it at the repo. It reads
   `render.yaml` and sets up the build/start commands automatically.
3. Deploy. Render assigns a public URL like `https://thailocate-server.onrender.com` —
   send that to your QA tester; the demo page is at the root URL.

The server reads the listen port from the `$PORT` env var if `-addr` isn't
passed, which is what Render (and most other free hosts — Cloud Run, Fly.io,
Koyeb, Railway) inject automatically, so the same binary deploys anywhere
without extra config.

Free tier note: the service spins down after ~15 minutes of inactivity and
takes 30-60s to wake back up on the next request — expected for a demo/QA
link, not meant for production traffic.

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
├── locator.go            # public API: Locator, LocationDetail, New(), GetLocationDetail(),
│                         # SubdistrictMatch, FindSubdistrictsIntersecting()
├── geometry.go            # point-in-polygon, polygon-intersects-polygon + GeoJSON parsing (stdlib only)
├── data/                  # embedded boundary polygons
│   ├── province.geojson
│   ├── amphoe.geojson
│   └── tambon.geojson
├── cmd/thailocate-server/ # optional HTTP server
└── example/basic/         # minimal usage example
```

## Publishing your own fork

This repo (`github.com/siwakorne/thailocate`) is already published this way —
tagged and `go get`-able. If you fork it under a different module path, here's
how to publish your copy:

1. **Create a public repo** on GitHub (or GitLab/etc.).
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
   go get github.com/you/thailocate@v0.1.0
   # or, to just run the server without cloning:
   go install github.com/you/thailocate/cmd/thailocate-server@latest
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
- **Semver matters**: once you tag `v1.0.0`+, any breaking change to the
  public API (`LocationDetail` fields, function signatures) needs a `v2`
  module path bump per Go's module rules — so it's worth living on `v0.x.y`
  until the API feels settled.
- Optional polish: add a `pkg.go.dev` badge (it indexes automatically once
  the module is fetched at least once) and a short `CHANGELOG.md`.

## License

Code is MIT-licensed — see [`LICENSE`](LICENSE).

The boundary data in `data/*.geojson` is **not** covered by that license: it's
CC BY-IGO (redistribution and commercial use are fine, attribution is
required). See [`DATA_ATTRIBUTION.md`](DATA_ATTRIBUTION.md) for the required
credit line — keep that file alongside the code (and any compiled binaries
that embed the data) if you redistribute this.

## Notes / limitations

- If you fork this under your own module path, update `go.mod` (currently
  `github.com/siwakorne/thailocate`) and the import paths in
  `cmd/thailocate-server` and `example/basic` to match — see
  [Publishing your own fork](#publishing-your-own-fork).
- Polygons are simplified for size (~6MB of embedded data total), so results
  right on a border are approximate to a few tens of meters at worst.
- No postal codes included. If you need them, join `SubdistrictCode`
  (`ADM3_PCODE`, e.g. `TH100101`) against a postal-code dataset such as
  [thailand-geography-data/thailand-geography-json](https://github.com/thailand-geography-data/thailand-geography-json).
