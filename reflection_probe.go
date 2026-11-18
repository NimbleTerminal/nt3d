// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"slices"
)

// MaxReflectionProbes is the number of authored probes supported per frame.
// Four separate panorama bindings fit WebGPU's core sampled-texture limits,
// including normal, ORM, emissive, transmission, and shadow textures.
const MaxReflectionProbes = 4

// ReflectionProbe supplies local specular light from an authored panorama.
// Environment is an immutable NewEnvironment/NewHDREnvironment image captured
// at Position, with the usual +Z-up, +X-center panorama orientation. Bounds is
// both its axis-aligned influence volume and the box used for parallax correction.
// Position must lie strictly inside the nonempty box. Strength >= 0 scales
// radiance; zero means 1, so Off is how to make a probe add nothing: it is
// passed over, as if it were not in the list, and the next probe that holds
// a surface lights it. Probes do not change diffuse lighting or the sky.
//
// Scene probes use world coordinates. In a hand-built Frame, Position and
// Bounds use frame-relative coordinates, like Frame.Eye. Overlaps use the
// first containing probe; there is no edge fade, blending, or automatic capture.
type ReflectionProbe struct {
	Environment *Environment
	Position    nt3dmath.Vec3
	Bounds      nt3dmath.AABB
	Strength    float32
	Off         bool
}

// probesOn reports whether any of the frame's reflection probes is on.
func (f *Frame) probesOn() bool {
	for i := range f.ReflectionProbes {
		if !f.ReflectionProbes[i].Off {
			return true
		}
	}
	return false
}

func validReflectionProbes(probes []ReflectionProbe) error {
	if len(probes) > MaxReflectionProbes {
		return fmt.Errorf("nt3d: at most %d reflection probes are supported", MaxReflectionProbes)
	}
	for i, p := range probes {
		b := p.Bounds
		if p.Environment == nil || !p.Environment.valid() || !finiteVec(p.Position) || !b.IsValid() || !finiteVec(b.Min) || !finiteVec(b.Max) || !finiteVec(b.Max.Sub(b.Min)) || !validFloat(p.Strength) || p.Strength < 0 ||
			p.Position.X <= b.Min.X || p.Position.X >= b.Max.X || p.Position.Y <= b.Min.Y || p.Position.Y >= b.Max.Y || p.Position.Z <= b.Min.Z || p.Position.Z >= b.Max.Z {
			return fmt.Errorf("nt3d: invalid reflection probe %d: require an environment, finite positive box, interior position, and nonnegative strength", i)
		}
	}
	return nil
}

// SetReflectionProbes replaces the scene's probes with a detached snapshot.
// Passing nil removes them. Invalid input leaves the scene unchanged.
func (s *Scene) SetReflectionProbes(probes []ReflectionProbe) error {
	if err := validReflectionProbes(probes); err != nil {
		return err
	}
	s.reflectionProbes = slices.Clone(probes)
	s.changed(false, false)
	return nil
}

// ReflectionProbes returns a detached snapshot in world coordinates.
func (s *Scene) ReflectionProbes() []ReflectionProbe { return slices.Clone(s.reflectionProbes) }

// reflectionProbesRelative is the scene's probes in a frame's coordinates,
// which are float32 around origin. A probe far enough from origin loses its
// box there: its bounds and capture position round to the same number. It
// lights nothing the frame can show, and a frame holding it would be refused
// whole, so it is left out.
func (s *Scene) reflectionProbesRelative(origin [3]float64) []ReflectionProbe {
	shift := func(v nt3dmath.Vec3) nt3dmath.Vec3 {
		return nt3dmath.Vec3{X: float32(float64(v.X) - origin[0]), Y: float32(float64(v.Y) - origin[1]), Z: float32(float64(v.Z) - origin[2])}
	}
	if len(s.reflectionProbes) == 0 {
		return nil
	}
	out := make([]ReflectionProbe, 0, len(s.reflectionProbes))
	for _, p := range s.reflectionProbes {
		p.Position, p.Bounds.Min, p.Bounds.Max = shift(p.Position), shift(p.Bounds.Min), shift(p.Bounds.Max)
		if lo, at, hi := p.Bounds.Min, p.Position, p.Bounds.Max; lo.X < at.X && at.X < hi.X && lo.Y < at.Y && at.Y < hi.Y && lo.Z < at.Z && at.Z < hi.Z {
			out = append(out, p)
		}
	}
	return out
}

// boxProjectedDirection intersects the reflected ray with the room boundary,
// then looks from the capture position toward that hit. Only points inside
// the influence box participate. Parallel axes do not divide by zero.
func boxProjectedDirection(probe ReflectionProbe, point, direction nt3dmath.Vec3) (nt3dmath.Vec3, bool) {
	b := probe.Bounds
	if point.X < b.Min.X || point.X > b.Max.X || point.Y < b.Min.Y || point.Y > b.Max.Y || point.Z < b.Min.Z || point.Z > b.Max.Z {
		return direction, false
	}
	distance := float32(math.MaxFloat32)
	p := [3]float32{point.X, point.Y, point.Z}
	d := [3]float32{direction.X, direction.Y, direction.Z}
	lo := [3]float32{b.Min.X, b.Min.Y, b.Min.Z}
	hi := [3]float32{b.Max.X, b.Max.Y, b.Max.Z}
	hit := false
	for i := range d {
		if d[i] > 0 {
			distance = min(distance, (hi[i]-p[i])/d[i])
			hit = true
		} else if d[i] < 0 {
			distance = min(distance, (lo[i]-p[i])/d[i])
			hit = true
		}
	}
	if !hit {
		return direction, false
	}
	corrected := point.Add(direction.Scale(distance)).Sub(probe.Position).Normalize()
	return corrected, true
}

func localReflection(f *Frame, point, direction nt3dmath.Vec3, roughness float32, filtered bool) indirectSample {
	for _, probe := range f.ReflectionProbes {
		if probe.Off {
			continue
		}
		corrected, ok := boxProjectedDirection(probe, point, direction)
		if !ok {
			continue
		}
		var c [3]float32
		if filtered {
			v := probe.Environment.sampleSpecular(corrected, roughness)
			c = [3]float32{v[0], v[1], v[2]}
		} else {
			v := probe.Environment.sample(corrected)
			c = [3]float32{v[0], v[1], v[2]}
		}
		strength := probe.Strength
		if strength == 0 {
			strength = 1
		}
		for k := range c {
			if !f.Linear {
				c[k] = float32(encodeSRGB(float64(c[k])))
			}
			c[k] *= strength
		}
		return coveredIndirect(c, 1)
	}
	return indirectSample{}
}

func sceneIndirectReflection(f *Frame, point, direction nt3dmath.Vec3, visibility float32) indirectSample {
	local := localReflection(f, point, direction, 0, false).occluded(visibility)
	if local.coverage == 1 {
		return local
	}
	var fallback indirectSample
	if f.Environment != nil && !f.EnvironmentBackgroundOnly {
		fallback = indirectReflection(f.Environment, direction, f.EnvironmentStrength, f.Linear, visibility)
	}
	return local.over(fallback)
}
