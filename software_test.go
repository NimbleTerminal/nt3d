// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func depthAt(r SoftwareResult, x, y int) float32 { return r.Depth[y*r.Width+x] }

// The picture is the software renderer's, and depth is the depth it tests.
func TestRenderSoftware(t *testing.T) {
	red := color.RGBA{220, 0, 0, 255}
	f := blendScene(t, nil, quad(.5, .3, red, nil, false))
	for _, s := range []int{1, 2} {
		f.Supersample = s
		f.Overlays = []OverlayRect{{X: 0, Y: 0, Width: 6, Height: 6, Color: color.RGBA{0, 0, 0, 255}}}
		r, err := RenderSoftware(f)
		check(t, err)
		want, err := NewSoftwareRenderer().Render(f)
		check(t, err)
		if r.Width != f.Width || r.Height != f.Height || len(r.Depth) != f.Width*f.Height || !bytes.Equal(r.Image.Pix, want.(*image.RGBA).Pix) {
			t.Fatalf("supersample %d: result %d×%d with %d depths, or pictures differ", s, r.Width, r.Height, len(r.Depth))
		}
		// The pane is nearer than the floor, at the depth its point projects
		// to; overlays write no depth.
		_, _, z, w := f.Matrix.Transform(nt3dmath.Vec3{Z: .3})
		if d := depthAt(r, 40, 40); math.Abs(float64(d-z/w)) > 1e-3 {
			t.Fatalf("supersample %d: pane depth %v, want %v", s, d, z/w)
		}
		if floor := depthAt(r, 40, 5); floor <= depthAt(r, 40, 40) || math.IsInf(float64(floor), 0) {
			t.Fatalf("supersample %d: floor depth %v", s, floor)
		}
		if d := depthAt(r, 2, 2); math.IsInf(float64(d), 0) {
			t.Fatalf("supersample %d: the overlay hid the floor's depth", s)
		}
	}
	// Where nothing is drawn, depth is +Inf.
	bare := blendScene(t, nil)
	bare.Geometry, bare.Instances, bare.GeometryRevisions, bare.Shared = nil, nil, nil, nil
	r, err := RenderSoftware(bare)
	check(t, err)
	for _, d := range r.Depth {
		if !math.IsInf(float64(d), 1) {
			t.Fatalf("empty frame depth %v", d)
		}
	}
	if _, err := RenderSoftware(Frame{}); err == nil {
		t.Fatal("invalid frame rendered")
	}
}

// Transparent surfaces hide nothing, so they leave depth to what is behind.
func TestRenderSoftwareTransparentDepth(t *testing.T) {
	pane := blendScene(t, nil, quad(.5, .3, color.RGBA{220, 0, 0, 128}, glass, false))
	floor := blendScene(t, nil)
	a, err := RenderSoftware(pane)
	check(t, err)
	b, err := RenderSoftware(floor)
	check(t, err)
	if depthAt(a, 40, 40) != depthAt(b, 40, 40) {
		t.Fatalf("glass wrote depth: %v, floor %v", depthAt(a, 40, 40), depthAt(b, 40, 40))
	}
}
