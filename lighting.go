// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// Lighting is how a frame's surfaces are lit: ambient light from all
// around, plus at most MaxLights lights. Surfaces are lit per pixel, by
// their interpolated normals. The zero Lighting has no light at all, so
// lit surfaces are black; Material.Unlit surfaces, points, lines, arrows,
// and overlays are never lit.
type Lighting struct {
	Ambient Ambient
	Lights  []Light
}

// Ambient light reaches every surface, whichever way it faces: Sky's color
// on surfaces facing up (+Z), Ground's facing down, blending between.
// Equal colors give even ambient light. Intensity scales both; zero means 1.
type Ambient struct {
	Sky, Ground color.RGBA
	Intensity   float32
}

// LightKind is a Light's kind.
type LightKind uint8

const (
	// DirectionalLight comes from far away, from Direction, the same
	// everywhere, as sunlight.
	DirectionalLight LightKind = iota
	// PointLight shines from Position in every direction, fading with
	// distance.
	PointLight
	// SpotLight shines from Position along Direction within OuterCone,
	// fading with distance like a PointLight and smoothly from InnerCone to
	// OuterCone.
	SpotLight
)

// MaxLights is the most lights a frame's Lighting may have.
const MaxLights = 8

// defaultSpotOuterCone is the default OuterCone half-angle (45 degrees) when
// a SpotLight leaves OuterCone zero.
const defaultSpotOuterCone = math.Pi / 4

// Light is a directional, point, or spot light. Color is sRGB; zero (alpha
// zero) means white. Intensity scales it; zero means 1. A point or spot light
// with a Range fades smoothly to nothing there, by (1-(d/Range)²)²; without
// one it fades as 1/(1+d²), in world units, and never quite reaches nothing.
//
// For DirectionalLight, Direction points toward the light and is required.
// For SpotLight, Direction is the aim vector the beam shines along from
// Position (required), OuterCone is the outer cone half-angle in radians in
// (0, π/2) (zero means π/4), and InnerCone is the full-strength inner cone
// half-angle in [0, OuterCone].
type Light struct {
	Kind      LightKind
	Direction nt3dmath.Vec3 // Directional: toward the light; Spot: beam direction; required for both.
	Position  nt3dmath.Vec3 // Point and Spot: in world space.
	Color     color.RGBA
	Intensity float32
	Range     float32 // Point and Spot only.
	InnerCone float32 // Spot only: inner cone half-angle in radians, 0..OuterCone.
	OuterCone float32 // Spot only: outer cone half-angle in radians, (0, π/2); 0 means π/4.
	Shadows   bool    // Only the first light with Shadows casts shadows.
}

// defaultKey is the default key light's intensity: with the default
// ambient, a gray .3 that a linear frame decodes to about .074, a surface
// facing the light is fully lit.
var defaultKey = 1 - srgbDecode[77]

// DefaultLighting is a gray ambient light and one white key light from
// above, front, and right.
func DefaultLighting() Lighting {
	gray := color.RGBA{77, 77, 77, 255}
	return Lighting{
		Ambient: Ambient{Sky: gray, Ground: gray, Intensity: 1},
		Lights:  []Light{{Kind: DirectionalLight, Direction: nt3dmath.Vec3{X: 1, Y: -1, Z: 2}.Normalize(), Intensity: defaultKey}},
	}
}

func (a Ambient) intensity() float32 {
	if a.Intensity == 0 {
		return 1
	}
	return a.Intensity
}

func (l Light) intensity() float32 {
	if l.Intensity == 0 {
		return 1
	}
	return l.Intensity
}

func (l Light) outerCone() float32 {
	if l.OuterCone == 0 {
		return defaultSpotOuterCone
	}
	return l.OuterCone
}

func validLighting(l Lighting) bool {
	if len(l.Lights) > MaxLights || !validFloat(l.Ambient.Intensity) || l.Ambient.Intensity < 0 {
		return false
	}
	for _, x := range l.Lights {
		if x.Kind > SpotLight || !finiteVec(x.Direction) || !finiteVec(x.Position) || !validFloat(x.Intensity, x.Range, x.InnerCone, x.OuterCone) || x.Intensity < 0 || x.Range < 0 {
			return false
		}
		// Directional and spot lights need a direction: without one, a
		// directional light is most likely a point light missing its Kind.
		if (x.Kind == DirectionalLight || x.Kind == SpotLight) && x.Direction == (nt3dmath.Vec3{}) {
			return false
		}
		if x.Kind == SpotLight {
			outer := x.outerCone()
			if x.InnerCone < 0 || outer <= 0 || outer >= math.Pi/2 || x.InnerCone > outer {
				return false
			}
		}
	}
	return true
}

// lightRig is a frame's lighting ready to shade with: colors in the frame's
// working space (decoded when it works in linear light) times their
// intensities, directions normalized.
type lightRig struct {
	screenOcclusion   float32 // 1 - screen-space visibility; zero when disabled.
	environmentActive bool
	environment       [9]nt3dsh.RGB
	shadow            *shadowMap
	sky, ground       [3]float32
	lights            []rigLight
	shadowLight       int  // Index of the shadow-casting light, or -1.
	points            bool // Whether any light is positional, so shading needs surface positions.
}

type rigLight struct {
	kind     LightKind
	v        nt3dmath.Vec3 // Directional: unit direction toward the light. Point/Spot: position.
	spotDir  nt3dmath.Vec3 // Spot: unit beam direction from position.
	cosOuter float32
	invSpan  float32
	color    [3]float32
	rng      float32
}

func newRig(f *Frame) lightRig {
	work := func(c color.RGBA, k float32) [3]float32 {
		if f.Linear {
			return [3]float32{srgbDecode[c.R] * k, srgbDecode[c.G] * k, srgbDecode[c.B] * k}
		}
		return [3]float32{float32(c.R) / 255 * k, float32(c.G) / 255 * k, float32(c.B) / 255 * k}
	}
	l := f.Lighting
	r := lightRig{shadowLight: shadowCaster(l), sky: work(l.Ambient.Sky, l.Ambient.intensity()), ground: work(l.Ambient.Ground, l.Ambient.intensity())}
	if f.Environment != nil && !f.EnvironmentBackgroundOnly {
		r.environmentActive = true
		strength := f.EnvironmentStrength
		if strength == 0 {
			strength = 1
		}
		for j := range r.environment {
			for k := range 3 {
				r.environment[j][k] = f.Environment.diffuse[j][k] * strength
			}
		}
	}
	for _, x := range l.Lights {
		c := x.Color
		if c.A == 0 {
			c = color.RGBA{255, 255, 255, 255}
		}
		rl := rigLight{kind: x.Kind, v: x.Position, color: work(c, x.intensity()), rng: x.Range}
		if x.Kind == DirectionalLight {
			rl.v = x.Direction.Normalize()
		} else if x.Kind == SpotLight {
			rl.spotDir = x.Direction.Normalize()
			outer := float64(x.outerCone())
			inner := float64(x.InnerCone)
			cosOut := float32(math.Cos(outer))
			cosIn := float32(math.Cos(inner))
			rl.cosOuter = cosOut
			rl.invSpan = 1 / max(1e-4, cosIn-cosOut)
		}
		r.points = r.points || x.Kind != DirectionalLight
		r.lights = append(r.lights, rl)
	}
	return r
}

// toward is the unit direction from world point p toward the light, and how
// much of its light reaches p. A positional light at p reaches it not at all.
func (l *rigLight) toward(p nt3dmath.Vec3) (nt3dmath.Vec3, float32) {
	if l.kind == DirectionalLight {
		return l.v, 1
	}
	v := l.v.Sub(p)
	d2 := v.Dot(v)
	if d2 == 0 {
		return nt3dmath.Vec3{}, 0
	}
	dir := v.Scale(float32(1 / math.Sqrt(float64(d2))))
	fade := 1 / (1 + d2)
	if l.rng > 0 {
		k := max(0, 1-d2/(l.rng*l.rng))
		fade = k * k
	}
	if l.kind == SpotLight {
		cone := max(0, min(1, (-dir.Dot(l.spotDir)-l.cosOuter)*l.invSpan))
		fade *= cone * cone
	}
	return dir, fade
}

// light multiplies a surface color, per channel, by the light reaching
// the surface at world point p with unit world normal n: ambient, from
// ground to sky as n turns up, plus each light's diffuse light. It works
// in place, as it runs for every lit pixel.
func (r *lightRig) light(rgb *[3]float32, n, p nt3dmath.Vec3) { r.lightNormal(rgb, n, p, n) }
func (r *lightRig) lightNormal(rgb *[3]float32, n, p, geometric nt3dmath.Vec3) {
	c := r.indirectDiffuse(n, 1-r.screenOcclusion).radiance
	c0, c1, c2 := c[0], c[1], c[2]
	for i := range r.lights {
		l := &r.lights[i]
		var s float32
		dir := l.v
		if l.kind == DirectionalLight {
			s = n.X*l.v.X + n.Y*l.v.Y + n.Z*l.v.Z
		} else {
			var fade float32
			dir, fade = l.toward(p)
			s = n.Dot(dir) * fade
		}
		if s > 0 && i == r.shadowLight && r.shadow != nil {
			s *= r.shadow.visibility(p, geometric, dir)
		}
		if s > 0 {
			c0 += l.color[0] * s
			c1 += l.color[1] * s
			c2 += l.color[2] * s
		}
	}
	rgb[0] *= c0
	rgb[1] *= c1
	rgb[2] *= c2
}

// highlight is the Blinn-Phong specular light, per channel, at world point
// p with unit world normal n, seen from unit direction view: for each light
// falling on the surface's side, Specular times the cosine between the
// normal and the half vector raised to Shininess, tinted by the light and
// faded as its diffuse light is. Callers check the material shines.
func (r *lightRig) highlight(m *Material, n, p, view nt3dmath.Vec3) [3]float32 {
	return r.highlightNormal(m, n, p, view, n)
}
func (r *lightRig) highlightNormal(m *Material, n, p, view, geometric nt3dmath.Vec3) [3]float32 {
	var c [3]float32
	for i := range r.lights {
		l := &r.lights[i]
		dir, fade := l.toward(p)
		if fade == 0 || n.Dot(dir) <= 0 {
			continue
		}
		h := dir.Add(view)
		if h.Dot(h) == 0 {
			continue
		}
		s := m.Specular * float32(math.Pow(float64(max(0, n.Dot(h.Normalize()))), float64(m.shininess()))) * fade
		if i == r.shadowLight && r.shadow != nil {
			s *= r.shadow.visibility(p, geometric, dir)
		}
		for k := range c {
			c[k] += l.color[k] * s
		}
	}
	return c
}

// toonLight is lightNormal for a toon material: flat ambient tinted by the
// style's shade, each light's cosine and cast shadow snapped to bands, and a
// rim highlight on lit surface seen edge-on from unit direction view. The rim
// is returned, as highlight's light is, in light units for the caller to add.
func (r *lightRig) toonLight(rgb *[3]float32, t ToonStyle, n, p, geometric, view nt3dmath.Vec3) (rimLight [3]float32) {
	w := toonEdge(t.Softness, t.Bands)
	sw := toonShadowEdge(t.ShadowSoftness)
	var direct [3]float32
	for i := range r.lights {
		l := &r.lights[i]
		dir, fade := l.toward(p)
		s := n.Dot(dir)
		if fade == 0 || s <= 0 {
			continue
		}
		sh := float32(1)
		if i == r.shadowLight && r.shadow != nil {
			sh = toonStep(.5, sw, r.shadow.visibilityToon(p, geometric, dir, t.ShadowSoftness))
		}
		b := toonBand(s, t.Bands, w) * sh * fade
		for k := range direct {
			direct[k] += l.color[k] * b
		}
	}
	var rim float32
	if t.Rim > 0 {
		rim = t.Rim * toonStep(toonRim, w, 1-max(0, n.Dot(view)))
	}
	shade := t.shade()
	ambient := r.indirectToonDiffuse(1 - r.screenOcclusion).radiance
	for k := range rgb {
		rgb[k] *= ambient[k]*shade[k] + direct[k]
		rimLight[k] = rim * direct[k]
	}
	return rimLight
}

// toonHighlight is highlightNormal for a toon material: each light's
// specular lobe becomes a flat spot.
func (r *lightRig) toonHighlight(m *Material, n, p, view, geometric nt3dmath.Vec3) [3]float32 {
	w := toonEdge(m.Toon.Softness, m.Toon.Bands)
	sw := toonShadowEdge(m.Toon.ShadowSoftness)
	var c [3]float32
	for i := range r.lights {
		l := &r.lights[i]
		dir, fade := l.toward(p)
		if fade == 0 || n.Dot(dir) <= 0 {
			continue
		}
		h := dir.Add(view)
		if h.Dot(h) == 0 {
			continue
		}
		lobe := float32(math.Pow(float64(max(0, n.Dot(h.Normalize()))), float64(m.shininess())))
		s := m.Specular * toonStep(toonSpot, w, lobe) * fade
		if i == r.shadowLight && r.shadow != nil {
			s *= toonStep(.5, sw, r.shadow.visibilityToon(p, geometric, dir, m.Toon.ShadowSoftness))
		}
		for k := range c {
			c[k] += l.color[k] * s
		}
	}
	return c
}
