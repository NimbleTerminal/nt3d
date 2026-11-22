// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

func holdoutSplatScene(t *testing.T, bands int, offset float32) Frame {
	t.Helper()
	s := NewScene()

	// Proxy box from Z=-1..0, X=-0.5..0.5, Y=-0.5..0.5: invisible holdout with
	// black silhouette and crease ink.
	proxy, err := BuildCuboid(Cuboid{
		Size: nt3dmath.Vec3{X: 1.0, Y: 1.0, Z: 1.0},
		Material: &Material{
			Shading:     ShadeUnlit,
			Alpha:       AlphaHoldout,
			DepthOffset: offset,
		},
	})
	check(t, err)
	proxy.Outline = OutlineStyle{Width: 2, Color: color.RGBA{10, 12, 18, 255}, Crease: 45}
	_, err = s.SetGeometry("proxy", proxy)
	check(t, err)

	// A gradient of Gaussians on the front face (Y = -0.5) and one hidden
	// Gaussian behind the proxy (Y = +1.5).
	var pts []Point
	var sh []nt3dsh.RGB
	for i := range 9 {
		u := float32(i) / 8 // 0..1 brightness gradient
		x := -0.36 + float32(i)*0.09
		pts = append(pts, Point{
			Position:     nt3dmath.Vec3{X: x, Y: -0.5, Z: 0},
			Radius:       1,
			Color:        color.RGBA{255, 255, 255, 250},
			GaussianAxes: [3]nt3dmath.Vec3{{X: 0.08}, {Y: 0.01}, {Z: 0.25}},
		})
		sh = append(sh, nt3dsh.RGB{(u - 0.5) / 0.28209479, (0.5*u - 0.5) / 0.28209479, (0.2*u - 0.5) / 0.28209479})
	}
	// Hidden bright green Gaussian behind the holdout cube.
	pts = append(pts, Point{
		Position:     nt3dmath.Vec3{X: 0, Y: 1.5, Z: 0},
		Radius:       1,
		Color:        color.RGBA{0, 255, 0, 255},
		GaussianAxes: [3]nt3dmath.Vec3{{X: 0.3}, {Y: 0.02}, {Z: 0.3}},
	})
	sh = append(sh, nt3dsh.RGB{-1.7, 1.8, -1.7})

	splats := Geometry{
		Points:     pts,
		GaussianSH: sh,
		PointStyle: PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend, ToonBands: bands},
	}
	splats.Bounds = GeometryBounds(splats)
	_, err = s.SetGeometry("splats", splats)
	check(t, err)

	cam := DefaultCamera().LookAt(nt3dmath.Vec3{X: 0, Y: -2.4, Z: 0.4}, nt3dmath.Vec3{})
	cam.Projection = Perspective
	f, err := s.StillFrame(cam, 128, 128)
	check(t, err)
	f.Background = color.RGBA{180, 200, 230, 255}
	return f
}

func TestHoldoutOccludesBehindAndDrawsInkOverToonSplats(t *testing.T) {
	f := holdoutSplatScene(t, 3, 0.08)
	img := softwareRender(f)

	// No green Gaussian behind the holdout cube should leak through center.
	cx := at(img, 64, 64)
	if cx.G > cx.R+30 {
		t.Fatalf("hidden green Gaussian leaked through AlphaHoldout: center=%v", cx)
	}
	// Black ink outline/crease pixels must appear on top of the scene.
	ink := color.RGBA{10, 12, 18, 255}
	if n := coloredPixels(img, ink); n < 40 {
		t.Fatalf("expected holdout outline/crease ink pixels, got %d", n)
	}

	// Verify ApplyPointToon updates point ToonBands on a scene subtree.
	s := NewScene()
	id, err := s.SetGeometry("pts", pointGeometry())
	check(t, err)
	check(t, s.ApplyPointToon(Root, 4))
	f2, err := s.StillFrame(DefaultCamera(), 32, 32)
	check(t, err)
	if f2.Draws[0].Node != id || f2.Draws[0].Geometry.PointStyle.ToonBands != 4 {
		t.Fatalf("ApplyPointToon did not set ToonBands: %+v", f2.Draws[0].Geometry.PointStyle)
	}
	if s.ApplyPointToon(Root, 1) == nil {
		t.Fatal("ApplyPointToon accepted bands=1")
	}
}
