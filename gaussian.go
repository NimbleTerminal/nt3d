// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"nimbleterminal.dev/nt3d/math"
)

func validGaussian(style PointStyle, p Point) bool {
	if style.Shape != GaussianPoint {
		return true
	}
	for _, a := range p.GaussianAxes {
		if !finiteVec(a) || a.Dot(a) <= 0 || !validFloat(a.Dot(a)) {
			return false
		}
	}
	return true
}

// gaussianEllipse is a Cholesky factor of the projected covariance, in
// screen pixels (y down). Its support is a three-sigma ellipse. Use the
// homogeneous projection Jacobian so perspective, orthographic, and affine
// node transforms follow exactly the same path.
type gaussianEllipse struct{ a, b, c, radius float32 }

func projectGaussian(p Point, m *nt3dmath.Mat4, width, height int, px float32) gaussianEllipse {
	x, y, _, w := m.Transform(p.Position)
	xx, xy, yy := .3*px*px, float32(0), .3*px*px
	for _, axis := range p.GaussianAxes {
		dx := m[0]*axis.X + m[4]*axis.Y + m[8]*axis.Z
		dy := m[1]*axis.X + m[5]*axis.Y + m[9]*axis.Z
		dw := m[3]*axis.X + m[7]*axis.Y + m[11]*axis.Z
		sx := (dx - x/w*dw) / w * float32(width) * .5
		sy := -(dy - y/w*dw) / w * float32(height) * .5
		xx += sx * sx
		xy += sx * sy
		yy += sy * sy
	}
	a := float32(math.Sqrt(float64(xx)))
	b := xy / a
	c := float32(math.Sqrt(float64(max(yy-b*b, .000001*px*px))))
	radius := 3 * float32(math.Sqrt(float64(max(xx, yy))))
	scale := min(float32(1), MaxPointPixels*px/radius)
	return gaussianEllipse{a * scale, b * scale, c * scale, radius * scale}
}
