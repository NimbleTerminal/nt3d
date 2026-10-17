// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// Fog fades what is drawn toward Color with distance from the camera: not at
// all nearer than Near, wholly at Far and beyond, linearly between, in world
// units. Far no greater than Near means no fog. A zero Color (alpha zero)
// means the frame's background, so distant things fade into it. Distance is
// from Frame.Eye, or along Frame.Forward for orthographic views, which
// Scene.Frame sets. Overlays and wireframe are not fogged.
type Fog struct {
	Near, Far float32
	Color     color.RGBA
}

// on reports whether the fog does anything.
func (g Fog) on() bool { return g.Far > g.Near }

// amount is how far toward the fog's color a point at distance d fades.
func (g Fog) amount(d float32) float32 {
	if !g.on() {
		return 0
	}
	return max(0, min(1, (d-g.Near)/(g.Far-g.Near)))
}

// color is the fog's color: its own, or the background.
func (g Fog) color(f *Frame) color.RGBA {
	if g.Color.A == 0 {
		return f.Background
	}
	return g.Color
}

func validFog(g Fog) bool { return validFloat(g.Near, g.Far) }

// eyeDistance is a world point's distance from the camera, as fog measures it.
func (f *Frame) eyeDistance(p nt3dmath.Vec3) float32 {
	d := p.Sub(f.Eye)
	if f.Perspective {
		return float32(math.Sqrt(float64(d.Dot(d))))
	}
	return d.Dot(f.Forward)
}

// shininess is a material's specular exponent; zero means 32.
func (m *Material) shininess() float32 {
	if m.Shininess == 0 {
		return 32
	}
	return m.Shininess
}
