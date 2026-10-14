// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

func shade(c color.RGBA, n nt3dmath.Vec3, l Light) color.RGBA {
	if n.Dot(n) == 0 {
		return c
	}
	v := l.Ambient + (1-l.Ambient)*max(0, n.Normalize().Dot(l.Direction.Normalize()))
	return color.RGBA{uint8(float32(c.R) * v), uint8(float32(c.G) * v), uint8(float32(c.B) * v), 255}
}

func softwareRender(f Frame) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	depth := make([]float32, f.Width*f.Height)
	for i := range depth {
		depth[i] = 2
		off := i * 4
		img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3] = f.Background.R, f.Background.G, f.Background.B, 255
	}
	put := func(x, y int, z float32, c color.RGBA) {
		if x < 0 || y < 0 || x >= f.Width || y >= f.Height || z < 0 || z > 1 {
			return
		}
		i := y*f.Width + x
		if z <= depth[i]+.00001 {
			depth[i] = z
			off := i * 4
			img.Pix[off], img.Pix[off+1], img.Pix[off+2] = c.R, c.G, c.B
		}
	}
	// tri draws a local-space triangle: m takes it to clip space, and normals
	// takes its normals to world space, where the light is.
	tri := func(m, normals nt3dmath.Mat4, a, b, c Vertex, uv [3]UV, material *Material) {
		if material != nil && material.Unlit {
			a.Normal = nt3dmath.Vec3{}
			b.Normal = nt3dmath.Vec3{}
			c.Normal = nt3dmath.Vec3{}
		}
		// Project each corner once, as Mat4.Project does, keeping w.
		project := func(p nt3dmath.Vec3) (x, y, z, w float32) {
			x, y, z, w = m.Transform(p)
			return (x/w + 1) * float32(f.Width) * .5, (1 - y/w) * float32(f.Height) * .5, z / w, w
		}
		x0, y0, z0, w0 := project(a.Position)
		x1, y1, z1, w1 := project(b.Position)
		x2, y2, z2, w2 := project(c.Position)
		if min(w0, w1, w2) <= 0 {
			return
		}
		area := (x1-x0)*(y2-y0) - (y1-y0)*(x2-x0)
		if area >= -.000001 {
			return
		} // CCW faces become clockwise with screen Y down
		loX, hiX := max(0, int(math.Floor(float64(min(x0, x1, x2))))), min(f.Width-1, int(math.Ceil(float64(max(x0, x1, x2)))))
		loY, hiY := max(0, int(math.Floor(float64(min(y0, y1, y2))))), min(f.Height-1, int(math.Ceil(float64(max(y0, y1, y2)))))
		if material != nil && material.Texture != nil {
			a.Color = color.RGBA{255, 255, 255, 255}
			b.Color = a.Color
			c.Color = a.Color
		}
		ca, cb, cc := shade(a.Color, normals.Direction(a.Normal), f.Light), shade(b.Color, normals.Direction(b.Normal), f.Light), shade(c.Color, normals.Direction(c.Normal), f.Light)
		for y := loY; y <= hiY; y++ {
			for x := loX; x <= hiX; x++ {
				px, py := float32(x)+.5, float32(y)+.5
				u := ((x1-px)*(y2-py) - (y1-py)*(x2-px)) / area
				v := ((x2-px)*(y0-py) - (y2-py)*(x0-px)) / area
				t := 1 - u - v
				if min(u, v, t) < 0 {
					continue
				}
				mix := func(a, b, c uint8) uint8 { return uint8(max(0, min(255, u*float32(a)+v*float32(b)+t*float32(c)))) }
				pixel := color.RGBA{mix(ca.R, cb.R, cc.R), mix(ca.G, cb.G, cc.G), mix(ca.B, cb.B, cc.B), 255}
				if material != nil && material.Texture != nil {
					q0, q1, q2 := u/w0, v/w1, t/w2
					sum := q0 + q1 + q2
					q0 /= sum
					q1 /= sum
					q2 /= sum
					tex := material.Texture.sample(UV{q0*uv[0].U + q1*uv[1].U + q2*uv[2].U, q0*uv[0].V + q1*uv[1].V + q2*uv[2].V})
					light := (q0*float32(ca.R) + q1*float32(cb.R) + q2*float32(cc.R)) / 255
					pixel = color.RGBA{uint8(float32(tex.R)*light + .5), uint8(float32(tex.G)*light + .5), uint8(float32(tex.B)*light + .5), 255}
				}
				put(x, y, u*z0+v*z1+t*z2, pixel)
			}
		}
	}
	for i := 0; i+1 < len(f.DynamicLines); i += 2 {
		rasterStroke(f, f.DynamicLines[i], f.DynamicLines[i+1], 1, 0, .00002, put)
	}
	// Interactive frames sample screen-space marks (points, lines, arrows),
	// which stay legible thinned. Every triangle is drawn: sampling surfaces
	// leaves dots; the widget lowers resolution when frames are slow instead.
	sampled := func(n, limit int) int {
		if f.Complete {
			return 1
		}
		return max(1, (n+limit-1)/limit)
	}
	for gi, g := range f.Geometry {
		inst := instance(f, gi)
		if !inst.Visible {
			continue
		}
		// Transform as primitives are drawn rather than copying the scene.
		local := f
		local.Matrix = f.Matrix.Mul(inst.World)
		inverse, _ := inst.World.InverseAffine()
		normals := inverse.Transpose()
		step := sampled(len(g.Points), 10000)
		for i := 0; i < len(g.Points); i += step {
			p := g.Points[i]
			x, y, z, ok := local.Matrix.Project(p.Position, f.Width, f.Height)
			if !ok {
				continue
			}
			radius := max(1, min(8, int(p.Radius)))
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					if dx*dx+dy*dy <= radius*radius {
						put(int(x)+dx, int(y)+dy, z, p.Color)
					}
				}
			}
		}
		for i := 0; i+2 < len(g.Indices); i += 3 {
			var uv [3]UV
			if len(g.UV) > 0 {
				for j := range 3 {
					uv[j] = g.UV[g.Indices[i+j]]
				}
			}
			tri(local.Matrix, normals, g.Vertices[g.Indices[i]], g.Vertices[g.Indices[i+1]], g.Vertices[g.Indices[i+2]], uv, g.Material)
		}
		for _, b := range g.Boxes {
			v := boxVertices(b)
			for j := 0; j < len(v); j += 3 {
				tri(local.Matrix, normals, v[j], v[j+1], v[j+2], [3]UV{}, nil)
			}
		}
		lineStep := 2 * sampled(len(g.Lines)/2, 20000)
		for i := 0; i+1 < len(g.Lines); i += lineStep {
			rasterStroke(local, g.Lines[i], g.Lines[i+1], g.LineWidth, 0, -.00001, put)
		}
		arrowStep := sampled(len(g.Arrows), 10000)
		for i := 0; i < len(g.Arrows); i += arrowStep {
			a := g.Arrows[i]
			rasterStroke(local, Vertex{Position: a.Start, Color: a.Color}, Vertex{Position: a.End, Color: a.Color}, a.Width, a.HeadSize, 0, put)
		}
	}
	drawOverlays(img, f)
	return img
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
