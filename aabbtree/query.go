// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// QueryOverlap calls fn for every leaf whose bounds overlap bounds, touching
// included, until fn returns false. The order is deterministic for a given
// tree but otherwise unspecified. fn must not modify the tree.
func (t *Tree[T]) QueryOverlap(bounds nt3dmath.AABB, fn func(id ID, value T) bool) {
	if t.root == null {
		return
	}
	lo, hi := bounds.Min, bounds.Max
	stack := make([]int32, 1, 64)
	stack[0] = t.root
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := &t.nodes[i]
		if !overlaps(n.min, n.max, lo, hi) {
			continue
		}
		if n.child1 == null {
			if !fn(ID(i), t.values[i]) {
				return
			}
			continue
		}
		stack = append(stack, n.child2, n.child1)
	}
}

// overlaps reports whether two boxes share a point, faces included.
func overlaps(alo, ahi, blo, bhi nt3dmath.Vec3) bool {
	return alo.X <= bhi.X && ahi.X >= blo.X && alo.Y <= bhi.Y && ahi.Y >= blo.Y && alo.Z <= bhi.Z && ahi.Z >= blo.Z
}

// RayCast calls fn for every leaf the ray meets at or after its origin,
// with the ray parameter where it enters the leaf's bounds (0 for a leaf
// containing the origin), until fn returns false. Direction need not be
// unit length; t is in units of it, and a zero component means the ray is
// parallel to that slab. Children nearer the origin are visited first, so
// hits arrive roughly near to far, but the order is not guaranteed across
// the whole tree: a nearest-hit search keeps the smallest t seen and
// continues. A ray with a NaN or infinite component meets nothing. fn must
// not modify the tree.
func (t *Tree[T]) RayCast(ray nt3dmath.Ray, fn func(id ID, value T, t float32) bool) {
	if t.root == null || !finite(ray.Origin) || !finite(ray.Direction) {
		return
	}
	origin := [3]float32{ray.Origin.X, ray.Origin.Y, ray.Origin.Z}
	inverse := [3]float32{1 / ray.Direction.X, 1 / ray.Direction.Y, 1 / ray.Direction.Z} // ±Inf for a zero component.
	stack := make([]int32, 1, 64)
	stack[0] = t.root
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := &t.nodes[i]
		if n.child1 == null {
			if d, ok := slab(origin, inverse, n.min, n.max); ok && !fn(ID(i), t.values[i], d) {
				return
			}
			continue
		}
		c1, c2 := &t.nodes[n.child1], &t.nodes[n.child2]
		d1, ok1 := slab(origin, inverse, c1.min, c1.max)
		d2, ok2 := slab(origin, inverse, c2.min, c2.max)
		// Push the farther child first so the nearer is visited first.
		switch {
		case ok1 && ok2 && d1 <= d2:
			stack = append(stack, n.child2, n.child1)
		case ok1 && ok2:
			stack = append(stack, n.child1, n.child2)
		case ok1:
			stack = append(stack, n.child1)
		case ok2:
			stack = append(stack, n.child2)
		}
	}
}

func finite(v nt3dmath.Vec3) bool {
	return nt3dmath.IsFinite(float64(v.X)) && nt3dmath.IsFinite(float64(v.Y)) && nt3dmath.IsFinite(float64(v.Z))
}

// slab returns where a ray enters a box, clamped to zero for an origin
// inside it, and false if the ray misses the box or the box lies entirely
// behind the origin. inverse holds the reciprocal direction, ±Inf where the
// ray is parallel to a slab.
func slab(origin, inverse [3]float32, lower, upper nt3dmath.Vec3) (float32, bool) {
	lo := [3]float32{lower.X, lower.Y, lower.Z}
	hi := [3]float32{upper.X, upper.Y, upper.Z}
	near, far := float32(0), float32(math.Inf(1))
	for k := range 3 {
		if math.IsInf(float64(inverse[k]), 0) {
			// Parallel to this slab: inside it or never.
			if origin[k] < lo[k] || origin[k] > hi[k] {
				return 0, false
			}
			continue
		}
		t0, t1 := (lo[k]-origin[k])*inverse[k], (hi[k]-origin[k])*inverse[k]
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
