// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "nimbleterminal.dev/nt3d/math"

// overhang is how far, in render pixels, a geometry's screen-sized marks
// (points, line widths, arrow heads) may reach past its bounds, plus one.
func overhang(g *Geometry) float32 {
	r := g.Outline.Width
	if len(g.Lines) > 0 {
		r = max(r, lineWidth(g.LineWidth))
	}
	if g.PointStyle.Units == WorldUnits && len(g.Points) > 0 {
		r = max(r, MaxPointPixels) // As far as a world radius reaches on screen.
	} else if g.pointUpdate != nil {
		r = max(r, 32) // Validated pixel points never exceed 32; avoid a frame scan.
	} else {
		for _, p := range g.Points {
			r = max(r, p.Radius)
		}
	}
	for _, a := range g.Arrows {
		r = max(r, lineWidth(a.Width), a.HeadSize)
	}
	return r + 1
}

// outside reports whether a local box, taken by m into clip space, lies
// wholly outside the view volume, widened by pad (in normalized device
// units) to the sides: every corner beyond one of its six planes. The planes
// are linear in clip coordinates, so the test holds for corners behind the
// eye too. A box that straddles a corner of the volume without entering it
// is kept, as it is cheap to draw for nothing.
func outside(b nt3dmath.AABB, m nt3dmath.Mat4, padX, padY float32) bool {
	common := 0x3f
	for i := range 8 {
		p := b.Min
		if i&1 != 0 {
			p.X = b.Max.X
		}
		if i&2 != 0 {
			p.Y = b.Max.Y
		}
		if i&4 != 0 {
			p.Z = b.Max.Z
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

// drawn reports, for each of a frame's geometries, whether renderers draw it:
// whether its instance is visible and, when the frame culls, whether its
// bounds reach into the view. Pixel sizes are scaled by scale, as a
// supersampled software frame's are.
func (f Frame) drawn(scale float32) []bool {
	out := make([]bool, len(f.Geometry))
	for i := range f.Geometry {
		inst := instance(f, i)
		out[i] = inst.Visible
		g := &f.Geometry[i]
		if out[i] && f.Cull && g.Bounds.IsValid() {
			pad := overhang(g) * scale
			out[i] = !outside(g.Bounds, f.Matrix.Mul(inst.World), 2*pad/float32(f.Width), 2*pad/float32(f.Height))
		}
	}
	return out
}
