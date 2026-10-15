// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dmath

// AABB is an axis-aligned bounding box with minimum and maximum corners.
// The zero value is empty until Include is called.
type AABB struct {
	Min, Max Vec3
	valid    bool
}

// IsValid returns true if the bounding box is valid (has been initialized with Include).
func (b AABB) IsValid() bool {
	return b.valid
}

// Include expands b to contain p. A non-finite p is ignored: min and max
// would carry its NaN to every later bound.
func (b *AABB) Include(p Vec3) {
	if !IsFinite(float64(p.X)) || !IsFinite(float64(p.Y)) || !IsFinite(float64(p.Z)) {
		return
	}
	if !b.valid {
		b.Min = p
		b.Max = p
		b.valid = true
		return
	}
	b.Min = Vec3{min(b.Min.X, p.X), min(b.Min.Y, p.Y), min(b.Min.Z, p.Z)}
	b.Max = Vec3{max(b.Max.X, p.X), max(b.Max.Y, p.Y), max(b.Max.Z, p.Z)}
}

// Normalization returns a matrix that centers b at the origin and scales its
// longest extent to two units, along with the original center and scale factor.
// Extents below 0.001 use 0.001. An invalid box returns identity, zero, and one.
func (b AABB) Normalization() (Mat4, Vec3, float32) {
	m := Identity()
	if !b.valid {
		return m, Vec3{}, 1
	}
	center := b.Min.Add(b.Max).Scale(.5)
	d := b.Max.Sub(b.Min)
	s := float32(2) / max(d.X, d.Y, d.Z, .001)
	m[0] = s
	m[5] = s
	m[10] = s
	m[12] = -center.X * s
	m[13] = -center.Y * s
	m[14] = -center.Z * s
	return m, center, s
}
