// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"math"
	"slices"

	"nimbleterminal.dev/nt3d/math"
)

// Transparent geometries draw each side in its own pass: back faces, lit as
// seen from behind, then front faces.
const (
	opaqueFaces = iota // Front faces, opaque: the usual pass.
	backFaces
	frontFaces
)

// blendOrder lists the drawn geometries with transparent triangles or
// blended points in the order they are blended: farthest first, by the clip-space depth of their
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
		if !drawn[i] || !transparent(g) && !blendedPoints(g) {
			continue
		}
		b := blendBounds(g)
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

// blendBounds is a blended geometry's bounds, computed if the frame left them out.
func blendBounds(g *Geometry) nt3dmath.AABB {
	if g.Bounds.IsValid() {
		return g.Bounds
	}
	return GeometryBounds(*g)
}

// blendSlices is how many depth slices a frame's blended work falls into,
// far to near; maxBlendSlots how many alpha point geometries a frame orders
// (a key holds the geometry's slot in 12 bits).
const (
	blendSlices   = 64
	maxBlendSlots = 4096
)

// blendPlan is a frame's blended work: alpha point geometries as slots, in
// blendOrder; transparent meshes and additive point geometries by the slice
// of their bounds-center depth; the depth range the keys quantize. Every
// renderer orders blended points by these keys, so they draw one picture.
type blendPlan struct {
	slots     []int   // frame geometry indices of alpha-blended point geometries, in blendOrder
	bySlice   [][]int // per slice (64), frame geometry indices of transparent meshes and additive point geometries, in blendOrder
	near, far float32 // clip-space depth range over the eight bounds corners of every blended geometry
	points    int     // total alpha points in slots
}

// planBlend plans the drawn geometries' blended work. A frame with more
// alpha point geometries than slots fails as the GPU's binding limit does,
// the plan still holding them all.
func (f Frame) planBlend(drawn []bool) (blendPlan, error) {
	order := f.blendOrder(drawn)
	plan := blendPlan{bySlice: make([][]int, blendSlices)}
	near, far := float32(math.Inf(1)), float32(math.Inf(-1))
	for _, gi := range order {
		g := &f.Geometry[gi]
		m := f.Matrix.Mul(instance(f, gi).World)
		b := blendBounds(g)
		for c := range 8 {
			corner := b.Min
			if c&1 != 0 {
				corner.X = b.Max.X
			}
			if c&2 != 0 {
				corner.Y = b.Max.Y
			}
			if c&4 != 0 {
				corner.Z = b.Max.Z
			}
			_, _, z, _ := m.Transform(corner)
			near, far = min(near, z), max(far, z)
		}
	}
	if len(order) > 0 {
		plan.near, plan.far = near, far
	}
	for _, gi := range order {
		g := &f.Geometry[gi]
		if blendedPoints(g) && g.PointStyle.Blend == AlphaBlend {
			plan.slots = append(plan.slots, gi)
			plan.points += len(g.Points)
		}
		if transparent(g) || blendedPoints(g) && g.PointStyle.Blend == AdditiveBlend {
			b := blendBounds(g)
			_, _, z, _ := f.Matrix.Mul(instance(f, gi).World).Transform(b.Min.Add(b.Max).Scale(.5))
			s := plan.sliceOf(z)
			plan.bySlice[s] = append(plan.bySlice[s], gi)
		}
	}
	if len(plan.slots) > maxBlendSlots {
		return plan, fmt.Errorf("nt3d: frame exceeds GPU binding limit")
	}
	return plan, nil
}

// depth16 quantizes a clip-space depth over the plan's range to 16 bits,
// farthest 0 and nearest 65535, finer near the viewer by a square root. A
// plan with a single depth puts everything at 65535.
func (p *blendPlan) depth16(z float32) uint32 {
	var t float32
	if p.far > p.near {
		t = min(1, max(0, (z-p.near)/(p.far-p.near)))
	}
	if t != t { // NaN
		t = 0
	}
	q := uint32(float32(math.Sqrt(float64(t))) * 65535)
	return 65535 - q
}

// key orders a point of slot at depth z: slice, then slot, then depth, each
// ascending, so a sort draws far to near within a slot's slice.
func (p *blendPlan) key(slot int, z float32) uint32 {
	k := p.depth16(z)
	return k>>10<<22 | uint32(slot)<<10 | k&1023
}

// sliceOf is the slice a depth z falls in, 0 the farthest.
func (p *blendPlan) sliceOf(z float32) int { return int(p.depth16(z) >> 10) }
