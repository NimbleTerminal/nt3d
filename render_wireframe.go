// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/ntcanvas"
)

func wireRender(f Frame, cols, rows int) string {
	c := ntcanvas.NewGraphemeGrid(cols, rows)
	styleFor := func(ink color.Color) ntcanvas.CellStyle {
		if ink == nil {
			return ntcanvas.CellStyle{}
		}
		rgb := color.RGBAModel.Convert(ink).(color.RGBA)
		return ntcanvas.CellStyle{Foreground: ntcanvas.RGBColor(rgb.R, rgb.G, rgb.B)}
	}
	// visible, when set, keeps the cells of a stroke that no surface hides.
	drawCells := func(x, y, xx, yy float32, ink color.Color, visible func(ntcanvas.Point) bool) {
		style := styleFor(ink)
		ntcanvas.RasterLine(c.Bounds(),
			ntcanvas.Float64Point{X: float64(x), Y: float64(y)},
			ntcanvas.Float64Point{X: float64(xx), Y: float64(yy)},
			func(p ntcanvas.Point) {
				if visible == nil || visible(p) {
					c.SetGrapheme(p, "·", style)
				}
			},
		)
	}
	cellFrame := f
	cellFrame.Width, cellFrame.Height = cols, rows
	depth := newWireDepth(f, cols, rows)
	// Mesh edges are hidden behind nearer surfaces; decorations are not.
	drawLine := func(cells Frame, a, b nt3dmath.Vec3, ink color.Color, occluded bool) {
		s, ok := projectStroke(cells, a, b)
		if !ok {
			return
		}
		var visible func(ntcanvas.Point) bool
		if occluded {
			dx, dy := s.b.x-s.a.x, s.b.y-s.a.y
			length := dx*dx + dy*dy
			visible = func(p ntcanvas.Point) bool {
				t := float32(0)
				if length > 0 {
					t = min(1, max(0, ((float32(p.X)+.5-s.a.x)*dx+(float32(p.Y)+.5-s.a.y)*dy)/length))
				}
				// Depth as the buffer keeps it: perspective-correct view distance,
				// or the depth coordinate, which is affine without perspective.
				at := s.a.z*(1-t) + s.b.z*t
				if depth.projective {
					at = 1 / ((1-t)/s.a.w + t/s.b.w)
				}
				return !depth.hidden(p.X, p.Y, at)
			}
		}
		drawCells(s.a.x, s.a.y, s.b.x, s.b.y, ink, visible)
	}
	depth.features(func(x, y int) { c.SetGrapheme(ntcanvas.Point{X: x, Y: y}, "·", ntcanvas.CellStyle{}) })
	for i := 0; i+1 < len(f.DynamicLines); i += 2 {
		drawLine(cellFrame, f.DynamicLines[i].Position, f.DynamicLines[i+1].Position, f.DynamicLines[i].Color, false)
	}
	for gi, g := range f.Geometry {
		inst := instance(f, gi)
		if !inst.Visible || edgeOn(g) {
			continue
		}
		// Project local coordinates through the node's transform.
		local, cells := f, cellFrame
		local.Matrix = f.Matrix.Mul(inst.World)
		cells.Matrix = local.Matrix
		line := func(a, b nt3dmath.Vec3) { drawLine(cells, a, b, nil, true) }
		step := max(1, (len(g.Points)+1999)/2000)
		for i := 0; i < len(g.Points); i += step {
			p := g.Points[i]
			x, y, _, ok := local.Matrix.Project(p.Position, cols, rows)
			if ok {
				c.SetGrapheme(ntcanvas.Point{X: int(x), Y: int(y)}, "•", ntcanvas.CellStyle{})
			}
		}
		if g.WireGrid != ([2]int{}) {
			wireGrid(local, g, cols, rows, line)
		} else if !depth.dense(gi) {
			// Triangles large enough to read: their front faces' edges.
			// Dense meshes are drawn by their feature lines instead.
			for i := 0; i+2 < len(g.Indices); i += 3 {
				a, b, cc := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
				if frontFacing(cells, a, b, cc) {
					line(a, b)
					line(b, cc)
					line(cc, a)
				}
			}
		}
		for _, b := range g.Boxes {
			lo, hi := b.Min, b.Min.Add(b.Size)
			corner := func(i int) nt3dmath.Vec3 {
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
				return p
			}
			for i := range 8 {
				for _, bit := range []int{1, 2, 4} {
					if i&bit == 0 {
						line(corner(i), corner(i|bit))
					}
				}
			}
		}
		for i := 0; i+1 < len(g.Lines); i += 2 {
			drawLine(cells, g.Lines[i].Position, g.Lines[i+1].Position, g.Lines[i].Color, false)
		}
		arrowStep := max(1, (len(g.Arrows)+1999)/2000)
		for i := 0; i < len(g.Arrows); i += arrowStep {
			a := g.Arrows[i]
			s, ok := projectStroke(local, a.Start, a.End)
			if !ok {
				continue
			}
			sx, sy := float32(cols)/float32(f.Width), float32(rows)/float32(f.Height)
			draw := func(x, y, xx, yy float32) { drawCells(x*sx, y*sy, xx*sx, yy*sy, a.Color, nil) }
			draw(s.a.x, s.a.y, s.b.x, s.b.y)
			hl, hw := s.head(a.HeadSize, lineWidth(a.Width))
			if hl > 0 {
				x, y := s.b.x-s.dx*hl, s.b.y-s.dy*hl
				draw(s.b.x, s.b.y, x-s.dy*hw, y+s.dx*hw)
				draw(s.b.x, s.b.y, x+s.dy*hw, y-s.dx*hw)
			}
		}
	}
	for _, label := range f.CellText {
		c.SetTextWithStyle(ntcanvas.Point{X: label.X, Y: label.Y}, label.Text, styleFor(label.Color))
	}
	return c.Render(c.Bounds())
}

// Retain every vertex along each chosen row/column, preserving hills rather
// than connecting widely separated samples by flat chords. Always draw borders.
func wireGrid(f Frame, g Geometry, cols, rows int, draw func(nt3dmath.Vec3, nt3dmath.Vec3)) {
	nx, ny := g.WireGrid[0], g.WireGrid[1]
	f.Width, f.Height = cols, rows
	point := func(x, y int) nt3dmath.Vec3 { return g.Vertices[y*nx+x].Position }
	// Estimate each axis' on-screen extent using three cross-sections. Cell
	// spacing accounts for terminal cells being taller than they are wide.
	extent := func(a, b nt3dmath.Vec3) float32 {
		s, ok := projectStroke(f, a, b)
		if !ok {
			return 0
		}
		return max(min(abs(s.b.x-s.a.x), float32(cols))/7, min(abs(s.b.y-s.a.y), float32(rows))/3.5)
	}
	var ex, ey float32
	for _, y := range []int{0, ny / 2, ny - 1} {
		ex = max(ex, extent(point(0, y), point(nx-1, y)))
	}
	for _, x := range []int{0, nx / 2, nx - 1} {
		ey = max(ey, extent(point(x, 0), point(x, ny-1)))
	}
	selectLines := func(n int, e float32, visit func(int)) {
		intervals := min(n-1, max(1, int(e)))
		for i := 0; i <= intervals; i++ {
			visit((i*(n-1) + intervals/2) / intervals)
		}
	}
	selectLines(ny, ey, func(y int) {
		for x := 0; x < nx-1; x++ {
			draw(point(x, y), point(x+1, y))
		}
	})
	selectLines(nx, ex, func(x int) {
		for y := 0; y < ny-1; y++ {
			draw(point(x, y), point(x, y+1))
		}
	})
}

// frontFacing reports whether a triangle faces the viewer, as culling decides.
// The determinant of homogeneous screen coordinates has the sign of the
// projected area for corners before the eye, and stays right when some lie
// behind it, where dividing by w would not.
func frontFacing(f Frame, a, b, c nt3dmath.Vec3) bool {
	x0, y0, _, w0 := f.Matrix.Transform(a)
	x1, y1, _, w1 := f.Matrix.Transform(b)
	x2, y2, _, w2 := f.Matrix.Transform(c)
	return x0*(y1*w2-w1*y2)-y0*(x1*w2-w1*x2)+w0*(x1*y2-y1*x2) > 0
}
