// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// lodIndexMask selects a point's index from a packed selection entry; the
// scale byte f8 sits above it.
const lodIndexMask = 1<<24 - 1

// Every point index fits below the scale byte: this fails to compile if
// MaxPointIndex, which PointLimit never exceeds, outgrows the mask.
const _ = uint(lodIndexMask + 1 - MaxPointIndex)

// lodRange is a run of one cluster's points a slot draws: count entries of
// Clusters.Order from first, written to the slot's selection from outBase. f8 is the fraction of the cluster drawn in 1/255ths
// (1..255); a thinned cluster's splats grow by 1/sqrt of it to keep its
// coverage.
type lodRange struct {
	first, count, outBase uint32
	f8                    uint8
}

// lodSlot is one blend slot's selection.
type lodSlot struct {
	ranges   []lodRange // nil when the slot draws every point (no Clusters, or an unlimited budget)
	selected int        // points this slot draws
}

// lodPlan is a frame's splat selection: what both renderers draw of each
// blend slot. It is stateless, a function of the frame alone.
type lodPlan struct {
	slots           []lodSlot // parallel to blendPlan.slots
	drawn, resident int       // splats drawn; splats held by the slots
}

// planLOD shares the frame's splat budget among the clusters of plan's
// slots. Points without clusters count against the budget first. Each
// visible cluster then wants as many splats as cover half a pixel for each
// pixel of its footprint, at most all of them; if the wants exceed what is left, each is
// scaled down alike, but a visible cluster keeps one. It sets plan.points to
// the splats drawn, which the sort uses in place of the slots' point counts.
func (f Frame) planLOD(plan *blendPlan) lodPlan {
	budget := f.SplatBudget
	if budget == 0 {
		budget = DefaultSplatBudget
	}
	lod := lodPlan{slots: make([]lodSlot, len(plan.slots))}
	wants := make([][]uint32, len(plan.slots)) // Per clustered slot, per cluster; 0 for culled.
	fixed, wanted := 0, 0
	for k, gi := range plan.slots {
		g := &f.Geometry[gi]
		lod.resident += len(g.Points)
		if g.Clusters == nil || g.PointStyle.Shape != GaussianPoint || budget < 0 {
			lod.slots[k].selected = len(g.Points)
			fixed += len(g.Points)
			continue
		}
		m := f.Matrix.Mul(instance(f, gi).World)
		pad := overhang(g)
		clusters := g.Clusters.Clusters
		want := make([]uint32, len(clusters))
		for i, c := range clusters {
			if c.Count == 0 || f.Cull && outside(c.Bounds, m, 2*pad/float32(f.Width), 2*pad/float32(f.Height)) {
				continue
			}
			want[i] = lodWant(c.Count, lodProjectedRadius(c.Center, c.Radius, m, f.Width, f.Height, f.Perspective))
			wanted += int(want[i])
		}
		wants[k] = want
	}
	remaining := max(budget-fixed, 0)
	for k, gi := range plan.slots {
		if wants[k] == nil {
			continue
		}
		slot := &lod.slots[k]
		slot.ranges = []lodRange{}
		var out uint32
		for i, c := range f.Geometry[gi].Clusters.Clusters {
			n := wants[k][i]
			if n == 0 {
				continue
			}
			if wanted > remaining {
				n = min(n, max(1, uint32(math.Ceil(float64(n)*float64(remaining)/float64(wanted)))))
			}
			f8 := uint8(min(255, max(1, math.Round(255*float64(n)/float64(c.Count)))))
			slot.ranges = append(slot.ranges, lodRange{c.First, n, out, f8})
			out += n
		}
		slot.selected = int(out)
	}
	for _, s := range lod.slots {
		lod.drawn += s.selected
	}
	plan.points = lod.drawn
	return lod
}

// planLODScaled is planLOD for f drawn at scale times its frame's size, as
// a supersampled frame is: the selection is the frame's own, as the GPU and
// SplatSelection plan it.
func (f Frame) planLODScaled(plan *blendPlan, scale int) lodPlan {
	f.Width, f.Height = max(1, f.Width/scale), max(1, f.Height/scale)
	return f.planLOD(plan)
}

// lodWant is how many of a cluster's count splats cover a sphere r pixels
// across at half a splat per pixel of its footprint. A sphere the eye is inside, or one whose
// size is not finite, wants them all; a visible cluster wants one at least.
func lodWant(count uint32, r float32) uint32 {
	if r != r || math.IsInf(float64(r), 0) {
		return count
	}
	want := math.Ceil(.5 * math.Pi * float64(r) * float64(r))
	return uint32(min(float64(count), max(1, want)))
}

// SplatSelection reports how many splats Frame selects for drawing and how
// many its Gaussian clouds hold, under the frame's SplatBudget: exactly what
// the renderers select, as the selection depends on the frame alone. drawn
// counts every selected splat, including those the renderers then cull
// behind the eye or past the clip planes. A frame that fails to plan
// reports zeros.
func (f Frame) SplatSelection() (drawn, resident int) {
	plan, err := f.planBlend(f.drawn(1))
	if err != nil {
		return 0, 0
	}
	lod := f.planLOD(&plan)
	return lod.drawn, lod.resident
}

// lodProjectedRadius is the on-screen radius, in pixels, of a sphere of the
// given radius about center, under the matrix m (the frame's matrix with the
// instance's world transform). Clip x and y vary across the sphere by its
// radius times the lengths of m's x and y rows (xyz), which the viewport's
// half width and half height take to pixels; the larger sizes it. A
// perspective view divides by the sphere's nearest w: its center's, less the
// radius times the length of m's w row. A sphere that holds the eye is
// infinitely large.
func lodProjectedRadius(center nt3dmath.Vec3, radius float32, m nt3dmath.Mat4, width, height int, perspective bool) float32 {
	row := func(i int) float64 { return math.Sqrt(float64(m[i]*m[i] + m[i+4]*m[i+4] + m[i+8]*m[i+8])) }
	s := float32(max(row(0)*float64(width)/2, row(1)*float64(height)/2))
	r := radius * s
	if !perspective {
		return r
	}
	_, _, _, w := m.Transform(center)
	reach := radius * float32(row(3))
	if w <= reach {
		return float32(math.Inf(1))
	}
	return r / max(w-reach, 1e-3)
}

// packLOD is a selection entry: a point's index, with its cluster's f8 above.
func packLOD(index uint32, f8 uint8) uint32 {
	return index&lodIndexMask | uint32(max(f8, 1))<<24
}

// unpackLOD splits a selection entry into the point's index and the factor
// its splat grows by, 1/sqrt of its cluster's f8 fraction.
func unpackLOD(p uint32) (index uint32, scale float32) {
	return p & lodIndexMask, float32(1 / math.Sqrt(float64(max(p>>24, 1))/255))
}
