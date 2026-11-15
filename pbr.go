// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

const (
	minPBRRoughness   = .045
	minSheenRoughness = .07
)

func sheenActive(m *Material) bool {
	return m != nil && !m.Unlit && (m.SheenColor.R > 0 || m.SheenColor.G > 0 || m.SheenColor.B > 0)
}

func pbrActive(m *Material) bool {
	return m != nil && !m.Unlit && (m.PBR || m.Metallic > 0 || m.Roughness > 0 || m.ORMMap != nil || m.Clearcoat > 0 || sheenActive(m))
}

func coatParams(m *Material) [2]float32 {
	if m == nil || m.Unlit || m.Clearcoat <= 0 {
		return [2]float32{}
	}
	return [2]float32{
		max(0, min(1, m.Clearcoat)),
		max(minPBRRoughness, min(1, m.ClearcoatRoughness)),
	}
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

func envBRDFApprox(f0 [3]float32, roughness, nv float32) [3]float32 {
	r0 := -roughness + 1
	r1 := -.0275*roughness + .0425
	r2 := -.572*roughness + 1.04
	r3 := .022*roughness - .04
	e := float32(math.Exp2(float64(-9.28 * nv)))
	a004 := min(r0*r0, e)*r0 + r1
	abX := -1.04*a004 + r2
	abY := 1.04*a004 + r3
	f90 := min(1, max(0, 50*max(f0[0], max(f0[1], f0[2]))))
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
	ao := 1 + (orm[0]-1)*occlusionStrength(m)
	alpha := roughness * roughness
	a2 := alpha * alpha

	df0 := dielectricF0(m)
	f0 := [3]float32{
		df0 + (base[0]-df0)*metallic,
		df0 + (base[1]-df0)*metallic,
		df0 + (base[2]-df0)*metallic,
	}
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
						fk := f0[k] + (1-f0[k])*fvh5
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
		for k := range diffuseOut {
			amb := (rig.ground[k] + rig.sky[k]) * .5
			if rig.environmentActive {
				amb += max(0, rig.environment[0][k]*.2820948)
			}
			ambShaded := amb * shade[k] * ao * ccViewScale
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

	// Indirect diffuse (hemisphere + SH irradiance) with Fresnel, sheen, and clearcoat energy conservation.
	hemiT := n.Z*.5 + .5
	indirectDiff := [3]float32{
		rig.ground[0] + (rig.sky[0]-rig.ground[0])*hemiT,
		rig.ground[1] + (rig.sky[1]-rig.ground[1])*hemiT,
		rig.ground[2] + (rig.sky[2]-rig.ground[2])*hemiT,
	}
	if rig.environmentActive {
		basis := nt3dsh.BasisUnit(2, n)
		var shIrr [3]float32
		for i, c := range rig.environment {
			for k := range shIrr {
				shIrr[k] += c[k] * basis[i]
			}
		}
		for k := range indirectDiff {
			indirectDiff[k] += max(0, shIrr[k])
		}
	}
	fresnelTerm := pow5(1 - nv)
	baseIndScale := sheenViewScale * ccViewScale
	for k := range diffuseOut {
		f90 := max(1-roughness, f0[k])
		ks := f0[k] + (f90-f0[k])*fresnelTerm
		diffuseOut[k] = diffuseColor[k] * (1 - ks) * indirectDiff[k] * ao * baseIndScale
		if maxSheen > 0 {
			specularOut[k] += sheenCol[k] * sheenEnvV * indirectDiff[k] * ao * ccViewScale
		}
	}

	// Indirect specular (hemisphere ambient + prefiltered GGX environment IBL).
	envRefl := f.Environment != nil && (!f.EnvironmentBackgroundOnly || m.EnvironmentReflect)
	envStrength := f.EnvironmentStrength
	if envStrength == 0 {
		envStrength = 1
	}
	brdf := envBRDFApprox(f0, roughness, nv)
	sAO := specularAO(nv, ao, alpha)
	hemiReflT := reflected.Z*.5 + .5
	hemiSpecT := hemiReflT + (.5-hemiReflT)*alpha
	specEnv := [3]float32{
		rig.ground[0] + (rig.sky[0]-rig.ground[0])*hemiSpecT,
		rig.ground[1] + (rig.sky[1]-rig.ground[1])*hemiSpecT,
		rig.ground[2] + (rig.sky[2]-rig.ground[2])*hemiSpecT,
	}
	if envRefl {
		sky := f.Environment.sampleSpecular(reflected, roughness)
		for k := range specEnv {
			c := sky[k]
			if !f.Linear {
				c = float32(encodeSRGB(float64(c)))
			}
			specEnv[k] += c * envStrength
		}
	}
	for k := range specularOut {
		specularOut[k] += specEnv[k] * brdf[k] * sAO * baseIndScale
	}
	if cc > 0 {
		ccBRDF := envBRDFApprox([3]float32{.04, .04, .04}, ccRough, ccNV)
		ccSAO := specularAO(ccNV, ao, ccAlpha)
		ccHemiReflT := ccReflected.Z*.5 + .5
		ccHemiSpecT := ccHemiReflT + (.5-ccHemiReflT)*ccAlpha
		ccSpecEnv := [3]float32{
			rig.ground[0] + (rig.sky[0]-rig.ground[0])*ccHemiSpecT,
			rig.ground[1] + (rig.sky[1]-rig.ground[1])*ccHemiSpecT,
			rig.ground[2] + (rig.sky[2]-rig.ground[2])*ccHemiSpecT,
		}
		if envRefl {
			sky := f.Environment.sampleSpecular(ccReflected, ccRough)
			for k := range ccSpecEnv {
				c := sky[k]
				if !f.Linear {
					c = float32(encodeSRGB(float64(c)))
				}
				ccSpecEnv[k] += c * envStrength
			}
		}
		for k := range specularOut {
			specularOut[k] += cc * ccSpecEnv[k] * ccBRDF[k] * ccSAO
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
				fk := f0[k] + (1-f0[k])*fvh5
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
