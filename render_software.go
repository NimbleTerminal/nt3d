// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"math"
	"slices"

	"nimbleterminal.dev/nt3d/math"
)

// softwareRender draws a frame, at its supersampling factor if it has one:
// the scene at that multiple of its size, averaged back down, with overlays
// drawn after, at the frame's own size.
func softwareRender(f Frame) image.Image {
	img, _ := softwareRenderDepth(f)
	return img
}

// softwareRenderScratch uses private raster depth; it never returns that slice.
// Unlike RenderSoftware, image-only rendering needs no reduced depth result.
func softwareRenderScratch(f Frame, depth []float32) image.Image {
	s := supersampleFactor(f)
	if f.PostProcess.on() {
		if s == 1 {
			scene := f
			scene.Overlays = nil
			img, _, acc := softwareDrawDepthHDR(scene, 1, depth, true)
			applyPostSoftware(f, img, acc)
			drawOverlays(img, f)
			return img
		}
		big := f
		big.Width, big.Height, big.Overlays = f.Width*s, f.Height*s, nil
		_, bigDepth, bigAcc := softwareDrawDepthHDR(big, s, depth, true)
		acc1x, _ := shrinkPost(f.Width, f.Height, s, bigAcc, bigDepth, f.Linear)
		img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
		fLin := f
		fLin.Linear = true
		applyPostSoftware(fLin, img, acc1x)
		drawOverlays(img, f)
		return img
	}
	if s == 1 {
		img, _ := softwareDrawDepth(f, 1, depth)
		return img
	}
	big := f
	big.Width, big.Height, big.Overlays = f.Width*s, f.Height*s, nil
	bigImg, _ := softwareDrawDepth(big, s, depth)
	img := shrink(bigImg, s, f.Linear)
	drawOverlays(img, f)
	return img
}

// softwareRenderDepth is softwareRender with its depth buffer, at the
// frame's size: each pixel's depth, or noDepth where nothing is. A
// supersampled pixel's is the nearest of its block.
func softwareRenderDepth(f Frame) (*image.RGBA, []float32) {
	s := supersampleFactor(f)
	if f.PostProcess.on() {
		if s == 1 {
			scene := f
			scene.Overlays = nil
			img, depth, acc := softwareDrawDepthHDR(scene, 1, make([]float32, f.Width*f.Height), true)
			applyPostSoftware(f, img, acc)
			drawOverlays(img, f)
			return img, depth
		}
		big := f
		big.Width, big.Height, big.Overlays = f.Width*s, f.Height*s, nil
		_, bigDepth, bigAcc := softwareDrawDepthHDR(big, s, make([]float32, big.Width*big.Height), true)
		acc1x, depth := shrinkPost(f.Width, f.Height, s, bigAcc, bigDepth, f.Linear)
		img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
		fLin := f
		fLin.Linear = true
		applyPostSoftware(fLin, img, acc1x)
		drawOverlays(img, f)
		return img, depth
	}
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
	return softwareDrawDepth(f, scale, make([]float32, f.Width*f.Height))
}

// depth is exactly Width*Height values and is reset even on equal-area resizes.
func softwareDrawDepth(f Frame, scale int, depth []float32) (*image.RGBA, []float32) {
	img, d, _ := softwareDrawDepthHDR(f, scale, depth, false)
	return img, d
}

func softwareDrawDepthHDR(f Frame, scale int, depth []float32, wantHDR bool) (*image.RGBA, []float32, []float32) {
	var ao []float32
	if f.PostProcess.SSAO.on() && f.Complete {
		_, d, _ := softwareDrawWithAO(f, scale, depth, false, nil, true)
		ao = softwareSSAO(f, d)
	}
	return softwareDrawWithAO(f, scale, depth, wantHDR, ao, false)
}

func softwareDrawWithAO(f Frame, scale int, depth []float32, wantHDR bool, ao []float32, prepass bool) (*image.RGBA, []float32, []float32) {
	px := float32(scale)
	inverseScene, _ := f.Matrix.Inverse()
	right, up, _ := f.skyBasis()
	right, up = right.Normalize(), up.Normalize()
	img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	for i := range depth {
		depth[i] = noDepth
		off := i * 4
		img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3] = f.Background.R, f.Background.G, f.Background.B, 255
	}
	// Surfaces are shaded in 0..255, in linear light or as encoded, as the
	// frame works; pixels are 8-bit sRGB.
	linear, rig := f.Linear, newRig(&f)
	drawn := f.drawn(px)
	if rig.shadowLight >= 0 && !prepass {
		rig.shadow = buildShadowMap(&f, drawn, softwareShadowSize(&f))
	}
	dst := softwareTarget{img: img, depth: depth, width: f.Width, height: f.Height, linear: linear}
	mesh := softwareMeshPass{frame: &f, dst: &dst, rig: &rig, scale: px, aniso: f.anisotropy(),
		prepass: prepass, ao: ao, inverseScene: inverseScene, right: right, up: up}
	if !prepass && (wantHDR || (f.Environment != nil && f.Environment.hdr && linear)) {
		dst.accumulate()
	}
	if f.Environment != nil && !prepass {
		right, up, forward := f.skyBasis()
		for y := 0; y < f.Height; y++ {
			row := forward.Add(up.Scale(1 - 2*(float32(y)+.5)/float32(f.Height)))
			for x := 0; x < f.Width; x++ {
				ray := row.Add(right.Scale(2*(float32(x)+.5)/float32(f.Width) - 1)).Normalize()
				c := f.Environment.sample(ray)
				px := color.RGBA{encode8(c[0]), encode8(c[1]), encode8(c[2]), 255}
				img.SetRGBA(x, y, px)
				if dst.acc != nil {
					i := (y*f.Width + x) * 3
					if linear {
						dst.acc[i], dst.acc[i+1], dst.acc[i+2] = max(0, c[0])*255, max(0, c[1])*255, max(0, c[2])*255
					} else {
						dst.acc[i], dst.acc[i+1], dst.acc[i+2] = float32(px.R), float32(px.G), float32(px.B)
					}
				}
			}
		}
	}
	// Fog mixes toward its color, in the working space.
	fogged := f.Fog.on()
	fc := f.Fog.color(&f)
	mesh.fogColor = [3]float32{dst.fromPixel(fc.R), dst.fromPixel(fc.G), dst.fromPixel(fc.B)}
	fogColor := func(c color.RGBA, d float32) color.RGBA {
		if !fogged {
			return c
		}
		rgb := [3]float32{dst.fromPixel(c.R), dst.fromPixel(c.G), dst.fromPixel(c.B)}
		mesh.fogRGB(&rgb, d)
		return color.RGBA{dst.toPixel(rgb[0]), dst.toPixel(rgb[1]), dst.toPixel(rgb[2]), c.A}
	}
	// strokeFog fogs a stroke from world points a to b, or is nil.
	strokeFog := func(a, b nt3dmath.Vec3) func(color.RGBA, float32) color.RGBA {
		if !fogged {
			return nil
		}
		return func(c color.RGBA, t float32) color.RGBA { return fogColor(c, f.eyeDistance(a.Add(b.Sub(a).Scale(t)))) }
	}
	dynWidth := lineWidth(f.DynamicLineWidth) * px
	for i := 0; i+1 < len(f.DynamicLines); i += 2 {
		rasterStroke(f, f.DynamicLines[i], f.DynamicLines[i+1], dynWidth, 0, .00002, dst.put, strokeFog(f.DynamicLines[i].Position, f.DynamicLines[i+1].Position))
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
	// pointStep is how many points a geometry's drawing steps over: interactive
	// frames draw every step-th point, and a blended one stands for step of
	// them, its alpha multiplied (to 1), so a sampled cloud keeps about its
	// brightness. Gaussians are never sampled.
	pointStep := func(g *Geometry) int {
		if g.PointStyle.Shape == GaussianPoint {
			return 1
		}
		return sampled(len(g.Points), 10000)
	}
	// drawPoint draws point i of a geometry, standing for step points, m
	// taking them to clip space and shFromWorld its spherical harmonics'
	// directions from world, and a Gaussian's axes grown by scale (a thinned
	// cluster's splats grow to keep its coverage): a disc of its radius on screen, covering the
	// pixels whose centers it holds, as the GPU's quad does. One whose center
	// is off screen may still hang into view; only depth clips it.
	drawPoint := func(g *Geometry, world nt3dmath.Mat4, m *nt3dmath.Mat4, i, step int, shFromWorld nt3dmath.Mat4, scale float32) {
		style := g.PointStyle
		p := g.Points[i]
		cx, cy, cz, cw := m.Transform(p.Position)
		if cw <= 0 || cz < 0 || cz > cw {
			return
		}
		x, y, z := (cx/cw+1)*float32(f.Width)*.5, (1-cy/cw)*float32(f.Height)*.5, cz/cw
		r := p.Radius
		if style.Shape != GaussianPoint && scale != 1 {
			r *= scale
			if style.Units == PixelUnits {
				r = min(32, r)
			}
		}
		radius := f.pointRadius(style, r, cw, px)
		var ellipse gaussianEllipse
		if style.Shape == GaussianPoint {
			q := p
			if scale != 1 {
				for k := range q.GaussianAxes {
					q.GaussianAxes[k] = q.GaussianAxes[k].Scale(scale)
				}
			}
			ellipse = projectGaussian(q, m, f.Width, f.Height, px)
			radius = ellipse.radius
		}
		distance := f.eyeDistance(world.Point(p.Position))
		c := p.Color
		rgb := [3]float32{dst.fromPixel(c.R), dst.fromPixel(c.G), dst.fromPixel(c.B)}
		if len(g.GaussianSH) > 0 {
			rgb = f.gaussianColor(g, i, world, shFromWorld)
			for k := range rgb {
				rgb[k] *= 255
			}
		}
		alpha := min(1, float32(c.A)/255*float32(step))
		if style.Shape == GaussianPoint {
			alpha = min(.99, alpha)
		}
		if style.Texture == nil {
			switch style.Blend {
			case OpaqueBlend:
				c = fogColor(c, distance)
			case AlphaBlend:
				if fogged {
					mesh.fogRGB(&rgb, distance)
				}
			case AdditiveBlend:
				// Fog fades an additive point out, rather than toward its color.
				alpha *= 1 - f.Fog.amount(distance)
			}
		}
		loX, hiX := max(0, int(math.Floor(float64(x-radius)))), min(f.Width-1, int(math.Ceil(float64(x+radius))))
		loY, hiY := max(0, int(math.Floor(float64(y-radius)))), min(f.Height-1, int(math.Ceil(float64(y+radius))))
		for sy := loY; sy <= hiY; sy++ {
			for sx := loX; sx <= hiX; sx++ {
				dx, dy := float32(sx)+.5-x, float32(sy)+.5-y
				pixelAlpha := alpha
				if style.Blend != OpaqueBlend && style.SoftDistance > 0 && depth[sy*f.Width+sx] < 1 {
					pixelAlpha *= min(1, sceneGap(inverseScene, float32(sx)+.5, float32(sy)+.5, z, depth[sy*f.Width+sx], f.Width, f.Height)/style.SoftDistance)
				}
				if style.Texture != nil {
					if dx < -radius || dx >= radius || dy < -radius || dy >= radius {
						continue
					}
					tex := style.spriteSample(p, (dx/radius+1)*.5, (dy/radius+1)*.5)
					if style.Blend == OpaqueBlend && tex[3] < .5 {
						continue
					}
					tinted := rgb
					for k := range tinted {
						v := tex[k]
						if !linear {
							v = float32(encodeSRGB(float64(v)))
						}
						tinted[k] *= v
					}
					opacity := pixelAlpha * tex[3]
					if style.Blend == AdditiveBlend {
						dst.add(sx, sy, z, tinted, opacity*(1-f.Fog.amount(distance)))
					} else {
						if fogged {
							mesh.fogRGB(&tinted, distance)
						}
						if style.Blend == AlphaBlend {
							dst.blend(sx, sy, z, tinted, opacity)
						} else {
							dst.putWork(sx, sy, z, tinted)
						}
					}
					continue
				}
				d2 := dx*dx + dy*dy
				if style.Shape == GaussianPoint {
					u := dx / ellipse.a
					v := (dy - ellipse.b*u) / ellipse.c
					d2 = (u*u + v*v) * radius * radius / 9
				}
				if d2 > radius*radius {
					continue
				}
				switch style.Blend {
				case OpaqueBlend:
					dst.put(sx, sy, z, c)
				case AlphaBlend:
					dst.blend(sx, sy, z, rgb, pixelAlpha*softness(style.Shape, d2/(radius*radius)))
				default:
					dst.add(sx, sy, z, rgb, pixelAlpha*softness(style.Shape, d2/(radius*radius)))
				}
			}
		}
	}
	// drawPointsUnsorted draws a geometry's points in their own order: opaque
	// ones with the opaque pass, additive ones where the blended pass puts
	// their geometry.
	drawPointsUnsorted := func(g *Geometry, world nt3dmath.Mat4, m *nt3dmath.Mat4) {
		inverse, _ := world.InverseAffine()
		shFromWorld := gaussianSHMatrix(g).Mul(inverse)
		step := pointStep(g)
		for i := 0; i < len(g.Points); i += step {
			drawPoint(g, world, m, i, step, shFromWorld, 1)
		}
	}
	for gi := range f.Draws {
		g := f.Draws[gi].Geometry
		if !drawn[gi] {
			continue
		}
		world := f.Draws[gi].world()
		// Transform as primitives are drawn rather than copying the scene.
		local := f
		local.Matrix = f.Matrix.Mul(world)
		inverse, _ := world.InverseAffine()
		normals := inverse.Transpose()
		if !blendedPoints(&f.Draws[gi].Geometry) {
			drawPointsUnsorted(&f.Draws[gi].Geometry, world, &local.Matrix)
		}
		if !transparent(&f.Draws[gi].Geometry) && !transmitting(&f.Draws[gi].Geometry) {
			verts := displacedVertices(&g, world)
			mesh.drawMesh(&local.Matrix, &world, &normals, &g, verts, g.Material, opaqueFaces)
		}
		for _, b := range g.Boxes {
			v := boxVertices(b)
			for j := 0; j < len(v); j += 3 {
				mesh.drawTriangle(&local.Matrix, &world, &normals, v, [3]uint32{uint32(j), uint32(j + 1), uint32(j + 2)}, nil, nil, nil, nil, nil, opaqueFaces, OutlineStyle{}, false)
			}
		}
		lineStep := 2 * sampled(len(g.Lines)/2, 20000)
		for i := 0; i+1 < len(g.Lines); i += lineStep {
			rasterStroke(local, g.Lines[i], g.Lines[i+1], lineWidth(g.LineWidth)*px, 0, lineBias, dst.put, strokeFog(world.Point(g.Lines[i].Position), world.Point(g.Lines[i+1].Position)))
		}
		arrowStep := sampled(len(g.Arrows), 10000)
		for i := 0; i < len(g.Arrows); i += arrowStep {
			a := g.Arrows[i]
			rasterStroke(local, Vertex{Position: a.Start, Color: a.Color}, Vertex{Position: a.End, Color: a.Color}, lineWidth(a.Width)*px, a.HeadSize*px, 0, dst.put, strokeFog(world.Point(a.Start), world.Point(a.End)))
		}
	}

	if prepass {
		return img, depth, dst.acc
	}

	// Opaque hulls share the surface depth buffer. Draw after all opaque
	// geometry so nearer surfaces hide them regardless of scene order.
	drawOutline := func(gi int, side int) {
		g := &f.Draws[gi].Geometry
		if !drawn[gi] || g.Outline.Width == 0 {
			return
		}
		world := f.Draws[gi].world()
		m := f.Matrix.Mul(world)
		inverse, _ := world.InverseAffine()
		normals := inverse.Transpose()
		// Hulls keep alpha sampling, but never lighting or normal-map detail.
		material := Material{Shading: ShadeUnlit}
		if g.Material != nil {
			material = *g.Material
			material.Shading = ShadeUnlit
			material.Shader = nil
		}
		verts := displacedVertices(g, world)
		if f.Draws[gi].state.posed == nil && g.Displacement == nil {
			if codes := outlineNormals(verts); codes != nil {
				verts = slices.Clone(verts)
				for i, code := range codes {
					if code != 0 {
						verts[i].Normal = unpackOutlineNormal(code)
					}
				}
			}
		}

		mesh.drawMesh(&m, &world, &normals, g, verts, &material, side)
	}
	for gi := range f.Draws {
		if !transparent(&f.Draws[gi].Geometry) {
			drawOutline(gi, outlineOpaque)
		}
	}

	// Crease lines are strokes in the outline's color and width along the
	// sharp edges inside a silhouette, depth tested as lines are. Opaque and
	// cutout creases draw here; transmissive creases draw after transmissive
	// surfaces write front-face depth; transparent creases blend with their
	// mesh in OIT or sorted slice order.
	drawCrease := func(gi int) {
		g := &f.Draws[gi].Geometry
		if !drawn[gi] || !creased(g) {
			return
		}
		world := f.Draws[gi].world()
		verts := displacedVertices(g, world)
		segments := cachedCreaseSegments(g, verts)
		if len(segments) == 0 {
			return
		}
		local := f
		local.Matrix = f.Matrix.Mul(world)
		m := &local.Matrix
		ink := color.RGBA{g.Outline.Color.R, g.Outline.Color.G, g.Outline.Color.B, 255}
		isTrans := transparent(g)
		_, creaseBaseAlpha, _ := baseColorFactors(g.Material, false)
		hasTex := g.Material != nil && g.Material.Texture != nil
		hasMatShader := g.Material != nil && g.Material.Shader != nil
		isCutout := !isTrans && g.Material != nil && g.Material.cutout() && (hasTex || hasMatShader)
		inkWork := [3]float32{dst.fromPixel(ink.R), dst.fromPixel(ink.G), dst.fromPixel(ink.B)}
		for i := 0; i+1 < len(segments); i += 2 {
			a, b := segments[i], segments[i+1]
			width := g.Outline.Width * px
			if g.Outline.Units == WorldUnits {
				mid := a.pos.Add(b.pos).Scale(.5)
				width = f.outlineWidth(g.Outline, m[3]*mid.X+m[7]*mid.Y+m[11]*mid.Z+m[15], px)
			}
			wa, wb := world.Point(a.pos), world.Point(b.pos)
			if !isTrans && !isCutout {
				rasterStroke(local, Vertex{Position: a.pos, Color: ink}, Vertex{Position: b.pos, Color: ink}, width, 0, lineBias, dst.put, strokeFog(wa, wb))
				continue
			}
			rasterCreaseStroke(local, a.pos, b.pos, width, lineBias, func(x, y int, z, wt float32) {
				uv := UV{a.uv.U*(1-wt) + b.uv.U*wt, a.uv.V*(1-wt) + b.uv.V*wt}
				wp := wa.Add(wb.Sub(wa).Scale(wt))
				texAlpha := float32(1)
				if hasTex {
					texAlpha = g.Material.Texture.sampleLinear(uv, g.Material.Repeat, 0, false)[3]
				}
				if isCutout {
					alpha := texAlpha
					if hasMatShader {
						alpha = evalMaterialColor(g.Material.Shader, wp, nt3dmath.Vec3{}, f.viewAt(wp), uv, [4]float32{1, 1, 1, alpha})[3]
					}
					if alpha < g.Material.threshold() {
						return
					}
					c := ink
					if fogged {
						c = fogColor(c, f.eyeDistance(wp))
					}
					dst.put(x, y, z, c)
					return
				}
				alpha := (a.opacity*(1-wt) + b.opacity*wt) * creaseBaseAlpha * texAlpha
				if hasMatShader {
					alpha = evalMaterialColor(g.Material.Shader, wp, nt3dmath.Vec3{}, f.viewAt(wp), uv, [4]float32{1, 1, 1, alpha})[3]
				}
				rgb := inkWork
				if fogged {
					mesh.fogRGB(&rgb, f.eyeDistance(wp))
				}
				dst.blend(x, y, z, rgb, alpha)
			})
		}
	}
	for gi := range f.Draws {
		if !transparent(&f.Draws[gi].Geometry) && !transmitting(&f.Draws[gi].Geometry) {
			drawCrease(gi)
		}
	}

	// rasterDisc draws a clipped shadow triangle without writing depth.
	rasterDisc := func(a, b, c *clipCorner) {
		screen := projectTriangle(a, b, c, f.Width, f.Height)
		if screen.area > -.000001 && screen.area < .000001 {
			return
		}
		for y := screen.loY; y <= screen.hiY; y++ {
			for x := screen.loX; x <= screen.hiX; x++ {
				w, ok := screen.coverage(float32(x)+.5, float32(y)+.5)
				if !ok {
					continue
				}
				q := w.perspective(a.w, b.w, c.w)
				uv := q.uv(a.uv, b.uv, c.uv)
				d2 := uv.U*uv.U + uv.V*uv.V
				if d2 > 1 {
					continue
				}
				rgb := [3]float32{}
				if fogged {
					p := a.world.Scale(q.a).Add(b.world.Scale(q.b)).Add(c.world.Scale(q.c))
					mesh.fogRGB(&rgb, f.eyeDistance(p))
				}
				dst.blend(x, y, screen.depth(w)+lineBias, rgb, a.cut*(1-d2)*(1-d2))
			}
		}
	}

	for gi := range f.Draws {
		if !drawn[gi] {
			continue
		}
		center, radius, strength, ok := dropDisc(&f.Draws[gi].Geometry, &f.Draws[gi].state, f.Draws[gi].world())
		if !ok {
			continue
		}
		var corners [4]clipCorner
		for k, uv := range []UV{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
			p := center.Add(nt3dmath.Vec3{X: uv.U * radius, Y: uv.V * radius})
			corners[k].x, corners[k].y, corners[k].z, corners[k].w = f.Matrix.Transform(p)
			corners[k].world, corners[k].uv, corners[k].cut = p, uv, strength
		}
		for _, ids := range [][3]int{{0, 1, 2}, {0, 2, 3}} {
			var storage [4]clipCorner
			polygon := clipNear([3]clipCorner{corners[ids[0]], corners[ids[1]], corners[ids[2]]}, &storage)
			for k := 1; k+1 < len(polygon); k++ {
				if polygon[0].w > 0 && polygon[k].w > 0 && polygon[k+1].w > 0 {
					rasterDisc(&polygon[0], &polygon[k], &polygon[k+1])
				}
			}
		}
	}
	// Capture opaque color and depth once. Transmitters write scene depth but
	// never sample one another, independent of geometry submission order.
	if needsSceneSnapshot(f, drawn) {
		mesh.opaqueColor = append([]byte(nil), img.Pix...)
		mesh.opaqueDepth = append([]float32(nil), depth...)
		for gi := range f.Draws {
			g := &f.Draws[gi].Geometry
			if !drawn[gi] || !transmitting(g) {
				continue
			}
			world := f.Draws[gi].world()
			verts := displacedVertices(g, world)
			m := f.Matrix.Mul(world)
			inv, _ := world.InverseAffine()
			normals := inv.Transpose()
			mesh.drawMesh(&m, &world, &normals, g, verts, g.Material, opaqueFaces)
		}
		for gi := range f.Draws {
			if transmitting(&f.Draws[gi].Geometry) {
				drawCrease(gi)
			}
		}
	}
	if hasOITWork(f, drawn) {
		dst.accumulate()
		dst.oitAccum = make([]float32, len(depth)*4)
		dst.oitReveal = make([]float32, len(depth))
		for i := range dst.oitReveal {
			dst.oitReveal[i] = 1
		}
		dst.oitMode = true
		for gi := range f.Draws {
			g := &f.Draws[gi].Geometry
			if !drawn[gi] || !transparent(g) {
				continue
			}
			drawOutline(gi, outlineTransparent)
			world := f.Draws[gi].world()
			verts := displacedVertices(g, world)
			m := f.Matrix.Mul(world)
			inverse, _ := world.InverseAffine()
			normals := inverse.Transpose()
			for _, side := range []int{backFaces, frontFaces} {
				mesh.drawMesh(&m, &world, &normals, g, verts, g.Material, side)
			}
			drawCrease(gi)
		}
		dst.oitMode = false
		for i, rev := range dst.oitReveal {
			if rev >= .99999 {
				continue
			}
			wSum := max(dst.oitAccum[i*4+3], 1e-5)
			cov := 1 - rev
			off, a := i*4, dst.acc[i*3:i*3+3]
			for k := range 3 {
				avg := dst.oitAccum[i*4+k] / wSum
				a[k] = avg*cov + a[k]*(1-cov)
				img.Pix[off+k] = dst.toPixel(a[k])
			}
		}
	}

	// Blended work goes over everything opaque, far to near in depth slices:
	// each slice's transparent meshes and additive points, then its alpha
	// points, ordered across geometries by the plan's keys, as the GPU does.
	// validateFrame refuses a frame with too many alpha geometries, but it
	// plans at scale 1 and a supersampled culling frame draws a padded mask,
	// so the plan can still run over: the slots beyond the limit then draw
	// unsorted at the slice of their center, rather than overflowing a key.
	plan, err := f.planBlend(drawn)
	overflow := make([][]int, blendSlices)
	if err != nil {
		for _, gi := range plan.slots[maxBlendSlots:] {
			b := blendBounds(&f.Draws[gi].Geometry)
			_, _, z, _ := f.Matrix.Mul(f.Draws[gi].world()).Transform(b.Min.Add(b.Max).Scale(.5))
			s := plan.sliceOf(z)
			overflow[s] = append(overflow[s], gi)
		}
		plan.slots = plan.slots[:maxBlendSlots]
	}
	lod := f.planLODScaled(&plan, scale)
	type slotDraw struct {
		g           *Geometry
		world       nt3dmath.Mat4
		m           nt3dmath.Mat4
		shFromWorld nt3dmath.Mat4
		step        int
	}
	slots := make([]slotDraw, len(plan.slots))
	keys, indices := make([]uint32, 0, plan.points), make([]uint32, 0, plan.points)
	for slot, gi := range plan.slots {
		g, world := &f.Draws[gi].Geometry, f.Draws[gi].world()
		m := f.Matrix.Mul(world)
		inverse, _ := world.InverseAffine()
		slots[slot] = slotDraw{g, world, m, gaussianSHMatrix(g).Mul(inverse), pointStep(g)}
		key := func(i uint32, f8 uint8) {
			_, _, z, w := m.Transform(g.Points[i].Position)
			if w <= 0 || z < 0 || z > w {
				return
			}
			keys, indices = append(keys, plan.key(slot, z)), append(indices, packLOD(i, f8))
		}
		if ranges := lod.slots[slot].ranges; ranges != nil {
			for _, r := range ranges {
				for _, i := range g.Clusters.Order[r.first : r.first+r.count] {
					key(i, r.f8)
				}
			}
			continue
		}
		for i := 0; i < len(g.Points); i += slots[slot].step {
			key(uint32(i), 255)
		}
	}
	var sorter pointSorter
	order := sorter.sortKeys(keys)
	next := 0 // The first sorted point not yet drawn.
	for slice, list := range plan.bySlice {
		for _, gi := range list {
			g, world := &f.Draws[gi].Geometry, f.Draws[gi].world()
			m := f.Matrix.Mul(world)
			if transparent(g) {
				drawOutline(gi, outlineTransparent)
				verts := displacedVertices(g, world)
				inverse, _ := world.InverseAffine()
				normals := inverse.Transpose()
				for _, side := range []int{backFaces, frontFaces} {
					mesh.drawMesh(&m, &world, &normals, g, verts, g.Material, side)
				}
				if !f.OIT {
					drawCrease(gi)
				}
			}
			if blendedPoints(g) && g.PointStyle.Blend == AdditiveBlend {
				drawPointsUnsorted(g, world, &m)
			}
		}
		for _, gi := range overflow[slice] {
			g, world := &f.Draws[gi].Geometry, f.Draws[gi].world()
			m := f.Matrix.Mul(world)
			drawPointsUnsorted(g, world, &m)
		}
		for ; next < len(order) && int(keys[order[next]]>>22) == slice; next++ {
			k := keys[order[next]]
			s := &slots[k>>10&4095]
			i, scale := unpackLOD(indices[order[next]])
			drawPoint(s.g, s.world, &s.m, int(i), s.step, s.shFromWorld, scale)
		}
	}
	drawOverlays(img, f)
	return img, depth, dst.acc
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
