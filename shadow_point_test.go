// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func pointShadowScene(t *testing.T) Frame {
	f := shadowScenes(t)["box casts"]
	f.Lighting = Lighting{Ambient: Ambient{Sky: color.RGBA{45, 45, 45, 255}, Ground: color.RGBA{45, 45, 45, 255}}, Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{X: 1.3, Z: 2}, Intensity: 2, Range: 8, Shadows: true}}}
	f.Complete = true
	return f
}
func TestPointShadowFacesAndSeams(t *testing.T) {
	var g Geometry
	directions := []nt3dmath.Vec3{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}, {Z: 1}, {Z: -1}, {X: 1, Y: 1}, {X: -1, Z: -1}}
	for _, d := range directions {
		g.Boxes = append(g.Boxes, Box{Min: d.Sub(nt3dmath.Vec3{X: .2, Y: .2, Z: .2}), Size: nt3dmath.Vec3{X: .4, Y: .4, Z: .4}, Color: white})
	}
	g.Bounds.Include(nt3dmath.Vec3{X: -3, Y: -3, Z: -3})
	g.Bounds.Include(nt3dmath.Vec3{X: 3, Y: 3, Z: 3})
	f := Frame{Geometry: []Geometry{g}, Lighting: Lighting{Lights: []Light{{Kind: PointLight, Shadows: true}}}}
	m := buildShadowMap(&f, []bool{true}, 128)
	if m == nil || len(m.faces) != 6 {
		t.Fatal("missing cube faces")
	}
	for _, d := range directions {
		n := d.Normalize().Scale(-1)
		if v := m.visibility(d.Scale(2), n, n); v > .2 {
			t.Errorf("shadow missing toward %v: %v", d, v)
		}
	}
	if v := m.visibility(nt3dmath.Vec3{X: 2, Y: -2}, nt3dmath.Vec3{}, nt3dmath.Vec3{}); v < .9 {
		t.Fatal("open direction shadowed", v)
	}
	if v := m.visibility(nt3dmath.Vec3{}, nt3dmath.Vec3{}, nt3dmath.Vec3{}); v != 1 {
		t.Fatal("light origin", v)
	}
	f.Lighting.Lights[0].Range = .5
	m = buildShadowMap(&f, []bool{true}, 128)
	if v := m.visibility(nt3dmath.Vec3{X: 2}, nt3dmath.Vec3{X: -1}, nt3dmath.Vec3{X: -1}); v != 1 {
		t.Fatal("outside range", v)
	}
	if v := fitShadows(&f, []bool{false}, 128); len(v) != 0 {
		t.Fatal("culled bounds retained")
	}
}
func TestSoftwarePointShadowMoves(t *testing.T) {
	f := pointShadowScene(t)
	m := buildShadowMap(&f, f.drawn(1), pointShadowMapSize)
	p := nt3dmath.Vec3{X: -1.3, Z: -1}
	n := nt3dmath.Vec3{Z: 1}
	dir := f.Lighting.Lights[0].Position.Sub(p).Normalize()
	if m.visibility(p, n, dir) > .2 {
		t.Fatal("point light cast no shadow")
	}
	shadow := softwareRender(f)
	f.Lighting.Lights[0].Shadows = false
	plain := softwareRender(f)
	x, y := pixel(f, p)
	if int(at(shadow, x, y).B)+15 >= int(at(plain, x, y).B) {
		t.Fatal("point shadow does not darken the receiver", at(shadow, x, y), at(plain, x, y))
	}
	f.Lighting.Lights[0].Shadows = true
	f.Lighting.Lights[0].Position.X = -1.3
	m = buildShadowMap(&f, f.drawn(1), pointShadowMapSize)
	if m.visibility(p, n, f.Lighting.Lights[0].Position.Sub(p).Normalize()) < .9 {
		t.Fatal("moving light left a stale shadow")
	}
}
func TestFixedDropFloor(t *testing.T) {
	g := quad(1, 2, white, nil, false)
	for _, v := range g.Vertices {
		g.Bounds.Include(v.Position)
	}
	g.DropShadow = DropShadow{Strength: .5, FixedFloor: true, FloorZ: -1}
	world := nt3dmath.Translation(nt3dmath.Vec3{X: 3, Z: 5})
	p, _, _, ok := dropDisc(&g, world)
	if !ok || p.X != 3 || p.Z != -1 {
		t.Fatal("floor followed instance", p)
	}
	g.DropShadow.FloorZ = 0
	p, _, _, _ = dropDisc(&g, world)
	if p.Z != 0 {
		t.Fatal("zero floor not supported")
	}
	g.DropShadow.FixedFloor = false
	p, _, _, _ = dropDisc(&g, world)
	if p.Z != 7 {
		t.Fatal("automatic floor changed", p)
	}
	g.DropShadow.FloorZ = float32(math.NaN())
	if g.Validate() == nil {
		t.Fatal("nonfinite floor accepted")
	}
}
