// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
	"nimbleterminal.dev/nt3d/math"
)

// ID names a leaf from Insert until Remove. Removed IDs may be reused by
// later inserts.
type ID uint32

// null marks a missing parent, child, or root.
const null = -1

// node is one tree node. A leaf has child1 == null and height 0; a free
// node has height -1 and uses parent as the free list's next link.
type node struct {
	min, max       nt3dmath.Vec3
	parent         int32
	child1, child2 int32
	height         int32
}

// Tree is a dynamic AABB tree whose leaves carry a value of type T. The zero
// value is not usable; call New.
type Tree[T any] struct {
	nodes  []node
	values []T // Parallel to nodes; meaningful for leaves only.
	root   int32
	free   int32 // Head of the free list, or null.
	count  int   // Live leaves.
}

// New returns an empty tree.
func New[T any]() *Tree[T] {
	return &Tree[T]{root: null, free: null}
}

// Len returns the number of leaves.
func (t *Tree[T]) Len() int { return t.count }

// Height returns the number of levels: 0 for an empty tree, 1 for one leaf.
func (t *Tree[T]) Height() int {
	if t.root == null {
		return 0
	}
	return int(t.nodes[t.root].height) + 1
}

// Insert adds a leaf with bounds and value and returns its ID. It panics if
// bounds is not finite or its minimum exceeds its maximum on any axis.
func (t *Tree[T]) Insert(bounds nt3dmath.AABB, value T) ID {
	validate(bounds)
	leaf := t.allocate()
	n := &t.nodes[leaf]
	n.min, n.max = bounds.Min, bounds.Max
	n.child1, n.child2 = null, null
	n.height = 0
	t.values[leaf] = value
	t.insertLeaf(leaf)
	t.count++
	return ID(leaf)
}

// Bounds returns the leaf's bounds. It panics if id does not name a leaf.
func (t *Tree[T]) Bounds(id ID) nt3dmath.AABB {
	n := &t.nodes[t.leaf(id)]
	var b nt3dmath.AABB
	b.Include(n.min)
	b.Include(n.max)
	return b
}

// Value returns the leaf's value. It panics if id does not name a leaf.
func (t *Tree[T]) Value(id ID) T {
	return t.values[t.leaf(id)]
}

// leaf returns the node index of a live leaf, panicking otherwise.
func (t *Tree[T]) leaf(id ID) int32 {
	if int(id) >= len(t.nodes) || t.nodes[id].height != 0 {
		panic("nt3daabbtree: unknown id")
	}
	return int32(id)
}

func validate(b nt3dmath.AABB) {
	lo, hi := b.Min, b.Max
	if !(lo.X <= hi.X && lo.Y <= hi.Y && lo.Z <= hi.Z) ||
		!nt3dmath.IsFinite(float64(lo.X)) || !nt3dmath.IsFinite(float64(lo.Y)) || !nt3dmath.IsFinite(float64(lo.Z)) ||
		!nt3dmath.IsFinite(float64(hi.X)) || !nt3dmath.IsFinite(float64(hi.Y)) || !nt3dmath.IsFinite(float64(hi.Z)) {
		panic("nt3daabbtree: invalid bounds")
	}
}

// allocate returns a free node index, growing the slices if needed. The
// node's fields are stale; callers set them all.
func (t *Tree[T]) allocate() int32 {
	if t.free != null {
		i := t.free
		t.free = t.nodes[i].parent
		return i
	}
	t.nodes = append(t.nodes, node{})
	var zero T
	t.values = append(t.values, zero)
	return int32(len(t.nodes) - 1)
}

// release returns a node to the free list and drops its value.
func (t *Tree[T]) release(i int32) {
	var zero T
	t.values[i] = zero
	t.nodes[i] = node{parent: t.free, child1: null, child2: null, height: -1}
	t.free = i
}

// area is the surface area of a box, the insertion cost measure.
func area(lo, hi nt3dmath.Vec3) float32 {
	d := hi.Sub(lo)
	return 2 * (d.X*d.Y + d.Y*d.Z + d.Z*d.X)
}

func union(alo, ahi, blo, bhi nt3dmath.Vec3) (lo, hi nt3dmath.Vec3) {
	return nt3dmath.Vec3{X: min(alo.X, blo.X), Y: min(alo.Y, blo.Y), Z: min(alo.Z, blo.Z)},
		nt3dmath.Vec3{X: max(ahi.X, bhi.X), Y: max(ahi.Y, bhi.Y), Z: max(ahi.Z, bhi.Z)}
}

// insertLeaf links a prepared leaf node into the tree: descend to the
// sibling whose joining costs the least surface area, splice in a new parent
// above it, then refit and rebalance every ancestor.
func (t *Tree[T]) insertLeaf(leaf int32) {
	if t.root == null {
		t.root = leaf
		t.nodes[leaf].parent = null
		return
	}
	lo, hi := t.nodes[leaf].min, t.nodes[leaf].max
	index := t.root
	for t.nodes[index].child1 != null {
		n := &t.nodes[index]
		c1, c2 := n.child1, n.child2
		current := area(n.min, n.max)
		clo, chi := union(n.min, n.max, lo, hi)
		combined := area(clo, chi)
		// Cost of making a new parent for this node and the leaf.
		cost := 2 * combined
		// Every descent below also enlarges this node.
		inheritance := 2 * (combined - current)
		cost1 := t.descentCost(c1, lo, hi) + inheritance
		cost2 := t.descentCost(c2, lo, hi) + inheritance
		if cost < cost1 && cost < cost2 {
			break
		}
		if cost1 < cost2 {
			index = c1
		} else {
			index = c2
		}
	}
	sibling := index
	oldParent := t.nodes[sibling].parent
	newParent := t.allocate() // May move t.nodes; index by value below.
	plo, phi := union(lo, hi, t.nodes[sibling].min, t.nodes[sibling].max)
	t.nodes[newParent] = node{min: plo, max: phi, parent: oldParent, child1: sibling, child2: leaf, height: t.nodes[sibling].height + 1}
	if oldParent != null {
		if t.nodes[oldParent].child1 == sibling {
			t.nodes[oldParent].child1 = newParent
		} else {
			t.nodes[oldParent].child2 = newParent
		}
	} else {
		t.root = newParent
	}
	t.nodes[sibling].parent = newParent
	t.nodes[leaf].parent = newParent
	t.refit(newParent)
}

// descentCost is the surface area added by pushing a box into child c.
func (t *Tree[T]) descentCost(c int32, lo, hi nt3dmath.Vec3) float32 {
	n := &t.nodes[c]
	ulo, uhi := union(n.min, n.max, lo, hi)
	if n.child1 == null {
		return area(ulo, uhi)
	}
	return area(ulo, uhi) - area(n.min, n.max)
}

// refit walks from index to the root, rebalancing each node and recomputing
// its height and bounds from its children.
func (t *Tree[T]) refit(index int32) {
	for index != null {
		index = t.balance(index)
		n := &t.nodes[index]
		c1, c2 := &t.nodes[n.child1], &t.nodes[n.child2]
		n.height = 1 + max(c1.height, c2.height)
		n.min, n.max = union(c1.min, c1.max, c2.min, c2.max)
		index = n.parent
	}
}

// balance rotates the subtree at iA if its children's heights differ by
// more than one, and returns the index of the subtree's new root.
func (t *Tree[T]) balance(iA int32) int32 {
	a := &t.nodes[iA]
	if a.child1 == null || a.height < 2 {
		return iA
	}
	iB, iC := a.child1, a.child2
	b, c := &t.nodes[iB], &t.nodes[iC]
	switch d := c.height - b.height; {
	case d > 1:
		return t.rotate(iA, iC, iB)
	case d < -1:
		return t.rotate(iA, iB, iC)
	}
	return iA
}

// rotate lifts the taller child iUp of iA into iA's place, with iA taking
// the shorter of iUp's children and keeping its other child iKeep.
func (t *Tree[T]) rotate(iA, iUp, iKeep int32) int32 {
	a, up := &t.nodes[iA], &t.nodes[iUp]
	iF, iG := up.child1, up.child2
	f, g := &t.nodes[iF], &t.nodes[iG]
	// up replaces a under a's parent.
	up.child1 = iA
	up.parent = a.parent
	a.parent = iUp
	if up.parent != null {
		if t.nodes[up.parent].child1 == iA {
			t.nodes[up.parent].child1 = iUp
		} else {
			t.nodes[up.parent].child2 = iUp
		}
	} else {
		t.root = iUp
	}
	// The taller grandchild stays with up; a takes the other.
	iStay, iMove, stay, move := iF, iG, f, g
	if g.height > f.height {
		iStay, iMove, stay, move = iG, iF, g, f
	}
	up.child2 = iStay
	a.child1, a.child2 = iKeep, iMove
	move.parent = iA
	keep := &t.nodes[iKeep]
	a.min, a.max = union(keep.min, keep.max, move.min, move.max)
	a.height = 1 + max(keep.height, move.height)
	up.min, up.max = union(a.min, a.max, stay.min, stay.max)
	up.height = 1 + max(a.height, stay.height)
	return iUp
}

// Remove deletes the leaf and drops its value. It panics if id does not
// name a leaf. The ID may be reused by a later Insert.
func (t *Tree[T]) Remove(id ID) {
	leaf := t.leaf(id)
	t.removeLeaf(leaf)
	t.release(leaf)
	t.count--
}

// Update moves or resizes the leaf, keeping its ID and value. It panics if
// id does not name a leaf or bounds is invalid (see Insert); a rejected
// update leaves the tree unchanged.
func (t *Tree[T]) Update(id ID, bounds nt3dmath.AABB) {
	leaf := t.leaf(id)
	validate(bounds)
	t.removeLeaf(leaf)
	t.nodes[leaf].min, t.nodes[leaf].max = bounds.Min, bounds.Max
	t.insertLeaf(leaf)
}

// removeLeaf unlinks a leaf without releasing its node: its parent is
// replaced by its sibling, and the ancestors refit.
func (t *Tree[T]) removeLeaf(leaf int32) {
	if leaf == t.root {
		t.root = null
		return
	}
	parent := t.nodes[leaf].parent
	grand := t.nodes[parent].parent
	sibling := t.nodes[parent].child1
	if sibling == leaf {
		sibling = t.nodes[parent].child2
	}
	t.nodes[sibling].parent = grand
	if grand == null {
		t.root = sibling
	} else {
		if t.nodes[grand].child1 == parent {
			t.nodes[grand].child1 = sibling
		} else {
			t.nodes[grand].child2 = sibling
		}
	}
	t.release(parent)
	t.refit(grand)
}

// Node describes one tree node to Ancestors and Walk: its depth (0 at the
// root), its bounds, and, for a leaf, its ID.
type Node struct {
	Depth  int
	Bounds nt3dmath.AABB
	Leaf   bool
	ID     ID
}

// Ancestors calls fn for the leaf's ancestors from the root down, ending with
// the leaf itself, until fn returns false. It panics if id does not name a
// leaf. It is meant for inspecting or drawing the tree.
func (t *Tree[T]) Ancestors(id ID, fn func(Node) bool) {
	leaf := t.leaf(id)
	chain := make([]int32, 0, 32)
	for i := leaf; i != null; i = t.nodes[i].parent {
		chain = append(chain, i)
	}
	for depth := range chain {
		i := chain[len(chain)-1-depth]
		if !fn(t.describe(i, depth)) {
			return
		}
	}
}

// Walk calls fn for every node in depth-first order, each parent before its
// children, until fn returns false. It is meant for inspecting or drawing
// the tree.
func (t *Tree[T]) Walk(fn func(Node) bool) {
	if t.root == null {
		return
	}
	type entry struct{ index, depth int32 }
	stack := make([]entry, 1, 64)
	stack[0] = entry{t.root, 0}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !fn(t.describe(e.index, int(e.depth))) {
			return
		}
		if n := &t.nodes[e.index]; n.child1 != null {
			stack = append(stack, entry{n.child2, e.depth + 1}, entry{n.child1, e.depth + 1})
		}
	}
}

func (t *Tree[T]) describe(i int32, depth int) Node {
	n := &t.nodes[i]
	var b nt3dmath.AABB
	b.Include(n.min)
	b.Include(n.max)
	return Node{Depth: depth, Bounds: b, Leaf: n.child1 == null, ID: ID(i)}
}
