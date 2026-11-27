// Copyright (c) 2026 Neomantra Corp.

package nt3dgeo

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

type vec struct{ X, Y, Z float64 }

func (v vec) d() nt3dmath.Vec3d { return nt3dmath.Vec3d{X: v.X, Y: v.Y, Z: v.Z} }

// vectors is testdata/geo-vectors.json: reference values computed by PROJ,
// shared with the other implementations of these frames.
type vectors struct {
	GeodeticECEF []struct {
		Name     string
		Geodetic Geodetic
		ECEF     vec
	}
	StamfordENU struct {
		Origin Geodetic
		Points []struct {
			Name     string
			Geodetic Geodetic
			ENU      vec
		}
	}
	Region struct {
		Radians     [6]float64
		CornersECEF [8]vec
	}
	RegionSurfaces []struct {
		Name        string
		Radians     [6]float64
		SurfaceECEF []vec
	}
}

func readVectors(t testing.TB) vectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/geo-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.GeodeticECEF) == 0 || len(v.StamfordENU.Points) == 0 || len(v.RegionSurfaces) == 0 {
		t.Fatal("the reference vectors are missing")
	}
	return v
}

// Geodetic positions convert to Earth-centered meters, and back, as PROJ
// converts them, to a micrometer: at the equator, both poles, the
// antimeridian, and at Stamford from 500 m below the ellipsoid to 10 km up.
func TestGeodeticAndECEFAgreeWithPROJ(t *testing.T) {
	for _, c := range readVectors(t).GeodeticECEF {
		t.Run(c.Name, func(t *testing.T) {
			if err := c.Geodetic.Validate(); err != nil {
				t.Fatal(err)
			}
			if d := c.Geodetic.ECEF().Dist(c.ECEF.d()); d > 1e-6 {
				t.Fatalf("ECEF is %g m from PROJ's", d)
			}
			back, err := FromECEF(c.ECEF.d())
			if err != nil {
				t.Fatal(err)
			}
			// Compared as points, since a pole has every longitude.
			if d := back.ECEF().Dist(c.ECEF.d()); d > 1e-6 {
				t.Fatalf("the position found, %+v, is %g m from the point", back, d)
			}
			if math.Abs(back.Lat-c.Geodetic.Lat) > 1e-11 || math.Abs(back.Height-c.Geodetic.Height) > 1e-6 {
				t.Fatalf("found %+v, want %+v", back, c.Geodetic)
			}
		})
	}
}

// Points 1 m, 1 km, and 10 km from an origin at Stamford land in its
// east-north-up frame where PROJ's topocentric conversion puts them, to a
// micrometer, including the drop below the tangent plane that the Earth's
// curve gives a distant point. The frame's matrices say the same.
func TestENUAgreesWithPROJ(t *testing.T) {
	v := readVectors(t)
	frame := ENU{Origin: v.StamfordENU.Origin}
	for _, c := range v.StamfordENU.Points {
		t.Run(c.Name, func(t *testing.T) {
			got := frame.Point(c.Geodetic)
			if d := got.Dist(c.ENU.d()); d > 1e-6 {
				t.Fatalf("ENU %+v is %g m from PROJ's", got, d)
			}
			if d := frame.FromECEF().Point(c.Geodetic.ECEF()).Dist(got); d > 1e-8 {
				t.Fatalf("FromECEF disagrees with Point by %g m", d)
			}
			if d := frame.ToECEF().Point(got).Dist(c.Geodetic.ECEF()); d > 1e-8 {
				t.Fatalf("ToECEF misses the point by %g m", d)
			}
			back, err := frame.Geodetic(got)
			if err != nil {
				t.Fatal(err)
			}
			if d := back.ECEF().Dist(c.Geodetic.ECEF()); d > 1e-6 {
				t.Fatalf("Geodetic misses the position by %g m", d)
			}
		})
	}
}

// A point 10 km from the origin sits below the frame's plane by what the
// Earth curves in that distance, about 15.7 m at 14 km: the frame is exact in
// three dimensions and not a flat map.
func TestENUKeepsTheEarthsCurve(t *testing.T) {
	frame := ENU{Origin: Geodetic{Lat: 41.0465, Lon: -73.5418}}
	p := frame.Point(Geodetic{Lat: 41.1365, Lon: -73.4228})
	if p.Z > -15.5 || p.Z < -15.9 {
		t.Fatalf("a point on the ellipsoid 14 km away is %g m from the plane, want about -15.7", p.Z)
	}
}

// The box around a region holds the region's whole curved surface and not
// only its corners: every PROJ point on its edges and middle, at both
// heights, across a city block, a 10 km tile, a region over the antimeridian
// and the equator, and a continent. It is not much larger than it must be.
func TestRegionBoxHoldsTheCurvedSurface(t *testing.T) {
	v := readVectors(t)
	downtown, err := RegionFromRadians(v.Region.Radians).Box()
	if err != nil {
		t.Fatal(err)
	}
	for i, corner := range v.Region.CornersECEF {
		if d := downtown.Distance(corner.d()); d > 1e-6 {
			t.Errorf("corner %d is %g m outside the box", i, d)
		}
	}
	for _, c := range v.RegionSurfaces {
		t.Run(c.Name, func(t *testing.T) {
			region := RegionFromRadians(c.Radians)
			box, err := region.Box()
			if err != nil {
				t.Fatal(err)
			}
			for i, p := range c.SurfaceECEF {
				if d := box.Distance(p.d()); d > 1e-6 {
					t.Errorf("surface point %d is %g m outside the box", i, d)
				}
			}
			// Positions drawn from all over the region, not only its edges.
			rng := rand.New(rand.NewSource(1))
			span := region.East - region.West
			if span < 0 {
				span += 360
			}
			for range 2000 {
				g := Geodetic{
					Lat:    region.South + rng.Float64()*(region.North-region.South),
					Lon:    wrapLongitude(region.West + rng.Float64()*span),
					Height: region.MinHeight + rng.Float64()*(region.MaxHeight-region.MinHeight),
				}
				if d := box.Distance(g.ECEF()); d > 1e-6 {
					t.Fatalf("%+v is %g m outside the box", g, d)
				}
			}
		})
	}
}

// Across a 10 km tile the surface rises about 2 m above its corners, and the
// box is taller than the corners need by that and little more.
func TestRegionBoxIsTightForACityTile(t *testing.T) {
	region := Region{West: -73.60, South: 41.00, East: -73.48, North: 41.09, MinHeight: 0, MaxHeight: 0}
	box, err := region.Box()
	if err != nil {
		t.Fatal(err)
	}
	up := box.Axes[2].Len() * 2
	if up < 2 || up > 4.5 {
		t.Fatalf("the box around a flat 10 km tile is %g m tall, want the 2 to 4 m its curve needs", up)
	}
	east, north := box.Axes[0].Len()*2, box.Axes[1].Len()*2
	if east < 10080 || east > 10100 || north < 9990 || north > 10010 {
		t.Fatalf("the box is %g by %g m, want about 10090 by 9995", east, north)
	}
}

// A region that bounds nothing is refused, and one whose west is past its
// east is the one across the antimeridian.
func TestRegionValidate(t *testing.T) {
	good := Region{West: 170, South: -10, East: -170, North: 10, MaxHeight: 100}
	if err := good.Validate(); err != nil {
		t.Fatalf("a region across the antimeridian was refused: %v", err)
	}
	box, err := good.Box()
	if err != nil {
		t.Fatal(err)
	}
	if d := box.Distance(Geodetic{Lat: 0, Lon: 180}.ECEF()); d > 1e-6 {
		t.Fatalf("the antimeridian is %g m outside its region's box", d)
	}
	if d := box.Distance(Geodetic{Lat: 0, Lon: 0}.ECEF()); d < 1e6 {
		t.Fatalf("the far side of the Earth is only %g m from the box", d)
	}
	for name, bad := range map[string]Region{
		"south above north":  {South: 10, North: -10},
		"heights reversed":   {North: 1, East: 1, MinHeight: 5, MaxHeight: 1},
		"latitude past pole": {North: 91},
		"longitude past 180": {East: 181},
		"not finite":         {East: math.NaN()},
	} {
		if bad.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := bad.Box(); err == nil {
			t.Errorf("%s: boxed", name)
		}
	}
	if got := RegionFromRadians(good.Radians()); math.Abs(got.West-170) > 1e-12 || math.Abs(got.East+170) > 1e-12 {
		t.Fatalf("radians and back gave %+v", got)
	}
}

// A position off the Earth is an error, and so is an Earth-centered point
// that is not a number.
func TestWrongPositionsAreErrors(t *testing.T) {
	for name, g := range map[string]Geodetic{
		"latitude":  {Lat: 90.5},
		"longitude": {Lon: -180.5},
		"height":    {Height: math.Inf(1)},
	} {
		if g.Validate() == nil {
			t.Errorf("%s out of range was accepted", name)
		}
	}
	if _, err := FromECEF(nt3dmath.Vec3d{X: math.NaN()}); err == nil {
		t.Error("a point that is not a number was converted")
	}
}

// Place splits a tile's transform into ECEF so that nothing large reaches
// float32: a model 10 km from the scene's origin lands within a tenth of a
// millimeter of where float64 puts it, where rounding the whole transform to
// float32 would be off by half a meter.
func TestPlaceKeepsATileExactTenKilometersOut(t *testing.T) {
	scene := ENU{Origin: Geodetic{Lat: 41.0465, Lon: -73.5418}}
	tile := ENU{Origin: Geodetic{Lat: 41.1365, Lon: -73.4228, Height: 100}}
	origin, local, err := scene.Place(tile.ToECEF())
	if err != nil {
		t.Fatal(err)
	}
	if local[12] != 0 || local[13] != 0 || local[14] != 0 {
		t.Fatalf("the float32 matrix carries a translation: %v", local[12:15])
	}
	// A vertex 30 m from the tile's own origin, as tile content is.
	vertex := nt3dmath.Vec3d{X: 30, Y: -20, Z: 5}
	want := scene.FromECEF().Point(tile.ToECEF().Point(vertex))
	turned := local.Point(vertex.Vec3()).D()
	got := nt3dmath.Vec3d{X: origin[0] + turned.X, Y: origin[1] + turned.Y, Z: origin[2] + turned.Z}
	if d := got.Dist(want); d > 1e-4 {
		t.Fatalf("placed %g m from where it belongs", d)
	}
	if want.Len() < 14000 {
		t.Fatalf("the tile is only %g m out; the test means one about 14 km away", want.Len())
	}
	// The two frames are turned against each other, by the angle the Earth
	// curves between them: small, and not nothing.
	if tilt := math.Acos(float64(local[10])); tilt < 0.002 || tilt > 0.003 {
		t.Fatalf("the tile's up leans %g radians from the scene's, want about 0.0022", tilt)
	}
	bad := tile.ToECEF()
	bad[3] = 1
	if _, _, err := scene.Place(bad); err == nil {
		t.Error("a transform that is not affine was placed")
	}
}
