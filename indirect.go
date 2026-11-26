// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

// indirectSample is incoming light before albedo, Fresnel, or layer response.
// radiance is premultiplied by coverage (0..1); zero coverage is a miss, while
// black with full coverage is a valid dark hit. Diffuse uses irradiance/pi;
// specular uses radiance filtered for the requested roughness. Values are HDR
// linear light when Frame.Linear is true, otherwise legacy working-space light.
// All sources in a cascade must use the same units, space, and filter footprint.
// Go and WGSL's IndirectSample share this contract; this is not a GPU ABI.
type indirectSample struct {
	radiance [3]float32
	coverage float32
}

// coveredIndirect converts unweighted incoming light to the cascade contract.
func coveredIndirect(radiance [3]float32, coverage float32) indirectSample {
	for k := range radiance {
		radiance[k] *= coverage
	}
	return indirectSample{radiance, coverage}
}

// over fills only the uncovered fraction from fallback. Never infer coverage
// from brightness or add an SSR/SSGI result over an already complete fallback.
func (s indirectSample) over(fallback indirectSample) indirectSample {
	remaining := 1 - s.coverage
	for k := range s.radiance {
		s.radiance[k] += remaining * fallback.radiance[k]
	}
	s.coverage += remaining * fallback.coverage
	return s
}

// occluded attenuates this source without exposing the next source through it.
// Apply AO to cached/environment sources before composing; a future screen-space
// hit already accounts for visibility and must not be darkened again afterward.
func (s indirectSample) occluded(visibility float32) indirectSample {
	for k := range s.radiance {
		s.radiance[k] *= visibility
	}
	return s
}

// indirectDiffuse is the complete hemisphere + SH fallback. Future local or
// screen-space sources go over this sample, after its source-local occlusion.
func (r *lightRig) indirectDiffuse(n nt3dmath.Vec3, visibility float32) indirectSample {
	if r.bakedDiffuse.coverage > 0 {
		return r.bakedDiffuse.occluded(visibility)
	}
	t := n.Z*.5 + .5
	c := [3]float32{r.ground[0] + (r.sky[0]-r.ground[0])*t, r.ground[1] + (r.sky[1]-r.ground[1])*t, r.ground[2] + (r.sky[2]-r.ground[2])*t}
	if r.environmentActive {
		basis := nt3dsh.BasisUnit(2, n)
		var irradiance [3]float32
		for i, coefficient := range r.environment {
			for k := range irradiance {
				irradiance[k] += coefficient[k] * basis[i]
			}
		}
		for k := range c {
			c[k] += max(0, irradiance[k])
		}
	}
	return coveredIndirect(c, 1).occluded(visibility)
}

// Toon lighting intentionally uses only the direction-independent SH term.
func (r *lightRig) indirectToonDiffuse(visibility float32) indirectSample {
	if r.bakedDiffuse.coverage > 0 {
		return r.bakedDiffuse.occluded(visibility)
	}
	var c [3]float32
	for k := range c {
		c[k] = (r.ground[k] + r.sky[k]) * .5
		if r.environmentActive {
			c[k] += max(0, r.environment[0][k]*nt3dsh.Y00)
		}
	}
	return coveredIndirect(c, 1).occluded(visibility)
}

// indirectSpecular selects a local probe before the complete hemisphere + GGX
// environment fallback.
// Roughness determines the incoming-light filter; the caller still owns the
// BRDF, specular-occlusion heuristic, and base/clearcoat energy conservation.
func (r *lightRig) indirectSpecular(f *Frame, point, direction nt3dmath.Vec3, roughness float32, visibility float32) indirectSample {
	local := localReflection(f, point, direction, roughness, true).occluded(visibility)
	if local.coverage == 1 {
		return local
	}
	t := direction.Z*.5 + .5
	t += (.5 - t) * (roughness * roughness)
	c := [3]float32{r.ground[0] + (r.sky[0]-r.ground[0])*t, r.ground[1] + (r.sky[1]-r.ground[1])*t, r.ground[2] + (r.sky[2]-r.ground[2])*t}
	if f.Environment != nil && !f.EnvironmentBackgroundOnly {
		sky := f.Environment.sampleSpecular(direction, roughness)
		strength := f.EnvironmentStrength
		if strength == 0 {
			strength = 1
		}
		for k := range c {
			v := sky[k]
			if !f.Linear {
				v = float32(encodeSRGB(float64(v)))
			}
			c[k] += v * strength
		}
	}
	return local.over(coveredIndirect(c, 1).occluded(visibility))
}

// indirectReflection preserves the classic material's sharp level-zero
// environment source, without PBR's additional hemisphere or GGX filtering.
func indirectReflection(environment *Environment, direction nt3dmath.Vec3, strength float32, linear bool, visibility float32) indirectSample {
	sample := environment.sample(direction)
	c := [3]float32{sample[0], sample[1], sample[2]}
	if strength == 0 {
		strength = 1
	}
	for k := range c {
		if !linear {
			c[k] = float32(encodeSRGB(float64(c[k])))
		}
		c[k] *= strength
	}
	return coveredIndirect(c, 1).occluded(visibility)
}
