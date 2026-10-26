// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"math"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
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

// gaussianSHCount is the RGB entries per point in GaussianSH: the whole
// coefficient set, or with a palette just the DC. gaussianSHTotal is how many a
// splat evaluates.
func gaussianSHCount(g *Geometry) int {
	if len(g.Points) == 0 {
		return 0
	}
	return len(g.GaussianSH) / len(g.Points)
}
func gaussianSHMatrix(g *Geometry) nt3dmath.Mat4 {
	if g.GaussianSHTransform == (nt3dmath.Mat4{}) {
		return nt3dmath.Identity()
	}
	m := g.GaussianSHTransform
	m[12], m[13], m[14] = 0, 0, 0 // Directions are independent of translations.
	return m
}

// validateGaussianSH checks the SH, its palette and labels. certified means a
// scene already checked this palette and these labels (storedGeometry).
func validateGaussianSH(g Geometry, certified bool) error {
	if err := validateSHPalette(g, certified); err != nil {
		return err
	}
	if len(g.GaussianSH) == 0 {
		return nil
	}
	n := gaussianSHCount(&g)
	if g.PointStyle.Shape != GaussianPoint || nt3dsh.Degree(n) < 0 || len(g.GaussianSH) != n*len(g.Points) {
		return fmt.Errorf("nt3d: Gaussian SH requires 1, 4, 9, or 16 RGB coefficients per Gaussian")
	}
	if _, ok := gaussianSHMatrix(&g).InverseAffine(); !ok {
		return fmt.Errorf("nt3d: invalid Gaussian SH coordinate transform")
	}
	for _, c := range g.GaussianSH {
		if !validFloat(c[:]...) {
			return fmt.Errorf("nt3d: nonfinite Gaussian SH")
		}
	}
	return nil
}

func (f *Frame) gaussianColor(g *Geometry, index int, world, shFromWorld nt3dmath.Mat4) [3]float32 {
	p := g.Points[index]
	ray := f.Forward
	if f.Perspective {
		ray = world.Point(p.Position).Sub(f.Eye)
	}
	var coefficients []nt3dsh.RGB
	if pal := g.GaussianSHPalette; pal != nil {
		var scratch [16]nt3dsh.RGB
		scratch[0] = g.GaussianSH[index]
		coefficients = scratch[:1]
		if !g.PointStyle.SHDCOnly {
			stride, label := pal.Stride(), int(g.GaussianSHLabels[index])
			coefficients = scratch[:1+copy(scratch[1:], pal.Coefficients[label*stride:(label+1)*stride])]
		}
	} else {
		n := gaussianSHCount(g)
		coefficients = g.GaussianSH[index*n : (index+1)*n]
		if g.PointStyle.SHDCOnly {
			coefficients = coefficients[:1]
		}
	}
	rgb := nt3dsh.Evaluate(coefficients, shFromWorld.Direction(ray))
	for k := range rgb {
		rgb[k] = max(0, rgb[k]+.5)
		if f.Linear {
			rgb[k] = float32(decodeSRGB(float64(rgb[k])))
		}
	}
	return rgb
}
