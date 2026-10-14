// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// cullScene is a field of spheres seen from close inside it, so most lie
// beside or behind the camera, with marks whose pixel sizes overhang their
// positions: a large point just past the right edge (its node second to
// last), whose disc reaches into the frame, and a line and an arrow crossing
// it from far outside.
func cullScene(t *testing.T) Frame {
	t.Helper()
	s := NewScene()
	ball, err := BuildSphere(Sphere{Radius: .3, Color: color.RGBA{200, 90, 60, 255}})
	check(t, err)
	for x := -4; x <= 4; x++ {
		for y := -4; y <= 4; y++ {
			id, err := s.SetGeometry(fmt.Sprint("ball ", x, y), ball)
			check(t, err)
			check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: float32(x), Y: float32(y)})))
		}
	}
	camera := DefaultCamera()
	camera.Distance, camera.Projection = 1.5, Perspective
	const w, h = 160, 120
	f, err := s.Frame(camera, w, h)
	check(t, err)
	// Walk right from the target until the point's center is 6 pixels past
	// the frame's right edge; an 8-pixel disc still shows.
	_, right, _, _ := camera.basis()
	lo, hi := float32(0), float32(10)
	for range 60 {
		mid := (lo + hi) / 2
		if x, _, _, cw := f.Matrix.Transform(right.Scale(mid)); (x/cw+1)*w/2 < w+6 {
			lo = mid
		} else {
			hi = mid
		}
	}
	_, err = s.SetGeometry("point", Geometry{Points: []Point{{Position: right.Scale(lo), Radius: 8, Color: color.RGBA{20, 20, 200, 255}}}})
	check(t, err)
	marks := Geometry{
		Lines:  []Vertex{{Position: right.Scale(-50), Color: color.RGBA{0, 150, 0, 255}}, {Position: right.Scale(50), Color: color.RGBA{0, 150, 0, 255}}},
		Arrows: []Arrow{{Start: right.Scale(-40).Add(nt3dmath.Vec3{Z: .2}), End: right.Scale(40).Add(nt3dmath.Vec3{Z: .2}), Width: 2, HeadSize: 8, Color: color.RGBA{90, 0, 90, 255}}},
	}
	_, err = s.SetGeometry("marks", marks)
	check(t, err)
	f, err = s.Frame(camera, w, h)
	check(t, err)
	return f
}

func culled(f Frame) int {
	n := 0
	for i, d := range f.drawn(1) {
		if !d && instance(f, i).Visible {
			n++
		}
	}
	return n
}

// Culling leaves out only what would not have shown: every renderer draws
// the same picture with it as without.
func TestCullMatchesUnculled(t *testing.T) {
	f := cullScene(t)
	if !f.Cull {
		t.Fatal("Scene.Frame does not cull")
	}
	if n := culled(f); n < 40 || n >= 81 {
		t.Fatalf("%d of 81 spheres culled", n)
	}
	off := f
	off.Cull = false
	if culled(off) != 0 {
		t.Fatal("a frame without Cull culled")
	}
	for _, s := range []int{1, 2} {
		f.Supersample, off.Supersample = s, s
		a, b := softwareRender(f).(*image.RGBA), softwareRender(off).(*image.RGBA)
		if !bytes.Equal(a.Pix, b.Pix) {
			t.Fatalf("supersample %d: software pictures differ", s)
		}
		if drawn(a, f.Background) < f.Width*f.Height/10 {
			t.Fatalf("supersample %d: little was drawn", s)
		}
	}
	if a, b := wireRender(f, 80, 30), wireRender(off, 80, 30); a != b {
		t.Fatal("wireframe pictures differ")
	}
	// The point past the edge is kept: its 8-pixel disc reaches into the
	// frame, as the GPU draws it. A small one is culled.
	point := len(f.Geometry) - 2
	if !f.drawn(1)[point] {
		t.Fatal("the overhanging point was culled")
	}
	small := f
	small.Geometry = append([]Geometry(nil), f.Geometry...)
	small.Geometry[point].Points = []Point{f.Geometry[point].Points[0]}
	small.Geometry[point].Points[0].Radius = 2
	if small.drawn(1)[point] {
		t.Fatal("a point wholly outside was drawn")
	}
	s := NewScene()
	for x := range f.Width {
		for y := range f.Height {
			if x%7 != 0 || y%7 != 0 {
				continue
			}
			camera := DefaultCamera()
			camera.Distance, camera.Projection = 1.5, Perspective
			a, b := s.Pick(camera, f, float32(x), float32(y), 4), s.Pick(camera, off, float32(x), float32(y), 4)
			if (a == nil) != (b == nil) || a != nil && (a.Node != b.Node || a.Index != b.Index) {
				t.Fatalf("pick at %d,%d: %v, unculled %v", x, y, a, b)
			}
		}
	}
	// The view moved after Scene.Frame, as the widget's display aspect moves
	// it, is the one culled against.
	wide := f
	wide.Matrix = (Camera{Alpha: 25, Beta: 40, Distance: 1.5, Projection: Perspective}).Matrix(3)
	wideOff := wide
	wideOff.Cull = false
	if !bytes.Equal(softwareRender(wide).(*image.RGBA).Pix, softwareRender(wideOff).(*image.RGBA).Pix) {
		t.Fatal("culling used a stale view")
	}
}

func TestOutside(t *testing.T) {
	camera := DefaultCamera()
	camera.Projection = Perspective
	m := camera.Matrix(1)
	eye, right, _, forward := camera.basis()
	box := func(c nt3dmath.Vec3, r float32) (b nt3dmath.AABB) {
		b.Include(c.Sub(nt3dmath.Vec3{X: r, Y: r, Z: r}))
		b.Include(c.Add(nt3dmath.Vec3{X: r, Y: r, Z: r}))
		return
	}
	for _, c := range []struct {
		name string
		box  nt3dmath.AABB
		out  bool
	}{
		{"at the target", box(nt3dmath.Vec3{}, .5), false},
		{"far to the right", box(right.Scale(50), .5), true},
		{"behind the eye", box(eye.Sub(forward.Scale(5)), .5), true},
		{"around the eye", box(eye, 1), false},
		{"across the near plane", box(eye.Add(forward.Scale(.01)), .05), false},
		{"past the far plane", box(eye.Add(forward.Scale(1000)), 1), true},
		{"enclosing the view", box(nt3dmath.Vec3{}, 1e4), false},
	} {
		if got := outside(c.box, m, 0, 0); got != c.out {
			t.Errorf("%s: outside %v", c.name, got)
		}
	}
	// A box just right of the view is kept when pixel sizes pad the view.
	near := box(right.Scale(1.6), .05)
	if !outside(near, m, 0, 0) {
		t.Fatal("test box is in view")
	}
	if outside(near, m, 1, 1) {
		t.Fatal("padding did not keep the box")
	}
}
