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
	probe, far := ray.Probe(), float32(math.Inf(1))
	root := &t.nodes[t.root]
	d, ok := probe.Enter(root.min, root.max, 0, far)
	if !ok {
		return
	}
	type entry struct {
		index int32
		t     float32
	}
	stack := make([]entry, 1, 64)
	stack[0] = entry{t.root, d}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := &t.nodes[e.index]
		if n.child1 == null {
			if !fn(ID(e.index), t.values[e.index], e.t) {
				return
			}
			continue
		}
		c1, c2 := &t.nodes[n.child1], &t.nodes[n.child2]
		d1, ok1 := probe.Enter(c1.min, c1.max, 0, far)
		d2, ok2 := probe.Enter(c2.min, c2.max, 0, far)
		// Push the farther child first so the nearer is visited first.
		switch {
		case ok1 && ok2 && d1 <= d2:
			stack = append(stack, entry{n.child2, d2}, entry{n.child1, d1})
		case ok1 && ok2:
			stack = append(stack, entry{n.child1, d1}, entry{n.child2, d2})
		case ok1:
			stack = append(stack, entry{n.child1, d1})
		case ok2:
			stack = append(stack, entry{n.child2, d2})
		}
	}
}

func finite(v nt3dmath.Vec3) bool {
	return nt3dmath.IsFinite(float64(v.X)) && nt3dmath.IsFinite(float64(v.Y)) && nt3dmath.IsFinite(float64(v.Z))
}
