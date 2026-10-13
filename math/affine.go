// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

func Translation(v Vec3) Mat4 {
	m := Identity()
	m[12], m[13], m[14] = v.X, v.Y, v.Z
	return m
}
func Scaling(v Vec3) Mat4 {
	m := Identity()
	m[0], m[5], m[10] = v.X, v.Y, v.Z
	return m
}

// RotationZ rotates counterclockwise about Z, in radians.
func RotationZ(radians float64) Mat4 {
	m := Identity()
	c, s := float32(math.Cos(radians)), float32(math.Sin(radians))
	m[0], m[1], m[4], m[5] = c, s, -s, c
	return m
}
func (m Mat4) Point(p Vec3) Vec3 { x, y, z, _ := m.Transform(p); return Vec3{x, y, z} }
func (m Mat4) Direction(v Vec3) Vec3 {
	return Vec3{m[0]*v.X + m[4]*v.Y + m[8]*v.Z, m[1]*v.X + m[5]*v.Y + m[9]*v.Z, m[2]*v.X + m[6]*v.Y + m[10]*v.Z}
}
func (m Mat4) Transpose() (out Mat4) {
	for i := range 4 {
		for j := range 4 {
			out[i*4+j] = m[j*4+i]
		}
	}
	return
}

// InverseAffine rejects non-finite, singular, and non-affine matrices.
func (m Mat4) InverseAffine() (Mat4, bool) {
	for _, v := range m {
		if !IsFinite(float64(v)) {
			return Mat4{}, false
		}
	}
	if m[3] != 0 || m[7] != 0 || m[11] != 0 || m[15] != 1 {
		return Mat4{}, false
	}
	a, b, c, d, e, f, g, h, i := float64(m[0]), float64(m[4]), float64(m[8]), float64(m[1]), float64(m[5]), float64(m[9]), float64(m[2]), float64(m[6]), float64(m[10])
	det := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if det == 0 || !IsFinite(det) {
		return Mat4{}, false
	}
	out := Identity()
	out[0], out[4], out[8] = float32((e*i-f*h)/det), float32((c*h-b*i)/det), float32((b*f-c*e)/det)
	out[1], out[5], out[9] = float32((f*g-d*i)/det), float32((a*i-c*g)/det), float32((c*d-a*f)/det)
	out[2], out[6], out[10] = float32((d*h-e*g)/det), float32((b*g-a*h)/det), float32((a*e-b*d)/det)
	t := out.Direction(Vec3{m[12], m[13], m[14]}).Scale(-1)
	out[12], out[13], out[14] = t.X, t.Y, t.Z
	for _, v := range out {
		if !IsFinite(float64(v)) {
			return Mat4{}, false
		}
	}
	return out, true
}
func (m Mat4) Determinant3() float64 {
	return float64(m[0])*(float64(m[5])*float64(m[10])-float64(m[9])*float64(m[6])) - float64(m[4])*(float64(m[1])*float64(m[10])-float64(m[9])*float64(m[2])) + float64(m[8])*(float64(m[1])*float64(m[6])-float64(m[5])*float64(m[2]))
}
func (b AABB) Transformed(m Mat4) (out AABB) {
	if !b.IsValid() {
		return
	}
	for _, x := range []float32{b.Min.X, b.Max.X} {
		for _, y := range []float32{b.Min.Y, b.Max.Y} {
			for _, z := range []float32{b.Min.Z, b.Max.Z} {
				out.Include(m.Point(Vec3{x, y, z}))
			}
		}
	}
	return
}
