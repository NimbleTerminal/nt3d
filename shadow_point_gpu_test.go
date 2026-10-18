//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func assertShadowParity(t *testing.T, r Renderer, f Frame) {
	t.Helper()
	gpu, err := r.Render(f)
	check(t, err)
	sw := softwareRender(f)
	off := 0
	for y := range f.Height {
		for x := range f.Width {
			if !closeRGB(at(gpu, x, y), at(sw, x, y), 6) {
				off++
			}
		}
	}
	if off > f.Width*f.Height/50 {
		t.Fatalf("%d/%d shadow pixels differ", off, f.Width*f.Height)
	}
}
func TestGPUPointShadows(t *testing.T) {
	r := NewGPURenderer()
	defer r.Close()
	// Rotate the light, receiver and camera together to exercise every cube face.
	rotations := []nt3dmath.Mat4{
		nt3dmath.Identity(),
		{1, 0, 0, 0, 0, -1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1},
		{1, 0, 0, 0, 0, 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1},
		{1, 0, 0, 0, 0, 0, -1, 0, 0, 1, 0, 0, 0, 0, 0, 1},
		{0, 0, 1, 0, 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 0, 1},
		{0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1},
	}
	for i, rotation := range rotations {
		f := pointShadowScene(t)
		inv, _ := rotation.InverseAffine()
		f.Matrix = f.Matrix.Mul(inv)
		f.Eye = rotation.Point(f.Eye)
		f.Forward = rotation.Point(f.Forward)
		f.Lighting.Lights[0].Position = rotation.Point(f.Lighting.Lights[0].Position)
		for j := range f.Geometry {
			f.Instances[j].World = rotation.Mul(f.Instances[j].World)
		}
		t.Run(string(rune('0'+i)), func(t *testing.T) { assertShadowParity(t, r, f) })
	}
	// Reuse one renderer across opaque, cutout, transparent, and disabled shadows.
	for name, f := range shadowScenes(t) {
		f.Lighting = pointShadowScene(t).Lighting
		f.Complete = true
		t.Run(name, func(t *testing.T) { assertShadowParity(t, r, f) })
	}
	f := pointShadowScene(t)
	f.Supersample = 2
	f.Linear = true
	assertShadowParity(t, r, f)
	f.Lighting.Lights[0].Position.X = -1.3
	assertShadowParity(t, r, f)
	f.Lighting.Lights[0].Shadows = false
	assertShadowParity(t, r, f)
	assertShadowParity(t, r, shadowScenes(t)["quad casts"])
}
func TestGPUFixedDropFloor(t *testing.T) {
	f := pointShadowScene(t)
	f.Lighting.Lights[0].Shadows = false
	f.Geometry[1].DropShadow = DropShadow{Strength: .7, Radius: 1, FixedFloor: true, FloorZ: -1}
	f.Instances[1].World = nt3dmath.Translation(nt3dmath.Vec3{X: .5, Z: .7})
	r := NewGPURenderer()
	defer r.Close()
	assertShadowParity(t, r, f)
	f.Supersample = 2
	f.Linear = true
	assertShadowParity(t, r, f)
}
