// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// surfaceFrame is where a mark sits on a surface: an origin and three unit
// axes, right and up across the surface and normal out of it.
type surfaceFrame struct {
	center, right, up, normal nt3dmath.Vec3
}

// newSurfaceFrame builds the frame Decal and FlatGaussians are placed by,
// from a center, a normal out of the surface, and an up that need not be
// perpendicular to it. Neither need be unit length. what names the thing
// placed, for the errors.
func newSurfaceFrame(what string, center, normal, up nt3dmath.Vec3, normalOffset float32) (surfaceFrame, error) {
	if !finiteVec(center) || !finiteVec(normal) || !finiteVec(up) {
		return surfaceFrame{}, fmt.Errorf("nt3d: %s vectors must be finite", what)
	}
	if !validFloat(normalOffset) || normalOffset < 0 || normalOffset > 1e6 {
		return surfaceFrame{}, fmt.Errorf("nt3d: %s normal offset must be finite and nonnegative", what)
	}
	nLen := math.Sqrt(float64(normal.Dot(normal)))
	if nLen < 1e-6 {
		return surfaceFrame{}, fmt.Errorf("nt3d: %s normal must be nonzero", what)
	}
	n := normal.Scale(float32(1 / nLen))
	rightRaw := up.Cross(n)
	rLen := math.Sqrt(float64(rightRaw.Dot(rightRaw)))
	if rLen < 1e-6 {
		return surfaceFrame{}, fmt.Errorf("nt3d: %s up vector must not be zero or parallel to normal", what)
	}
	right := rightRaw.Scale(float32(1 / rLen))
	return surfaceFrame{center: center, right: right, up: n.Cross(right).Normalize(), normal: n}, nil
}
