//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// splatRow is n Gaussians without SH across the view, each a little
// different in shape, tilt, and color.
func splatRow(n int) Geometry {
	g := gaussianGeometry()
	p := g.Points[0]
	g.Points = nil
	for i := range n {
		q := p
		t := float32(i) / float32(max(n-1, 1))
		q.Position = nt3dmath.Vec3{X: -.6 + 1.2*t, Y: .3 - .6*t, Z: .3 + .4*t}
		q.GaussianAxes = [3]nt3dmath.Vec3{{X: .08 + .04*t, Y: .03 * t}, {X: -.02, Y: .05}, {Z: .03}}
		q.Color = color.RGBA{uint8(40 + 30*i), uint8(200 - 20*i), uint8(90 + 15*i), 200}
		g.Points = append(g.Points, q)
	}
	return g
}

// gpuMatchesSoftware renders f on the GPU and in software and fails when
// more pixels than the parity budget differ.
func gpuMatchesSoftware(t *testing.T, r *gpuRenderer, f Frame, budget int) {
	t.Helper()
	gpu, err := r.Render(f)
	check(t, err)
	sw := softwareRender(f)
	off := 0
	for y := range f.Height {
		for x := range f.Width {
			if !closeRGB(at(gpu, x, y), at(sw, x, y), 4) {
				off++
			}
		}
	}
	if off > budget {
		t.Fatalf("%d pixels differ", off)
	}
}

// Compact splats without SH still upload a 48-byte SH header (its flag
// selects the compact decode) and 24 bytes of half axes a splat.
func TestGPUCompactDegreeZero(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, n := range []int{1, 5, 7} {
		g := splatRow(n)
		f := flatFrame(80, 80, g)
		f.Revision = nextRevision.Add(1)
		gpuMatchesSoftware(t, r, f, 12)
		node := r.nodes[0]
		if node.axes.size != uint64(24*n) {
			t.Errorf("%d splats: compact axis buffer is %d bytes, want %d", n, node.axes.size, 24*n)
		}
		if node.sh.size != 48 {
			t.Errorf("%d splats: SH header buffer is %d bytes, want 48", n, node.sh.size)
		}
		if node.sprite.buffer != nil {
			t.Errorf("%d splats: compact geometry uploaded float axes", n)
		}
	}
}

// Setting Precise on a shown geometry uploads it again in the float layout;
// a node kept from the compact upload would draw from the wrong records.
func TestGPUPreciseToggleReuploads(t *testing.T) {
	s := NewScene()
	g := shGaussianGeometry(2)
	id, err := s.SetGeometry("splat", g)
	check(t, err)
	check(t, s.SetTransform(id, nt3dmath.RotationZ(.5)))
	r := &gpuRenderer{}
	defer r.Close()
	gpuMatchesSoftware(t, r, frame(t, s), 12)
	if r.nodes[0].axes.buffer == nil {
		t.Fatal("default geometry did not upload compact axes")
	}
	g.PointStyle.Precise = true
	_, err = s.SetGeometry("splat", g)
	check(t, err)
	gpuMatchesSoftware(t, r, frame(t, s), 12)
	if r.nodes[0].axes.buffer != nil || r.nodes[0].sprite.buffer == nil {
		t.Fatal("precise geometry did not upload float axes")
	}
}
