// Copyright (c) 2026 Neomantra Corp.
package nt3dclod

import (
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// MaxLevels is the most levels of detail a selection distinguishes.
const MaxLevels = 8

// View is what a frame sees, in the space of the set's positions: for a
// set in a node's local space, Matrix is the frame's matrix times the
// node's world transform, and Eye the frame's eye taken into local space.
type View struct {
	Matrix      nt3dmath.Mat4 // Positions to clip space, as Frame.Matrix.
	Eye         nt3dmath.Vec3 // The camera's position, for perspective distances.
	Perspective bool
	// Width and Height are the frame's render pixels, and Margin how many
	// pixels of slack around the view keep a cluster whose items draw past
	// their positions: a point's pixel radius, say.
	Width, Height int
	Margin        float32
}

// Level is one level of detail. Clusters nearer than Range use it; the
// last level has no range and takes everything farther. Fraction is how
// many of a cluster's items the level lists, 0 to 1: 1 lists them all, .25
// a spread quarter, 0 none, leaving the caller to draw its own
// representative of the cluster from its Pick.
type Level struct {
	Range    float32
	Fraction float32
}

// Pick is a selected cluster: its index, its level, and its listed items,
// Selection.Items[First : First+Count].
type Pick struct{ Cluster, Level, First, Count uint32 }

// Selection is what to draw this frame: the visible clusters in index
// order, and their listed items pick by pick.
type Selection struct {
	Picks []Pick
	Items []uint32
}

// Gather appends src's items, in the selection's order, to dst[:0]: the
// draw list for the point or instance path.
func Gather[T any](dst []T, src []T, items []uint32) []T {
	dst = dst[:0]
	for _, i := range items {
		dst = append(dst, src[i])
	}
	return dst
}

func validLevels(levels []Level) error {
	if len(levels) < 1 || len(levels) > MaxLevels {
		return fmt.Errorf("nt3dclod: 1 to %d levels", MaxLevels)
	}
	for i, l := range levels {
		if !nt3dmath.IsFinite(float64(l.Fraction)) || l.Fraction < 0 || l.Fraction > 1 {
			return fmt.Errorf("nt3dclod: level %d fraction must be 0 to 1", i)
		}
		if i == len(levels)-1 {
			break
		}
		if !nt3dmath.IsFinite(float64(l.Range)) || i > 0 && l.Range < levels[i-1].Range {
			return fmt.Errorf("nt3dclod: level ranges must be finite and ascending")
		}
	}
	return nil
}

func validView(v View) error {
	if v.Width < 1 || v.Height < 1 {
		return fmt.Errorf("nt3dclod: view needs a width and height")
	}
	if !nt3dmath.IsFinite(float64(v.Margin)) || v.Margin < 0 {
		return fmt.Errorf("nt3dclod: view margin must be finite and not negative")
	}
	for _, x := range v.Matrix {
		if !nt3dmath.IsFinite(float64(x)) {
			return fmt.Errorf("nt3dclod: view matrix is not finite")
		}
	}
	if !nt3dmath.IsFinite(float64(v.Eye.X)) || !nt3dmath.IsFinite(float64(v.Eye.Y)) || !nt3dmath.IsFinite(float64(v.Eye.Z)) {
		return fmt.Errorf("nt3dclod: view eye is not finite")
	}
	return nil
}

// pads are the view's margins in normalized device units.
func (v View) pads() (float32, float32) {
	return 2 * v.Margin / float32(v.Width), 2 * v.Margin / float32(v.Height)
}

// orthoDistance is how far every cluster counts as being in an
// orthographic view, which converges nowhere: the view's height in world
// units, twice the reciprocal of the matrix's clip-y scale, which for
// nt3d's orbit camera is its Distance in either projection.
func (v View) orthoDistance() float32 {
	m := &v.Matrix
	k := float32(math.Sqrt(float64(m[1]*m[1] + m[5]*m[5] + m[9]*m[9])))
	if k == 0 {
		return float32(math.Inf(1))
	}
	return 2 / k
}

// outside reports whether the box lies wholly outside the view volume under
// m, widened by padX and padY to the sides: every corner beyond one of its
// six planes. It is nt3d's culling test, so a cluster is kept whenever
// nt3d would draw it. The GPU shader repeats it expression for expression.
func outside(lo, hi nt3dmath.Vec3, m *nt3dmath.Mat4, padX, padY float32) bool {
	common := 0x3f
	for i := range 8 {
		p := lo
		if i&1 != 0 {
			p.X = hi.X
		}
		if i&2 != 0 {
			p.Y = hi.Y
		}
		if i&4 != 0 {
			p.Z = hi.Z
		}
		x, y, z, w := m.Transform(p)
		sides := 0
		if x < -w*(1+padX) {
			sides |= 1
		}
		if x > w*(1+padX) {
			sides |= 2
		}
		if y < -w*(1+padY) {
			sides |= 4
		}
		if y > w*(1+padY) {
			sides |= 8
		}
		if z < 0 {
			sides |= 16
		}
		if z > w {
			sides |= 32
		}
		if common &= sides; common == 0 {
			return false
		}
	}
	return true
}

// distance is how far the cluster is from the viewer: in perspective, from
// the eye to its bounding sphere, zero inside it.
func (v View) distance(c *Cluster, ortho float32) float32 {
	if !v.Perspective {
		return ortho
	}
	d := c.Center.Sub(v.Eye)
	return max(0, float32(math.Sqrt(float64(d.Dot(d))))-c.Radius)
}

// levelFor is the first level whose range the distance is within; the
// last takes the rest.
func levelFor(d float32, levels []Level) int {
	for i := range len(levels) - 1 {
		if d < levels[i].Range {
			return i
		}
	}
	return len(levels) - 1
}

// listed is how many of a cluster's items a level lists: the fraction of
// them, rounded up, at least one unless the fraction is zero.
func listed(count uint32, fraction float32) uint32 {
	if fraction == 0 {
		return 0
	}
	return max(1, uint32(math.Ceil(float64(float32(count)*fraction))))
}

// Select picks the clusters within the view, each at the level its
// distance calls for, into out, whose slices it reuses. On an error, out
// is untouched.
func (s *Set) Select(v View, levels []Level, out *Selection) error {
	if err := validView(v); err != nil {
		return err
	}
	if err := validLevels(levels); err != nil {
		return err
	}
	out.Picks, out.Items = out.Picks[:0], out.Items[:0]
	padX, padY := v.pads()
	ortho := v.orthoDistance()
	for c := range s.Clusters {
		cl := &s.Clusters[c]
		if outside(cl.Bounds.Min, cl.Bounds.Max, &v.Matrix, padX, padY) {
			continue
		}
		level := levelFor(v.distance(cl, ortho), levels)
		n := listed(cl.Count, levels[level].Fraction)
		out.Picks = append(out.Picks, Pick{uint32(c), uint32(level), uint32(len(out.Items)), n})
		out.Items = append(out.Items, s.Order[cl.First:cl.First+n]...)
	}
	return nil
}
