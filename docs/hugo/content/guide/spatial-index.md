---
title: "Spatial index"
weight: 48
---

# Spatial index

`nimbleterminal.dev/nt3d/aabbtree` (`nt3daabbtree`) is a dynamic AABB tree: a spatial index over a changing set of boxed objects that answers "which objects overlap this box?" and "which objects does this ray meet?" in logarithmic time. It is independent of scenes and renderers; you hand it bounds and a value of your own type, and it hands them back from queries. It is not a collision solver, and the static triangle hierarchy that [picking](../picking/) uses stays as it is.

```go
type Object struct{ Name string; Bounds nt3dmath.AABB }

tree := nt3daabbtree.New[*Object]()
id := tree.Insert(obj.Bounds, obj)      // stable until Remove
tree.Update(id, movedBounds)            // same ID, same value
tree.Remove(id)

tree.QueryOverlap(region, func(id nt3daabbtree.ID, o *Object) bool {
	// every leaf whose box touches region
	return true // false stops early
})
tree.RayCast(ray, func(id nt3daabbtree.ID, o *Object, t float32) bool {
	// t is where the ray enters the leaf's box, in units of ray.Direction
	return true
})
```

The value type should be a pointer, an index, or another small value: the tree copies it into the leaf and out to every callback. The tree stores exactly the bounds you give; if your objects move a little every frame, enlarge their boxes yourself and update only when they leave.

Insertion follows the surface-area heuristic and rotations keep the tree balanced, so tens of thousands of leaves query in a few hundred nanoseconds for a small box and a few microseconds for a ray. Ray hits arrive roughly near to far (the nearer child is visited first) but the order is not guaranteed across the whole tree: a nearest-hit search keeps the smallest `t` seen and continues. The same sequence of operations always builds the same tree and reports in the same order.

Programming errors panic rather than return errors: a never-issued ID, a removed ID not yet reused, or bounds that are not finite or have minimum above maximum. Removed IDs are reused by later inserts, often the very next one, so a stale ID can quietly name a new leaf; clear IDs you keep when you remove their leaves. Boxes that only touch count as overlapping; a box containing the ray's origin reports `t = 0`; a ray with a NaN or infinite component meets nothing.
