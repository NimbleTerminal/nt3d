// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import (
	"math"
	"testing"
)

// The inverse of an affine float64 transform undoes it at Earth scale, where
// a float32 inverse would lose the translation, and a transform that cannot
// be undone is refused.
func TestMat4dInverseAffineUndoesAnEarthScaleTransform(t *testing.T) {
	m := TRSd(Vec3d{1364789.133621286, -4619824.799433342, 4166319.166594518}, QuatdAxisAngle(Vec3d{1, 2, 3}.Normalize(), 0.7), Vec3d{2, 3, 0.5})
	inv, ok := m.InverseAffine()
	if !ok {
		t.Fatal("an invertible transform was refused")
	}
	p := Vec3d{12.25, -7.5, 3.125}
	if d := inv.Point(m.Point(p)).Dist(p); d > 1e-9 {
		t.Fatalf("the inverse missed by %g", d)
	}
	flat := m
	flat[8], flat[9], flat[10] = 0, 0, 0
	projective := m
	projective[3] = 1
	nan := m
	nan[12] = math.NaN()
	for name, bad := range map[string]Mat4d{"singular": flat, "projective": projective, "not finite": nan} {
		if _, ok := bad.InverseAffine(); ok {
			t.Errorf("a %s transform was inverted", name)
		}
	}
}

// An oriented box measures distance to its nearest point: zero inside, the
// gap to a face outside, and the gap to a corner past one. A flat box, with
// a half axis of zero, still measures.
func TestOBBdDistanceIsToTheNearestPointOfTheBox(t *testing.T) {
	turn := TRSd(Vec3d{100, 0, 0}, QuatdAxisAngle(Vec3d{0, 0, 1}, math.Pi/2), Vec3d{1, 1, 1})
	b := OBBd{Axes: [3]Vec3d{{X: 2}, {Y: 1}, {Z: .5}}}.Transformed(turn)
	// After the quarter turn about Z the box reaches 1 along X and 2 along Y.
	for _, c := range []struct {
		name string
		p    Vec3d
		want float64
	}{
		{"inside", Vec3d{100.5, 1.5, .25}, 0},
		{"past a face", Vec3d{104, 0, 0}, 3},
		{"past a corner", Vec3d{104, 6, .5}, 5},
	} {
		if got := b.Distance(c.p); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("%s: distance %g, want %g", c.name, got, c.want)
		}
	}
	flat := OBBd{Axes: [3]Vec3d{{X: 1}, {Y: 1}, {}}}
	if got := flat.Distance(Vec3d{0, 0, 3}); got != 3 {
		t.Errorf("flat box: distance %g, want 3", got)
	}
	if got, want := b.Radius(), math.Sqrt(4+1+.25); math.Abs(got-want) > 1e-12 {
		t.Errorf("radius %g, want %g", got, want)
	}
}
