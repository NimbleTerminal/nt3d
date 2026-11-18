// Copyright (c) 2026 Neomantra Corp.

// Package nt3dcurve compiles 3D control points into piecewise cubic splines and
// precomputes arc-length lookup tables for O(log n) constant-speed evaluation.
package nt3dcurve

import (
	"fmt"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Vec3 is a float64 3D vector or position: nt3dmath.Vec3d, under the name
// this package has always used.
type Vec3 = nt3dmath.Vec3d

// Knot is a cubic Bezier anchor P with world-space handles In and Out.
type Knot struct {
	P   Vec3
	In  Vec3
	Out Vec3
}

// CubicSpan is a single polynomial span P(u) = ((C3*u + C2)*u + C1)*u + C0 for u in [0, 1].
type CubicSpan struct {
	C0, C1, C2, C3 Vec3
}

// Eval evaluates P(u) via Horner's rule.
func (s CubicSpan) Eval(u float64) Vec3 {
	return Vec3{
		X: ((s.C3.X*u+s.C2.X)*u+s.C1.X)*u + s.C0.X,
		Y: ((s.C3.Y*u+s.C2.Y)*u+s.C1.Y)*u + s.C0.Y,
		Z: ((s.C3.Z*u+s.C2.Z)*u+s.C1.Z)*u + s.C0.Z,
	}
}

// Deriv evaluates dP/du = (3*C3*u + 2*C2)*u + C1.
func (s CubicSpan) Deriv(u float64) Vec3 {
	return Vec3{
		X: (3*s.C3.X*u+2*s.C2.X)*u + s.C1.X,
		Y: (3*s.C3.Y*u+2*s.C2.Y)*u + s.C1.Y,
		Z: (3*s.C3.Z*u+2*s.C2.Z)*u + s.C1.Z,
	}
}

func hermiteSpan(p0, m0, p1, m1 Vec3) CubicSpan {
	d := p1.Sub(p0)
	return CubicSpan{
		C0: p0,
		C1: m0,
		C2: d.Scale(3).Sub(m0.Scale(2)).Sub(m1),
		C3: d.Scale(-2).Add(m0).Add(m1),
	}
}

func bezierSpan(b0, b1, b2, b3 Vec3) CubicSpan {
	return CubicSpan{
		C0: b0,
		C1: b1.Sub(b0).Scale(3),
		C2: b2.Sub(b1.Scale(2)).Add(b0).Scale(3),
		C3: b3.Sub(b2.Scale(3)).Add(b1.Scale(3)).Sub(b0),
	}
}

func validatePoints(pts []Vec3) error {
	if len(pts) == 0 {
		return fmt.Errorf("nt3dcurve: position track requires at least 1 point")
	}
	for i, p := range pts {
		if !p.IsFinite() {
			return fmt.Errorf("nt3dcurve: non-finite control point %d", i)
		}
	}
	return nil
}

// BuildLinear compiles a piecewise-linear polyline through pts.
func BuildLinear(pts []Vec3, closed bool) ([]CubicSpan, error) {
	if err := validatePoints(pts); err != nil {
		return nil, err
	}
	n := len(pts)
	if n == 1 {
		return []CubicSpan{{C0: pts[0]}}, nil
	}
	count := n - 1
	if closed {
		count = n
	}
	spans := make([]CubicSpan, count)
	for i := range count {
		p0 := pts[i]
		p1 := pts[(i+1)%n]
		spans[i] = CubicSpan{C0: p0, C1: p1.Sub(p0)}
	}
	return spans, nil
}

// BuildCatmullRomCentripetal compiles a centripetal (alpha = 0.5) Catmull-Rom
// spline through pts (Yuksel, Schaefer & Keyser, 2011).
func BuildCatmullRomCentripetal(pts []Vec3, closed bool) ([]CubicSpan, error) {
	if err := validatePoints(pts); err != nil {
		return nil, err
	}
	n := len(pts)
	if n == 1 {
		return []CubicSpan{{C0: pts[0]}}, nil
	}
	if n == 2 {
		return BuildLinear(pts, closed)
	}
	count := n - 1
	if closed {
		count = n
	}
	spans := make([]CubicSpan, count)
	for i := range count {
		var q0, q1, q2, q3 Vec3
		if closed {
			q0 = pts[(i-1+n)%n]
			q1 = pts[i]
			q2 = pts[(i+1)%n]
			q3 = pts[(i+2)%n]
		} else {
			q1 = pts[i]
			q2 = pts[i+1]
			if i == 0 {
				q0 = q1.Scale(2).Sub(q2)
			} else {
				q0 = pts[i-1]
			}
			if i+2 >= n {
				q3 = q2.Scale(2).Sub(q1)
			} else {
				q3 = pts[i+2]
			}
		}
		spans[i] = centripetalSpan(q0, q1, q2, q3)
	}
	return spans, nil
}

func centripetalSpan(q0, q1, q2, q3 Vec3) CubicSpan {
	dt1 := math.Sqrt(math.Sqrt(q2.Sub(q1).LenSq()))
	if dt1 < 1e-12 {
		return CubicSpan{C0: q1}
	}
	dt0 := math.Sqrt(math.Sqrt(q1.Sub(q0).LenSq()))
	if dt0 < 1e-12 {
		q0 = q1
		dt0 = dt1
	}
	dt2 := math.Sqrt(math.Sqrt(q3.Sub(q2).LenSq()))
	if dt2 < 1e-12 {
		q3 = q2
		dt2 = dt1
	}
	d10 := q1.Sub(q0).Scale(1.0 / dt0)
	d20 := q2.Sub(q0).Scale(1.0 / (dt0 + dt1))
	d21 := q2.Sub(q1).Scale(1.0 / dt1)
	d31 := q3.Sub(q1).Scale(1.0 / (dt1 + dt2))
	d32 := q3.Sub(q2).Scale(1.0 / dt2)

	m1 := d10.Sub(d20).Add(d21).Scale(dt1)
	m2 := d21.Sub(d31).Add(d32).Scale(dt1)
	return hermiteSpan(q1, m1, q2, m2)
}

// BuildBezier compiles piecewise cubic Bezier spans from explicit handle knots.
func BuildBezier(knots []Knot, closed bool) ([]CubicSpan, error) {
	if len(knots) == 0 {
		return nil, fmt.Errorf("nt3dcurve: bezier track requires at least 1 knot")
	}
	for i, k := range knots {
		if !k.P.IsFinite() || !k.In.IsFinite() || !k.Out.IsFinite() {
			return nil, fmt.Errorf("nt3dcurve: non-finite bezier knot %d", i)
		}
	}
	n := len(knots)
	if n == 1 {
		return []CubicSpan{{C0: knots[0].P}}, nil
	}
	count := n - 1
	if closed {
		count = n
	}
	spans := make([]CubicSpan, count)
	for i := range count {
		k0 := knots[i]
		k1 := knots[(i+1)%n]
		spans[i] = bezierSpan(k0.P, k0.Out, k1.In, k1.P)
	}
	return spans, nil
}

// BuildBSpline compiles an approximating uniform cubic B-spline (clamped to
// endpoints when open, or C2-periodic when closed).
func BuildBSpline(pts []Vec3, closed bool) ([]CubicSpan, error) {
	if err := validatePoints(pts); err != nil {
		return nil, err
	}
	n := len(pts)
	if n == 1 {
		return []CubicSpan{{C0: pts[0]}}, nil
	}
	if n == 2 {
		return BuildLinear(pts, closed)
	}
	if closed {
		spans := make([]CubicSpan, n)
		for i := range n {
			q0 := pts[(i-1+n)%n]
			q1 := pts[i]
			q2 := pts[(i+1)%n]
			q3 := pts[(i+2)%n]
			b0 := q0.Add(q1.Scale(4)).Add(q2).Scale(1.0 / 6.0)
			b1 := q1.Scale(2).Add(q2).Scale(1.0 / 3.0)
			b2 := q1.Add(q2.Scale(2)).Scale(1.0 / 3.0)
			b3 := q1.Add(q2.Scale(4)).Add(q3).Scale(1.0 / 6.0)
			spans[i] = bezierSpan(b0, b1, b2, b3)
		}
		return spans, nil
	}
	if n == 3 {
		// Clamped quadratic B-spline elevated to cubic Bezier.
		b0 := pts[0]
		b1 := pts[0].Scale(1.0 / 3.0).Add(pts[1].Scale(2.0 / 3.0))
		b2 := pts[1].Scale(2.0 / 3.0).Add(pts[2].Scale(1.0 / 3.0))
		b3 := pts[2]
		return []CubicSpan{bezierSpan(b0, b1, b2, b3)}, nil
	}

	// Open clamped cubic B-spline over knot vector [0,0,0,0, 1, 2, ..., m-1, m,m,m,m]
	// where m = n - 3 is the number of non-zero knot spans.
	m := n - 3
	knots := make([]float64, n+4)
	for i := 1; i < m; i++ {
		knots[i+3] = float64(i)
	}
	for i := m + 3; i < len(knots); i++ {
		knots[i] = float64(m)
	}

	spans := make([]CubicSpan, m)
	for span := range m {
		kIdx := span + 3
		p0 := deBoorCubic(pts, knots, kIdx, float64(span))
		p1 := deBoorCubic(pts, knots, kIdx, float64(span)+1.0/3.0)
		p2 := deBoorCubic(pts, knots, kIdx, float64(span)+2.0/3.0)
		p3 := deBoorCubic(pts, knots, kIdx, float64(span)+1.0)

		b1 := p0.Scale(-5).Add(p1.Scale(18)).Sub(p2.Scale(9)).Add(p3.Scale(2)).Scale(1.0 / 6.0)
		b2 := p0.Scale(2).Sub(p1.Scale(9)).Add(p2.Scale(18)).Sub(p3.Scale(5)).Scale(1.0 / 6.0)
		spans[span] = bezierSpan(p0, b1, b2, p3)
	}
	return spans, nil
}

func deBoorCubic(pts []Vec3, knots []float64, k int, t float64) Vec3 {
	var d [4]Vec3
	for j := range 4 {
		d[j] = pts[k-3+j]
	}
	for r := 1; r <= 3; r++ {
		for j := 3; j >= r; j-- {
			left := knots[j+k-3]
			right := knots[j+1+k-r]
			denom := right - left
			alpha := 0.0
			if denom > 0 {
				alpha = (t - left) / denom
			}
			d[j] = d[j-1].Scale(1 - alpha).Add(d[j].Scale(alpha))
		}
	}
	return d[3]
}
