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

// softwareRender draws a frame, at its supersampling factor if it has one:
// the scene at that multiple of its size, averaged back down, with overlays
// drawn after, at the frame's own size.
func softwareRender(f Frame) image.Image {
	s := supersampleFactor(f)
	if s == 1 {
		return softwareDraw(f, 1)
	}
	big := f
	big.Width, big.Height, big.Overlays = f.Width*s, f.Height*s, nil
	img := shrink(softwareDraw(big, s), s)
	drawOverlays(img, f)
	return img
}

// softwareDraw draws a frame whose pixel sizes (points, line widths, arrow
// heads) are scaled by scale, as a supersampled frame's are.
func softwareDraw(f Frame, scale int) *image.RGBA {
	px := float32(scale)
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
	// raster fills a clip-space triangle that lies past the near plane.
	raster := func(a, b, c *clipCorner, material *Material) {
		textured := material != nil && material.Texture != nil
		cutout := textured && material.Cutout
		threshold := uint8(math.Ceil(float64(material.threshold()) * 255))
		x0, y0, z0 := (a.x/a.w+1)*float32(f.Width)*.5, (1-a.y/a.w)*float32(f.Height)*.5, a.z/a.w
		x1, y1, z1 := (b.x/b.w+1)*float32(f.Width)*.5, (1-b.y/b.w)*float32(f.Height)*.5, b.z/b.w
		x2, y2, z2 := (c.x/c.w+1)*float32(f.Width)*.5, (1-c.y/c.w)*float32(f.Height)*.5, c.z/c.w
		area := (x1-x0)*(y2-y0) - (y1-y0)*(x2-x0)
		if area >= -.000001 {
			return
		} // CCW faces become clockwise with screen Y down
		loX, hiX := max(0, int(math.Floor(float64(min(x0, x1, x2))))), min(f.Width-1, int(math.Ceil(float64(max(x0, x1, x2)))))
		loY, hiY := max(0, int(math.Floor(float64(min(y0, y1, y2))))), min(f.Height-1, int(math.Ceil(float64(max(y0, y1, y2)))))
		for y := loY; y <= hiY; y++ {
			for x := loX; x <= hiX; x++ {
				px, py := float32(x)+.5, float32(y)+.5
				u := ((x1-px)*(y2-py) - (y1-py)*(x2-px)) / area
				v := ((x2-px)*(y0-py) - (y2-py)*(x0-px)) / area
				t := 1 - u - v
				if min(u, v, t) < 0 {
					continue
				}
				mix := func(k int) uint8 { return uint8(max(0, min(255, u*a.shade[k]+v*b.shade[k]+t*c.shade[k]+.5))) }
				pixel := color.RGBA{mix(0), mix(1), mix(2), 255}
				if textured {
					q0, q1, q2 := u/a.w, v/b.w, t/c.w
					sum := q0 + q1 + q2
					q0 /= sum
					q1 /= sum
					q2 /= sum
					tex := material.Texture.sampleWrap(UV{q0*a.uv.U + q1*b.uv.U + q2*c.uv.U, q0*a.uv.V + q1*b.uv.V + q2*c.uv.V}, material.Repeat)
					if cutout && tex.A < threshold {
						continue // Cut out: neither color nor depth.
					}
					light := (q0*a.shade[0] + q1*b.shade[0] + q2*c.shade[0]) / 255
					pixel = color.RGBA{uint8(float32(tex.R)*light + .5), uint8(float32(tex.G)*light + .5), uint8(float32(tex.B)*light + .5), 255}
				}
				put(x, y, u*z0+v*z1+t*z2, pixel)
			}
		}
	}
	// tri draws a local-space triangle: m takes it to clip space, and normals
	// takes its normals to world space, where the light is. A triangle that
	// crosses the near plane is cut there, as the GPU does, not discarded.
	tri := func(m, normals *nt3dmath.Mat4, vertices []Vertex, index [3]uint32, uv []UV, material *Material) {
		var corners [3]clipCorner
		for k, i := range index {
			corners[k].x, corners[k].y, corners[k].z, corners[k].w = m.Transform(vertices[i].Position)
			if len(uv) > 0 {
				corners[k].uv = uv[i]
			}
		}
		// Most triangles lie wholly past the near plane: cull those facing
		// away before lighting them.
		whole := corners[0].z >= 0 && corners[1].z >= 0 && corners[2].z >= 0
		if whole && (min(corners[0].w, corners[1].w, corners[2].w) <= 0 || !facing(&corners[0], &corners[1], &corners[2])) {
			return
		}
		unlit, textured := material != nil && material.Unlit, material != nil && material.Texture != nil
		for k, i := range index {
			c, n := vertices[i].Color, vertices[i].Normal
			if unlit {
				n = nt3dmath.Vec3{}
			}
			if textured {
				c = color.RGBA{255, 255, 255, 255}
			}
			lit := shade(c, normals.Direction(n), f.Light)
			corners[k].shade = [3]float32{float32(lit.R), float32(lit.G), float32(lit.B)}
		}
		if whole {
			raster(&corners[0], &corners[1], &corners[2], material)
			return
		}
		var storage [4]clipCorner
		polygon := clipNear(corners, &storage)
		for k := 1; k+1 < len(polygon); k++ {
			if polygon[0].w > 0 && polygon[k].w > 0 && polygon[k+1].w > 0 {
				raster(&polygon[0], &polygon[k], &polygon[k+1], material)
			}
		}
	}
	for i := 0; i+1 < len(f.DynamicLines); i += 2 {
		rasterStroke(f, f.DynamicLines[i], f.DynamicLines[i+1], px, 0, .00002, put)
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
			radius := max(1, min(8, int(p.Radius))) * scale
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					if dx*dx+dy*dy <= radius*radius {
						put(int(x)+dx, int(y)+dy, z, p.Color)
					}
				}
			}
		}
		for i := 0; i+2 < len(g.Indices); i += 3 {
			tri(&local.Matrix, &normals, g.Vertices, [3]uint32{g.Indices[i], g.Indices[i+1], g.Indices[i+2]}, g.UV, g.Material)
		}
		for _, b := range g.Boxes {
			v := boxVertices(b)
			for j := 0; j < len(v); j += 3 {
				tri(&local.Matrix, &normals, v, [3]uint32{uint32(j), uint32(j + 1), uint32(j + 2)}, nil, nil)
			}
		}
		lineStep := 2 * sampled(len(g.Lines)/2, 20000)
		for i := 0; i+1 < len(g.Lines); i += lineStep {
			rasterStroke(local, g.Lines[i], g.Lines[i+1], lineWidth(g.LineWidth)*px, 0, -.00001, put)
		}
		arrowStep := sampled(len(g.Arrows), 10000)
		for i := 0; i < len(g.Arrows); i += arrowStep {
			a := g.Arrows[i]
			rasterStroke(local, Vertex{Position: a.Start, Color: a.Color}, Vertex{Position: a.End, Color: a.Color}, lineWidth(a.Width)*px, a.HeadSize*px, 0, put)
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
