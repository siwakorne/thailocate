package thailocate

import "testing"

func TestFindSubdistrictsIntersecting_RealBoundaryData(t *testing.T) {
	loc, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// A box spanning several subdistricts around the Grand Palace / Silom area.
	geometry := Geometry{
		Type: "Polygon",
		Coordinates: [][][2]float64{{
			{100.490, 13.720},
			{100.540, 13.720},
			{100.540, 13.755},
			{100.490, 13.755},
			{100.490, 13.720},
		}},
	}

	matches, err := loc.FindSubdistrictsIntersecting(geometry)
	if err != nil {
		t.Fatalf("FindSubdistrictsIntersecting failed: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected a large box to intersect multiple subdistricts, got %d: %+v", len(matches), matches)
	}

	found := false
	for _, m := range matches {
		if m.ProvinceEN != "Bangkok" {
			t.Fatalf("expected all matches to be in Bangkok, got %+v", m)
		}
		if m.SubdistrictTH == "" || m.SubdistrictEN == "" {
			t.Fatalf("expected subdistrict names to be populated, got %+v", m)
		}
		if m.DistrictEN == "Bang Rak" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Bang Rak district among matches, got %+v", matches)
	}
}

func TestFindSubdistrictsIntersecting_NoMatchInTheOcean(t *testing.T) {
	loc, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	geometry := Geometry{
		Type: "Polygon",
		Coordinates: [][][2]float64{{
			{90.0, 5.0}, {90.1, 5.0}, {90.1, 5.1}, {90.0, 5.1}, {90.0, 5.0},
		}},
	}

	matches, err := loc.FindSubdistrictsIntersecting(geometry)
	if err != nil {
		t.Fatalf("FindSubdistrictsIntersecting failed: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no matches in open ocean, got %d: %+v", len(matches), matches)
	}
}

func TestFindSubdistrictsIntersecting_GenericInterfaceCoordinates(t *testing.T) {
	loc, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Mimics coordinates decoded generically out of JSON/BSON into `any`,
	// rather than a caller-typed [][][2]float64 literal.
	geometry := Geometry{
		Type: "Polygon",
		Coordinates: []any{
			[]any{
				[]any{100.490, 13.720},
				[]any{100.540, 13.720},
				[]any{100.540, 13.755},
				[]any{100.490, 13.755},
				[]any{100.490, 13.720},
			},
		},
	}

	matches, err := loc.FindSubdistrictsIntersecting(geometry)
	if err != nil {
		t.Fatalf("FindSubdistrictsIntersecting failed: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected generic-coordinate input to resolve the same as typed input, got %d matches", len(matches))
	}
}

func TestFindSubdistrictsIntersecting_MultiPolygon(t *testing.T) {
	loc, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Two disjoint boxes: one over Bangkok, one in open ocean.
	geometry := Geometry{
		Type: "MultiPolygon",
		Coordinates: [][][][2]float64{
			{{
				{100.490, 13.720}, {100.540, 13.720}, {100.540, 13.755}, {100.490, 13.755}, {100.490, 13.720},
			}},
			{{
				{90.0, 5.0}, {90.1, 5.0}, {90.1, 5.1}, {90.0, 5.1}, {90.0, 5.0},
			}},
		},
	}

	matches, err := loc.FindSubdistrictsIntersecting(geometry)
	if err != nil {
		t.Fatalf("FindSubdistrictsIntersecting failed: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected the Bangkok part to still resolve multiple subdistricts, got %d", len(matches))
	}
}

func TestFindSubdistrictsIntersecting_MalformedInput(t *testing.T) {
	loc, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	cases := []struct {
		name     string
		geometry Geometry
	}{
		{"unsupported type", Geometry{Type: "Point", Coordinates: [2]float64{100.5, 13.7}}},
		{"empty rings", Geometry{Type: "Polygon", Coordinates: [][][2]float64{}}},
		{"ring too short", Geometry{Type: "Polygon", Coordinates: [][][2]float64{{{100.5, 13.7}, {100.6, 13.7}}}}},
		{"non-numeric coordinates", Geometry{Type: "Polygon", Coordinates: []any{[]any{[]any{"a", "b"}}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loc.FindSubdistrictsIntersecting(tc.geometry); err == nil {
				t.Fatalf("expected an error for %s, got nil", tc.name)
			}
		})
	}
}
