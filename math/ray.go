// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// Ray is a ray starting at Origin and extending along Direction.
type Ray struct{ Origin, Direction Vec3 }

// RayProbe is a ray prepared for testing against many boxes, as a spatial
// index does while it walks its tree: the origin, and the reciprocal of the
// direction along each axis, which is ±Inf where the ray runs parallel to it.
type RayProbe struct {
	Origin, Inverse [3]float32
}

// Probe prepares r for box tests. The direction need not be a unit vector;
// distances along the ray are in multiples of it.
func (r Ray) Probe() RayProbe {
	return RayProbe{
		Origin:  [3]float32{r.Origin.X, r.Origin.Y, r.Origin.Z},
		Inverse: [3]float32{1 / r.Direction.X, 1 / r.Direction.Y, 1 / r.Direction.Z},
	}
}

// Enter returns how far along the ray it enters the box from lower to upper,
// no less than near, and whether it does so no later than far. Pass 0 and +Inf
// for the whole ray; a search narrows far to the nearest hit it has found. A
// ray that starts inside the box enters it at near.
func (p RayProbe) Enter(lower, upper Vec3, near, far float32) (float32, bool) {
	lo := [3]float32{lower.X, lower.Y, lower.Z}
	hi := [3]float32{upper.X, upper.Y, upper.Z}
	for k := range 3 {
		if math.IsInf(float64(p.Inverse[k]), 0) {
			// Parallel to this slab: inside it or never.
			if p.Origin[k] < lo[k] || p.Origin[k] > hi[k] {
				return 0, false
			}
			continue
		}
		t0, t1 := (lo[k]-p.Origin[k])*p.Inverse[k], (hi[k]-p.Origin[k])*p.Inverse[k]
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		near, far = max(near, t0), min(far, t1)
		if far < near {
			return 0, false
		}
	}
	return near, true
}

// Hits reports whether r, from its origin onward, meets the box. An empty box
// is never met.
func (b AABB) Hits(r Ray) bool {
	if !b.IsValid() {
		return false
	}
	_, ok := r.Probe().Enter(b.Min, b.Max, 0, float32(math.Inf(1)))
	return ok
}
