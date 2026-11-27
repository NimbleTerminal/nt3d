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

// A box carried through a transform that stretches it askew has half axes
// that are no longer perpendicular. Its distance is still to its nearest
// point: zero for a point inside, and for one outside the least over the
// whole box, never more.
func TestOBBdDistanceHoldsForASkewedBox(t *testing.T) {
	// A square turned an eighth and stretched four times along X: a long
	// diamond with corners at (±283, 0) and (0, ±71).
	skew := TRSd(Vec3d{}, Quatd{W: 1}, Vec3d{4, 1, 1}).Mul(TRSd(Vec3d{}, QuatdAxisAngle(Vec3d{0, 0, 1}, math.Pi/4), Vec3d{1, 1, 1}))
	b := OBBd{Center: Vec3d{0, 0, 5}, Axes: [3]Vec3d{{X: 50}, {Y: 50}, {Z: 5}}}.Transformed(skew)
	if got := b.Distance(Vec3d{200, 10, 5}); got != 0 {
		t.Errorf("a point inside the skewed box is %g from it, want 0", got)
	}
	if got := b.Nearest(Vec3d{200, 10, 5}); got != (Vec3d{200, 10, 5}) {
		t.Errorf("the nearest point to one inside is %v, want the point", got)
	}
	// Outside: against the least distance to a fine sampling of the box.
	for _, p := range []Vec3d{{300, 0, 5}, {200, 60, 5}, {0, 100, 40}, {-150, -80, -20}, {10, 0, 30}, {290, 3, 12}} {
		least := math.Inf(1)
		const n = 80
		for i := 0; i <= n; i++ {
			for j := 0; j <= n; j++ {
				for k := 0; k <= n; k += n / 8 {
					u, v, w := float64(2*i)/n-1, float64(2*j)/n-1, float64(2*k)/n-1
					q := b.Center.Add(b.Axes[0].Scale(u)).Add(b.Axes[1].Scale(v)).Add(b.Axes[2].Scale(w))
					least = math.Min(least, q.Dist(p))
				}
			}
		}
		got := b.Distance(p)
		if got > least+1e-9 || got < least-3 {
			t.Errorf("point %v: distance %g, and the nearest sample is %g away", p, got, least)
		}
	}
}

// A skewed box with a long, thin face still measures a point just off that
// face to well under the gap: the foot of the point is found along the
// face's normal, not by solving for it in the face's nearly parallel edges.
func TestOBBdDistanceHoldsOffALongThinFace(t *testing.T) {
	b := OBBd{Center: Vec3d{0, 0, -1}, Axes: [3]Vec3d{{1000, .01, 0}, {1000, -.01, 0}, {0, 0, 1}}}
	// One micrometer above the top face, over its middle and toward an end.
	for _, p := range []Vec3d{{0, 0, 1e-6}, {700, .002, 1e-6}, {-1500, 0, 1e-6}} {
		if got := b.Distance(p); math.Abs(got-1e-6) > 1e-12 {
			t.Errorf("point %v: distance %g, want 1e-06", p, got)
		}
	}
}
