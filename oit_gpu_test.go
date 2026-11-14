// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestGPUOITOrderInvarianceMSAAAndSoftwareParity(t *testing.T) {
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

	red, blue := crossedGlassQuads(t)
	tex := testTexture(t, color.RGBA{60, 195, 120, 220})
	pane := quad(0.45, -2.1, color.RGBA{255, 255, 255, 160}, &Material{
		Unlit:       true,
		Transparent: true,
		Texture:     tex,
	}, false)
	pane.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	pane.Outline = OutlineStyle{Width: 2, Color: color.RGBA{255, 220, 80, 255}}

	wallBase := quad(0.32, -1.4, color.RGBA{210, 195, 175, 255}, &Material{Unlit: true}, false)
	wall, err := TransformGeometry(wallBase, nt3dmath.Translation(nt3dmath.Vec3{Y: -0.45}))
	check(t, err)

	makeFrame := func(rev uint64, msaa bool, geoms ...Geometry) Frame {
		f := depthFrame(96, 96, geoms...)
		f.Revision = rev
		f.Linear = true
		f.OIT = true
		f.MSAA = msaa
		f.Background = color.RGBA{20, 22, 28, 255}
		return f
	}

	// 1x OIT: order invariance + Software parity + Session sharing.
	f1RB := makeFrame(1, false, wall, red, blue, pane)
	f1BR := makeFrame(2, false, wall, pane, blue, red)

	gpu1RB, err := v1.RenderToImage(f1RB)
	check(t, err)
	gpu1BR, err := v1.RenderToImage(f1BR)
	check(t, err)
	gpu2RB, err := v2.RenderToImage(f1RB)
	check(t, err)
	swRB := softwareRender(f1RB)

	// Verify non-OIT flips order noticeably on GPU, while OIT is order-invariant within binary16 rounding (<= 1-2/255).
	fSortedRB := f1RB
	fSortedRB.OIT = false
	fSortedBR := f1BR
	fSortedBR.OIT = false
	gpuSortedRB, err := v1.RenderToImage(fSortedRB)
	check(t, err)
	gpuSortedBR, err := v1.RenderToImage(fSortedBR)
	check(t, err)
	if mad := meanAbsDiff(gpuSortedRB, gpuSortedBR); mad < 3.0 {
		t.Fatalf("expected non-OIT intersecting meshes to differ by order on GPU, got %.3f", mad)
	}

	if mad := meanAbsDiff(gpu1RB, gpu1BR); mad > 0.25 {
		t.Fatalf("GPU 1x OIT was not order-invariant: mean abs diff %.4f", mad)
	}
	if mad := meanAbsDiff(gpu1RB, gpu2RB); mad > 0.05 {
		t.Fatalf("GPUSession views diverged on OIT render: %.4f", mad)
	}
	if mad := meanAbsDiff(gpu1RB, swRB); mad > 3.5 {
		t.Fatalf("GPU 1x OIT vs Software mean abs diff %.2f exceeds tolerance", mad)
	}

	// 4x MSAA OIT: hardware MSAA resolve order invariance + Software parity!
	f4RB := makeFrame(3, true, wall, red, blue, pane)
	f4BR := makeFrame(4, true, wall, pane, blue, red)

	gpu4RB, err := v1.RenderToImage(f4RB)
	check(t, err)
	gpu4BR, err := v1.RenderToImage(f4BR)
	check(t, err)
	sw4RB := softwareRender(f4RB)

	if mad := meanAbsDiff(gpu4RB, gpu4BR); mad > 0.25 {
		t.Fatalf("GPU 4x MSAA OIT was not order-invariant: mean abs diff %.4f", mad)
	}
	if mad := meanAbsDiff(gpu4RB, sw4RB); mad > 4.0 {
		t.Fatalf("GPU 4x MSAA OIT vs Software mean abs diff %.2f exceeds tolerance", mad)
	}
}
