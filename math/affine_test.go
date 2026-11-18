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

func TestRotationXYRightHanded(t *testing.T) {
	rx := RotationX(math.Pi / 2).Direction(Vec3{Y: 1})
	if math.Abs(float64(rx.Z-1)) > 1e-6 || math.Abs(float64(rx.X)) > 1e-6 || math.Abs(float64(rx.Y)) > 1e-6 {
		t.Fatalf("RotationX(pi/2)(+Y) = %v, want +Z", rx)
	}
	ry := RotationY(math.Pi / 2).Direction(Vec3{Z: 1})
	if math.Abs(float64(ry.X-1)) > 1e-6 || math.Abs(float64(ry.Y)) > 1e-6 || math.Abs(float64(ry.Z)) > 1e-6 {
		t.Fatalf("RotationY(pi/2)(+Z) = %v, want +X", ry)
	}
}

// Inverse undoes any invertible matrix, a perspective projection included,
// and refuses a singular one.
func TestMat4Inverse(t *testing.T) {
	perspective := Mat4{1.2, 0, 0, 0, 0, 1.6, 0, 0, 0, 0, -1.002, -1, 0, 0, -0.2002, 0}
	affine := Translation(Vec3{X: 3, Y: -2, Z: 5}).Mul(RotationZ(.7)).Mul(Scaling(Vec3{X: 2, Y: .5, Z: 3}))
	for name, m := range map[string]Mat4{"perspective": perspective, "affine": affine, "identity": Identity()} {
		inv, ok := m.Inverse()
		if !ok {
			t.Fatalf("%s: no inverse", name)
		}
		product, want := m.Mul(inv), Identity()
		for i := range product {
			if d := product[i] - want[i]; d > 1e-5 || d < -1e-5 {
				t.Fatalf("%s: m times its inverse is %v", name, product)
			}
		}
	}
	// The general inverse agrees with the affine one where both apply.
	general, _ := affine.Inverse()
	special, ok := affine.InverseAffine()
	if !ok {
		t.Fatal("no affine inverse")
	}
	for i := range general {
		if d := general[i] - special[i]; d > 1e-5 || d < -1e-5 {
			t.Fatalf("general inverse %v, affine inverse %v", general, special)
		}
	}
	for name, m := range map[string]Mat4{"zero": {}, "flattened": Scaling(Vec3{X: 1, Y: 1}), "not finite": {float32(math.NaN()), 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}} {
		if inv, ok := m.Inverse(); ok || inv != (Mat4{}) {
			t.Errorf("%s: inverse %v, %v; want the zero matrix and false", name, inv, ok)
		}
	}
}
