// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import (
	"math"
	"testing"
)

func nearVec(a, b Vec3, eps float32) bool {
	d := a.Sub(b)
	return d.Dot(d) <= eps*eps
}

func nearVecd(a, b Vec3d, eps float64) bool { return a.Dist(b) <= eps }

// aboutZ is a rotation by radians about +Z.
func aboutZ(radians float64) Quat {
	return Quat{Z: float32(math.Sin(radians / 2)), W: float32(math.Cos(radians / 2))}
}

// TRS scales, then rotates, then translates.
func TestTRS(t *testing.T) {
	m := TRS(Vec3{X: 1, Y: 2, Z: 3}, aboutZ(math.Pi/2), Vec3{X: 2, Y: 2, Z: 2})
	if got, want := m.Point(Vec3{X: 1}), (Vec3{X: 1, Y: 4, Z: 3}); !nearVec(got, want, 1e-5) {
		t.Fatalf("+X scaled by 2, turned a quarter about Z, moved by (1,2,3) is %v, want %v", got, want)
	}
	// A rotation that is not unit length is normalized, and none at all is the identity.
	long := aboutZ(math.Pi / 2)
	long = Quat{long.X * 3, long.Y * 3, long.Z * 3, long.W * 3}
	if TRS(Vec3{}, long, Vec3{X: 1, Y: 1, Z: 1}) != TRS(Vec3{}, aboutZ(math.Pi/2), Vec3{X: 1, Y: 1, Z: 1}) {
		t.Fatal("a scaled quaternion gave another rotation")
	}
	if TRS(Vec3{}, Quat{}, Vec3{X: 1, Y: 1, Z: 1}) != Identity() {
		t.Fatal("the zero quaternion is not the identity rotation")
	}
}

// The float64 composition agrees with the float32 one.
func TestTRSdAgreesWithTRS(t *testing.T) {
	tr, q, s := Vec3{X: -4, Y: .5, Z: 9}, Quat{X: .1, Y: -.7, Z: .3, W: .6}.Normalize(), Vec3{X: 2, Y: .5, Z: 3}
	single, double := TRS(tr, q, s), TRSd(tr.D(), q.D(), s.D()).Mat4()
	for i := range single {
		if d := single[i] - double[i]; d > 1e-5 || d < -1e-5 {
			t.Fatalf("element %d: float32 %v, float64 %v", i, single[i], double[i])
		}
	}
}

func TestQuatSlerp(t *testing.T) {
	a, b := aboutZ(0), aboutZ(math.Pi/2)
	if got := a.Slerp(b, 0); got != a {
		t.Fatalf("slerp at 0 is %v, want %v", got, a)
	}
	end := a.Slerp(b, 1)
	if d := end.Dot(b); d < .99999 {
		t.Fatalf("slerp at 1 is %v, want %v", end, b)
	}
	mid, want := a.Slerp(b, .5), aboutZ(math.Pi/4)
	if d := mid.Dot(want); d < .99999 {
		t.Fatalf("halfway from 0 to a quarter turn is %v, want an eighth turn %v", mid, want)
	}
	// A quaternion and its negation are one rotation: the arc taken is the shorter.
	neg := Quat{-b.X, -b.Y, -b.Z, -b.W}
	if viaNeg := a.Slerp(neg, .5); viaNeg.Dot(want) < .99999 {
		t.Fatalf("slerp toward the negated quaternion took the long way: %v", viaNeg)
	}
	// Rotations nearly alike do not divide by a vanishing sine.
	close := a.Slerp(aboutZ(1e-5), .5)
	if math.IsNaN(float64(close.W)) || math.Abs(float64(close.Dot(close))-1) > 1e-5 {
		t.Fatalf("slerp between nearly equal rotations is %v", close)
	}
}

// Rotation recovers a transform's rotation whatever scale it carries.
func TestMat4Rotation(t *testing.T) {
	for _, q := range []Quat{aboutZ(.7), Quat{X: .1, Y: -.7, Z: .3, W: .6}.Normalize(), Quat{X: 1}, Quat{Y: 1}, Quat{Z: 1}, QuatIdentity()} {
		got := TRS(Vec3{X: 5}, q, Vec3{X: 2, Y: .5, Z: 3}).Rotation()
		if d := got.Dot(q); d < .9999 && d > -.9999 {
			t.Errorf("rotation of a scaled transform by %v is %v", q, got)
		}
	}
}

func TestMat4d(t *testing.T) {
	a := Translation(Vec3{X: 3, Y: -2, Z: 5}).Mul(RotationZ(.7)).Mul(Scaling(Vec3{X: 2, Y: .5, Z: 3}))
	b := RotationX(-.3).Mul(Translation(Vec3{X: 1, Y: 1, Z: 1}))
	single, double := a.Mul(b), a.D().Mul(b.D()).Mat4()
	for i := range single {
		if d := single[i] - double[i]; d > 1e-5 || d < -1e-5 {
			t.Fatalf("element %d of the product: float32 %v, float64 %v", i, single[i], double[i])
		}
	}
	p := Vec3{X: .5, Y: -1.5, Z: 2}
	if got, want := a.D().Point(p.D()).Vec3(), a.Point(p); !nearVec(got, want, 1e-5) {
		t.Fatalf("point: float64 %v, float32 %v", got, want)
	}
	if got, want := a.D().Direction(p.D()).Vec3(), a.Direction(p); !nearVec(got, want, 1e-5) {
		t.Fatalf("direction: float64 %v, float32 %v", got, want)
	}
	if got, want := a.D().Determinant3(), a.Determinant3(); math.Abs(got-want) > 1e-5 {
		t.Fatalf("determinant: float64 %v, float32 %v", got, want)
	}
	if IdentityD().Mat4() != Identity() || !IdentityD().IsFinite() {
		t.Fatal("the float64 identity is not the identity")
	}
	bad := IdentityD()
	bad[5] = math.Inf(1)
	if bad.IsFinite() {
		t.Fatal("an infinite element passed as finite")
	}
	// Float64 keeps what float32 loses: a small step at a geographic offset.
	far := IdentityD()
	far[12] = 4_000_000
	if got := far.Point(Vec3d{X: .001}).X - 4_000_000; math.Abs(got-.001) > 1e-9 {
		t.Fatalf("a millimeter at four thousand kilometers came back as %v", got)
	}
}

// Quatd turns vectors, composes, and round-trips through a basis.
func TestQuatd(t *testing.T) {
	quarter := QuatdAxisAngle(Vec3d{Z: 1}, math.Pi/2)
	if got, want := quarter.Rotate(Vec3d{X: 1}), (Vec3d{Y: 1}); !nearVecd(got, want, 1e-12) {
		t.Fatalf("+X turned a quarter about Z is %v, want %v", got, want)
	}
	if got, want := quarter.Mul(quarter).Rotate(Vec3d{X: 1}), (Vec3d{X: -1}); !nearVecd(got, want, 1e-12) {
		t.Fatalf("two quarter turns take +X to %v, want %v", got, want)
	}
	if got := quarter.Mul(quarter.Conj()); !nearVecd(Vec3d{X: got.X, Y: got.Y, Z: got.Z}, Vec3d{}, 1e-12) || math.Abs(got.W-1) > 1e-12 {
		t.Fatalf("a rotation times its conjugate is %v", got)
	}
	look := QuatdLookRotation(Vec3d{X: 1}, Vec3d{Z: 1})
	right, up, forward := look.Basis()
	if !nearVecd(forward, Vec3d{X: 1}, 1e-12) || !nearVecd(up, Vec3d{Z: 1}, 1e-12) || !nearVecd(right, Vec3d{Y: -1}, 1e-12) {
		t.Fatalf("looking along +X with +Z up: right %v, up %v, forward %v", right, up, forward)
	}
	if back := QuatdFromBasis(right, up, forward); math.Abs(back.Dot(look)) < 1-1e-12 {
		t.Fatalf("the basis gives back %v, not %v", back, look)
	}
	if mid := QuatdIdentity().Slerp(quarter, .5); math.Abs(mid.Dot(QuatdAxisAngle(Vec3d{Z: 1}, math.Pi/4))) < 1-1e-12 {
		t.Fatalf("halfway to a quarter turn is %v", mid)
	}
	if (Quatd{}).Normalize() != QuatdIdentity() {
		t.Fatal("the zero quaternion does not normalize to the identity")
	}
	if got, want := quarter.Quat().D(), quarter; math.Abs(got.Dot(want)) < 1-1e-6 {
		t.Fatalf("to float32 and back: %v", got)
	}
}

func TestVec3d(t *testing.T) {
	a, b := Vec3d{X: 3, Y: 4}, Vec3d{Z: 2}
	if a.Len() != 5 || a.Dist(b) != math.Sqrt(29) || a.Dot(b) != 0 {
		t.Fatalf("length %v, distance %v, dot %v", a.Len(), a.Dist(b), a.Dot(b))
	}
	if got := (Vec3d{X: 1}).Cross(Vec3d{Y: 1}); got != (Vec3d{Z: 1}) {
		t.Fatalf("X cross Y is %v", got)
	}
	if got := a.Lerp(b, .5); got != (Vec3d{X: 1.5, Y: 2, Z: 1}) {
		t.Fatalf("halfway is %v", got)
	}
	if (Vec3d{}).Normalize() != (Vec3d{}) || !a.IsFinite() || (Vec3d{X: math.NaN()}).IsFinite() {
		t.Fatal("zero normalizes to something, or finiteness is wrong")
	}
	if got := (Vec3{X: 1.5, Y: -2, Z: .25}).D().Vec3(); got != (Vec3{X: 1.5, Y: -2, Z: .25}) {
		t.Fatalf("to float64 and back: %v", got)
	}
}
