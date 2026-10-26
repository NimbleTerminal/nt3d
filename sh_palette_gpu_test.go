// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"testing"

	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
)

// TestGPUSHPaletteMatchesSoftware renders a paletted cloud, compact and
// Precise, and compares it with the software renderer and with its
// expanded twin.
func TestGPUSHPaletteMatchesSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, precise := range []bool{false, true} {
		g := paletteGeometry(t, 200, 9)
		g.PointStyle.Precise = precise
		e := g
		ExpandSHPalette(&e)
		for _, dcOnly := range []bool{false, true} {
			g.PointStyle.SHDCOnly, e.PointStyle.SHDCOnly = dcOnly, dcOnly
			// Each frame is 80×80 with a fresh revision, so the GPU uploads
			// it anew rather than drawing the previous frame's buffers.
			f := lodGPUFrame(lodPerspective(3, g), 0)
			f.Perspective = true
			gpu, err := r.Render(f)
			check(t, err)
			if off := lodOff(gpu, softwareRender(f)); off > 12 {
				t.Errorf("precise %v dcOnly %v: %d pixels differ from software", precise, dcOnly, off)
			}
			twin, err := r.Render(lodGPUFrame(lodPerspective(3, e), 0))
			check(t, err)
			if off := lodOff(gpu, twin); off > 12 {
				t.Errorf("precise %v dcOnly %v: %d pixels differ from the expanded twin", precise, dcOnly, off)
			}
		}
	}
}

// TestGPUSHPaletteBudgeted draws a clustered, paletted cloud under a budget.
func TestGPUSHPaletteBudgeted(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	g := paletteGeometry(t, 4096, 64)
	positions := make([]nt3dmath.Vec3, len(g.Points))
	for i, p := range g.Points {
		positions[i] = p.Position
	}
	set, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: 64})
	check(t, err)
	g.Clusters = set
	f := lodGPUFrame(lodPerspective(20, g), 500)
	gpu, err := r.Render(f)
	check(t, err)
	drawn, _ := f.SplatSelection()
	if r.plan.points != drawn || drawn >= 4096 {
		t.Fatalf("sorted %d, selected %d", r.plan.points, drawn)
	}
	if off := lodOff(gpu, softwareRender(f)); off > 12 {
		t.Errorf("%d pixels differ", off)
	}
}
