package nt3d

import (
	"math"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
	"testing"
)

func shGaussianGeometry(degree int) Geometry {
	g := gaussianGeometry()
	g.GaussianSH = make([]nt3dsh.RGB, nt3dsh.Count(degree))
	for i := range g.GaussianSH {
		g.GaussianSH[i] = nt3dsh.RGB{float32(i%3-1) * .1, float32(i%4-2) * .07, float32(i%5-2) * .08}
	}
	return g
}
func TestGaussianSHViewAndTransforms(t *testing.T) {
	g := shGaussianGeometry(1)
	g.GaussianSH[3] = nt3dsh.RGB{.8, 0, 0}
	f := flatFrame(80, 80, g)
	f.Forward = nt3dmath.Vec3{X: 1}
	identity := nt3dmath.Identity()
	a := f.gaussianColor(&g, 0, identity, identity)
	f.Forward.X = -1
	b := f.gaussianColor(&g, 0, identity, identity)
	if b[0]-a[0] < .7 {
		t.Fatal("SH did not respond to viewing direction")
	}
	g.PointStyle.SHDCOnly = true
	c := f.gaussianColor(&g, 0, identity, identity)
	f.Forward.X = 1
	d := f.gaussianColor(&g, 0, identity, identity)
	if c != d {
		t.Fatal("DC-only color changed with view")
	}
	g.PointStyle.SHDCOnly = false
	m := nt3dmath.RotationZ(.7).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: .5, Z: 1}))
	baked, err := TransformGeometry(g, m)
	check(t, err)
	inv, _ := m.InverseAffine()
	a = f.gaussianColor(&g, 0, m, inv)
	b = f.gaussianColor(&baked, 0, identity, gaussianSHMatrix(&baked))
	for k := range a {
		if math.Abs(float64(a[k]-b[k])) > 1e-5 {
			t.Fatalf("baked transform changed SH: %v %v", a, b)
		}
	}
}
func TestGaussianSHOwnershipClippingAndMerge(t *testing.T) {
	g := shGaussianGeometry(3)
	p := g.Points[0]
	p.Position.X = .5
	g.Points = append(g.Points, p)
	other := append([]nt3dsh.RGB{}, g.GaussianSH...)
	other[2][1] = .7
	g.GaussianSH = append(g.GaussianSH, other...)
	s := NewScene()
	id, err := s.SetGeometry("SH", g)
	check(t, err)
	first := frame(t, s)
	g.GaussianSH[0][0] = 99
	if first.Geometry[0].GaussianSH[0][0] == 99 {
		t.Fatal("scene retained caller coefficients")
	}
	var bounds nt3dmath.AABB
	bounds.Include(nt3dmath.Vec3{X: .4, Y: -1, Z: 0})
	bounds.Include(nt3dmath.Vec3{X: 1, Y: 1, Z: 1})
	clipped := clipGeometry(first.Geometry[0], bounds)
	check(t, clipped.Validate())
	if len(clipped.Points) != 1 || clipped.GaussianSH[2][1] != .7 {
		t.Fatal("clipping lost coefficient alignment")
	}
	merged, err := MergeGeometry(clipped, clipped)
	check(t, err)
	if len(merged.GaussianSH) != 32 {
		t.Fatal("merge lost SH")
	}
	check(t, s.SetNodePoints(id, g.Points, GeometryBounds(g)))
	updated := frame(t, s)
	check(t, validateFrame(updated))
	if updated.Geometry[0].GaussianSH[0][0] != first.Geometry[0].GaussianSH[0][0] {
		t.Fatal("point update lost SH")
	}
	bad := clipped
	bad.GaussianSH = bad.GaussianSH[:15]
	if bad.Validate() == nil {
		t.Fatal("accepted incomplete coefficient set")
	}
}
