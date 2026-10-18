// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestDropShadowValidation(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	for _, c := range []struct {
		s, r float32
		ok   bool
	}{{0, 0, true}, {.5, 0, true}, {1, 2, true}, {1.5, 0, false}, {-.1, 0, false}, {.5, -1, false}, {float32(math.NaN()), 0, false}, {.5, float32(math.Inf(1)), false}} {
		g.DropShadow = DropShadow{c.s, c.r}
		if err := g.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestDropShadowSurvivesTransformAndMerge(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	g.DropShadow = DropShadow{Strength: .4, Radius: 2}
	moved, err := TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{X: 1}))
	check(t, err)
	later := quad(1, 2, white, nil, false)
	later.DropShadow = DropShadow{Strength: .8, Radius: 3}
	merged, err := MergeGeometry(quad(1, 1, white, nil, false), g, later)
	check(t, err)
	if moved.DropShadow != g.DropShadow || merged.DropShadow != g.DropShadow {
		t.Fatal("lost drop shadow or failed to retain first active shadow")
	}
}

func TestShadowCaster(t *testing.T) {
	down := Light{Direction: nt3dmath.Vec3{Z: 1}, Shadows: true}
	cases := []struct {
		l    Lighting
		want int
	}{
		{Lighting{}, -1},
		{Lighting{Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{Z: 1}, Shadows: true}}}, -1},
		{Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}}, down}}, 1},
		{Lighting{Lights: []Light{down, down}}, 0},
	}
	for i, c := range cases {
		if got := shadowCaster(c.l); got != c.want {
			t.Errorf("%d: %d", i, got)
		}
	}
}
func TestFitShadowEnclosesBounds(t *testing.T) {
	f := blendScene(t, nil, quad(.5, .3, white, nil, false)) // floor ±3 at z -1, quad at z .3
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}, Shadows: true}}}
	v, ok := fitShadow(&f, f.drawn(1), 2048)
	if !ok || v.light != 0 {
		t.Fatal(ok, v.light)
	}
	for _, p := range []nt3dmath.Vec3{{X: -3, Y: -3, Z: -1}, {X: 3, Y: 3, Z: -1}, {X: .5, Y: .5, Z: .3}} {
		x, y, z := v.mapCoords(p)
		if x < 0 || x > 2048 || y < 0 || y > 2048 || z < 0 || z > 1 {
			t.Errorf("%v -> %v %v %v", p, x, y, z)
		}
	}
	if v.texel <= 0 || v.texel > 6.2/2048*1.1 {
		t.Errorf("texel %v", v.texel)
	}
}

// A front face keeps its winding seen from the light: the floor's upward
// CCW quad, under a light from above, projects with the same screen-space
// sign as the camera gives it (negative area, y down), so CullModeBack and
// raster's test keep it.
func TestFitShadowKeepsWinding(t *testing.T) {
	f := blendScene(t, nil)
	for _, dir := range []nt3dmath.Vec3{{Z: 1}, {X: 1, Z: 1}, {X: -1, Y: 2, Z: .5}} {
		f.Lighting = Lighting{Lights: []Light{{Direction: dir, Shadows: true}}}
		v, ok := fitShadow(&f, f.drawn(1), 64)
		if !ok {
			t.Fatal(dir)
		}
		q := f.Geometry[0] // The floor: vertices 0, 1, 2 are its first CCW triangle.
		var x, y [3]float32
		for k := range 3 {
			x[k], y[k], _ = v.mapCoords(q.Vertices[q.Indices[k]].Position)
		}
		if area := (x[1]-x[0])*(y[2]-y[0]) - (y[1]-y[0])*(x[2]-x[0]); area >= 0 {
			t.Errorf("%v: area %v, should be negative like the camera's", dir, area)
		}
	}
}
func TestFitShadowStraightDownAndNoBounds(t *testing.T) {
	f := blendScene(t, nil)
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Shadows: true}}}
	if _, ok := fitShadow(&f, f.drawn(1), 64); !ok {
		t.Fatal("vertical light should fit")
	}
	f.Geometry[0].Bounds = nt3dmath.AABB{}
	if _, ok := fitShadow(&f, f.drawn(1), 64); ok {
		t.Fatal("no bounds, no fit")
	}
}

func TestFitShadowDrawnWorldBoundsAndPadding(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	for _, vertex := range g.Vertices {
		g.Bounds.Include(vertex.Position)
	}
	lighting := Lighting{Lights: []Light{{Kind: PointLight, Shadows: true}, {Direction: nt3dmath.Vec3{Z: 1}, Shadows: true}}}
	f := Frame{Geometry: []Geometry{g, g}, Lighting: lighting, Instances: []Instance{
		{World: nt3dmath.Translation(nt3dmath.Vec3{X: 10, Z: 4}), Visible: true},
		{World: nt3dmath.Translation(nt3dmath.Vec3{X: 100}), Visible: true},
	}}
	v, ok := fitShadow(&f, []bool{true, false}, 100)
	if !ok || v.light != 1 || newRig(&f).shadowLight != 1 {
		t.Fatalf("fit %v, light %v, rig %v", ok, v.light, newRig(&f).shadowLight)
	}
	if math.Abs(float64(v.texel-.0204)) > 1e-6 {
		t.Fatalf("texel %v, want .0204", v.texel)
	}
	x, y, z := v.mapCoords(nt3dmath.Vec3{X: 10, Z: 4})
	if math.Abs(float64(x-50)) > .001 || math.Abs(float64(y-50)) > .001 || z < 0 || z > 1 {
		t.Fatalf("world center maps to %v %v %v", x, y, z)
	}
	if _, ok := fitShadow(&f, []bool{false, false}, 100); ok {
		t.Fatal("undrawn bounds must not fit")
	}
	f.Lighting = Lighting{}
	if _, ok := fitShadow(&f, []bool{true, true}, 100); ok || newRig(&f).shadowLight != -1 {
		t.Fatal("no casting light must disable shadow fit and rig")
	}
}
