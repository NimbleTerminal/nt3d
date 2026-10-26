//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"slices"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
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

// Compact splats without SH still upload a 64-byte SH header (its flag
// selects the compact decode) and 24 bytes of half axes a splat; beside
// them in the same frame, a degree-0 cloud (one coefficient a splat) adds
// only its SH bytes.
func TestGPUCompactDegreeZero(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, n := range []int{1, 5, 7} {
		g := splatRow(n)
		dc := splatRow(n)
		dc.GaussianSH = make([]nt3dsh.RGB, n)
		for i := range dc.Points {
			dc.Points[i].Position.Y -= .25
			dc.GaussianSH[i] = nt3dsh.RGB{.3, -.2, .1 * float32(i)}
		}
		check(t, dc.Validate())
		f := flatFrame(80, 80, g, dc)
		f.Revision = nextRevision.Add(1)
		gpuMatchesSoftware(t, r, f, 12)
		for i, node := range r.nodes {
			if node.axes.size != uint64(24*n) {
				t.Errorf("%d splats, geometry %d: compact axis buffer is %d bytes, want %d", n, i, node.axes.size, 24*n)
			}
			if node.sh.size != 64 {
				t.Errorf("%d splats, geometry %d: SH header buffer is %d bytes, want 64", n, i, node.sh.size)
			}
			if node.sprite.buffer != nil {
				t.Errorf("%d splats, geometry %d: compact geometry uploaded float axes", n, i)
			}
		}
		if len(r.nodes) != 2 || r.nodes[0].shBytes.buffer != nil || r.nodes[1].shBytes.size != uint64((3*n+3)&^3) {
			t.Errorf("%d splats: SH bytes wrong for the no-SH or degree-0 cloud", n)
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

// A partial update of a compact geometry writes its axes at 24 bytes a
// splat from the first changed one; the frame after it must match software
// and the float-record render of the same updates.
func TestGPUCompactPartialUpdate(t *testing.T) {
	const n, first, end = 12, 4, 9
	var frames [2][2]Frame
	var gpu [2][2]image.Image
	for mode, precise := range []bool{false, true} {
		g := splatRow(n)
		g.PointStyle.Precise = precise
		s := NewScene()
		id, err := s.SetGeometry("splats", g)
		check(t, err)
		r := &gpuRenderer{}
		defer r.Close()
		render := func(step int) {
			t.Helper()
			f, err := s.Frame(DefaultCamera(), 80, 80)
			check(t, err)
			img, err := r.Render(f)
			check(t, err)
			frames[mode][step], gpu[mode][step] = f, img
		}
		render(0)
		update := slices.Clone(g.Points[first:end])
		for i := range update {
			update[i].Position.X += .25
			update[i].Position.Y -= .2
			update[i].GaussianAxes = [3]nt3dmath.Vec3{{X: .03, Y: .1 + .02*float32(i)}, {X: -.07, Y: .02}, {Z: .04}}
			update[i].Color.R = 255 - update[i].Color.R
		}
		check(t, s.UpdateNodePoints(id, first, update, pointBounds()))
		render(1)
		if !precise && r.nodes[0].axes.buffer == nil {
			t.Fatal("compact geometry has no axis buffer")
		}
		f := frames[mode][1]
		sw := softwareRender(f)
		off := 0
		for y := range f.Height {
			for x := range f.Width {
				if !closeRGB(at(gpu[mode][1], x, y), at(sw, x, y), 4) {
					off++
				}
			}
		}
		if off > 12 {
			t.Errorf("precise=%v: %d pixels differ from software after the update", precise, off)
		}
	}
	changed, off := 0, 0
	for y := range 80 {
		for x := range 80 {
			if !closeRGB(at(gpu[0][0], x, y), at(gpu[0][1], x, y), 4) {
				changed++
			}
			if !closeRGB(at(gpu[0][1], x, y), at(gpu[1][1], x, y), 4) {
				off++
			}
		}
	}
	if changed < 50 {
		t.Errorf("the update changed only %d pixels", changed)
	}
	if off > 12 {
		t.Errorf("compact and precise frames differ in %d pixels", off)
	}
}
