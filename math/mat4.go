// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dmath

// Mat4 is a column-major 4×4 matrix. Projection uses WebGPU depth range [0, 1].
type Mat4 [16]float32

// Identity returns the identity matrix.
func Identity() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1}
}

// Mul returns a * b without changing either matrix. When transforming a point,
// the resulting matrix applies b first, then a.
func (a Mat4) Mul(b Mat4) (m Mat4) {
	// SIMD on its way....
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

// Transform multiplies m by the position (p.X, p.Y, p.Z, 1).
// It returns homogeneous coordinates without dividing by w.
func (m Mat4) Transform(p Vec3) (x, y, z, w float32) {
	x = m[0]*p.X + m[4]*p.Y + m[8]*p.Z + m[12]
	y = m[1]*p.X + m[5]*p.Y + m[9]*p.Z + m[13]
	z = m[2]*p.X + m[6]*p.Y + m[10]*p.Z + m[14]
	w = m[3]*p.X + m[7]*p.Y + m[11]*p.Z + m[15]
	return x, y, z, w
}

// Project maps p to a viewport of width w and height h, with Y increasing down.
// ok reports whether p is inside the clip volume, with depth z in [0, 1].
// If the homogeneous w is nonpositive, it returns zeros and false.
func (m Mat4) Project(p Vec3, w, h int) (x, y, z float32, ok bool) {
	a, b, c, d := m.Transform(p)
	if d <= 0 {
		return 0, 0, 0, false
	}
	a /= d
	b /= d
	c /= d
	x = (a + 1) * float32(w) * .5
	y = (1 - b) * float32(h) * .5
	z = c
	ok = a >= -1 && a <= 1 && b >= -1 && b <= 1 && c >= 0 && c <= 1
	return x, y, z, ok
}
