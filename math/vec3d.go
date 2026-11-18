// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// Vec3d is Vec3 in float64: a position or direction where float32 does not
// reach, such as geographic coordinates and camera paths. It converts to a
// Vec3 with Vec3 and from one with Vec3.D.
type Vec3d struct {
	X, Y, Z float64
}

// D returns v in float64.
func (v Vec3) D() Vec3d { return Vec3d{float64(v.X), float64(v.Y), float64(v.Z)} }

// Vec3 returns a rounded to float32.
func (a Vec3d) Vec3() Vec3 { return Vec3{float32(a.X), float32(a.Y), float32(a.Z)} }

// Add returns a + b.
func (a Vec3d) Add(b Vec3d) Vec3d { return Vec3d{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a - b.
func (a Vec3d) Sub(b Vec3d) Vec3d { return Vec3d{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Scale returns a * s.
func (a Vec3d) Scale(s float64) Vec3d { return Vec3d{a.X * s, a.Y * s, a.Z * s} }

// Dot returns the dot product a · b.
func (a Vec3d) Dot(b Vec3d) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross returns the cross product a × b.
func (a Vec3d) Cross(b Vec3d) Vec3d {
	return Vec3d{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// LenSq returns |a|^2.
func (a Vec3d) LenSq() float64 { return a.X*a.X + a.Y*a.Y + a.Z*a.Z }

// Len returns |a|.
func (a Vec3d) Len() float64 { return math.Sqrt(a.LenSq()) }

// Dist returns |a - b|.
func (a Vec3d) Dist(b Vec3d) float64 { return a.Sub(b).Len() }

// Normalize returns a / |a|, or zero if |a| == 0.
func (a Vec3d) Normalize() Vec3d {
	n := a.Len()
	if n == 0 {
		return Vec3d{}
	}
	inv := 1.0 / n
	return Vec3d{a.X * inv, a.Y * inv, a.Z * inv}
}

// Lerp linearly interpolates between a (u=0) and b (u=1).
func (a Vec3d) Lerp(b Vec3d, u float64) Vec3d {
	return Vec3d{
		X: a.X + u*(b.X-a.X),
		Y: a.Y + u*(b.Y-a.Y),
		Z: a.Z + u*(b.Z-a.Z),
	}
}

// IsFinite reports whether all components of a are finite numbers.
func (a Vec3d) IsFinite() bool {
	return !math.IsNaN(a.X) && !math.IsInf(a.X, 0) &&
		!math.IsNaN(a.Y) && !math.IsInf(a.Y, 0) &&
		!math.IsNaN(a.Z) && !math.IsInf(a.Z, 0)
}
