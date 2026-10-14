// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "slices"

// Transparent geometries draw each side in its own pass: back faces, lit as
// seen from behind, then front faces.
const (
	opaqueFaces = iota // Front faces, opaque: the usual pass.
	backFaces
	frontFaces
)

// blendOrder lists the drawn geometries with transparent triangles in the
// order they are blended: farthest first, by the clip-space depth of their
// bounds' centers, which grows away from the viewer in both projections.
// Ties keep the frame's order.
func (f Frame) blendOrder(drawn []bool) []int {
	type entry struct {
		index int
		depth float32
	}
	var entries []entry
	for i := range f.Geometry {
		g := &f.Geometry[i]
		if !drawn[i] || !transparent(g) {
			continue
		}
		b := g.Bounds
		if !b.IsValid() {
			b = GeometryBounds(*g)
		}
		_, _, z, _ := f.Matrix.Mul(instance(f, i).World).Transform(b.Min.Add(b.Max).Scale(.5))
		entries = append(entries, entry{i, z})
	}
	slices.SortStableFunc(entries, func(a, b entry) int {
		switch {
		case a.depth > b.depth:
			return -1
		case a.depth < b.depth:
			return 1
		}
		return 0
	})
	order := make([]int, len(entries))
	for i, e := range entries {
		order[i] = e.index
	}
	return order
}
