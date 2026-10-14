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
	img := shrink(softwareDraw(big, s), s, f.Linear)
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
	// Surfaces are shaded in 0..255, in linear light or as encoded, as the
	// frame works; pixels are 8-bit sRGB.
	linear, ambient := f.Linear, ambientLight(&f)
	toPixel := func(v float32) uint8 {
		if linear {
			return encode8(v / 255)
		}
		return uint8(max(0, min(255, v+.5)))
	}
	fromPixel := func(p uint8) float32 {
		if linear {
			return srgbDecode[p] * 255
		}
		return float32(p)
	}
	// Fog mixes toward its color, in the working space.
	fogged := f.Fog.on()
	fc := f.Fog.color(&f)
	fogWork := [3]float32{fromPixel(fc.R), fromPixel(fc.G), fromPixel(fc.B)}
	fogRGB := func(rgb *[3]float32, d float32) {
		if k := f.Fog.amount(d); k > 0 {
			for i := range rgb {
				rgb[i] += (fogWork[i] - rgb[i]) * k
			}
		}
	}
	fogColor := func(c color.RGBA, d float32) color.RGBA {
		if !fogged {
			return c
		}
		rgb := [3]float32{fromPixel(c.R), fromPixel(c.G), fromPixel(c.B)}
		fogRGB(&rgb, d)
		return color.RGBA{toPixel(rgb[0]), toPixel(rgb[1]), toPixel(rgb[2]), c.A}
	}
	// strokeFog fogs a stroke from world points a to b, or is nil.
	strokeFog := func(a, b nt3dmath.Vec3) func(color.RGBA, float32) color.RGBA {
		if !fogged {
			return nil
		}
		return func(c color.RGBA, t float32) color.RGBA { return fogColor(c, f.eyeDistance(a.Add(b.Sub(a).Scale(t)))) }
	}
	// blend mixes a transparent fragment over the pixel, if no surface
	// hides it, leaving the depth for what lies behind.
	blend := func(x, y int, z float32, rgb [3]float32, alpha float32) {
		if x < 0 || y < 0 || x >= f.Width || y >= f.Height || z < 0 || z > 1 {
			return
		}
		i := y*f.Width + x
		if z <= depth[i]+.00001 {
			alpha = max(0, min(1, alpha))
			off := i * 4
			for k, v := range rgb {
				img.Pix[off+k] = toPixel(v*alpha + fromPixel(img.Pix[off+k])*(1-alpha))
			}
		}
	}
	// raster fills a clip-space triangle that lies past the near plane, if
	// it shows the side drawn: front faces, or for transparent geometry's
	// first pass, back faces. A transparent material's corners carry their
	// opacity in cut.
	raster := func(a, b, c *clipCorner, material *Material, side int) {
		textured := material != nil && material.Texture != nil
		cutout := textured && material.Cutout
		blended := side != opaqueFaces
		specular := material != nil && material.Specular > 0 && !material.Unlit
		x0, y0, z0 := (a.x/a.w+1)*float32(f.Width)*.5, (1-a.y/a.w)*float32(f.Height)*.5, a.z/a.w
		x1, y1, z1 := (b.x/b.w+1)*float32(f.Width)*.5, (1-b.y/b.w)*float32(f.Height)*.5, b.z/b.w
		x2, y2, z2 := (c.x/c.w+1)*float32(f.Width)*.5, (1-c.y/c.w)*float32(f.Height)*.5, c.z/c.w
		area := (x1-x0)*(y2-y0) - (y1-y0)*(x2-x0)
		if side == backFaces && area <= .000001 || side != backFaces && area >= -.000001 {
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
				mix := func(k int) float32 { return u*a.shade[k] + v*b.shade[k] + t*c.shade[k] }
				rgb := [3]float32{mix(0), mix(1), mix(2)}
				var opacity float32
				if blended {
					q0, q1, q2 := u/a.w, v/b.w, t/c.w
					opacity = (q0*a.cut + q1*b.cut + q2*c.cut) / (q0 + q1 + q2)
				}
				if textured {
					q0, q1, q2 := u/a.w, v/b.w, t/c.w
					sum := q0 + q1 + q2
					q0 /= sum
					q1 /= sum
					q2 /= sum
					tex := material.Texture.sampleWrap(UV{q0*a.uv.U + q1*b.uv.U + q2*c.uv.U, q0*a.uv.V + q1*b.uv.V + q2*c.uv.V}, material.Repeat)
					if cutout && float32(tex.A) < (q0*a.cut+q1*b.cut+q2*c.cut)*255 {
						continue // Cut out: neither color nor depth.
					}
					// Lit white, or the tint, times the texture.
					lit := func(k int) float32 { return (q0*a.shade[k] + q1*b.shade[k] + q2*c.shade[k]) / 255 }
					rgb = [3]float32{fromPixel(tex.R) * lit(0), fromPixel(tex.G) * lit(1), fromPixel(tex.B) * lit(2)}
					opacity *= float32(tex.A) / 255
				}
				if specular || fogged {
					q0, q1, q2 := u/a.w, v/b.w, t/c.w
					sum := q0 + q1 + q2
					q0, q1, q2 = q0/sum, q1/sum, q2/sum
					// The world point, exact under perspective: distance from
					// the camera is not affine, so it is found here.
					p := a.world.Scale(q0).Add(b.world.Scale(q1)).Add(c.world.Scale(q2))
					if specular {
						n := a.normal.Scale(q0).Add(b.normal.Scale(q1)).Add(c.normal.Scale(q2))
						if s := f.highlight(material, n, p) * 255; s > 0 {
							rgb[0], rgb[1], rgb[2] = rgb[0]+s, rgb[1]+s, rgb[2]+s
						}
					}
					if fogged {
						fogRGB(&rgb, f.eyeDistance(p))
					}
				}
				if blended {
					blend(x, y, u*z0+v*z1+t*z2, rgb, opacity)
					continue
				}
				put(x, y, u*z0+v*z1+t*z2, color.RGBA{toPixel(rgb[0]), toPixel(rgb[1]), toPixel(rgb[2]), 255})
			}
		}
	}
	// tri draws a local-space triangle: m takes it to clip space, and normals
	// takes its normals to world space, where the light is. A triangle that
	// crosses the near plane is cut there, as the GPU does, not discarded.
	// side chooses the faces drawn, and how: opaque front faces, or a
	// transparent geometry's back or front faces.
	tri := func(m, world, normals *nt3dmath.Mat4, vertices []Vertex, index [3]uint32, uv []UV, material *Material, side int) {
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
		if whole && (min(corners[0].w, corners[1].w, corners[2].w) <= 0 || facing(&corners[0], &corners[1], &corners[2]) == (side == backFaces)) {
			return
		}
		unlit, textured := material != nil && material.Unlit, material != nil && material.Texture != nil
		for k, i := range index {
			c, n := vertices[i].Color, vertices[i].Normal
			if side == backFaces {
				n = n.Scale(-1) // Lit as seen from behind.
			}
			if side != opaqueFaces {
				corners[k].cut = float32(c.A) / 255
			} else if textured && material.EdgeOn {
				corners[k].cut = edgeOnCut(material.threshold(), normals.Direction(n), f.viewAt(world.Point(vertices[i].Position)))
			} else if material != nil {
				corners[k].cut = material.threshold()
			}
			if unlit {
				n = nt3dmath.Vec3{}
			}
			corners[k].world = world.Point(vertices[i].Position)
			if dir := normals.Direction(n); dir.Dot(dir) != 0 {
				corners[k].normal = dir.Normalize()
			}
			if textured && !material.Tint {
				c = color.RGBA{255, 255, 255, 255}
			}
			if linear {
				level, dir := float32(1), normals.Direction(n)
				if dir.Dot(dir) != 0 {
					level = ambient + (1-ambient)*max(0, dir.Normalize().Dot(f.Light.Direction.Normalize()))
				}
				corners[k].shade = [3]float32{srgbDecode[c.R] * 255 * level, srgbDecode[c.G] * 255 * level, srgbDecode[c.B] * 255 * level}
				continue
			}
			lit := shade(c, normals.Direction(n), f.Light)
			corners[k].shade = [3]float32{float32(lit.R), float32(lit.G), float32(lit.B)}
		}
		if whole {
			raster(&corners[0], &corners[1], &corners[2], material, side)
			return
		}
		var storage [4]clipCorner
		polygon := clipNear(corners, &storage)
		for k := 1; k+1 < len(polygon); k++ {
			if polygon[0].w > 0 && polygon[k].w > 0 && polygon[k+1].w > 0 {
				raster(&polygon[0], &polygon[k], &polygon[k+1], material, side)
			}
		}
	}
	for i := 0; i+1 < len(f.DynamicLines); i += 2 {
		rasterStroke(f, f.DynamicLines[i], f.DynamicLines[i+1], px, 0, .00002, put, strokeFog(f.DynamicLines[i].Position, f.DynamicLines[i+1].Position))
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
	drawn := f.drawn(px)
	for gi, g := range f.Geometry {
		if !drawn[gi] {
			continue
		}
		inst := instance(f, gi)
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
			c := fogColor(p.Color, f.eyeDistance(inst.World.Point(p.Position)))
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					if dx*dx+dy*dy <= radius*radius {
						put(int(x)+dx, int(y)+dy, z, c)
					}
				}
			}
		}
		if !transparent(&f.Geometry[gi]) {
			for i := 0; i+2 < len(g.Indices); i += 3 {
				tri(&local.Matrix, &inst.World, &normals, g.Vertices, [3]uint32{g.Indices[i], g.Indices[i+1], g.Indices[i+2]}, g.UV, g.Material, opaqueFaces)
			}
		}
		for _, b := range g.Boxes {
			v := boxVertices(b)
			for j := 0; j < len(v); j += 3 {
				tri(&local.Matrix, &inst.World, &normals, v, [3]uint32{uint32(j), uint32(j + 1), uint32(j + 2)}, nil, nil, opaqueFaces)
			}
		}
		lineStep := 2 * sampled(len(g.Lines)/2, 20000)
		for i := 0; i+1 < len(g.Lines); i += lineStep {
			rasterStroke(local, g.Lines[i], g.Lines[i+1], lineWidth(g.LineWidth)*px, 0, -.00001, put, strokeFog(inst.World.Point(g.Lines[i].Position), inst.World.Point(g.Lines[i+1].Position)))
		}
		arrowStep := sampled(len(g.Arrows), 10000)
		for i := 0; i < len(g.Arrows); i += arrowStep {
			a := g.Arrows[i]
			rasterStroke(local, Vertex{Position: a.Start, Color: a.Color}, Vertex{Position: a.End, Color: a.Color}, lineWidth(a.Width)*px, a.HeadSize*px, 0, put, strokeFog(inst.World.Point(a.Start), inst.World.Point(a.End)))
		}
	}
	// Transparent triangles blend over everything opaque, farthest first.
	for _, gi := range f.blendOrder(drawn) {
		g, inst := &f.Geometry[gi], instance(f, gi)
		m := f.Matrix.Mul(inst.World)
		inverse, _ := inst.World.InverseAffine()
		normals := inverse.Transpose()
		for _, side := range []int{backFaces, frontFaces} {
			for i := 0; i+2 < len(g.Indices); i += 3 {
				tri(&m, &inst.World, &normals, g.Vertices, [3]uint32{g.Indices[i], g.Indices[i+1], g.Indices[i+2]}, g.UV, g.Material, side)
			}
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
