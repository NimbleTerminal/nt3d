// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// Mat4d is Mat4 in float64: a column-major transform for where float32 does
// not reach, such as node transforms carrying geographic origins and the
// matrices a model file stores. Compose in float64 and round once, with Mat4.
type Mat4d [16]float64

// IdentityD returns the float64 identity transform.
func IdentityD() Mat4d { return Mat4d{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }

// D returns m in float64.
func (m Mat4) D() (out Mat4d) {
	for k := range m {
		out[k] = float64(m[k])
	}
	return out
}

// Mat4 returns a rounded to float32.
func (a Mat4d) Mat4() (out Mat4) {
	for k := range a {
		out[k] = float32(a[k])
	}
	return out
}

// Mul returns a*b, the transform that applies b and then a.
func (a Mat4d) Mul(b Mat4d) (m Mat4d) {
	for c := range 4 {
		for r := range 4 {
			m[c*4+r] = a[r]*b[c*4] +
				a[4+r]*b[c*4+1] +
				a[8+r]*b[c*4+2] +
				a[12+r]*b[c*4+3]
		}
	}
	return m
}

// Point transforms a position by the affine transform a.
func (a Mat4d) Point(p Vec3d) Vec3d {
	x, y, z := p.X, p.Y, p.Z
	return Vec3d{a[0]*x + a[4]*y + a[8]*z + a[12], a[1]*x + a[5]*y + a[9]*z + a[13], a[2]*x + a[6]*y + a[10]*z + a[14]}
}

// Direction transforms a direction by a's linear part, ignoring translation.
func (a Mat4d) Direction(v Vec3d) Vec3d {
	x, y, z := v.X, v.Y, v.Z
	return Vec3d{a[0]*x + a[4]*y + a[8]*z, a[1]*x + a[5]*y + a[9]*z, a[2]*x + a[6]*y + a[10]*z}
}

// Determinant3 returns the determinant of a's linear part: negative when a
// mirrors, zero when it flattens.
func (a Mat4d) Determinant3() float64 {
	return a[0]*(a[5]*a[10]-a[9]*a[6]) - a[4]*(a[1]*a[10]-a[9]*a[2]) + a[8]*(a[1]*a[6]-a[5]*a[2])
}

// IsFinite reports whether every element of a is a finite number.
func (a Mat4d) IsFinite() bool {
	for _, v := range a {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// TRSd composes a float64 transform from a translation, a rotation, and a
// scale, applied to a point in the order scale, rotation, translation. A
// rotation that is not zero is normalized first.
func TRSd(t Vec3d, q Quatd, s Vec3d) Mat4d {
	x, y, z, w := q.X, q.Y, q.Z, q.W
	if l := math.Sqrt(x*x + y*y + z*z + w*w); l > 0 {
		x, y, z, w = x/l, y/l, z/l, w/l
	}
	r := Mat4d{
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0,
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0,
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0,
		0, 0, 0, 1,
	}
	scale := [3]float64{s.X, s.Y, s.Z}
	for c := range 3 {
		for k := range 3 {
			r[c*4+k] *= scale[c]
		}
	}
	r[12], r[13], r[14] = t.X, t.Y, t.Z
	return r
}

// InverseAffine returns the inverse of the affine transform a, and false with
// the zero matrix for one that is not finite, not affine, or singular. It is
// Mat4.InverseAffine in float64, for transforms whose translation float32
// cannot hold, such as a tile's placement on the Earth.
func (a Mat4d) InverseAffine() (Mat4d, bool) {
	if !a.IsFinite() || a[3] != 0 || a[7] != 0 || a[11] != 0 || a[15] != 1 {
		return Mat4d{}, false
	}
	det := a.Determinant3()
	if det == 0 || !IsFinite(det) {
		return Mat4d{}, false
	}
	out := IdentityD()
	out[0], out[4], out[8] = (a[5]*a[10]-a[9]*a[6])/det, (a[8]*a[6]-a[4]*a[10])/det, (a[4]*a[9]-a[8]*a[5])/det
	out[1], out[5], out[9] = (a[9]*a[2]-a[1]*a[10])/det, (a[0]*a[10]-a[8]*a[2])/det, (a[8]*a[1]-a[0]*a[9])/det
	out[2], out[6], out[10] = (a[1]*a[6]-a[5]*a[2])/det, (a[4]*a[2]-a[0]*a[6])/det, (a[0]*a[5]-a[4]*a[1])/det
	t := out.Direction(Vec3d{a[12], a[13], a[14]})
	out[12], out[13], out[14] = -t.X, -t.Y, -t.Z
	if !out.IsFinite() {
		return Mat4d{}, false
	}
	return out, true
}
