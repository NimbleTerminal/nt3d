// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
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
)

// MaxLights is the most lights a frame's Lighting may have.
const MaxLights = 8

// Light is a directional or point light. Color is sRGB; zero (alpha zero)
// means white. Intensity scales it; zero means 1. A point light with a
// Range fades smoothly to nothing there, by (1-(d/Range)²)²; without one
// it fades as 1/(1+d²), in world units, and never quite reaches nothing.
type Light struct {
	Kind      LightKind
	Direction nt3dmath.Vec3 // Directional: toward the light; required.
	Position  nt3dmath.Vec3 // Point: in world space.
	Color     color.RGBA
	Intensity float32
	Range     float32 // Point only.
	Shadows   bool    // Only the first directional light with Shadows casts shadows.
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

func validLighting(l Lighting) bool {
	if len(l.Lights) > MaxLights || !validFloat(l.Ambient.Intensity) || l.Ambient.Intensity < 0 {
		return false
	}
	for _, x := range l.Lights {
		if x.Kind > PointLight || !finiteVec(x.Direction) || !finiteVec(x.Position) || !validFloat(x.Intensity, x.Range) || x.Intensity < 0 || x.Range < 0 {
			return false
		}
		// A directional light needs a direction: without one, it is most
		// likely a point light missing its Kind.
		if x.Kind == DirectionalLight && x.Direction == (nt3dmath.Vec3{}) {
			return false
		}
	}
	return true
}

// lightRig is a frame's lighting ready to shade with: colors in the frame's
// working space (decoded when it works in linear light) times their
// intensities, directions normalized.
type lightRig struct {
	environment [4][3]float32
	shadow      *shadowMap
	sky, ground [3]float32
	lights      []rigLight
	shadowLight int  // Index of the shadow-casting light, or -1.
	points      bool // Whether any light is a point light, so shading needs surface positions.
}

type rigLight struct {
	kind  LightKind
	v     nt3dmath.Vec3 // Directional: the unit direction toward the light. Point: its position.
	color [3]float32
	rng   float32
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
		v := x.Position
		if x.Kind == DirectionalLight {
			v = x.Direction.Normalize()
		}
		r.points = r.points || x.Kind == PointLight
		r.lights = append(r.lights, rigLight{kind: x.Kind, v: v, color: work(c, x.intensity()), rng: x.Range})
	}
	return r
}

// toward is the unit direction from world point p toward the light, and how
// much of its light reaches p. A point light at p reaches it not at all.
func (l *rigLight) toward(p nt3dmath.Vec3) (nt3dmath.Vec3, float32) {
	if l.kind == DirectionalLight {
		return l.v, 1
	}
	v := l.v.Sub(p)
	d2 := v.Dot(v)
	if d2 == 0 {
		return nt3dmath.Vec3{}, 0
	}
	fade := 1 / (1 + d2)
	if l.rng > 0 {
		k := max(0, 1-d2/(l.rng*l.rng))
		fade = k * k
	}
	return v.Scale(float32(1 / math.Sqrt(float64(d2)))), fade
}

// light multiplies a surface color, per channel, by the light reaching
// the surface at world point p with unit world normal n: ambient, from
// ground to sky as n turns up, plus each light's diffuse light. It works
// in place, as it runs for every lit pixel.
func (r *lightRig) light(rgb *[3]float32, n, p nt3dmath.Vec3) { r.lightNormal(rgb, n, p, n) }
func (r *lightRig) lightNormal(rgb *[3]float32, n, p, geometric nt3dmath.Vec3) {
	t := n.Z*.5 + .5
	c0, c1, c2 := r.ground[0]+(r.sky[0]-r.ground[0])*t, r.ground[1]+(r.sky[1]-r.ground[1])*t, r.ground[2]+(r.sky[2]-r.ground[2])*t
	c0 += max(0, r.environment[0][0]+r.environment[1][0]*n.X+r.environment[2][0]*n.Y+r.environment[3][0]*n.Z)
	c1 += max(0, r.environment[0][1]+r.environment[1][1]*n.X+r.environment[2][1]*n.Y+r.environment[3][1]*n.Z)
	c2 += max(0, r.environment[0][2]+r.environment[1][2]*n.X+r.environment[2][2]*n.Y+r.environment[3][2]*n.Z)
	for i := range r.lights {
		l := &r.lights[i]
		var s float32
		if l.kind == DirectionalLight {
			s = n.X*l.v.X + n.Y*l.v.Y + n.Z*l.v.Z
		} else {
			dir, fade := l.toward(p)
			s = n.Dot(dir) * fade
		}
		if i == r.shadowLight && r.shadow != nil {
			s *= r.shadow.visibility(p, geometric, l.v)
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
