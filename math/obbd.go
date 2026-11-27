// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// OBBd is an oriented box in float64: a center and three half axes, each
// reaching from the center to the middle of a face. It bounds things placed
// where float32 does not reach, such as a tile of a city on the Earth. The
// half axes are expected to be perpendicular to one another; one of zero
// length makes the box flat along it.
type OBBd struct {
	Center Vec3d
	Axes   [3]Vec3d
}

// IsFinite reports whether every number of b is finite.
func (b OBBd) IsFinite() bool {
	return b.Center.IsFinite() && b.Axes[0].IsFinite() && b.Axes[1].IsFinite() && b.Axes[2].IsFinite()
}

// Transformed returns b carried through the affine transform m: its center as
// a point and its half axes as directions. A transform that shears leaves the
// axes no longer perpendicular.
func (b OBBd) Transformed(m Mat4d) OBBd {
	return OBBd{Center: m.Point(b.Center), Axes: [3]Vec3d{m.Direction(b.Axes[0]), m.Direction(b.Axes[1]), m.Direction(b.Axes[2])}}
}

// Corners returns b's eight corners.
func (b OBBd) Corners() (out [8]Vec3d) {
	for k := range out {
		p := b.Center
		for axis := range 3 {
			if k>>axis&1 == 1 {
				p = p.Add(b.Axes[axis])
			} else {
				p = p.Sub(b.Axes[axis])
			}
		}
		out[k] = p
	}
	return out
}

// Nearest returns the point of b nearest p, which is p itself when p is
// inside. It is exact whether or not the half axes are perpendicular: a box
// carried through a transform that stretches it askew is still measured to
// its nearest point.
func (b OBBd) Nearest(p Vec3d) Vec3d {
	rel := p.Sub(b.Center)
	if b.perpendicular() {
		out := b.Center
		for _, axis := range b.Axes {
			if l := axis.LenSq(); l > 0 {
				out = out.Add(axis.Scale(max(-1, min(1, rel.Dot(axis)/l))))
			}
		}
		return out
	}
	// Askew: p is inside when its coordinates along the half axes, found by
	// Cramer's rule, are each within one. Otherwise the nearest point is on
	// one of the six faces.
	a0, a1, a2 := b.Axes[0], b.Axes[1], b.Axes[2]
	if det := a0.Dot(a1.Cross(a2)); det != 0 {
		u := rel.Dot(a1.Cross(a2)) / det
		v := a0.Dot(rel.Cross(a2)) / det
		w := a0.Dot(a1.Cross(rel)) / det
		if math.Abs(u) <= 1 && math.Abs(v) <= 1 && math.Abs(w) <= 1 {
			return p
		}
	}
	best, least := b.Center, math.Inf(1)
	for axis := range 3 {
		e1, e2 := b.Axes[(axis+1)%3], b.Axes[(axis+2)%3]
		for _, side := range [2]float64{-1, 1} {
			q := nearestOnParallelogram(b.Center.Add(b.Axes[axis].Scale(side)), e1, e2, p)
			if d := q.Sub(p).LenSq(); d < least {
				best, least = q, d
			}
		}
	}
	return best
}

// perpendicular reports whether b's half axes are perpendicular to each
// other, to within rounding.
func (b OBBd) perpendicular() bool {
	for i := range 3 {
		x, y := b.Axes[i], b.Axes[(i+1)%3]
		if dot := x.Dot(y); dot*dot > 1e-24*x.LenSq()*y.LenSq() {
			return false
		}
	}
	return true
}

// nearestOnParallelogram returns the point nearest p of the parallelogram
// centered at c with half edges e1 and e2.
func nearestOnParallelogram(c, e1, e2, p Vec3d) Vec3d {
	q := p.Sub(c)
	// The foot of p on the plane, when it lands inside. It is found along
	// the plane's normal, and its place between the edges by cross products:
	// solving for it in the edges' dot products subtracts nearly equal
	// numbers when the edges are nearly parallel, and loses the answer.
	normal := e1.Cross(e2)
	if l := normal.LenSq(); l > 0 {
		s, t := q.Cross(e2).Dot(normal)/l, e1.Cross(q).Dot(normal)/l
		if math.Abs(s) <= 1 && math.Abs(t) <= 1 {
			return p.Sub(normal.Scale(q.Dot(normal) / l))
		}
	}
	// Otherwise it is on one of the four edges.
	best, least := c, math.Inf(1)
	for _, edge := range [4][2]Vec3d{{c.Add(e1), e2}, {c.Sub(e1), e2}, {c.Add(e2), e1}, {c.Sub(e2), e1}} {
		at := edge[0]
		if l := edge[1].LenSq(); l > 0 {
			at = at.Add(edge[1].Scale(max(-1, min(1, p.Sub(edge[0]).Dot(edge[1])/l))))
		}
		if dist := at.Sub(p).LenSq(); dist < least {
			best, least = at, dist
		}
	}
	return best
}

// Distance returns how far p is from b, zero when p is inside or on it.
func (b OBBd) Distance(p Vec3d) float64 {
	return b.Nearest(p).Dist(p)
}

// Radius returns the radius of the sphere around b's center that holds it.
func (b OBBd) Radius() float64 {
	r := 0.0
	for _, c := range b.Corners() {
		r = math.Max(r, c.Dist(b.Center))
	}
	return r
}
