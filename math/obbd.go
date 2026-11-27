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
// inside. It is exact when the half axes are perpendicular; when they are
// not, the point returned is in the box and may not be the nearest.
func (b OBBd) Nearest(p Vec3d) Vec3d {
	rel := p.Sub(b.Center)
	out := b.Center
	for _, axis := range b.Axes {
		if l := axis.LenSq(); l > 0 {
			out = out.Add(axis.Scale(max(-1, min(1, rel.Dot(axis)/l))))
		}
	}
	return out
}

// Distance returns how far p is from b, zero when p is inside or on it. Like
// Nearest, it is exact for perpendicular half axes and never too small
// otherwise.
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
