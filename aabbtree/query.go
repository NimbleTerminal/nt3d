// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
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
