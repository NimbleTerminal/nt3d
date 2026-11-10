// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image/color"
)

// ToonStyle quantizes a material's direct light into flat bands, for a cel
// (toon) look. Bands is the number of tones from shade to fully lit, 2 to 8;
// zero disables it. Each light's cosine, times its cast shadow, snaps to the
// nearest of Bands even levels, so cast shadows get the same hard edge; a
// point light's distance falloff stays smooth. Softness, 0 to 1, widens the
// blend between two bands as a fraction of one band; at zero the edge is
// about a pixel.
//
// A toon surface takes flat ambient light, the midpoint of the sky and
// ground colors (and an environment's constant term), tinted by Shade: the
// color of the unlit band, so shadows can turn cool or warm rather than only
// darker. Shade's alpha is ignored, except that zero means white. Specular
// highlights become flat spots, and Rim, 0 to 1, adds a flat highlight where
// a lit surface turns away from the viewer.
//
// Unlit materials ignore it. It applies to indexed triangles, as Material
// does; Scene.SetToon changes it on geometry already in a scene.
type ToonStyle struct {
	Bands    int
	Softness float32
	Shade    color.RGBA
	Rim      float32
}

func (t ToonStyle) on() bool { return t.Bands > 0 }

func (t ToonStyle) validate() error {
	if t.Bands != 0 && (t.Bands < 2 || t.Bands > 8) {
		return fmt.Errorf("nt3d: toon bands must be 0 or 2..8")
	}
	if !validFloat(t.Softness, t.Rim) || t.Softness < 0 || t.Softness > 1 || t.Rim < 0 || t.Rim > 1 {
		return fmt.Errorf("nt3d: toon softness and rim must be 0..1")
	}
	return nil
}

// shade is the tint of the unlit band, white when unset.
func (t ToonStyle) shade() [3]float32 {
	if t.Shade.A == 0 {
		return [3]float32{1, 1, 1}
	}
	return [3]float32{float32(t.Shade.R) / 255, float32(t.Shade.G) / 255, float32(t.Shade.B) / 255}
}

// toonStep is the smooth step from 0 below edge-w/2 to 1 above edge+w/2.
func toonStep(edge, w, x float32) float32 {
	t := min(1, max(0, (x-edge+w/2)/w))
	return t * t * (3 - 2*t)
}

// toonBand snaps x in 0..1 to the nearest below of bands even levels from 0
// to 1, stepping up at each k/bands across a blend w wide. The GPU's
// toonBand computes the same.
func toonBand(x float32, bands int, w float32) float32 {
	var sum float32
	for k := 1; k < bands; k++ {
		sum += toonStep(float32(k)/float32(bands), w, x)
	}
	return sum / float32(bands-1)
}

// toonEdge is the software renderer's blend width between bands: it has no
// screen derivatives, so a small constant stands in for the GPU's pixel.
func toonEdge(softness float32, bands int) float32 {
	return .01 + softness/float32(bands)
}

// toonRim is where the rim highlight starts, as one minus the cosine between
// the normal and the view, and toonSpot where a specular lobe becomes a spot.
const (
	toonRim  = .7
	toonSpot = .5
)
