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
	drawPixels := func(x, y, xx, yy float32, ink color.Color) {
		style := styleFor(ink)
		n := min(4*max(cols, rows), max(1, int(max(abs(xx-x), abs(yy-y)))))
		for i := 0; i <= n; i++ {
			t := float32(i) / float32(n)
			c.SetGrapheme(ntcanvas.Point{X: int(x + (xx-x)*t), Y: int(y + (yy-y)*t)}, "·", style)
		}
	}
	drawLine := func(a, b nt3dmath.Vec3, ink color.Color) {
		frame := f
		frame.Width, frame.Height = cols, rows
		s, ok := projectStroke(frame, a, b)
		if !ok {
			return
		}
		drawPixels(s.a.x, s.a.y, s.b.x, s.b.y, ink)
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
		step = 3 * max(1, (len(g.Indices)/3+1999)/2000)
		for i := 0; i+2 < len(g.Indices); i += step {
			a, b, cc := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
			line(a, b)
			line(b, cc)
			line(cc, a)
		}
		for i, b := range g.Boxes {
			if i >= 500 {
				break
			}
			v := boxVertices(b)
			for j := 0; j < len(v); j += 3 {
				line(v[j].Position, v[j+1].Position)
				line(v[j+1].Position, v[j+2].Position)
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
			draw := func(x, y, xx, yy float32) { drawPixels(x*sx, y*sy, xx*sx, yy*sy, a.Color) }
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
