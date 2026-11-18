// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestGPUHDREnvironmentSkyBloomAndIBLParity(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	v1, err := sess.NewView()
	check(t, err)
	defer v1.Close()
	v2, err := sess.NewView()
	check(t, err)
	defer v2.Close()

	env := testSunHDREnvironment(t)
	s := NewScene()
	s.SetEnvironment(env)

	chrome, err := BuildSphere(Sphere{
		Radius:   0.65,
		Segments: 40,
		Rings:    24,
		Color:    color.RGBA{245, 245, 250, 255},
		Material: &Material{PBR: true, Metallic: 1, Roughness: 0.1},
	})
	check(t, err)
	nChrome, err := s.SetGeometry("chrome", chrome)
	check(t, err)
	check(t, s.SetTransform(nChrome, nt3dmath.Translation(nt3dmath.Vec3{Y: -0.9})))

	// Look toward +X so the HDR sun is directly visible in the upper/right sky while
	// the chrome sphere sits on the left reflecting the HDR panorama.
	cam := DefaultCamera()
	cam.Elevation, cam.Azimuth, cam.Distance = 180, 0, 3.2
	f, err := s.Frame(cam, 128, 80)
	check(t, err)
	f.Linear = true
	f.Lighting = Lighting{}
	f.PostProcess = PostProcess{
		Bloom:    Bloom{Strength: 0.65, Threshold: 1.5, Radius: 1},
		Exposure: 0.7,
		ToneMap:  ACESToneMap,
	}

	gpu1, err := v1.RenderToImage(f)
	check(t, err)
	gpu2, err := v2.RenderToImage(f)
	check(t, err)
	sw := softwareRender(f)

	if mad := meanAbsDiff(gpu1, gpu2); mad > 0.1 {
		t.Fatalf("shared GPU session HDR environment views diverged: %.3f", mad)
	}
	if mad := meanAbsDiff(gpu1, sw); mad > 4.5 {
		t.Fatalf("HDR environment sky+IBL+bloom GPU vs Software mean abs diff %.2f too high", mad)
	}
}
