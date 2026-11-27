// Copyright (c) 2026 Neomantra Corp.

package nt3dgeo

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// FuzzGeodetic holds that any position from 5000 km below the ellipsoid to
// 100,000 km above it survives the trip to Earth-centered meters and back to
// within a micrometer, that an east-north-up frame there undoes itself, and
// that no Earth-centered point, however wild, makes FromECEF panic.
func FuzzGeodetic(f *testing.F) {
	f.Add(41.0465, -73.5418, 12.5)
	f.Add(90.0, 0.0, 0.0)
	f.Add(-90.0, 180.0, -500.0)
	f.Add(0.0, -180.0, 35786000.0)
	f.Add(89.99999999, 45.0, -5e6)
	f.Fuzz(func(t *testing.T, lat, lon, height float64) {
		_, _ = FromECEF(nt3dmath.Vec3d{X: lat, Y: lon, Z: height})
		g := Geodetic{Lat: lat, Lon: lon, Height: height}
		if g.Validate() != nil || height < -5e6 || height > 1e8 {
			return
		}
		p := g.ECEF()
		back, err := FromECEF(p)
		if err != nil {
			t.Fatalf("%+v: %v", g, err)
		}
		if err := back.Validate(); err != nil {
			t.Fatalf("%+v came back as %+v: %v", g, back, err)
		}
		if d := back.ECEF().Dist(p); d > 1e-6 {
			t.Fatalf("%+v came back as %+v, %g m away", g, back, d)
		}
		frame := ENU{Origin: g}
		if d := frame.FromECEF().Point(frame.ToECEF().Point(nt3dmath.Vec3d{X: 100, Y: -200, Z: 50})).Dist(nt3dmath.Vec3d{X: 100, Y: -200, Z: 50}); d > 1e-6 {
			t.Fatalf("the frame at %+v does not undo itself: %g m", g, d)
		}
	})
}

// FuzzRegionBox holds that no six numbers make Region.Box panic, and that a
// box it does return is finite and holds the region's middle and its corners.
func FuzzRegionBox(f *testing.F) {
	f.Add(-73.60, 41.00, -73.48, 41.09, -30.0, 300.0)
	f.Add(170.0, -20.0, -170.0, 10.0, -100.0, 9000.0)
	f.Add(-180.0, -90.0, 180.0, 90.0, 0.0, 0.0)
	f.Add(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
	f.Fuzz(func(t *testing.T, west, south, east, north, low, high float64) {
		r := Region{West: west, South: south, East: east, North: north, MinHeight: low, MaxHeight: high}
		box, err := r.Box()
		if err != nil {
			return
		}
		if !box.IsFinite() {
			t.Fatalf("%+v: the box is not finite", r)
		}
		if math.Abs(low) > 1e8 || math.Abs(high) > 1e8 {
			return
		}
		span := east - west
		if span < 0 {
			span += 360
		}
		scale := box.Radius() + SemiMajorAxis
		for _, at := range [][2]float64{{0, 0}, {1, 1}, {0, 1}, {1, 0}, {.5, .5}, {.3, .9}} {
			g := Geodetic{
				Lat:    max(-90, min(90, south+(north-south)*at[0])),
				Lon:    max(-180, min(180, wrapLongitude(west+span*at[1]))),
				Height: low + (high-low)*at[0],
			}
			if d := box.Distance(g.ECEF()); d > 1e-9*scale {
				t.Fatalf("%+v: %+v is %g m outside the box", r, g, d)
			}
		}
	})
}
