// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

const (
	minPBRRoughness   = .045
	minSheenRoughness = .07
)

func sheenActive(m *Material) bool {
	return m != nil && !m.Unlit && (m.SheenColor.R > 0 || m.SheenColor.G > 0 || m.SheenColor.B > 0)
}

func specularCustom(m *Material) bool {
	return m != nil && !m.Unlit && (m.SpecularColor != (color.RGBA{}) || m.SpecularIntensity > 0)
}

func pbrActive(m *Material) bool {
	return m != nil && !m.Unlit && (m.PBR || m.Metallic > 0 || m.Roughness > 0 || m.ORMMap != nil || m.Clearcoat > 0 || sheenActive(m) || specularCustom(m))
}

func coatParams(m *Material) [4]float32 {
	var out [4]float32
	if m != nil && !m.Unlit && m.Clearcoat > 0 {
		out[0] = max(0, min(1, m.Clearcoat))
		out[1] = max(minPBRRoughness, min(1, m.ClearcoatRoughness))
	}
	if m != nil && m.BaseColor != (color.RGBA{}) {
		c := m.BaseColor
		alpha := float32(1)
		if m.Transparent {
			alpha = float32(c.A) / 255
		}
		out[2] = float32(int(c.R)<<16 | int(c.G)<<8 | int(c.B))
		out[3] = 1 + alpha
	}
	return out
}

func baseColorFactors(m *Material, linear bool) ([3]float32, float32, bool) {
	if m == nil || m.BaseColor == (color.RGBA{}) {
		return [3]float32{1, 1, 1}, 1, false
	}
	c := m.BaseColor
	alpha := float32(1)
	if m.Transparent {
		alpha = float32(c.A) / 255
	}
	if linear {
		return [3]float32{srgbDecode[c.R], srgbDecode[c.G], srgbDecode[c.B]}, alpha, true
	}
	return [3]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255}, alpha, true
}

func sheenParams(m *Material, linear bool) [4]float32 {
	if !sheenActive(m) {
		return [4]float32{}
	}
	c := m.SheenColor
	rough := max(minSheenRoughness, min(1, m.SheenRoughness))
	if linear {
		return [4]float32{srgbDecode[c.R], srgbDecode[c.G], srgbDecode[c.B], rough}
	}
	return [4]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, rough}
}

func dielectricF0(m *Material) float32 {
	if m == nil || m.IOR == 0 {
		return .04
	}
	r := (m.IOR - 1) / (m.IOR + 1)
	return r * r
}

// dielectricSpecular returns the dielectric normal-incidence F0 per channel,
// the dielectric grazing F90 factor, and the GPU-packed sqrt-quantized F0
// payload (0 when default dielectric specular is in effect).
func dielectricSpecular(m *Material, linear bool) (df0 [3]float32, factor, packed float32) {
	base := dielectricF0(m)
	if !specularCustom(m) {
		return [3]float32{base, base, base}, 1, 0
	}
	c := m.SpecularColor
	if c == (color.RGBA{}) {
		c = color.RGBA{255, 255, 255, 255}
	}
	factor = float32(c.A) / 255
	intensity := m.SpecularIntensity
	if intensity == 0 {
		intensity = 1
	}
	var col [3]float32
	if linear {
		col = [3]float32{srgbDecode[c.R] * intensity, srgbDecode[c.G] * intensity, srgbDecode[c.B] * intensity}
	} else {
		col = [3]float32{float32(c.R) / 255 * intensity, float32(c.G) / 255 * intensity, float32(c.B) / 255 * intensity}
	}
	var q [3]uint8
	for k := range 3 {
		raw := min(1, max(0, base*col[k])) * factor
		q[k] = uint8(math.Sqrt(float64(raw))*255 + .5)
		s := float32(q[k]) / 255
		df0[k] = s * s
	}
	packed = float32(int(q[0])<<16|int(q[1])<<8|int(q[2])) + 1
	return df0, factor, packed
}

func occlusionStrength(m *Material) float32 {
	if m == nil || m.ORMMap == nil {
		return 0
	}
	if m.OcclusionStrength == 0 {
		return 1
	}
	return m.OcclusionStrength
}

func emissiveActive(m *Material) bool {
	if m == nil {
		return false
	}
	if m.EmissiveMap != nil {
		// Explicit opaque black disables map emission; zero color defaults to white.
		return m.Emissive.R > 0 || m.Emissive.G > 0 || m.Emissive.B > 0 || m.Emissive.A == 0
	}
	return m.Emissive.R > 0 || m.Emissive.G > 0 || m.Emissive.B > 0
}

// emissiveFactor returns the material's emissive RGB multiplier in the frame's
// working space (0..1+ scale; >1 for HDR).
func emissiveFactor(m *Material, linear bool) [3]float32 {
	if !emissiveActive(m) {
		return [3]float32{}
	}
	c := m.Emissive
	if m.EmissiveMap != nil && c == (color.RGBA{}) {
		c = color.RGBA{255, 255, 255, 255}
	}
	intensity := m.EmissiveIntensity
	if intensity == 0 {
		intensity = 1
	}
	if linear {
		return [3]float32{srgbDecode[c.R] * intensity, srgbDecode[c.G] * intensity, srgbDecode[c.B] * intensity}
	}
	return [3]float32{float32(c.R) / 255 * intensity, float32(c.G) / 255 * intensity, float32(c.B) / 255 * intensity}
}

func pow5(x float32) float32 {
	x = max(0, min(1, x))
	x2 := x * x
	return x2 * x2 * x
}

func envBRDFApprox(f0 [3]float32, f90In float32, customF90 bool, roughness, nv float32) [3]float32 {
	r0 := -roughness + 1
	r1 := -.0275*roughness + .0425
	r2 := -.572*roughness + 1.04
	r3 := .022*roughness - .04
	e := float32(math.Exp2(float64(-9.28 * nv)))
	a004 := min(r0*r0, e)*r0 + r1
	abX := -1.04*a004 + r2
	abY := 1.04*a004 + r3
	f90 := min(1, max(0, 50*max(f0[0], max(f0[1], f0[2]))))
	if customF90 {
		f90 = min(1, max(0, f90In))
	}
	return [3]float32{
		f0[0]*abX + f90*abY,
		f0[1]*abX + f90*abY,
		f0[2]*abX + f90*abY,
	}
}

func specularAO(nv, ao, alpha float32) float32 {
	if ao >= 1 {
		return 1
	}
	exp := math.Exp2(float64(-16*alpha - 1))
	v := float32(math.Pow(float64(max(0, nv+ao)), exp)) - 1 + ao
	return max(0, min(1, v))
}

// sheenDirectionalAlbedo approximates the Charlie microfacet directional albedo
// E(cosTheta, roughness) for base-layer energy conservation and ambient sheen.
func sheenDirectionalAlbedo(cosTheta, roughness float32) float32 {
	mu := max(0, min(1, cosTheta))
	om := 1 - mu
	e := .16*roughness*roughness + pow5(om)*(.35-.15*roughness) + om*om*.12*roughness
	return max(0, min(1, e))
}

// sheenBRDF evaluates pi * D_Charlie * V_Neubelt for half-vector cosine nh,
// light cosine nl, view cosine nv, and inverse squared roughness invAlpha.
func sheenBRDF(nh, nl, nv, invAlpha float32) float32 {
	sin2h := max(0, 1-nh*nh)
	var piD float32
	if sin2h > 1e-6 {
		piD = .5 * (2 + invAlpha) * float32(math.Pow(float64(sin2h), float64(.5*invAlpha)))
	}
	return piD / max(1e-4, 4*(nl+nv-nl*nv))
}

// pbrShade evaluates diffuse and specular PBR radiance in [0..1+] working-space
// units so transmissive surfaces can refract behind diffuse before adding
// Fresnel reflections and GGX specular highlights.
func pbrShade(base [3]float32, rig *lightRig, f *Frame, m *Material, orm [3]float32, n, p, geometric, view nt3dmath.Vec3) (diffuseOut, specularOut [3]float32) {
	roughness := max(minPBRRoughness, min(1, m.Roughness*orm[1]))
	metallic := max(0, min(1, m.Metallic*orm[2]))
	ao := min(1+(orm[0]-1)*occlusionStrength(m), 1-rig.screenOcclusion)
	alpha := roughness * roughness
	a2 := alpha * alpha

	df0, specFactor, specPacked := dielectricSpecular(m, f.Linear)
	f0 := [3]float32{
		df0[0] + (base[0]-df0[0])*metallic,
		df0[1] + (base[1]-df0[1])*metallic,
		df0[2] + (base[2]-df0[2])*metallic,
	}
	f90Base := specFactor + (1-specFactor)*metallic
	diffuseColor := [3]float32{
		base[0] * (1 - metallic),
		base[1] * (1 - metallic),
		base[2] * (1 - metallic),
	}

	rn, facing := n, n.Dot(view)
	if facing < 0 {
		rn, facing = n.Scale(-1), -facing
	}
	nv := max(1e-4, min(1, facing))
	reflected := rn.Scale(2 * facing).Sub(view).Normalize()

	coat := coatParams(m)
	cc, ccRough := coat[0], coat[1]
	ccAlpha := ccRough * ccRough
	ccA2 := ccAlpha * ccAlpha
	ccN, ccFacing := geometric, geometric.Dot(view)
	if ccFacing < 0 {
		ccN, ccFacing = geometric.Scale(-1), -ccFacing
	}
	ccNV := max(1e-4, min(1, ccFacing))
	ccReflected := ccN.Scale(2 * ccFacing).Sub(view).Normalize()
	ccViewScale := float32(1)
	if cc > 0 {
		ccViewScale = 1 - cc*(.04+.96*pow5(1-ccNV))
	}

	sheen := sheenParams(m, f.Linear)
	sheenCol := [3]float32{sheen[0], sheen[1], sheen[2]}
	sRough := sheen[3]
	maxSheen := max(sheenCol[0], max(sheenCol[1], sheenCol[2]))
	var sInvAlpha, sheenEnvV float32
	sheenViewScale := float32(1)
	if maxSheen > 0 {
		sInvAlpha = 1 / (sRough * sRough)
		sheenEnvV = sheenDirectionalAlbedo(nv, sRough)
		sheenViewScale = 1 - maxSheen*sheenEnvV
	}

	if m.Toon.on() {
		t := m.Toon
		w := toonEdge(t.Softness, t.Bands)
		sw := toonShadowEdge(t.ShadowSoftness)
		var direct, directDiff [3]float32
		for i := range rig.lights {
			l := &rig.lights[i]
			dir, fade := l.toward(p)
			nl := n.Dot(dir)
			ccNL := ccN.Dot(dir)
			if fade <= 0 || (nl <= 0 && (cc <= 0 || ccNL <= 0)) {
				continue
			}
			sh := float32(1)
			if i == rig.shadowLight && rig.shadow != nil {
				sh = toonStep(.5, sw, rig.shadow.visibilityToon(p, geometric, dir, t.ShadowSoftness))
			}
			var hn nt3dmath.Vec3
			var vh float32
			hasH := false
			if h := dir.Add(view); h.Dot(h) > 0 {
				hn = h.Normalize()
				vh = max(0, min(1, view.Dot(hn)))
				hasH = true
			}
			fvh5 := pow5(1 - vh)
			fc := .04 + .96*fvh5
			ccAtten := float32(1)
			if cc > 0 {
				ccAtten = 1 - cc*fc
			}
			if nl > 0 {
				sheenScale := sheenViewScale
				if maxSheen > 0 {
					sheenScale = 1 - maxSheen*max(sheenEnvV, sheenDirectionalAlbedo(nl, sRough))
				}
				b := toonBand(nl, t.Bands, w) * sh * fade
				for k := range direct {
					direct[k] += l.color[k] * b
					directDiff[k] += l.color[k] * b * ccAtten * sheenScale
				}
				if hasH {
					nh := max(0, min(1, n.Dot(hn)))
					denom := nh*nh*(a2-1) + 1
					lobe := a2 / max(1e-7, denom*denom) * .25
					spot := toonStep(toonSpot, w, lobe) * fade * sh * ccAtten * sheenScale
					for k := range specularOut {
						fk := f0[k] + (f90Base-f0[k])*fvh5
						specularOut[k] += l.color[k] * spot * fk
					}
					if maxSheen > 0 {
						shLobe := 2 * nl * sheenBRDF(nh, nl, nv, sInvAlpha)
						shSpot := toonStep(toonSpot, w, shLobe) * fade * sh * ccAtten
						for k := range specularOut {
							specularOut[k] += l.color[k] * shSpot * sheenCol[k]
						}
					}
				}
			}
			if cc > 0 && ccNL > 0 && hasH {
				ccNH := max(0, min(1, ccN.Dot(hn)))
				ccDenom := ccNH*ccNH*(ccA2-1) + 1
				ccLobe := ccA2 / max(1e-7, ccDenom*ccDenom) * .25
				ccSpot := toonStep(toonSpot, w, ccLobe) * fade * sh * cc * fc
				for k := range specularOut {
					specularOut[k] += l.color[k] * ccSpot
				}
			}
		}
		shade := t.shade()
		ambient := rig.indirectToonDiffuse(ao).radiance
		for k := range diffuseOut {
			ambShaded := ambient[k] * shade[k] * ccViewScale
			diffuseOut[k] = diffuseColor[k] * (ambShaded*sheenViewScale + directDiff[k])
			if maxSheen > 0 {
				specularOut[k] += sheenCol[k] * sheenEnvV * ambShaded
			}
		}
		if t.Rim > 0 {
			rim := t.Rim * toonStep(toonRim, w, 1-max(0, n.Dot(view)))
			for k := range specularOut {
				specularOut[k] += rim * direct[k]
			}
		}
		return diffuseOut, specularOut
	}

	// Sample incoming light before applying the material response.
	indirectDiff := rig.indirectDiffuse(n, ao).radiance
	fresnelTerm := pow5(1 - nv)
	baseIndScale := sheenViewScale * ccViewScale
	for k := range diffuseOut {
		f90 := max(f90Base*(1-roughness), f0[k])
		ks := f0[k] + (f90-f0[k])*fresnelTerm
		diffuseOut[k] = diffuseColor[k] * (1 - ks) * indirectDiff[k] * baseIndScale
		if maxSheen > 0 {
			specularOut[k] += sheenCol[k] * sheenEnvV * indirectDiff[k] * ccViewScale
		}
	}

	brdf := envBRDFApprox(f0, f90Base, specPacked > 0, roughness, nv)
	sAO := specularAO(nv, ao, alpha)
	specEnv := rig.indirectSpecular(f, reflected, roughness, m.EnvironmentReflect, sAO).radiance
	for k := range specularOut {
		specularOut[k] += specEnv[k] * brdf[k] * baseIndScale
	}
	if cc > 0 {
		ccBRDF := envBRDFApprox([3]float32{.04, .04, .04}, 1, false, ccRough, ccNV)
		ccSAO := specularAO(ccNV, ao, ccAlpha)
		ccSpecEnv := rig.indirectSpecular(f, ccReflected, ccRough, m.EnvironmentReflect, ccSAO).radiance
		for k := range specularOut {
			specularOut[k] += cc * ccSpecEnv[k] * ccBRDF[k]
		}
	}

	// Direct Cook-Torrance GGX, Charlie sheen, and clearcoat lights.
	oneMinusA2 := 1 - a2
	ccOneMinusA2 := 1 - ccA2
	for i := range rig.lights {
		l := &rig.lights[i]
		dir, fade := l.toward(p)
		nl := n.Dot(dir)
		ccNL := ccN.Dot(dir)
		if fade <= 0 || (nl <= 0 && (cc <= 0 || ccNL <= 0)) {
			continue
		}
		vis := fade
		if i == rig.shadowLight && rig.shadow != nil {
			vis *= rig.shadow.visibility(p, geometric, dir)
		}
		if vis <= 0 {
			continue
		}
		var hn nt3dmath.Vec3
		var vh float32
		hasH := false
		if h := dir.Add(view); h.Dot(h) > 0 {
			hn = h.Normalize()
			vh = max(0, min(1, view.Dot(hn)))
			hasH = true
		}
		fvh5 := pow5(1 - vh)
		fc := .04 + .96*fvh5
		ccAtten := float32(1)
		if cc > 0 {
			ccAtten = 1 - cc*fc
		}
		if nl > 0 {
			sheenScale := sheenViewScale
			if maxSheen > 0 {
				sheenScale = 1 - maxSheen*max(sheenEnvV, sheenDirectionalAlbedo(nl, sRough))
			}
			var dTerm, shTerm float32
			if hasH {
				nh := max(0, min(1, n.Dot(hn)))
				denom := nh*nh*(a2-1) + 1
				piD := a2 / max(1e-7, denom*denom)
				ggxV := nl * float32(math.Sqrt(float64(nv*nv*oneMinusA2+a2)))
				ggxL := nv * float32(math.Sqrt(float64(nl*nl*oneMinusA2+a2)))
				dTerm = piD * (.5 / max(1e-5, ggxV+ggxL))
				if maxSheen > 0 {
					shTerm = sheenBRDF(nh, nl, nv, sInvAlpha)
				}
			}
			w := vis * nl * ccAtten
			for k := range 3 {
				fk := f0[k] + (f90Base-f0[k])*fvh5
				diffuseOut[k] += l.color[k] * w * sheenScale * (1 - fk) * diffuseColor[k]
				specularOut[k] += l.color[k] * w * (sheenScale*dTerm*fk + shTerm*sheenCol[k])
			}
		}
		if cc > 0 && ccNL > 0 && hasH {
			ccNH := max(0, min(1, ccN.Dot(hn)))
			ccDenom := ccNH*ccNH*(ccA2-1) + 1
			ccPiD := ccA2 / max(1e-7, ccDenom*ccDenom)
			ccGGXV := ccNL * float32(math.Sqrt(float64(ccNV*ccNV*ccOneMinusA2+ccA2)))
			ccGGXL := ccNV * float32(math.Sqrt(float64(ccNL*ccNL*ccOneMinusA2+ccA2)))
			ccDTerm := ccPiD * (.5 / max(1e-5, ccGGXV+ccGGXL))
			ccW := vis * ccNL * cc * ccDTerm * fc
			for k := range 3 {
				specularOut[k] += l.color[k] * ccW
			}
		}
	}
	return diffuseOut, specularOut
}
