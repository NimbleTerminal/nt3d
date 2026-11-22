// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"

	"nimbleterminal.dev/nt3d/math"
)

// softwareMeshPass holds the frame inputs and target shared by its triangles.
type softwareMeshPass struct {
	frame        *Frame
	dst          *softwareTarget
	rig          *lightRig
	scale        float32
	aniso        int
	prepass      bool
	ao           []float32
	fogColor     [3]float32
	right, up    nt3dmath.Vec3
	inverseScene nt3dmath.Mat4
	opaqueColor  []byte
	opaqueDepth  []float32
}

func (pass *softwareMeshPass) fogRGB(rgb *[3]float32, d float32) {
	if k := pass.frame.Fog.amount(d); k > 0 {
		for i := range rgb {
			rgb[i] += (pass.fogColor[i] - rgb[i]) * k
		}
	}
}

// drawMesh takes the vertex stream selected for this pass, including any
// displacement or outline normals. All map coordinates come from the mesh.
func (pass *softwareMeshPass) drawMesh(m, world, normals *nt3dmath.Mat4, g *Geometry, vertices []Vertex, material *Material, side int) {
	isMapped := mapped(*g)
	for i := 0; i+2 < len(g.Indices); i += 3 {
		index := [3]uint32(g.Indices[i : i+3])
		pass.drawTriangle(m, world, normals, vertices, index, g.UV, g.NormalUV, g.LightMapUV, g.Tangents, material, side, g.Outline, isMapped)
	}
}

// drawTriangle draws a local-space triangle: m takes it to clip space, and normals
// takes its normals to world space, where the light is. A triangle that
// crosses the near plane is cut there, as the GPU does, not discarded.
// side chooses the faces drawn, and how: opaque front faces, or a
// transparent geometry's back or front faces.
func (pass *softwareMeshPass) drawTriangle(m, world, normals *nt3dmath.Mat4, vertices []Vertex, index [3]uint32, uv, normalUV, lightMapUV []UV, tangents []Tangent, material *Material, side int, outline OutlineStyle, isMapped bool) {
	hull := side < 0
	back := side == backFaces || hull
	blended := side == backFaces || side == frontFaces || side == outlineTransparent
	var corners [3]clipCorner
	for k, i := range index {
		if material != nil && material.holdout() && !hull {
			wp := world.Point(vertices[i].Position)
			if material.DepthOffset > 0 {
				dir := pass.frame.Forward
				if pass.frame.Perspective {
					if v := wp.Sub(pass.frame.Eye); v.Dot(v) > 1e-10 {
						dir = v.Normalize()
					}
				}
				wp = wp.Add(dir.Scale(material.DepthOffset))
			}
			corners[k].x, corners[k].y, corners[k].z, corners[k].w = pass.frame.Matrix.Transform(wp)
			continue
		}
		corners[k].x, corners[k].y, corners[k].z, corners[k].w = m.Transform(vertices[i].Position)
		if len(uv) > 0 {
			corners[k].uv = uv[i]
			corners[k].normalUV = uv[i]
			corners[k].flow = materialFlow(material, uv[i])
		}
		if len(lightMapUV) > 0 {
			corners[k].lightMapUV = lightMapUV[i]
		}
		if len(normalUV) > 0 {
			corners[k].normalUV = normalUV[i]
		}
		if len(tangents) > 0 {
			// Tangents turn with positions, not as normals do; a back
			// face's negated normal turns the handedness instead.
			corners[k].tangent, corners[k].tangentW = world.Direction(tangents[i].Direction), tangents[i].W
			if side == backFaces {
				corners[k].tangentW = -tangents[i].W
			}
		}
		if hull {
			expandOutline(&corners[k], normals.Direction(vertices[i].Normal), pass.frame, pass.frame.outlineWidth(outline, corners[k].w, pass.scale))
		}
	}
	// Most triangles lie wholly past the near plane: cull those facing
	// away before lighting them.
	whole := corners[0].z >= 0 && corners[1].z >= 0 && corners[2].z >= 0
	if whole && (min(corners[0].w, corners[1].w, corners[2].w) <= 0 || facing(&corners[0], &corners[1], &corners[2]) == back) {
		return
	}
	unlit, textured := material.unlit(), material != nil && material.Texture != nil
	hasMatShader := material != nil && material.Shader != nil && !hull
	baseMul, baseAlpha, hasBaseColor := baseColorFactors(material, pass.frame.Linear)
	for k, i := range index {
		c, n := vertices[i].Color, vertices[i].Normal
		if side == backFaces {
			n = n.Scale(-1) // Lit as seen from behind.
		}
		if blended {
			corners[k].cut = (float32(c.A) / 255) * baseAlpha
		} else if textured && material.edgeOn() {
			corners[k].cut = edgeOnCut(material.threshold(), normals.Direction(n), pass.frame.viewAt(world.Point(vertices[i].Position)))
		} else if material != nil {
			corners[k].cut = material.threshold()
		}
		if (unlit && !hasMatShader) || hull {
			n = nt3dmath.Vec3{}
		}
		corners[k].world = world.Point(vertices[i].Position)
		if dir := normals.Direction(n); dir.Dot(dir) != 0 {
			corners[k].normal = dir.Normalize()
		}
		if textured && !material.Tint {
			c = color.RGBA{255, 255, 255, 255}
		}
		if hull {
			c = outline.Color
		}
		// The surface's own color, in the working space; it is lit
		// per pixel.
		if pass.frame.Linear {
			corners[k].shade = [3]float32{srgbDecode[c.R] * 255, srgbDecode[c.G] * 255, srgbDecode[c.B] * 255}
		} else {
			corners[k].shade = [3]float32{float32(c.R), float32(c.G), float32(c.B)}
		}
		if hasBaseColor && !hull {
			corners[k].shade[0] *= baseMul[0]
			corners[k].shade[1] *= baseMul[1]
			corners[k].shade[2] *= baseMul[2]
		}
	}
	if whole {
		pass.raster(&corners[0], &corners[1], &corners[2], material, side, isMapped)
		return
	}
	var storage [4]clipCorner
	polygon := clipNear(corners, &storage)
	for k := 1; k+1 < len(polygon); k++ {
		if polygon[0].w > 0 && polygon[k].w > 0 && polygon[k+1].w > 0 {
			pass.raster(&polygon[0], &polygon[k], &polygon[k+1], material, side, isMapped)
		}
	}
}

// softwareTriangle holds the inputs shared by all fragments of one clipped face.
type softwareTriangle struct {
	screenTriangle
	pass     *softwareMeshPass
	a, b, c  *clipCorner
	material *Material

	hull, holdout, blended, textured, cutout, fullSize, directTexture    bool
	perspective, mappedColor, located, needsNormal, normalMapped, framed bool
	lit, toon, pbr, reflective, specular, emissive, hasHook              bool

	tangent, bitangent nt3dmath.Vec3
	emissiveMul        [3]float32
}

func (pass *softwareMeshPass) raster(a, b, c *clipCorner, material *Material, side int, mapped bool) {
	screen := projectTriangle(a, b, c, pass.frame.Width, pass.frame.Height)
	back := side == backFaces || side < 0
	if back && screen.area <= .000001 || !back && screen.area >= -.000001 {
		return
	}
	tri := softwareTriangle{screenTriangle: screen, pass: pass, a: a, b: b, c: c, material: material}
	tri.prepare(side, mapped)
	for y := tri.loY; y <= tri.hiY; y++ {
		for x := tri.loX; x <= tri.hiX; x++ {
			px, py := float32(x)+.5, float32(y)+.5
			w, ok := tri.coverage(px, py)
			if !ok {
				continue
			}
			s := surfaceSample{px: px, py: py, depth: tri.depth(w)}
			if tri.holdout {
				pass.dst.putDepth(x, y, s.depth)
				continue
			}
			if tri.perspective {
				s.q = w.perspective(a.w, b.w, c.w)
			}
			if tri.directTexture {
				tri.putUnlitTexture(x, y, &s)
				continue
			}
			if !tri.surface(w, &s) {
				continue
			}
			if !pass.prepass {
				if tri.lit && s.hasNormal {
					tri.light(&s, x, y)
				}
				if tri.emissive {
					tri.emit(&s)
				}
				if pass.frame.Fog.on() {
					pass.fogRGB(&s.rgb, pass.frame.eyeDistance(s.world))
				}
				if tri.blended {
					pass.dst.blend(x, y, s.depth, s.rgb, s.opacity)
					continue
				}
			}
			pass.dst.putWork(x, y, s.depth, s.rgb)
		}
	}
}

// prepare selects the material features and interpolants this face uses.
func (tri *softwareTriangle) prepare(side int, mapped bool) {
	m, f := tri.material, tri.pass.frame
	a, b, c := tri.a, tri.b, tri.c
	tri.hull = side < 0
	tri.holdout = m != nil && m.holdout() && !tri.hull
	if tri.holdout {
		return
	}
	tri.blended = side == backFaces || side == frontFaces || side == outlineTransparent
	tri.textured = m != nil && m.Texture != nil
	tri.hasHook = m != nil && m.Shader != nil && (!tri.hull || m.cutout() || tri.blended)
	tri.cutout = m != nil && m.cutout() && (tri.textured || tri.hasHook)
	tri.fullSize = tri.textured && m.FullSize && m.Texture.shader == ""
	tri.pbr = pbrActive(m) && !tri.hull
	tri.emissive = emissiveActive(m) && !tri.hull
	if tri.emissive {
		tri.emissiveMul = emissiveFactor(m, f.Linear)
	}
	tri.directTexture = tri.textured && !f.Linear && m.unlit() && !m.Tint && m.BaseColor == (color.RGBA{}) &&
		!tri.hasHook && !tri.blended && !tri.hull && !tri.emissive && !f.Fog.on()
	tri.normalMapped = m.normalStrength() != 0 && (!m.unlit() || tri.hasHook) && !tri.hull
	// A clipped corner can have zero handedness even on a framed mesh.
	tri.framed = tri.normalMapped && (a.tangentW != 0 || b.tangentW != 0 || c.tangentW != 0)
	if tri.normalMapped && !tri.framed {
		tri.tangent, tri.bitangent = normalBasis(a, b, c)
	}
	tri.reflective = !tri.pbr && m != nil && m.Phong.Reflectivity > 0 && !m.unlit() && (f.probesOn() || f.Environment != nil && !f.EnvironmentBackgroundOnly)
	tri.specular = !tri.pbr && m != nil && m.Phong.Specular > 0 && !m.unlit()
	hasNormals := a.normal != (nt3dmath.Vec3{}) || b.normal != (nt3dmath.Vec3{}) || c.normal != (nt3dmath.Vec3{})
	tri.lit = hasNormals && !m.unlit() && !tri.hull
	tri.needsNormal = tri.lit || tri.hasHook && !tri.hull && hasNormals
	tri.toon = m != nil && m.Toon.on() && !m.unlit()
	lightNeedsPosition := tri.pbr || tri.specular || tri.reflective || m != nil && m.Transmission.Amount > 0 ||
		tri.pass.rig.points || tri.pass.rig.shadow != nil || tri.toon && m.Toon.Rim > 0
	tri.located = f.Fog.on() || tri.hasHook || tri.lit && lightNeedsPosition
	tri.mappedColor = mapped && !tri.textured
	tri.perspective = tri.blended || tri.textured || tri.lit || f.Fog.on() || mapped || tri.hasHook
}
