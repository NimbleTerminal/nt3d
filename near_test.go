// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"testing"
)

func TestClipNear(t *testing.T) {
	corner := func(z float32, shade float32) clipCorner {
		return clipCorner{x: z, y: 1, z: z, w: 1, shade: [3]float32{shade}, uv: UV{U: shade}}
	}
	var out [4]clipCorner
	for _, c := range []struct {
		name    string
		in      [3]clipCorner
		corners int
	}{
		{"in front", [3]clipCorner{corner(1, 0), corner(2, 0), corner(3, 0)}, 3},
		{"one behind", [3]clipCorner{corner(-1, 0), corner(1, 100), corner(1, 100)}, 4},
		{"two behind", [3]clipCorner{corner(-1, 0), corner(-1, 0), corner(1, 100)}, 3},
		{"all behind", [3]clipCorner{corner(-1, 0), corner(-2, 0), corner(-3, 0)}, 0},
	} {
		polygon := clipNear(c.in, &out)
		if len(polygon) != c.corners {
			t.Fatalf("%s: %d corners", c.name, len(polygon))
		}
		for _, p := range polygon {
			if p.z < 0 {
				t.Fatalf("%s: corner behind the near plane", c.name)
			}
			// Cut corners lie halfway, where z crosses zero; attributes follow.
			if p.z == 0 && (p.shade[0] != 50 || p.uv.U != 50) {
				t.Fatalf("%s: cut corner %+v", c.name, p)
			}
		}
	}
}

// nearScene is a floor reaching from far ahead to behind a low camera, in two
// triangles that each cross the near plane.
func nearScene(t *testing.T) (Frame, color.RGBA) {
	t.Helper()
	floor := color.RGBA{40, 160, 60, 255}
	g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -50, MaxX: 50, MinY: -50, MaxY: 50, Heights: make([]float32, 4), Material: &Material{Unlit: true}, Color: floor})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("floor", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Alpha, camera.Distance = Perspective, 8, 2
	f, err := s.Frame(camera, 160, 120)
	check(t, err)
	return f, floor
}

func TestSoftwareClipsAtTheNearPlane(t *testing.T) {
	f, floor := nearScene(t)
	img := softwareRender(f).(*image.RGBA)
	// Below the horizon, the whole width is floor, up to the bottom edge.
	for _, x := range []int{2, 80, 157} {
		if got := img.RGBAAt(x, 117); got != floor {
			t.Fatalf("pixel %d at the bottom is %v, not floor", x, got)
		}
	}
}

func TestWireframeClipsAtTheNearPlane(t *testing.T) {
	f, _ := nearScene(t)
	d := newWireDepth(f, 40, 20)
	if d.faces[0] == 0 || d.far[19*40+20] > 1e30 {
		t.Fatal("the floor near the camera is missing from the depth buffer")
	}
	// The floor covers the bottom row, which would otherwise show what is
	// behind it, and its edges are drawn.
	for x := range 40 {
		if d.far[19*40+x] > 1e30 {
			t.Fatalf("cell %d of the bottom row is uncovered", x)
		}
	}
	if _, err := RenderWireframe(f, 40, 20); err != nil {
		t.Fatal(err)
	}
}
