// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"slices"

	"nimbleterminal.dev/nt3d/math"
)

// bvhLeaf is the most triangles a leaf holds; bvhLinear is the fewest a
// mesh needs before an index is worth building for picking.
const (
	bvhLeaf   = 4
	bvhLinear = 256
)

// meshBVH is a bounding volume hierarchy over a mesh's triangles, in the
// mesh's local space, for ray queries. Nodes are stored depth first: an
// interior node's left child follows it, and right names the other.
type meshBVH struct {
	nodes []bvhNode
	tris  []int32 // Triangle numbers, grouped by leaf.
}

type bvhNode struct {
	min, max     nt3dmath.Vec3
	right        int32 // Interior nodes: the right child.
	start, count int32 // Leaves: tris[start : start+count].
}

func buildBVH(g *Geometry) *meshBVH {
	n := len(g.Indices) / 3
	b := &meshBVH{tris: make([]int32, n), nodes: make([]bvhNode, 0, 2*n/bvhLeaf+1)}
	centroids := make([]nt3dmath.Vec3, n)
	for i := range n {
		b.tris[i] = int32(i)
		a, c, d := g.Vertices[g.Indices[3*i]].Position, g.Vertices[g.Indices[3*i+1]].Position, g.Vertices[g.Indices[3*i+2]].Position
		centroids[i] = a.Add(c).Add(d).Scale(1. / 3)
	}
	b.build(g, centroids, 0, n)
	return b
}

// build adds the node for tris[lo:hi], and its descendants, returning its index.
func (b *meshBVH) build(g *Geometry, centroids []nt3dmath.Vec3, lo, hi int) int32 {
	var bounds, middles nt3dmath.AABB
	for _, t := range b.tris[lo:hi] {
		for k := range 3 {
			bounds.Include(g.Vertices[g.Indices[3*int(t)+k]].Position)
		}
		middles.Include(centroids[t])
	}
	index := int32(len(b.nodes))
	b.nodes = append(b.nodes, bvhNode{min: bounds.Min, max: bounds.Max, start: int32(lo), count: int32(hi - lo)})
	extent := components(middles.Max.Sub(middles.Min))
	axis := 0
	for k := 1; k < 3; k++ {
		if extent[k] > extent[axis] {
			axis = k
		}
	}
	if hi-lo <= bvhLeaf || extent[axis] == 0 {
		return index
	}
	at := func(t int32) float32 { return components(centroids[t])[axis] }
	// Split at the middle of the centroids; if every one falls on one side,
	// as float rounding can arrange, split them in half instead.
	split := components(middles.Min)[axis] + extent[axis]/2
	mid := lo
	for i := lo; i < hi; i++ {
		if at(b.tris[i]) < split {
			b.tris[i], b.tris[mid] = b.tris[mid], b.tris[i]
			mid++
		}
	}
	if mid == lo || mid == hi {
		slices.SortFunc(b.tris[lo:hi], func(p, q int32) int {
			switch x, y := at(p), at(q); {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		})
		mid = (lo + hi) / 2
	}
	b.nodes[index].count = 0
	b.build(g, centroids, lo, mid)
	right := b.build(g, centroids, mid, hi)
	b.nodes[index].right = right
	return index
}

// slab returns where a ray enters and leaves a box, given the reciprocal of
// its direction, and whether it does so between near and far.
func slab(origin, inverse [3]float32, lower, upper nt3dmath.Vec3, near, far float32) (float32, bool) {
	lo, hi := components(lower), components(upper)
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

// nearest finds the nearest front face the ray meets, returning its triangle
// number and ray parameter.
func (b *meshBVH) nearest(g *Geometry, r nt3dmath.Ray) (int, float32, bool) {
	origin, direction := components(r.Origin), components(r.Direction)
	var inverse [3]float32
	for k := range 3 {
		inverse[k] = 1 / direction[k] // ±Inf for a zero component.
	}
	best, found := float32(math.Inf(1)), -1
	stack := make([]int32, 1, 64)
	for len(stack) > 0 {
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := &b.nodes[index]
		if _, ok := slab(origin, inverse, node.min, node.max, 0, best); !ok {
			continue
		}
		if node.count > 0 {
			for _, t := range b.tris[node.start : node.start+node.count] {
				i := 3 * int(t)
				if d, ok := rayTriangle(r, g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position); ok && d < best {
					best, found = d, int(t)
				}
			}
			continue
		}
		// Visit the nearer child first, so the farther is often pruned.
		left, right := index+1, node.right
		l, lok := slab(origin, inverse, b.nodes[left].min, b.nodes[left].max, 0, best)
		rt, rok := slab(origin, inverse, b.nodes[right].min, b.nodes[right].max, 0, best)
		switch {
		case lok && rok && l <= rt:
			stack = append(stack, right, left)
		case lok && rok:
			stack = append(stack, left, right)
		case lok:
			stack = append(stack, left)
		case rok:
			stack = append(stack, right)
		}
	}
	return found, best, found >= 0
}
