// Copyright (c) 2026 Neomantra Corp.

package nt3d

// overhang is how far, in render pixels, a geometry's screen-sized marks
// (points, line widths, arrow heads) may reach past its bounds, plus one.
func overhang(g *Geometry, st *geometryState) float32 {
	r := g.Outline.Width
	if g.Outline.Units == WorldUnits && g.Outline.Width > 0 {
		r = 32 // As far as a world-unit outline reaches on screen.
	}
	if len(g.Lines) > 0 {
		r = max(r, lineWidth(g.LineWidth))
	}
	if g.PointStyle.Units == WorldUnits && len(g.Points) > 0 {
		r = max(r, MaxPointPixels) // As far as a world radius reaches on screen.
	} else if st.pointUpdate != nil {
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

// drawn reports, for each of a frame's geometries, whether renderers draw it:
// whether its instance is visible and, when the frame culls, whether its
// bounds reach into the view. Pixel sizes are scaled by scale, as a
// supersampled software frame's are.
func (f Frame) drawn(scale float32) []bool {
	out := make([]bool, len(f.Draws))
	for i := range f.Draws {
		inst := &f.Draws[i]
		out[i] = !inst.Hidden
		g := &f.Draws[i].Geometry
		if out[i] && f.Cull && g.Bounds.IsValid() {
			pad := overhang(g, &f.Draws[i].state) * scale
			clip := f.Matrix.Mul(inst.world())
			out[i] = !clip.Outside(g.Bounds.Min, g.Bounds.Max, 2*pad/float32(f.Width), 2*pad/float32(f.Height))
		}
	}
	return out
}
