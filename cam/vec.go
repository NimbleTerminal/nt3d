// Copyright (c) 2026 Neomantra Corp.

package nt3dcam

import nt3dmath "nimbleterminal.dev/nt3d/math"

// Vec3d is a 3D position or direction vector in float64 meters or unitless
// coordinates: nt3dmath.Vec3d, whose methods are documented there.
type Vec3d = nt3dmath.Vec3d

// DefaultUp is the default world up vector (+Z), matching NT3D, Blender, and ENU.
var DefaultUp = Vec3d{Z: 1}

// Quatd is a unit quaternion (X, Y, Z, W) in float64: nt3dmath.Quatd, whose
// methods are documented there. Valid camera rotations have unit length and
// map camera-local axes (+X right, +Y up, -Z forward) into world coordinates.
type Quatd = nt3dmath.Quatd

// IdentityQuat returns the identity rotation quaternion (0, 0, 0, 1).
func IdentityQuat() Quatd { return nt3dmath.QuatdIdentity() }

// AxisAngleQuat returns the unit quaternion rotating around axis by radians (right-handed).
func AxisAngleQuat(axis Vec3d, radians float64) Quatd { return nt3dmath.QuatdAxisAngle(axis, radians) }

// LookRotationQuat returns the unit camera-to-world quaternion aiming local -Z
// along forward with camera +Y aligned toward worldUp.
func LookRotationQuat(forward, worldUp Vec3d) Quatd {
	return nt3dmath.QuatdLookRotation(forward, worldUp)
}

// QuatFromBasis constructs a unit camera-to-world quaternion from orthonormal
// camera axes right (+X), up (+Y), and forward (-Z) in world coordinates.
func QuatFromBasis(right, up, forward Vec3d) Quatd {
	return nt3dmath.QuatdFromBasis(right, up, forward)
}

// GeoOrigin anchors an East-North-Up (FrameENU) coordinate frame in WGS84
// geodetic degrees and ellipsoidal altitude in meters.
type GeoOrigin struct {
	LatDeg float64
	LonDeg float64
	AltM   float64
}
