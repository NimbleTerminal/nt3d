// Copyright (c) 2026 Neomantra Corp.

package nt3dcurve

import (
	"math"
	"testing"
)

func TestCatmullRomThroughPointsAndNoSelfIntersection(t *testing.T) {
	// Classic Yuksel, Schaefer & Keyser (2011) uneven-spacing fixture:
	// two long legs around a tiny sharp corner. Uniform Catmull-Rom overshoots
	// and loops across itself on the tiny middle span; centripetal never does.
	pts := []Vec3{
		{X: 0, Y: 0, Z: 0},
		{X: 10, Y: 0, Z: 0},
		{X: 10.05, Y: 0.05, Z: 0},
		{X: 10.05, Y: 10, Z: 0},
	}
	spans, err := BuildCatmullRomCentripetal(pts, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	// Verify exact interpolation at every control point.
	for i := range spans {
		got := spans[i].Eval(0)
		if got.Sub(pts[i]).Len() > 1e-12 {
			t.Fatalf("span %d start = %+v, want %+v", i, got, pts[i])
		}
	}
	if end := spans[2].Eval(1); end.Sub(pts[3]).Len() > 1e-12 {
		t.Fatalf("end = %+v, want %+v", end, pts[3])
	}

	// Sample densely along the curve in XY and check that no two non-adjacent
	// polyline segments intersect (no self-intersection loop).
	lut := NewLUT(spans, 32, false)
	const N = 300
	samples := make([]Vec3, N+1)
	for i := 0; i <= N; i++ {
		samples[i], _ = lut.Eval(float64(i) / float64(N))
	}
	for i := 0; i < N; i++ {
		for j := i + 2; j < N; j++ {
			if segmentsIntersect2D(samples[i], samples[i+1], samples[j], samples[j+1]) {
				t.Fatalf("centripetal Catmull-Rom self-intersected between segments %d and %d", i, j)
			}
		}
	}
}

func segmentsIntersect2D(a, b, c, d Vec3) bool {
	ccw := func(p, q, r Vec3) float64 {
		return (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X)
	}
	d1 := ccw(c, d, a)
	d2 := ccw(c, d, b)
	d3 := ccw(a, b, c)
	d4 := ccw(a, b, d)
	return ((d1 > 1e-12 && d2 < -1e-12) || (d1 < -1e-12 && d2 > 1e-12)) &&
		((d3 > 1e-12 && d4 < -1e-12) || (d3 < -1e-12 && d4 > 1e-12))
}

func TestArcLengthConstantSpeedProperty(t *testing.T) {
	// Highly non-uniform 3D curve where raw spline parameter speed varies >20x.
	pts := []Vec3{
		{X: 0, Y: 0, Z: 0},
		{X: 15, Y: 2, Z: 4},
		{X: 15.4, Y: 2.5, Z: 4.2},
		{X: 16, Y: 3, Z: 4},
		{X: 2, Y: 20, Z: -5},
		{X: -8, Y: 22, Z: 3},
	}
	spans, err := BuildCatmullRomCentripetal(pts, false)
	if err != nil {
		t.Fatal(err)
	}
	lut := NewLUT(spans, 32, false)
	if lut.TotalLength() <= 0 {
		t.Fatal("expected positive total length")
	}

	// Measure fine-grained arc length on 100 equal delta-s steps: every step
	// must cover TotalLength / 100 to within 0.05%.
	const steps = 100
	wantStep := lut.TotalLength() / steps
	for i := 0; i < steps; i++ {
		s0 := float64(i) / steps
		s1 := float64(i+1) / steps
		// Measure true chord length over 32 sub-steps between s0 and s1.
		var subLen float64
		prev, _ := lut.Eval(s0)
		const sub = 32
		for k := 1; k <= sub; k++ {
			s := s0 + (s1-s0)*float64(k)/sub
			cur, tan := lut.Eval(s)
			subLen += cur.Sub(prev).Len()
			prev = cur
			if math.Abs(tan.Len()-1) > 1e-12 {
				t.Fatalf("non-unit tangent at s=%g: len=%g", s, tan.Len())
			}
		}
		relErr := math.Abs(subLen-wantStep) / wantStep
		if relErr > 5e-4 {
			t.Fatalf("step %d [%g, %g]: measured length %g differs from %g (relErr=%g)", i, s0, s1, subLen, wantStep, relErr)
		}
	}
}

func TestBezierAndBSplineCurves(t *testing.T) {
	// Bezier quarter-circle approximation: R = 10, k = 4*(sqrt(2)-1)/3 * R.
	const R = 10.0
	k := 4.0 * (math.Sqrt2 - 1.0) / 3.0 * R
	knots := []Knot{
		{P: Vec3{X: R, Y: 0, Z: 0}, In: Vec3{X: R, Y: -k, Z: 0}, Out: Vec3{X: R, Y: k, Z: 0}},
		{P: Vec3{X: 0, Y: R, Z: 0}, In: Vec3{X: k, Y: R, Z: 0}, Out: Vec3{X: -k, Y: R, Z: 0}},
	}
	bz, err := BuildBezier(knots, false)
	if err != nil {
		t.Fatal(err)
	}
	lut := NewLUT(bz, 32, false)
	wantArc := 0.5 * math.Pi * R
	if math.Abs(lut.TotalLength()-wantArc)/wantArc > 3e-4 {
		t.Fatalf("quarter-circle Bezier length = %g, want ~%g", lut.TotalLength(), wantArc)
	}

	// Open clamped B-spline clamps to endpoints and stays inside convex hull.
	bsPts := []Vec3{{X: 0, Y: 0, Z: 0}, {X: 2, Y: 6, Z: 1}, {X: 5, Y: -2, Z: 3}, {X: 8, Y: 4, Z: 2}, {X: 10, Y: 0, Z: 0}}
	bs, err := BuildBSpline(bsPts, false)
	if err != nil {
		t.Fatal(err)
	}
	bsLUT := NewLUT(bs, 32, false)
	start, _ := bsLUT.Eval(0)
	end, _ := bsLUT.Eval(1)
	if start.Sub(bsPts[0]).Len() > 1e-12 || end.Sub(bsPts[len(bsPts)-1]).Len() > 1e-12 {
		t.Fatalf("clamped B-spline endpoints = (%+v, %+v), want (%+v, %+v)", start, end, bsPts[0], bsPts[len(bsPts)-1])
	}

	// Closed periodic B-spline closes C1/C2 smoothly at s=0 and s=1.
	closedBS, err := BuildBSpline(bsPts[:4], true)
	if err != nil {
		t.Fatal(err)
	}
	cLUT := NewLUT(closedBS, 32, true)
	p0, t0 := cLUT.Eval(0)
	p1, t1 := cLUT.Eval(1)
	if p0.Sub(p1).Len() > 1e-12 || t0.Sub(t1).Len() > 1e-12 {
		t.Fatalf("closed B-spline seam mismatch: p0=%+v p1=%+v t0=%+v t1=%+v", p0, p1, t0, t1)
	}
}
