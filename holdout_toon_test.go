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
			Shading: ShadeUnlit,
			Alpha:   AlphaHoldout,
			Holdout: HoldoutStyle{DepthOffset: offset},
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
}

// ApplyToon is the one way to toon a subtree: it bands the meshes' light and,
// with the same Bands, the colors of points and Gaussian splats, which have
// no light to band. The zero style turns both off, and a frame built before
// the call keeps what it had.
func TestApplyToonBandsPointsAndSplatsWithTheMeshes(t *testing.T) {
	s := NewScene()
	mesh, err := s.SetGeometry("mesh", quad(.5, 0, color.RGBA{255, 255, 255, 255}, nil, false))
	check(t, err)
	pts, err := s.SetGeometry("pts", pointGeometry())
	check(t, err)
	bands := func(f Frame) (meshBands, pointBands int) {
		for _, d := range f.Draws {
			switch d.Node {
			case mesh:
				if d.Geometry.Material != nil {
					meshBands = d.Geometry.Material.Toon.Bands
				}
			case pts:
				pointBands = d.Geometry.PointStyle.ToonBands
			}
		}
		return
	}
	before, err := s.StillFrame(DefaultCamera(), 32, 32)
	check(t, err)
	check(t, s.ApplyToon(Root, ToonStyle{Bands: 4, Rim: .3}))
	after, err := s.StillFrame(DefaultCamera(), 32, 32)
	check(t, err)
	if m, p := bands(after); m != 4 || p != 4 {
		t.Fatalf("after ApplyToon with 4 bands: mesh %d, points %d", m, p)
	}
	if m, p := bands(before); m != 0 || p != 0 {
		t.Fatalf("a frame built before ApplyToon changed: mesh %d, points %d", m, p)
	}
	check(t, s.ApplyToon(Root, ToonStyle{}))
	off, err := s.StillFrame(DefaultCamera(), 32, 32)
	check(t, err)
	if m, p := bands(off); m != 0 || p != 0 {
		t.Fatalf("after the zero style: mesh %d, points %d", m, p)
	}
	if m, p := bands(after); m != 4 || p != 4 {
		t.Fatalf("turning toon off changed an earlier frame: mesh %d, points %d", m, p)
	}
}
