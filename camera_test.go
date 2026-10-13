package nt3d

import (
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestCameraProjectionAndRay(t *testing.T) {
	for _, proj := range []Projection{Orthographic, Perspective} {
		c := DefaultCamera()
		c.Projection = proj
		m := c.Matrix(2)
		x, y, z, ok := m.Project(nt3dmath.Vec3{}, 800, 400)
		if !ok || abs(x-400) > .001 || abs(y-200) > .001 || z <= 0 || z >= 1 {
			t.Fatalf("center: %v %v %v %v", x, y, z, ok)
		}
		r := c.ray(50, 25, 100, 50, 8, 8)
		if r.Origin.Scale(-1).Normalize().Dot(r.Direction) < .999 {
			t.Fatal("central ray misses target")
		}
		rr := c.RayFromCell(75, 25, 100, 50, 8, 16)
		if !nt3dmath.IsFinite(float64(rr.Direction.X)) {
			t.Fatal("invalid ray")
		}
	}
}

func TestOrthoDistanceAndCellAspect(t *testing.T) {
	c := DefaultCamera()
	c.Alpha = 0
	c.Beta = 0
	a := c.RayFromCell(4, 2, 5, 5, 8, 16)
	b := c.RayFromCell(4, 2, 5, 5, 16, 16)
	if math.Abs(float64(b.Origin.Sub(c.Target).Y)) < math.Abs(float64(a.Origin.Sub(c.Target).Y))*1.9 {
		t.Fatal("cell aspect ignored")
	}
	c.Distance *= 2
	d := c.RayFromCell(4, 2, 5, 5, 8, 16)
	if abs(d.Origin.Y-2*a.Origin.Y) > .001 {
		t.Fatal("ortho scale ignored")
	}
}

func TestSoftwareTriangle(t *testing.T) {
	c := DefaultCamera()
	g := Geometry{Vertices: []Vertex{{nt3dmath.Vec3{X: -1, Y: -1, Z: 0}, nt3dmath.Vec3{Z: 1}, color.RGBA{255, 0, 0, 255}}, {nt3dmath.Vec3{X: 1, Y: -1, Z: 0}, nt3dmath.Vec3{Z: 1}, color.RGBA{255, 0, 0, 255}}, {nt3dmath.Vec3{X: 0, Y: 1, Z: 0}, nt3dmath.Vec3{Z: 1}, color.RGBA{255, 0, 0, 255}}}, Indices: []uint32{0, 1, 2}}
	img := softwareRender(Frame{Width: 100, Height: 100, Matrix: c.Matrix(1), Geometry: []Geometry{g}, Light: Light{nt3dmath.Vec3{Z: 1}, .3}})
	n := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r > 0 {
				n++
			}
		}
	}
	if n < 30 {
		t.Fatalf("triangle invisible: %d", n)
	}
}
