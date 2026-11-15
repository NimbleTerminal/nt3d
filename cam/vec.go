// Copyright (c) 2026 Neomantra Corp.

package nt3dcam

import (
	"math"

	nt3dcurve "nimbleterminal.dev/nt3d/cam/curve"
	nt3dframes "nimbleterminal.dev/nt3d/cam/frames"
)

// Vec3d is a 3D position or direction vector in float64 meters or unitless coordinates.
type Vec3d struct {
	X, Y, Z float64
}

// DefaultUp is the default world up vector (+Z), matching NT3D, Blender, and ENU.
var DefaultUp = Vec3d{Z: 1}

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

func (a Vec3d) toCurve() nt3dcurve.Vec3   { return nt3dcurve.Vec3{X: a.X, Y: a.Y, Z: a.Z} }
func fromCurveVec(v nt3dcurve.Vec3) Vec3d { return Vec3d{X: v.X, Y: v.Y, Z: v.Z} }

// Quatd is a float64 rotation quaternion in XYZW order (matching glTF 2.0).
// Valid camera rotations have unit length and map camera-local axes
// (+X right, +Y up, -Z forward) into world coordinates.
type Quatd struct {
	X, Y, Z, W float64
}

// IdentityQuat returns the identity rotation quaternion (0, 0, 0, 1).
func IdentityQuat() Quatd {
	return Quatd{W: 1}
}

// AxisAngleQuat returns the unit quaternion rotating around axis by radians (right-handed).
func AxisAngleQuat(axis Vec3d, radians float64) Quatd {
	return fromFrameQuat(nt3dframes.AxisAngle(axis.toCurve(), radians))
}

// LookRotationQuat returns the unit camera-to-world quaternion aiming local -Z
// along forward with camera +Y aligned toward worldUp.
func LookRotationQuat(forward, worldUp Vec3d) Quatd {
	return fromFrameQuat(nt3dframes.LookRotation(forward.toCurve(), worldUp.toCurve()))
}

// QuatFromBasis constructs a unit camera-to-world quaternion from orthonormal
// camera axes right (+X), up (+Y), and forward (-Z) in world coordinates.
func QuatFromBasis(right, up, forward Vec3d) Quatd {
	return fromFrameQuat(nt3dframes.FromBasis(right.toCurve(), up.toCurve(), forward.toCurve()))
}

// Norm returns sqrt(x^2 + y^2 + z^2 + w^2).
func (q Quatd) Norm() float64 { return q.toFrame().Norm() }

// Normalize scales q to unit length, returning IdentityQuat if q is zero or non-finite.
func (q Quatd) Normalize() Quatd { return fromFrameQuat(q.toFrame().Normalize()) }

// Conj returns the conjugate (-x, -y, -z, w).
func (q Quatd) Conj() Quatd { return Quatd{-q.X, -q.Y, -q.Z, q.W} }

// Dot returns the 4D inner product a · b.
func (a Quatd) Dot(b Quatd) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }

// Mul returns the Hamilton product a ⊗ b.
func (a Quatd) Mul(b Quatd) Quatd { return fromFrameQuat(a.toFrame().Mul(b.toFrame())) }

// Rotate rotates v by unit quaternion q.
func (q Quatd) Rotate(v Vec3d) Vec3d { return fromCurveVec(q.toFrame().Rotate(v.toCurve())) }

// Basis returns the world-space right (+X), up (+Y), and forward (-Z) unit axes of q.
func (q Quatd) Basis() (right, up, forward Vec3d) {
	r, u, f := q.toFrame().Basis()
	return fromCurveVec(r), fromCurveVec(u), fromCurveVec(f)
}

// Slerp performs shortest-arc spherical linear interpolation from a (u=0) to b (u=1).
func (a Quatd) Slerp(b Quatd, u float64) Quatd {
	return fromFrameQuat(nt3dframes.Slerp(a.toFrame(), b.toFrame(), u))
}

// IsFinite reports whether all components of q are finite numbers.
func (q Quatd) IsFinite() bool { return q.toFrame().IsFinite() }

func (q Quatd) toFrame() nt3dframes.Quat    { return nt3dframes.Quat{X: q.X, Y: q.Y, Z: q.Z, W: q.W} }
func fromFrameQuat(q nt3dframes.Quat) Quatd { return Quatd{X: q.X, Y: q.Y, Z: q.Z, W: q.W} }

// GeoOrigin anchors an East-North-Up (FrameENU) coordinate frame in WGS84
// geodetic degrees and ellipsoidal altitude in meters.
type GeoOrigin struct {
	LatDeg float64
	LonDeg float64
	AltM   float64
}
