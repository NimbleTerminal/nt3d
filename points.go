// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "fmt"

// PointStyle is how a geometry's points are drawn. The zero value is hard
// discs, radius in pixels, opaque.
type PointStyle struct {
	Shape PointShape
	Units PointUnits
	Blend PointBlend
}

// PointShape is the shape of a point's disc.
type PointShape uint8

const (
	HardPoint PointShape = iota // A disc with a hard edge.
	SoftPoint                   // A disc whose alpha fades to nothing at its edge; drawn hard when opaque.
)

// PointUnits is what a point's Radius measures.
type PointUnits uint8

const (
	// PixelUnits is a radius of 1 to 32 pixels, the same at any distance.
	PixelUnits PointUnits = iota
	// WorldUnits is any positive radius in world units, smaller farther
	// away. Node transforms move points but do not scale their radii.
	WorldUnits
)

// PointBlend is how a point's color combines with what is behind it.
// Blended points are hidden by nearer opaque surfaces but hide nothing.
type PointBlend uint8

const (
	OpaqueBlend   PointBlend = iota // Opaque; color alpha is ignored.
	AlphaBlend                      // Over what is behind, by color alpha, a geometry's farthest points first.
	AdditiveBlend                   // Added to what is behind, times color alpha: brighter where points overlap.
)

// MaxPointPixels is the largest radius on screen, in pixels, of a point in
// world units, so a particle at the camera does not fill the frame. The
// smallest is half a pixel, so far particles thin out rather than vanish.
const MaxPointPixels = 256

func validPointStyle(s PointStyle) error {
	if s.Shape > SoftPoint || s.Units > WorldUnits || s.Blend > AdditiveBlend {
		return fmt.Errorf("nt3d: unknown point style")
	}
	return nil
}

// validPointRadius reports whether a point's radius suits its units.
func validPointRadius(units PointUnits, r float32) bool {
	if !validFloat(r) {
		return false
	}
	if units == WorldUnits {
		return r > 0
	}
	return r >= 1 && r <= 32
}
