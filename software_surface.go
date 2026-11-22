// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

type surfaceSample struct {
	px, py, depth            float32
	q                        barycentric // Perspective-correct weights at the pixel center.
	rgb                      [3]float32  // Working-space color, with 255 for white and headroom above.
	opacity                  float32
	world, normal, geometric nt3dmath.Vec3
	hasNormal                bool
}

// uvAt evaluates the perspective interpolation at an adjacent pixel center,
// including outside the triangle. It accepts any of the mesh's UV sets.
func (tri *softwareTriangle) uvAt(px, py float32, a, b, c UV) UV {
	u := ((tri.x1-px)*(tri.y2-py) - (tri.y1-py)*(tri.x2-px)) / tri.area
	v := ((tri.x2-px)*(tri.y0-py) - (tri.y2-py)*(tri.x0-px)) / tri.area
	q := barycentric{u / tri.a.w, v / tri.b.w, (1 - u - v) / tri.c.w}
	sum := q.a + q.b + q.c
	uv := q.uv(a, b, c)
	return UV{uv.U / sum, uv.V / sum}
}

func (tri *softwareTriangle) footprint(s *surfaceSample, a, b, c UV) (uv, dx, dy UV) {
	return s.q.uv(a, b, c), tri.uvAt(s.px+1, s.py, a, b, c), tri.uvAt(s.px, s.py+1, a, b, c)
}

func (tri *softwareTriangle) sampleMap(s *surfaceSample, tex *Texture, repeat bool, a, b, c UV) [4]float32 {
	uv, dx, dy := tri.footprint(s, a, b, c)
	return tex.sampleFootprint(uv, dx, dy, repeat, tri.pass.aniso)
}

func (tri *softwareTriangle) baseTexture(s *surfaceSample) [4]float32 {
	m := tri.material
	a, b, c := tri.a.uv, tri.b.uv, tri.c.uv
	// Full-size textures and shader or hull cutouts sample level zero.
	if tri.fullSize || tri.cutout && (tri.hull || m.Texture.shader != "") {
		return m.Texture.sampleLinear(s.q.uv(a, b, c), m.Repeat, 0, tri.cutout)
	}
	uv, dx, dy := tri.footprint(s, a, b, c)
	if tri.cutout {
		return m.Texture.sampleLinear(uv, m.Repeat, textureLOD(m.Texture, uv, dx, dy), true)
	}
	return m.Texture.sampleFootprint(uv, dx, dy, m.Repeat, tri.pass.aniso)
}

// putUnlitTexture writes an untinted texture directly in an encoded-color frame.
func (tri *softwareTriangle) putUnlitTexture(x, y int, s *surfaceSample) {
	tex := tri.baseTexture(s)
	q := s.q
	a, b, c := tri.a, tri.b, tri.c
	if tri.rejectTexel(q, tex[3]) {
		return
	}
	// Every corner is white, but its interpolated multiplier can round
	// away from one. Only the exact case can go straight to bytes.
	tint := q.mix(a.shade[0], b.shade[0], c.shade[0]) / 255
	if tint == 1 {
		tri.pass.dst.put(x, y, s.depth, color.RGBA{encodeOpaque8(tex[0]), encodeOpaque8(tex[1]), encodeOpaque8(tex[2]), 255})
		return
	}
	var rgb [3]float32
	for k := range rgb {
		rgb[k] = float32(encodeSRGB(float64(tex[k]))) * 255 * tint
	}
	tri.pass.dst.putWork(x, y, s.depth, rgb)
}

// A custom hook supplies the final alpha and may keep a transparent texel.
func (tri *softwareTriangle) rejectTexel(q barycentric, alpha float32) bool {
	return tri.cutout && !tri.hasHook && alpha < q.mix(tri.a.cut, tri.b.cut, tri.c.cut)
}

// worldAt interpolates the fragment position used by lights, fog and probes.
func (w barycentric) worldAt(a, b, c nt3dmath.Vec3) nt3dmath.Vec3 {
	// Start with a rounded first contribution on every axis. A change of
	// one ULP at a probe boundary can select a different environment.
	axis := func(a, b, c float32) float32 { return float32(w.a*a) + w.b*b + w.c*c }
	return nt3dmath.Vec3{X: axis(a.X, b.X, c.X), Y: axis(a.Y, b.Y, c.Y), Z: axis(a.Z, b.Z, c.Z)}
}

// surface evaluates the pre-lighting material and reports whether the
// fragment survives cutout. The AO prepass uses the same decision.
func (tri *softwareTriangle) surface(w barycentric, s *surfaceSample) bool {
	a, b, c := tri.a, tri.b, tri.c
	q := s.q
	if tri.mappedColor {
		w = q
	}
	for k := range s.rgb {
		s.rgb[k] = w.mix(a.shade[k], b.shade[k], c.shade[k])
	}
	if tri.blended {
		s.opacity = q.mix(a.cut, b.cut, c.cut)
	}
	texAlpha := float32(1)
	if tri.textured {
		tex := tri.baseTexture(s)
		texAlpha = tex[3]
		if tri.rejectTexel(q, texAlpha) {
			return false
		}
		tint := func(k int) float32 { return q.mix(a.shade[k], b.shade[k], c.shade[k]) / 255 }
		if !tri.hull {
			for k := range s.rgb {
				value := tex[k]
				if !tri.pass.frame.Linear {
					value = float32(encodeSRGB(float64(value)))
				}
				s.rgb[k] = (value * 255) * tint(k)
			}
		}
		s.opacity *= texAlpha
	}
	if tri.located {
		s.world = q.worldAt(a.world, b.world, c.world)
	}
	if tri.needsNormal {
		tri.surfaceNormal(s)
	}
	return !tri.hasHook || tri.materialHook(s, texAlpha)
}

func (tri *softwareTriangle) surfaceNormal(s *surfaceSample) {
	a, b, c := tri.a, tri.b, tri.c
	q := s.q
	n := a.normal.Scale(q.a).Add(b.normal.Scale(q.b)).Add(c.normal.Scale(q.c))
	if n.Dot(n) == 0 {
		return
	}
	n = n.Scale(1 / float32(math.Sqrt(float64(n.Dot(n)))))
	s.geometric = n
	if tri.normalMapped {
		uv, dx, dy := tri.footprint(s, a.normalUV, b.normalUV, c.normalUV)
		flow := q.uv(a.flow, b.flow, c.flow)
		sample := flowNormalSample(tri.material, uv, dx, dy, flow, tri.pass.aniso)
		t, bt := tri.tangent, tri.bitangent
		if tri.framed {
			t = a.tangent.Scale(q.a).Add(b.tangent.Scale(q.b)).Add(c.tangent.Scale(q.c))
			bt = n.Cross(t)
			if q.mix(a.tangentW, b.tangentW, c.tangentW) < 0 {
				bt = bt.Scale(-1)
			}
		}
		n = perturbNormal(n, t, bt, sample, tri.material.normalStrength())
	}
	s.normal, s.hasNormal = n, true
}

func (tri *softwareTriangle) materialHook(s *surfaceSample, alpha float32) bool {
	if tri.blended {
		alpha = s.opacity
	}
	base := [4]float32{1, 1, 1, alpha}
	f := tri.pass.frame
	if !tri.hull {
		for k := range s.rgb {
			v := s.rgb[k] / 255
			if !f.Linear {
				v = float32(decodeSRGB(float64(min(1, max(0, v)))))
			}
			base[k] = v
		}
	}
	a, b, c := tri.a, tri.b, tri.c
	uv := s.q.uv(a.uv, b.uv, c.uv)
	out := evalMaterialColor(tri.material.Shader, s.world, s.normal, f.viewAt(s.world), uv, base)
	if tri.cutout && out[3] < s.q.mix(a.cut, b.cut, c.cut) {
		return false
	}
	if tri.blended {
		s.opacity = out[3]
	}
	if !tri.hull {
		for k := range s.rgb {
			v := out[k]
			if !f.Linear {
				v = float32(encodeSRGB(float64(min(1, v))))
			}
			s.rgb[k] = v * 255
		}
	}
	return true
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
