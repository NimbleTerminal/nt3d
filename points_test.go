// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// A style is valid only with known values, and a radius only in its
// units' range: pixels 1 to 32, world units any finite positive size.
func TestPointStyleValidation(t *testing.T) {
	world := PointStyle{Units: WorldUnits}
	radius := func(r float32) []Point { return []Point{{Radius: r}} }
	for name, g := range map[string]Geometry{
		"unknown shape":           {PointStyle: PointStyle{Shape: 2}, Points: radius(4)},
		"unknown units":           {PointStyle: PointStyle{Units: 2}, Points: radius(4)},
		"unknown blend":           {PointStyle: PointStyle{Blend: 3}, Points: radius(4)},
		"unknown, without points": {PointStyle: PointStyle{Blend: 3}},
		"zero world radius":       {PointStyle: world, Points: radius(0)},
		"negative world radius":   {PointStyle: world, Points: radius(-1)},
		"NaN world radius":        {PointStyle: world, Points: radius(float32(math.NaN()))},
		"infinite world radius":   {PointStyle: world, Points: radius(float32(math.Inf(1)))},
		"pixel radius under 1":    {Points: radius(.5)},
		"pixel radius over 32":    {Points: radius(33)},
	} {
		if g.Validate() == nil {
			t.Errorf("%s: valid", name)
		}
	}
	for name, g := range map[string]Geometry{
		"small world radius":     {PointStyle: world, Points: radius(.001)},
		"large world radius":     {PointStyle: world, Points: radius(1000)},
		"every field set":        {PointStyle: PointStyle{SoftPoint, WorldUnits, AdditiveBlend}, Points: radius(.1)},
		"a style without points": {PointStyle: PointStyle{Blend: AlphaBlend}},
	} {
		if err := g.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Copies, clips, and transforms keep the style; merging keeps the merged
// points' style and refuses points of different styles.
func TestPointStyleCarried(t *testing.T) {
	soft := PointStyle{Shape: SoftPoint, Units: WorldUnits, Blend: AdditiveBlend}
	g := Geometry{PointStyle: soft, Points: []Point{{Radius: .1}, {Position: nt3dmath.Vec3{X: 2}, Radius: .1}}}
	var box nt3dmath.AABB
	box.Include(nt3dmath.Vec3{X: -1, Y: -1, Z: -1})
	box.Include(nt3dmath.Vec3{X: 1, Y: 1, Z: 1})
	clipped, err := Clip(g, box)
	check(t, err)
	moved, err := TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{Z: 1}))
	check(t, err)
	line := Geometry{PointStyle: PointStyle{Blend: AlphaBlend}, Lines: []Vertex{{}, {Position: nt3dmath.Vec3{X: 1}}}}
	merged, err := MergeGeometry(line, g, Geometry{PointStyle: soft, Points: []Point{{Radius: .2}}})
	check(t, err)
	for name, got := range map[string]Geometry{"clipped": clipped, "moved": moved, "merged": merged} {
		if got.PointStyle != soft {
			t.Errorf("%s: style %+v, want %+v", name, got.PointStyle, soft)
		}
	}
	if len(clipped.Points) != 1 || len(merged.Points) != 3 {
		t.Fatalf("clipped %d points, merged %d", len(clipped.Points), len(merged.Points))
	}
	if _, err := MergeGeometry(g, Geometry{Points: []Point{{Radius: 2}}}); err == nil {
		t.Fatal("merged points of different styles")
	}
}
