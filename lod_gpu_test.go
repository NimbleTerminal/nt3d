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
