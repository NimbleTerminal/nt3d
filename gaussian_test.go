package nt3d

import (
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func gaussianGeometry() Geometry {
	return styled(PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}, Point{Position: nt3dmath.Vec3{Z: .5}, Radius: 1, Color: color.RGBA{240, 90, 20, 220}, GaussianAxes: [3]nt3dmath.Vec3{{X: .18}, {Y: .05}, {Z: .03}}})
}
func TestGaussianProjection(t *testing.T) {
	p := gaussianGeometry().Points[0]
	m := nt3dmath.Identity()
	e := projectGaussian(p, &m, 100, 100, 1)
	if math.Abs(float64(e.a*e.a-81.3)) > 1e-4 || math.Abs(float64(e.c*e.c-6.55)) > 1e-4 || e.b != 0 {
		t.Fatalf("covariance: %+v", e)
	}
	rotated := nt3dmath.RotationZ(math.Pi / 4)
	e = projectGaussian(p, &rotated, 100, 100, 1)
	if e.b >= 0 {
		t.Fatal("rotation must tilt down-screen covariance negative")
	}
	transformed, err := TransformGeometry(gaussianGeometry(), rotated)
	check(t, err)
	want := projectGaussian(transformed.Points[0], &m, 100, 100, 1)
	if e != want {
		t.Fatalf("baked and node transforms differ: %+v %+v", e, want)
	}
	e2 := projectGaussian(p, &m, 200, 200, 2)
	e1 := projectGaussian(p, &m, 100, 100, 1)
	if e2.a != 2*e1.a || e2.c != 2*e1.c {
		t.Fatal("supersampling changes covariance")
	}
	// A perspective Jacobian includes the depth-axis contribution off-axis.
	m[11] = 1
	m[15] = 1
	p.Position.X = .4
	e = projectGaussian(p, &m, 100, 100, 1)
	if e.a >= e1.a || e.a <= .18*50/1.5 {
		t.Fatalf("perspective covariance: %+v", e)
	}
}
func TestGaussianSoftwareAndSort(t *testing.T) {
	g := gaussianGeometry()
	p := g.Points[0]
	p.Position.Z = .7
	p.Color = color.RGBA{0, 0, 255, 180}
	g.Points = append(g.Points, p)
	f := flatFrame(80, 80, g)
	a := softwareRender(f)
	g.Points[0], g.Points[1] = g.Points[1], g.Points[0]
	f.Geometry[0] = g
	b := softwareRender(f)
	for y := range 80 {
		for x := range 80 {
			if at(a, x, y) != at(b, x, y) {
				t.Fatal("input order affected sorted blending")
			}
		}
	}
	if at(a, 46, 40) == at(a, 40, 46) {
		t.Fatal("anisotropy missing")
	}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	g.Points[0].GaussianAxes[0].X = float32(math.NaN())
	if g.Validate() == nil {
		t.Fatal("accepted nonfinite axis")
	}
}

func TestGaussianWireframeCenters(t *testing.T) {
	g := gaussianGeometry()
	f := flatFrame(80, 80, g)
	gaussian, err := RenderWireframe(f, 40, 20)
	check(t, err)
	g.PointStyle = PointStyle{}
	g.Points[0].Radius = 1
	f.Geometry[0] = g
	points, err := RenderWireframe(f, 40, 20)
	check(t, err)
	if gaussian != points {
		t.Fatal("Gaussian wireframe differs from point centers")
	}
}
