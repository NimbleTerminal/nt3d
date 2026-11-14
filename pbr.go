// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

const minPBRRoughness = .045

func pbrActive(m *Material) bool {
	return m != nil && !m.Unlit && (m.PBR || m.Metallic > 0 || m.Roughness > 0 || m.ORMMap != nil)
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

	if m.Toon.on() {
		t := m.Toon
		w := toonEdge(t.Softness, t.Bands)
		var direct [3]float32
		for i := range rig.lights {
			l := &rig.lights[i]
			dir, fade := l.toward(p)
			nl := n.Dot(dir)
			if fade <= 0 || nl <= 0 {
				continue
			}
			vis := float32(1)
			if i == rig.shadowLight && rig.shadow != nil {
				vis = rig.shadow.visibility(p, geometric, dir)
			}
			b := toonBand(nl*vis, t.Bands, w) * fade
			for k := range direct {
				direct[k] += l.color[k] * b
			}
			if h := dir.Add(view); h.Dot(h) > 0 {
				hn := h.Normalize()
				nh := max(0, min(1, n.Dot(hn)))
				vh := max(0, min(1, view.Dot(hn)))
				denom := nh*nh*(a2-1) + 1
				lobe := a2 / max(1e-7, denom*denom) * .25
				spot := toonStep(toonSpot, w, lobe) * fade * vis
				fvh5 := pow5(1 - vh)
				for k := range specularOut {
					fk := f0[k] + (1-f0[k])*fvh5
					specularOut[k] += l.color[k] * spot * fk
				}
			}
		}
		shade := t.shade()
		for k := range diffuseOut {
			amb := (rig.ground[k] + rig.sky[k]) * .5
			if rig.environmentActive {
				amb += max(0, rig.environment[0][k]*.2820948)
			}
			diffuseOut[k] = diffuseColor[k] * (amb*shade[k]*ao + direct[k])
		}
		if t.Rim > 0 {
			rim := t.Rim * toonStep(toonRim, w, 1-max(0, n.Dot(view)))
			for k := range specularOut {
				specularOut[k] += rim * direct[k]
			}
		}
		return diffuseOut, specularOut
	}

	// Indirect diffuse (hemisphere + SH irradiance) with Fresnel energy conservation.
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
	for k := range diffuseOut {
		f90 := max(1-roughness, f0[k])
		ks := f0[k] + (f90-f0[k])*fresnelTerm
		diffuseOut[k] = diffuseColor[k] * (1 - ks) * indirectDiff[k] * ao
	}

	// Indirect specular (hemisphere ambient + prefiltered GGX environment IBL).
	brdf := envBRDFApprox(f0, roughness, nv)
	sAO := specularAO(nv, ao, alpha)
	hemiReflT := reflected.Z*.5 + .5
	hemiSpecT := hemiReflT + (.5-hemiReflT)*alpha
	specEnv := [3]float32{
		rig.ground[0] + (rig.sky[0]-rig.ground[0])*hemiSpecT,
		rig.ground[1] + (rig.sky[1]-rig.ground[1])*hemiSpecT,
		rig.ground[2] + (rig.sky[2]-rig.ground[2])*hemiSpecT,
	}
	if f.Environment != nil && (!f.EnvironmentBackgroundOnly || m.EnvironmentReflect) {
		sky := f.Environment.sampleSpecular(reflected, roughness)
		strength := f.EnvironmentStrength
		if strength == 0 {
			strength = 1
		}
		for k := range specEnv {
			c := sky[k]
			if !f.Linear {
				c = float32(encodeSRGB(float64(c)))
			}
			specEnv[k] += c * strength
		}
	}
	for k := range specularOut {
		specularOut[k] += specEnv[k] * brdf[k] * sAO
	}

	// Direct Cook-Torrance GGX lights.
	oneMinusA2 := 1 - a2
	for i := range rig.lights {
		l := &rig.lights[i]
		dir, fade := l.toward(p)
		nl := n.Dot(dir)
		if fade <= 0 || nl <= 0 {
			continue
		}
		vis := fade
		if i == rig.shadowLight && rig.shadow != nil {
			vis *= rig.shadow.visibility(p, geometric, dir)
		}
		if vis <= 0 {
			continue
		}
		var dTerm, vh float32
		if h := dir.Add(view); h.Dot(h) > 0 {
			hn := h.Normalize()
			nh := max(0, min(1, n.Dot(hn)))
			vh = max(0, min(1, view.Dot(hn)))
			denom := nh*nh*(a2-1) + 1
			piD := a2 / max(1e-7, denom*denom)
			ggxV := nl * float32(math.Sqrt(float64(nv*nv*oneMinusA2+a2)))
			ggxL := nv * float32(math.Sqrt(float64(nl*nl*oneMinusA2+a2)))
			vTerm := .5 / max(1e-5, ggxV+ggxL)
			dTerm = piD * vTerm
		}
		fvh5 := pow5(1 - vh)
		w := vis * nl
		for k := range 3 {
			fk := f0[k] + (1-f0[k])*fvh5
			diffuseOut[k] += l.color[k] * w * (1 - fk) * diffuseColor[k]
			specularOut[k] += l.color[k] * w * dTerm * fk
		}
	}
	return diffuseOut, specularOut
}
