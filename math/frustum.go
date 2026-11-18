// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

// Outside reports whether the box from lower to upper, taken by m into clip
// space, lies wholly outside the view volume, widened by padX and padY (in
// normalized device units) to the sides: every corner beyond one of its six
// planes. The planes are linear in clip coordinates, so the test holds for
// corners behind the eye too. A box that straddles a corner of the volume
// without entering it is kept, as it is cheap to draw for nothing.
//
// This is the culling test of every nt3d renderer, and the clustering shader
// repeats it expression for expression, so what one keeps the others draw.
func (m *Mat4) Outside(lower, upper Vec3, padX, padY float32) bool {
	common := 0x3f
	for i := range 8 {
		p := lower
		if i&1 != 0 {
			p.X = upper.X
		}
		if i&2 != 0 {
			p.Y = upper.Y
		}
		if i&4 != 0 {
			p.Z = upper.Z
		}
		x, y, z, w := m.Transform(p)
		sides := 0
		if x < -w*(1+padX) {
			sides |= 1
		}
		if x > w*(1+padX) {
			sides |= 2
		}
		if y < -w*(1+padY) {
			sides |= 4
		}
		if y > w*(1+padY) {
			sides |= 8
		}
		if z < 0 {
			sides |= 16
		}
		if z > w {
			sides |= 32
		}
		if common &= sides; common == 0 {
			return false
		}
	}
	return true
}
