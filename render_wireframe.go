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
	drawCells := func(x, y, xx, yy float32, ink color.Color) {
		style := styleFor(ink)
		ntcanvas.RasterLine(c.Bounds(),
			ntcanvas.Float64Point{X: float64(x), Y: float64(y)},
			ntcanvas.Float64Point{X: float64(xx), Y: float64(yy)},
			func(p ntcanvas.Point) { c.SetGrapheme(p, "·", style) },
		)
	}
	cellFrame := f
	cellFrame.Width, cellFrame.Height = cols, rows
	drawLine := func(a, b nt3dmath.Vec3, ink color.Color) {
		s, ok := projectStroke(cellFrame, a, b)
		if !ok {
			return
		}
		drawCells(s.a.x, s.a.y, s.b.x, s.b.y, ink)
	}
	line := func(a, b nt3dmath.Vec3) { drawLine(a, b, nil) }
	for i := 0; i+1 < len(f.DynamicLines); i += 2 {
		drawLine(f.DynamicLines[i].Position, f.DynamicLines[i+1].Position, f.DynamicLines[i].Color)
	}
	for _, g := range worldGeometry(f) {
		step := max(1, (len(g.Points)+1999)/2000)
		for i := 0; i < len(g.Points); i += step {
			p := g.Points[i]
			x, y, _, ok := f.Matrix.Project(p.Position, cols, rows)
			if ok {
				c.SetGrapheme(ntcanvas.Point{X: int(x), Y: int(y)}, "•", ntcanvas.CellStyle{})
			}
		}
		if g.WireGrid != ([2]int{}) {
			wireGrid(f, g, cols, rows, line)
		} else {
			step = 3 * max(1, (len(g.Indices)/3+1999)/2000)
			for i := 0; i+2 < len(g.Indices); i += step {
				a, b, cc := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
				line(a, b)
				line(b, cc)
				line(cc, a)
			}
		}
		for i := 0; i+1 < len(g.Lines); i += 2 {
			drawLine(g.Lines[i].Position, g.Lines[i+1].Position, g.Lines[i].Color)
		}
		arrowStep := max(1, (len(g.Arrows)+1999)/2000)
		for i := 0; i < len(g.Arrows); i += arrowStep {
			a := g.Arrows[i]
			s, ok := projectStroke(f, a.Start, a.End)
			if !ok {
				continue
			}
			sx, sy := float32(cols)/float32(f.Width), float32(rows)/float32(f.Height)
			draw := func(x, y, xx, yy float32) { drawCells(x*sx, y*sy, xx*sx, yy*sy, a.Color) }
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
