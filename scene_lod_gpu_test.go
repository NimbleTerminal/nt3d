// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"bytes"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestGPUSceneSetLODCacheAndParity(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	s := NewScene()
	nearMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 24, Rings: 12, Color: color.RGBA{230, 60, 60, 255}})
	check(t, err)
	midMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 12, Rings: 6, Color: color.RGBA{60, 210, 80, 255}})
	check(t, err)
	farMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 6, Rings: 4, Color: color.RGBA{60, 90, 230, 255}})
	check(t, err)

	id, err := s.Add(Root, "building")
	check(t, err)
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 10, Geometry: nearMesh, Outline: OutlineStyle{Width: 2, Crease: 35, Color: color.RGBA{15, 15, 15, 255}}},
		{MaxDistance: 25, Geometry: midMesh, Outline: OutlineStyle{Width: 1, Color: color.RGBA{15, 15, 15, 255}}},
		{MaxDistance: 50, Geometry: farMesh},
	}))

	frameAt := func(x float32) Frame {
		c := DefaultCamera()
		c.Projection = Perspective
		c = c.LookAt(nt3dmath.Vec3{X: x}, nt3dmath.Vec3{})
		f, err := s.Frame(c, 80, 80)
		check(t, err)
		return f
	}

	// Near frame uploads only tier 0 (lazy per-tier GPU upload).
	f0 := frameAt(6)
	gpu0, err := r.Render(f0)
	check(t, err)
	if r.uploads != 1 || len(r.geometries) != 1 {
		t.Fatalf("after near render: uploads=%d cached=%d, want 1, 1", r.uploads, len(r.geometries))
	}
	if off := lodOff(gpu0, softwareRender(f0)); off > 20 {
		t.Fatalf("near GPU/software mismatch: %d pixels differ", off)
	}

	// Mid frame uploads tier 1 while retaining tier 0 in cache.
	f1 := frameAt(18)
	gpu1, err := r.Render(f1)
	check(t, err)
	if r.uploads != 2 || len(r.geometries) != 2 {
		t.Fatalf("after mid render: uploads=%d cached=%d, want 2, 2", r.uploads, len(r.geometries))
	}
	if off := lodOff(gpu1, softwareRender(f1)); off > 20 {
		t.Fatalf("mid GPU/software mismatch: %d pixels differ", off)
	}

	// Returning to near reuses tier 0 without re-uploading.
	gpu0Again, err := r.Render(f0)
	check(t, err)
	if r.uploads != 2 || len(r.geometries) != 2 {
		t.Fatalf("after returning near: uploads=%d cached=%d, want 2, 2", r.uploads, len(r.geometries))
	}
	if !bytes.Equal(lodPixels(t, gpu0), lodPixels(t, gpu0Again)) {
		t.Fatal("returning to cached near tier changed rendered pixels")
	}

	// Distance-culled frame uploads tier 2 once (as the hidden placeholder) and retains tiers 0 and 1.
	fCull := frameAt(70)
	gpuCull, err := r.Render(fCull)
	check(t, err)
	if r.uploads != 3 || len(r.geometries) != 3 {
		t.Fatalf("after distance-culled render: uploads=%d cached=%d, want 3, 3", r.uploads, len(r.geometries))
	}
	if off := lodOff(gpuCull, softwareRender(fCull)); off != 0 {
		t.Fatalf("distance-culled frame drew %d unexpected pixels", off)
	}

	// Removing the node releases all cached LOD GPU buffers on the next frame.
	check(t, s.Remove(id))
	fEmpty := frameAt(6)
	_, err = r.Render(fEmpty)
	check(t, err)
	if len(r.geometries) != 0 {
		t.Fatalf("removed LOD node left %d cached GPU geometries", len(r.geometries))
	}
}

func TestGPUSessionLODDualViewports(t *testing.T) {
	inst, adapter, dev := newTestHostDevice(t)
	defer dev.Release()
	defer adapter.Release()
	defer inst.Release()

	mainView, err := NewGPUSession(dev)
	check(t, err)
	defer mainView.Close()
	minimapView, err := mainView.NewView()
	check(t, err)
	defer minimapView.Close()

	s := NewScene()
	nearMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 20, Rings: 10, Color: color.RGBA{220, 60, 60, 255}})
	check(t, err)
	farMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 6, Rings: 4, Color: color.RGBA{60, 80, 220, 255}})
	check(t, err)
	id, err := s.Add(Root, "block")
	check(t, err)
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 12, Geometry: nearMesh},
		{Geometry: farMesh},
	}))

	nearCam := DefaultCamera()
	nearCam.Projection = Perspective
	nearCam = nearCam.LookAt(nt3dmath.Vec3{X: 6}, nt3dmath.Vec3{})

	farCam := DefaultCamera()
	farCam.Projection = Perspective
	farCam = farCam.LookAt(nt3dmath.Vec3{X: 30}, nt3dmath.Vec3{})

	fNear, err := s.Frame(nearCam, 80, 80)
	check(t, err)
	fFar, err := s.Frame(farCam, 80, 80)
	check(t, err)

	// Render near in mainView and far in minimapView across multiple frames:
	// each tier uploads once total across the shared device cache.
	for range 3 {
		imgNear, err := mainView.RenderToImage(fNear)
		check(t, err)
		imgFar, err := minimapView.RenderToImage(fFar)
		check(t, err)
		if off := lodOff(imgNear, softwareRender(fNear)); off > 20 {
			t.Fatalf("mainView near mismatch: %d pixels", off)
		}
		if off := lodOff(imgFar, softwareRender(fFar)); off > 20 {
			t.Fatalf("minimapView far mismatch: %d pixels", off)
		}
	}
	if total := mainView.g.uploads + minimapView.g.uploads; total != 2 {
		t.Fatalf("total GPU uploads across dual LOD views = %d, want 2", total)
	}
}

func TestGPUSceneLODBiasHysteresisAndPathAgreement(t *testing.T) {
	gpu := &gpuRenderer{}
	defer gpu.Close()
	sw := NewSoftwareRenderer()
	defer sw.Close()

	makeScene := func() *Scene {
		s := NewScene()
		nearMesh, err := BuildSphere(Sphere{Radius: .8, Segments: 20, Rings: 10, Color: color.RGBA{225, 60, 55, 255}})
		check(t, err)
		midMesh, err := BuildSphere(Sphere{Radius: .8, Segments: 10, Rings: 6, Color: color.RGBA{55, 195, 85, 255}})
		check(t, err)
		farMesh, err := BuildSphere(Sphere{Radius: .8, Segments: 6, Rings: 4, Color: color.RGBA{65, 95, 225, 255}})
		check(t, err)
		sgNear, err := NewSharedGeometry(nearMesh)
		check(t, err)
		sgMid, err := NewSharedGeometry(midMesh)
		check(t, err)
		sgFar, err := NewSharedGeometry(farMesh)
		check(t, err)

		for i, pos := range []nt3dmath.Vec3{
			{X: -2, Y: 0},
			{X: 0, Y: 2},
			{X: 2.5, Y: -1.5},
			{X: -1.5, Y: -3.5},
		} {
			id, err := s.Add(Root, "tile"+itoaTest(i))
			check(t, err)
			check(t, s.SetTransform(id, nt3dmath.Translation(pos)))
			check(t, s.SetLOD(id, []LODLevel{
				{MaxDistance: 10, Shared: sgNear},
				{MaxDistance: 20, Shared: sgMid},
				{MaxDistance: 36, Shared: sgFar},
			}))
		}
		check(t, s.SetLODHysteresis(0.1))
		return s
	}

	// Independent GPU and software scene instances replayed through the exact same
	// sequence of camera path poses and bias steps must agree on every frame's tiers and pixels.
	gpuScene := makeScene()
	swScene := makeScene()

	type stepSpec struct {
		bias float32 // nonzero applies SetLODBias on both scenes
		eye  nt3dmath.Vec3
	}
	steps := []stepSpec{
		{eye: nt3dmath.Vec3{X: 8, Y: -4, Z: 3}},
		{eye: nt3dmath.Vec3{X: 11.2, Y: -4, Z: 3}}, // inside outward hysteresis band
		{eye: nt3dmath.Vec3{X: 15, Y: -5, Z: 4}},   // crosses into mid tier
		{eye: nt3dmath.Vec3{X: 11.2, Y: -4, Z: 3}}, // inside inward hysteresis band (stays mid)
		{bias: 0.7, eye: nt3dmath.Vec3{X: 15, Y: -5, Z: 4}},
		{bias: 1.5, eye: nt3dmath.Vec3{X: 15, Y: -5, Z: 4}},
		{eye: nt3dmath.Vec3{X: 48, Y: -10, Z: 8}}, // crosses final cutoff with bias 1.5 (36*1.5=54 -> still visible or partially culled)
		{bias: 0.5, eye: nt3dmath.Vec3{X: 25, Y: -5, Z: 4}},
	}

	var uploadsAfterWarm uint64
	for idx, st := range steps {
		if st.bias > 0 {
			check(t, gpuScene.SetLODBias(st.bias))
			check(t, swScene.SetLODBias(st.bias))
		}
		cam := DefaultCamera()
		cam.Projection = Perspective
		cam = cam.LookAt(st.eye, nt3dmath.Vec3{})

		fg, err := gpuScene.Frame(cam, 80, 64)
		check(t, err)
		fs, err := swScene.Frame(cam, 80, 64)
		check(t, err)

		sg := fg.Stats()
		ss := fs.Stats()
		if sg != ss {
			t.Fatalf("step %d: GPU scene stats %+v != software scene stats %+v", idx, sg, ss)
		}

		gpuImg, err := gpu.Render(fg)
		check(t, err)
		swImg, err := sw.Render(fs)
		check(t, err)
		if off := lodOff(gpuImg, swImg); off > 24 {
			t.Fatalf("step %d: GPU/software image mismatch (%d pixels differ)", idx, off)
		}
		if gpu.uploads > 3 {
			t.Fatalf("step %d: GPU uploaded %d meshes across 3 shared tiers (bias change must not re-upload)", idx, gpu.uploads)
		}
		uploadsAfterWarm = gpu.uploads
	}
	if uploadsAfterWarm != 3 {
		t.Fatalf("expected all 3 shared LOD tiers uploaded once total, got %d", uploadsAfterWarm)
	}
}
