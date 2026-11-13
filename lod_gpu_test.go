// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// lodGPUFrame is f at 80×80 on black, evenly lit, with a fresh revision so
// the GPU uploads its geometry anew.
func lodGPUFrame(f Frame, budget int) Frame {
	f.Width, f.Height, f.SplatBudget = 80, 80, budget
	f.Background, f.Lighting = color.RGBA{0, 0, 0, 255}, even
	f.Revision = nextRevision.Add(1)
	return f
}

// lodOff counts the pixels of a and b that differ by more than 4.
func lodOff(a, b image.Image) int {
	off := 0
	for y := range 80 {
		for x := range 80 {
			if !closeRGB(at(a, x, y), at(b, x, y), 4) {
				off++
			}
		}
	}
	return off
}

func lodPixels(t *testing.T, img image.Image) []byte {
	t.Helper()
	switch img := img.(type) {
	case *image.RGBA:
		return img.Pix
	case *image.NRGBA:
		return img.Pix
	}
	t.Fatalf("image is %T", img)
	return nil
}

func TestGPULODMatchesSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	clustered := lodCloud(t, 4096, 64)
	plain := clustered
	plain.Clusters = nil
	cameras := []struct {
		name string
		f    func(...Geometry) Frame
	}{{"near", lodOrtho}, {"far", func(g ...Geometry) Frame { return lodPerspective(20, g...) }}}
	for _, cam := range cameras {
		for _, budget := range []int{0, 500} {
			f := lodGPUFrame(cam.f(clustered), budget)
			drawn, resident := f.SplatSelection()
			gpu, err := r.Render(f)
			check(t, err)
			if r.plan.points != drawn {
				t.Errorf("%s budget %d: sorted %d, selected %d", cam.name, budget, r.plan.points, drawn)
			}
			if off := lodOff(gpu, softwareRender(f)); off > 12 {
				t.Errorf("%s budget %d (%d of %d drawn): %d pixels differ", cam.name, budget, drawn, resident, off)
			}
			if cam.name == "far" && budget == 500 {
				if drawn >= resident {
					t.Fatalf("far budget 500 drew all %d splats", drawn)
				}
				full, err := r.Render(lodGPUFrame(cam.f(plain), budget))
				check(t, err)
				if bytes.Equal(lodPixels(t, gpu), lodPixels(t, full)) {
					t.Error("far budget 500 matches the unclustered frame: nothing thinned")
				}
			}
		}
	}
}

// renderPair renders the clustered cloud and the same cloud without clusters
// under budget, from far away.
func renderPair(t *testing.T, budget int) (clustered, plain []byte) {
	t.Helper()
	r := &gpuRenderer{}
	defer r.Close()
	cloud := lodCloud(t, 4096, 64)
	bare := cloud
	bare.Clusters = nil
	a, err := r.Render(lodGPUFrame(lodPerspective(20, cloud), budget))
	check(t, err)
	b, err := r.Render(lodGPUFrame(lodPerspective(20, bare), budget))
	check(t, err)
	return bytes.Clone(lodPixels(t, a)), lodPixels(t, b)
}

func TestGPULODUnlimitedEqualsUnclustered(t *testing.T) {
	if a, b := renderPair(t, -1); !bytes.Equal(a, b) {
		t.Fatal("an unlimited budget changes the GPU picture")
	}
}

// A budget that selects every splat keys them through the ranges, at full
// scale, and draws what the unclustered cloud draws with its points in the
// ranges' order: the order equal keys keep.
func TestGPULODLargeBudgetEqualsUnclustered(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	cloud := lodCloud(t, 4096, 64)
	f := lodGPUFrame(lodPerspective(5, cloud), 1<<30)
	_, lod := plan(t, f)
	bare := cloud
	bare.Clusters, bare.Points = nil, nil
	for _, rg := range lod.slots[0].ranges {
		if rg.f8 != 255 {
			t.Fatalf("range %+v is thinned", rg)
		}
		for _, i := range cloud.Clusters.Order[rg.first : rg.first+rg.count] {
			bare.Points = append(bare.Points, cloud.Points[i])
		}
	}
	if len(bare.Points) != len(cloud.Points) {
		t.Fatalf("ranges hold %d of %d points", len(bare.Points), len(cloud.Points))
	}
	a, err := r.Render(f)
	check(t, err)
	clustered := bytes.Clone(lodPixels(t, a))
	b, err := r.Render(lodGPUFrame(lodPerspective(5, bare), 1<<30))
	check(t, err)
	if !bytes.Equal(clustered, lodPixels(t, b)) {
		t.Fatal("a budget that selects every splat changes the GPU picture")
	}
}

// A supersampled frame selects at its own size in both renderers.
func TestGPULODSupersampled(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	f := lodGPUFrame(lodPerspective(20, lodCloud(t, 4096, 64)), 0) // Footprints, not the budget, thin it.
	f.Supersample = 2
	drawn, resident := f.SplatSelection()
	gpu, err := r.Render(f)
	check(t, err)
	if drawn >= resident || r.plan.points != drawn {
		t.Fatalf("selected %d of %d, sorted %d", drawn, resident, r.plan.points)
	}
	if off := lodOff(gpu, softwareRender(f)); off > 12 {
		t.Errorf("supersampled (%d of %d drawn): %d pixels differ", drawn, resident, off)
	}
}

func TestGPULODMixedFrame(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	clustered := lodCloud(t, 4096, 64)
	plain := lodCloud(t, 512, 64)
	plain.Clusters = nil
	for i := range plain.Points {
		p := &plain.Points[i].Position
		*p = nt3dmath.Vec3{X: p.X*.4 + .5, Y: p.Y*.4 - .5, Z: p.Z * .4}
	}
	plain.Bounds = GeometryBounds(plain)
	style := PointStyle{Texture: spriteAtlas(t), Columns: 2, Rows: 2, Units: PixelUnits, Blend: AlphaBlend}
	sprites := styled(style, Point{Position: nt3dmath.Vec3{X: -.5, Y: .5}, Radius: 12, Color: white, Frame: 2}, Point{Position: nt3dmath.Vec3{X: .5, Y: .5, Z: -.5}, Radius: 12, Color: white, Frame: 1})
	sprites.Bounds = GeometryBounds(sprites)
	f := lodGPUFrame(lodPerspective(20, clustered, plain, sprites), 1500)
	drawn, resident := f.SplatSelection()
	if drawn >= resident-2 {
		t.Fatalf("budget 1500 drew %d of %d", drawn, resident)
	}
	gpu, err := r.Render(f)
	check(t, err)
	if off := lodOff(gpu, softwareRender(f)); off > 12 {
		t.Errorf("mixed frame (%d of %d drawn): %d pixels differ", drawn, resident, off)
	}
}

// A point update drops a cloud's clusters; the GPU then lets their order go
// and draws the cloud in full.
func TestGPULODPointUpdateDropsOrder(t *testing.T) {
	s := NewScene()
	cloud := lodCloud(t, 4096, 64)
	id, err := s.SetGeometry("cloud", cloud)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Distance = Perspective, 60
	r := &gpuRenderer{}
	defer r.Close()
	render := func() *gpuGeometry {
		f, err := s.Frame(camera, 80, 80)
		check(t, err)
		_, err = r.Render(f)
		check(t, err)
		for i, g := range f.Geometry {
			if len(g.Points) == 4096 {
				return r.nodes[i]
			}
		}
		t.Fatal("no cloud in the frame")
		return nil
	}
	if node := render(); node.order.buffer == nil || r.plan.points >= 4096 {
		t.Fatalf("clustered cloud: order %v, %d sorted", node.order.buffer, r.plan.points)
	}
	before, err := s.Frame(camera, 80, 80)
	check(t, err)
	check(t, s.SetNodePoints(id, cloud.Points, cloud.Bounds))
	if node := render(); node.order.buffer != nil || r.plan.points != 4096 {
		t.Fatalf("updated cloud: order %v, %d sorted", node.order.buffer, r.plan.points)
	}
	// A snapshot from before the update keeps its clusters, and renders.
	if _, err := r.Render(before); err != nil {
		t.Fatalf("older snapshot after the update: %v", err)
	}
	if r.plan.points >= 4096 {
		t.Fatalf("older snapshot drew %d splats, not its selection", r.plan.points)
	}
}

func TestGPULODPlainPointCloudMatchesSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	cloud := lodCloud(t, 4096, 64)
	cloud.PointStyle = PointStyle{Shape: SoftPoint, Units: WorldUnits, Blend: AlphaBlend}
	for i := range cloud.Points {
		cloud.Points[i].Radius = 0.05
	}
	f := lodGPUFrame(lodPerspective(20, cloud), 500)
	drawn, resident := f.SplatSelection()
	if drawn == 0 || drawn >= resident {
		t.Fatalf("plain point cloud selected %d of %d", drawn, resident)
	}
	gpu, err := r.Render(f)
	check(t, err)
	if r.plan.points != drawn {
		t.Fatalf("sorted %d, selected %d", r.plan.points, drawn)
	}
	if off := lodOff(gpu, softwareRender(f)); off > 12 {
		t.Fatalf("plain point cloud LOD (%d of %d drawn): %d pixels differ", drawn, resident, off)
	}
}

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
