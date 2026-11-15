// Copyright (c) 2026 Neomantra Corp.

package nt3dframes

import (
	"math"
	"testing"
	"time"

	nt3dcurve "nimbleterminal.dev/nt3d/cam/curve"
)

func TestFromBasisAndLookRotationRoundTrip(t *testing.T) {
	dirs := []nt3dcurve.Vec3{
		{X: 0, Y: 1, Z: 0},
		{X: 1, Y: 0, Z: 0},
		{X: 0, Y: 0, Z: 1},  // straight up (collinear with +Z worldUp!)
		{X: 0, Y: 0, Z: -1}, // straight down (collinear with +Z worldUp!)
		{X: 1, Y: 2, Z: -0.5},
		{X: -3, Y: -1, Z: 4},
	}
	worldUp := nt3dcurve.Vec3{Z: 1}
	for _, d := range dirs {
		q := LookRotation(d, worldUp)
		if math.Abs(q.Norm()-1) > 1e-14 {
			t.Fatalf("non-unit quaternion norm %g for dir %+v", q.Norm(), d)
		}
		r, u, f := q.Basis()
		wantF := d.Normalize()
		if f.Sub(wantF).Len() > 1e-12 {
			t.Fatalf("forward mismatch: got %+v, want %+v", f, wantF)
		}
		if math.Abs(r.Dot(u)) > 1e-12 || math.Abs(r.Dot(f)) > 1e-12 || math.Abs(u.Dot(f)) > 1e-12 {
			t.Fatalf("non-orthogonal basis for %+v", d)
		}
		// Verify right-handed camera basis: R × U == -F (local +Z).
		if r.Cross(u).Sub(f.Scale(-1)).Len() > 1e-12 {
			t.Fatalf("left-handed basis for %+v", d)
		}
	}
}

func TestParallelTransportNoFlipOnSCurveAndHelix(t *testing.T) {
	// 1. Planar S-curve in XY (inflection point in the middle where Frenet flips 180 deg).
	sPts := []nt3dcurve.Vec3{
		{X: 0, Y: 0, Z: 2},
		{X: 3, Y: 4, Z: 2},
		{X: 6, Y: 4, Z: 2},
		{X: 9, Y: 0, Z: 2},
		{X: 12, Y: 0, Z: 2},
		{X: 15, Y: 4, Z: 2},
	}
	spans, err := nt3dcurve.BuildCatmullRomCentripetal(sPts, false)
	if err != nil {
		t.Fatal(err)
	}
	lut := nt3dcurve.NewLUT(spans, 32, false)
	pt := NewParallelTransport(lut, nt3dcurve.Vec3{Z: 1})

	var prevR, prevU, prevF nt3dcurve.Vec3
	const N = 200
	for i := 0; i <= N; i++ {
		s := float64(i) / N
		q := pt.Eval(s, false)
		if math.Abs(q.Norm()-1) > 1e-13 {
			t.Fatalf("S-curve s=%g: non-unit quat norm=%g", s, q.Norm())
		}
		r, u, f := q.Basis()
		if math.Abs(u.Z-1) > 1e-12 {
			t.Fatalf("planar XY S-curve must preserve +Z up everywhere, got %+v at s=%g", u, s)
		}
		if i > 0 {
			if u.Dot(prevU) < 0.99 || r.Dot(prevR) < 0.95 || f.Dot(prevF) < 0.90 {
				t.Fatalf("frame discontinuity at s=%g", s)
			}
		}
		prevR, prevU, prevF = r, u, f
	}

	// 2. 3D Helix with vertical climb and continuous torsion.
	helixPts := make([]nt3dcurve.Vec3, 25)
	for i := range helixPts {
		ang := float64(i) * (2 * math.Pi / 8)
		helixPts[i] = nt3dcurve.Vec3{
			X: 5 * math.Cos(ang),
			Y: 5 * math.Sin(ang),
			Z: float64(i) * 0.8,
		}
	}
	hSpans, err := nt3dcurve.BuildCatmullRomCentripetal(helixPts, false)
	if err != nil {
		t.Fatal(err)
	}
	hLUT := nt3dcurve.NewLUT(hSpans, 32, false)
	hPT := NewParallelTransport(hLUT, nt3dcurve.Vec3{Z: 1})

	for i := 0; i <= 400; i++ {
		s := float64(i) / 400.0
		q := hPT.Eval(s, false)
		if math.Abs(q.Norm()-1) > 1e-13 {
			t.Fatalf("helix s=%g: non-unit quat norm=%g", s, q.Norm())
		}
		r, u, f := q.Basis()
		if math.Abs(r.Dot(u)) > 1e-12 || math.Abs(u.Dot(f)) > 1e-12 {
			t.Fatalf("helix s=%g: non-orthogonal frame", s)
		}
		if i > 0 && u.Dot(prevU) < 0.98 {
			t.Fatalf("helix frame jump at s=%g: dot=%g", s, u.Dot(prevU))
		}
		prevU = u
	}
}

func TestQuatTrackSquadSmoothnessAndUnitNorm(t *testing.T) {
	keys := []Key{
		{T: 0, Q: AxisAngle(nt3dcurve.Vec3{Z: 1}, 0)},
		{T: time.Second, Q: AxisAngle(nt3dcurve.Vec3{Z: 1}, 0.7)},
		{T: 2 * time.Second, Q: AxisAngle(nt3dcurve.Vec3{X: 1}, 0.5).Mul(AxisAngle(nt3dcurve.Vec3{Z: 1}, 1.2))},
		{T: 3 * time.Second, Q: AxisAngle(nt3dcurve.Vec3{Y: 1}, -0.4).Mul(AxisAngle(nt3dcurve.Vec3{Z: 1}, 1.6))},
	}
	squadTrack, err := NewTrack(keys, true)
	if err != nil {
		t.Fatal(err)
	}
	// Check exact keyframe pass-through (up to double-cover sign) and unit length everywhere.
	for _, k := range keys {
		got := squadTrack.Eval(k.T)
		if math.Abs(math.Abs(got.Dot(k.Q))-1) > 1e-12 {
			t.Fatalf("squad missed keyframe at %v: got %+v, want %+v", k.T, got, k.Q)
		}
	}
	// Check C1 angular velocity continuity across t = 1s interior keyframe.
	dt := time.Microsecond * 100
	qBefore := squadTrack.Eval(time.Second - dt)
	qAt := squadTrack.Eval(time.Second)
	qAfter := squadTrack.Eval(time.Second + dt)
	wLeft := quatLog(qBefore.Conj().Mul(qAt))
	wRight := quatLog(qAt.Conj().Mul(qAfter))
	if wLeft.Sub(wRight).Len() > 1e-4 {
		t.Fatalf("squad angular velocity discontinuous across keyframe: left=%+v right=%+v", wLeft, wRight)
	}
}
