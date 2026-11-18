// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestGPUSpotLightAndPerspectiveShadowParity(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	s := NewScene()
	floor, err := BuildCuboid(Cuboid{
		Size:     nt3dmath.Vec3{X: 5, Y: 5, Z: 0.2},
		Color:    color.RGBA{225, 225, 230, 255},
		Material: &Material{PBR: true, Roughness: 0.65},
	})
	check(t, err)
	nFloor, err := s.SetGeometry("floor", floor)
	check(t, err)
	check(t, s.SetTransform(nFloor, nt3dmath.Translation(nt3dmath.Vec3{Z: -1.1})))

	ball, err := BuildSphere(Sphere{
		Radius:   0.48,
		Segments: 32,
		Rings:    20,
		Color:    color.RGBA{230, 95, 65, 255},
		Material: &Material{PBR: true, Roughness: 0.3, Metallic: 0.1},
	})
	check(t, err)
	nBall, err := s.SetGeometry("ball", ball)
	check(t, err)
	check(t, s.SetTransform(nBall, nt3dmath.Translation(nt3dmath.Vec3{Z: -0.2})))

	cam := Camera{
		Projection: Perspective,
		Target:     nt3dmath.Vec3{Z: -0.6},
		Distance:   4.8,
		Elevation:  25,
		Azimuth:    -38,
	}
	f, err := s.Frame(cam, 112, 88)
	check(t, err)
	f.Linear = true
	f.Background = color.RGBA{12, 14, 18, 255}
	f.Lighting = Lighting{
		Ambient: Ambient{Sky: color.RGBA{60, 70, 90, 255}, Ground: color.RGBA{30, 28, 25, 255}, Intensity: 0.18},
		Lights: []Light{
			{
				Kind:       SpotLight,
				Position:   nt3dmath.Vec3{X: -1.4, Y: -1.0, Z: 2.2},
				Direction:  nt3dmath.Vec3{X: 1.4, Y: 1.0, Z: -2.8}.Normalize(),
				Color:      color.RGBA{255, 235, 205, 255},
				Intensity:  3.2,
				Range:      12,
				InnerAngle: 0.22 * 180 / math.Pi,
				OuterAngle: 0.52 * 180 / math.Pi,
				Shadows:    true,
			},
			{
				Kind:       SpotLight,
				Position:   nt3dmath.Vec3{X: 1.8, Y: -0.6, Z: 1.8},
				Direction:  nt3dmath.Vec3{X: -1.8, Y: 0.6, Z: -2.4}.Normalize(),
				Color:      color.RGBA{95, 175, 255, 255},
				Intensity:  1.6,
				Range:      10,
				InnerAngle: 0.18 * 180 / math.Pi,
				OuterAngle: 0.45 * 180 / math.Pi,
			},
		},
	}

	got, err := r.Render(f)
	check(t, err)
	want := softwareRender(f)

	if mad := meanAbsDiff(got, want); mad > 3.5 {
		t.Fatalf("SpotLight + perspective shadow GPU vs Software mean abs diff %.2f exceeds tolerance", mad)
	}
}
