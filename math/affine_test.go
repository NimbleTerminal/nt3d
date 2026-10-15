package nt3dmath

import (
	"math"
	"testing"
)

func TestAffineInverseAndBounds(t *testing.T) {
	m := Translation(Vec3{X: 3, Y: -2}).Mul(RotationZ(.7)).Mul(Scaling(Vec3{X: 2, Y: 3, Z: .5}))
	inv, ok := m.InverseAffine()
	if !ok {
		t.Fatal("inverse")
	}
	product := m.Mul(inv)
	for i, v := range Identity() {
		if math.Abs(float64(product[i]-v)) > 1e-5 {
			t.Fatal(product)
		}
	}
	var b AABB
	b.Include(Vec3{X: -1, Y: -2, Z: -3})
	b.Include(Vec3{X: 1, Y: 2, Z: 3})
	out := b.Transformed(m)
	for _, p := range []Vec3{b.Min, b.Max} {
		v := m.Point(p)
		if v.X < out.Min.X || v.X > out.Max.X || v.Y < out.Min.Y || v.Y > out.Max.Y || v.Z < out.Min.Z || v.Z > out.Max.Z {
			t.Fatal(v, out)
		}
	}
	for _, bad := range []Mat4{{}, {15: 1, 0: float32(math.Inf(1))}} {
		if _, ok := bad.InverseAffine(); ok {
			t.Fatal("invalid inverse")
		}
	}
}

// A non-finite point leaves bounds as they were, rather than carrying NaN
// into every later one.
func TestAABBIgnoresNonFinite(t *testing.T) {
	var b AABB
	nan := float32(math.NaN())
	b.Include(Vec3{Z: nan})
	if b.IsValid() {
		t.Fatal("a NaN point made the bounds valid")
	}
	b.Include(Vec3{X: 1, Y: 2, Z: 3})
	b.Include(Vec3{X: nan})
	b.Include(Vec3{Y: float32(math.Inf(-1))})
	b.Include(Vec3{X: -1})
	if b.Min != (Vec3{X: -1}) || b.Max != (Vec3{X: 1, Y: 2, Z: 3}) {
		t.Fatalf("bounds %+v", b)
	}
}
