// Copyright (c) 2026 Neomantra Corp.

// Package nt3daabbtree is a dynamic bounding-volume tree over axis-aligned
// boxes: a spatial index for a changing set of objects that answers "which
// objects overlap this box?" and "which objects does this ray meet?" in
// logarithmic time. Leaves hold a box and a caller-chosen value of type T;
// interior nodes hold boxes only. The tree is the whole package: it knows
// nothing of scenes, meshes, or rendering, and it does not resolve
// collisions. The static triangle hierarchy that picking uses is separate.
//
// Insert returns an ID that names the leaf until Remove; Update moves or
// resizes a leaf under the same ID. The tree keeps itself balanced
// (Box2D-style surface-area insertion with rotations), so a sequence of
// thousands to tens of thousands of inserts, removes, and updates stays
// fast. The same sequence of operations always builds the same tree and
// reports query results in the same order, on every platform: the cost
// arithmetic avoids fused multiply-adds.
//
// T should be a pointer, an index, or another small value: the tree copies
// it on Insert and hands a copy to every callback, so a large struct by
// value is copied on each hit.
//
// Programming errors panic rather than return errors, like an out-of-range
// slice index: a never-issued ID, a removed ID not yet reused, or bounds
// that are not finite or whose minimum exceeds the maximum. Removed IDs are
// reused by later inserts, often the very next one, so a stale ID can name
// a new leaf; callers that keep IDs past Remove must clear them. The zero
// AABB is a point at the origin and is accepted. Boxes that touch on a
// face, edge, or corner overlap. A ray's Direction need not be unit
// length; the reported t is in units of it, only hits at t ≥ 0 are
// reported, a box containing the origin reports t = 0, and a ray with a
// NaN or infinite component meets nothing.
//
// A typical use stores pointers to the caller's objects:
//
//	type Object struct{ Name string; Bounds nt3dmath.AABB }
//	tree := nt3daabbtree.New[*Object]()
//	id := tree.Insert(obj.Bounds, obj)
//	tree.QueryOverlap(region, func(_ nt3daabbtree.ID, o *Object) bool {
//		fmt.Println(o.Name)
//		return true // false stops the query
//	})
//	tree.Update(id, moved) // the object moved; the ID is unchanged
//	tree.Remove(id)
package nt3daabbtree
