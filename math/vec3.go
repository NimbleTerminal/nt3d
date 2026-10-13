// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// Vec3 is a 3D vector or position.
type Vec3 struct{ X, Y, Z float32 }

// Add returns a + b without changing either vector.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a - b without changing either vector.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Scale returns a multiplied by s without changing a.
func (a Vec3) Scale(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Dot returns the dot product of a and b.
func (a Vec3) Dot(b Vec3) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross returns the cross product a × b without changing either vector.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// Normalize returns a unit vector in a's direction, or a if it is zero.
// It does not change a.
func (a Vec3) Normalize() Vec3 {
	// Use float64 intermediates to avoid overflow or underflow in the length.
	x, y, z := float64(a.X), float64(a.Y), float64(a.Z)
	n := math.Sqrt(x*x + y*y + z*z)
	if n == 0 {
		return a
	}
	inv := 1 / n
	return Vec3{float32(x * inv), float32(y * inv), float32(z * inv)}
}
