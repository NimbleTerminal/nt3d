// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// softwareRender draws a frame, at its supersampling factor if it has one:
// the scene at that multiple of its size, averaged back down, with overlays
// drawn after, at the frame's own size.
func softwareRender(f Frame) image.Image {
	img, _ := softwareRenderDepth(f)
	return img
}

// softwareRenderDepth is softwareRender with its depth buffer, at the
// frame's size: each pixel's depth, or noDepth where nothing is. A
// supersampled pixel's is the nearest of its block.
func softwareRenderDepth(f Frame) (*image.RGBA, []float32) {
	s := supersampleFactor(f)
	if s == 1 {
		return softwareDraw(f, 1)
	}
	big := f
	big.Width, big.Height, big.Overlays = f.Width*s, f.Height*s, nil
	bigImg, bigDepth := softwareDraw(big, s)
	img := shrink(bigImg, s, f.Linear)
	depth := make([]float32, f.Width*f.Height)
	for y := range f.Height {
		for x := range f.Width {
			d := float32(noDepth)
			for j := range s {
				for i := range s {
					d = min(d, bigDepth[(y*s+j)*big.Width+x*s+i])
				}
			}
			depth[y*f.Width+x] = d
		}
	}
	drawOverlays(img, f)
	return img, depth
}

// noDepth is the depth buffer's value where nothing is drawn: past the far
// plane, at 1.
const noDepth = 2

// softwareDraw draws a frame whose pixel sizes (points, line widths, arrow
// heads) are scaled by scale, as a supersampled frame's are.
func softwareDraw(f Frame, scale int) (*image.RGBA, []float32) {
	px := float32(scale)
	img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	depth := make([]float32, f.Width*f.Height)
	for i := range depth {
		depth[i] = noDepth
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
	linear, rig, aniso := f.Linear, newRig(&f), f.anisotropy()
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
		// A triangle with normals is lit; unlit ones have none.
		lit := a.normal != (nt3dmath.Vec3{}) || b.normal != (nt3dmath.Vec3{}) || c.normal != (nt3dmath.Vec3{})
		// Whether pixels need the world point, and so perspective weights.
		located := fogged || lit && (specular || rig.points)
		perspective := blended || textured || lit || fogged
		x0, y0, z0 := (a.x/a.w+1)*float32(f.Width)*.5, (1-a.y/a.w)*float32(f.Height)*.5, a.z/a.w
		x1, y1, z1 := (b.x/b.w+1)*float32(f.Width)*.5, (1-b.y/b.w)*float32(f.Height)*.5, b.z/b.w
		x2, y2, z2 := (c.x/c.w+1)*float32(f.Width)*.5, (1-c.y/c.w)*float32(f.Height)*.5, c.z/c.w
		area := (x1-x0)*(y2-y0) - (y1-y0)*(x2-x0)
		if side == backFaces && area <= .000001 || side != backFaces && area >= -.000001 {
			return
		} // CCW faces become clockwise with screen Y down
		// A pixel center on an edge belongs to one triangle, by the top-left
		// rule the GPU follows, so a transparent shared edge blends once.
		// Each edge runs as it would around the triangle wound clockwise on
		// screen (positive area): a top edge runs right along a row, a left
		// edge up.
		sign := float32(1)
		if area < 0 {
			sign = -1
		}
		owns := func(xa, ya, xb, yb float32) bool {
			dx, dy := (xb-xa)*sign, (yb-ya)*sign
			return dy < 0 || dy == 0 && dx > 0
		}
		own0, own1, own2 := owns(x1, y1, x2, y2), owns(x2, y2, x0, y0), owns(x0, y0, x1, y1)
		loX, hiX := max(0, int(math.Floor(float64(min(x0, x1, x2))))), min(f.Width-1, int(math.Ceil(float64(max(x0, x1, x2)))))
		loY, hiY := max(0, int(math.Floor(float64(min(y0, y1, y2))))), min(f.Height-1, int(math.Ceil(float64(max(y0, y1, y2)))))
		for y := loY; y <= hiY; y++ {
			for x := loX; x <= hiX; x++ {
				px, py := float32(x)+.5, float32(y)+.5
				e0 := edge(x1, y1, x2, y2, px, py) * sign
				e1 := edge(x2, y2, x0, y0, px, py) * sign
				e2 := edge(x0, y0, x1, y1, px, py) * sign
				if e0 < 0 || e1 < 0 || e2 < 0 || e0 == 0 && !own0 || e1 == 0 && !own1 || e2 == 0 && !own2 {
					continue
				}
				u, v := e0*sign/area, e1*sign/area
				t := 1 - u - v
				mix := func(k int) float32 { return u*a.shade[k] + v*b.shade[k] + t*c.shade[k] }
				rgb := [3]float32{mix(0), mix(1), mix(2)}
				// Perspective-correct weights, for what varies other than
				// linearly across the screen.
				var q0, q1, q2 float32
				if perspective {
					q0, q1, q2 = u/a.w, v/b.w, t/c.w
					sum := q0 + q1 + q2
					q0, q1, q2 = q0/sum, q1/sum, q2/sum
				}
				var opacity float32
				if blended {
					opacity = q0*a.cut + q1*b.cut + q2*c.cut
				}
				if textured {
					uv := UV{q0*a.uv.U + q1*b.uv.U + q2*c.uv.U, q0*a.uv.V + q1*b.uv.V + q2*c.uv.V}
					// Cutouts sample full size; other textures the mip level
					// for how far their texels spread across a pixel, found
					// from the coordinates a pixel right and a pixel down, as
					// the GPU's derivatives find it.
					var tex [4]float32
					if cutout {
						tex = material.Texture.sampleLinear(uv, material.Repeat, 0)
					} else {
						tex = material.Texture.sampleFootprint(uv, uvAt(a, b, c, x0, y0, x1, y1, x2, y2, area, px+1, py), uvAt(a, b, c, x0, y0, x1, y1, x2, y2, area, px, py+1), material.Repeat, aniso)
					}
					if cutout && tex[3] < q0*a.cut+q1*b.cut+q2*c.cut {
						continue // Cut out: neither color nor depth.
					}
					// White, or the tint, times the texture, in the working
					// space: linear light, or encoded.
					tint := func(k int) float32 { return (q0*a.shade[k] + q1*b.shade[k] + q2*c.shade[k]) / 255 }
					work := func(v float32) float32 {
						if linear {
							return v * 255
						}
						return float32(encodeSRGB(float64(v))) * 255
					}
					rgb = [3]float32{work(tex[0]) * tint(0), work(tex[1]) * tint(1), work(tex[2]) * tint(2)}
					opacity *= tex[3]
				}
				if lit || fogged {
					// The world point, exact under perspective: distance from
					// the camera and from point lights is not affine, so it
					// is found here, when something needs it.
					var p nt3dmath.Vec3
					if located {
						p = a.world.Scale(q0).Add(b.world.Scale(q1)).Add(c.world.Scale(q2))
					}
					if n := a.normal.Scale(q0).Add(b.normal.Scale(q1)).Add(c.normal.Scale(q2)); lit && n.Dot(n) != 0 {
						n = n.Scale(1 / float32(math.Sqrt(float64(n.Dot(n)))))
						rig.light(&rgb, n, p)
						if specular {
							s := rig.highlight(material, n, p, f.viewAt(p))
							for k := range rgb {
								rgb[k] += s[k] * 255
							}
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
			// The surface's own color, in the working space; it is lit
			// per pixel.
			if linear {
				corners[k].shade = [3]float32{srgbDecode[c.R] * 255, srgbDecode[c.G] * 255, srgbDecode[c.B] * 255}
			} else {
				corners[k].shade = [3]float32{float32(c.R), float32(c.G), float32(c.B)}
			}
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
			// A disc of the point's radius covers the pixels whose centers
			// it holds, as the GPU's quad does: one whose center is off
			// screen may still hang into view. Only depth clips it.
			p := g.Points[i]
			cx, cy, cz, cw := local.Matrix.Transform(p.Position)
			if cw <= 0 || cz < 0 || cz > cw {
				continue
			}
			x, y, z := (cx/cw+1)*float32(f.Width)*.5, (1-cy/cw)*float32(f.Height)*.5, cz/cw
			radius := p.Radius * px
			c := fogColor(p.Color, f.eyeDistance(inst.World.Point(p.Position)))
			loX, hiX := max(0, int(math.Floor(float64(x-radius)))), min(f.Width-1, int(math.Ceil(float64(x+radius))))
			loY, hiY := max(0, int(math.Floor(float64(y-radius)))), min(f.Height-1, int(math.Ceil(float64(y+radius))))
			for sy := loY; sy <= hiY; sy++ {
				for sx := loX; sx <= hiX; sx++ {
					if dx, dy := float32(sx)+.5-x, float32(sy)+.5-y; dx*dx+dy*dy <= radius*radius {
						put(sx, sy, z, c)
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
			rasterStroke(local, g.Lines[i], g.Lines[i+1], lineWidth(g.LineWidth)*px, 0, lineBias, put, strokeFog(inst.World.Point(g.Lines[i].Position), inst.World.Point(g.Lines[i+1].Position)))
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
	return img, depth
}

// edge is twice the signed area of the triangle a, b, p: zero on the line
// through a and b. Each product is rounded before subtracting, so the
// compiler cannot fuse them: the edge from b to a is then exactly the
// negative, and triangles sharing it agree which side a pixel is on.
func edge(xa, ya, xb, yb, px, py float32) float32 {
	return float32((xa-px)*(yb-py)) - float32((ya-py)*(xb-px))
}

// uvAt is a triangle's perspective-correct texture coordinate at a screen
// point, inside it or not.
func uvAt(a, b, c *clipCorner, x0, y0, x1, y1, x2, y2, area, px, py float32) UV {
	u := ((x1-px)*(y2-py) - (y1-py)*(x2-px)) / area
	v := ((x2-px)*(y0-py) - (y2-py)*(x0-px)) / area
	q0, q1, q2 := u/a.w, v/b.w, (1-u-v)/c.w
	sum := q0 + q1 + q2
	return UV{(q0*a.uv.U + q1*b.uv.U + q2*c.uv.U) / sum, (q0*a.uv.V + q1*b.uv.V + q2*c.uv.V) / sum}
}

// textureLOD is the mip level for texture coordinates that move to dx and
// dy a pixel right and down: log2 of the larger step, in texels, as the GPU
// chooses.
func textureLOD(t *Texture, uv, dx, dy UV) float32 {
	w, h := float64(t.width), float64(t.height)
	sx := math.Hypot(float64(dx.U-uv.U)*w, float64(dx.V-uv.V)*h)
	sy := math.Hypot(float64(dy.U-uv.U)*w, float64(dy.V-uv.V)*h)
	rho := max(sx, sy)
	if !(rho > 1) {
		return 0
	}
	return float32(math.Log2(rho))
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
