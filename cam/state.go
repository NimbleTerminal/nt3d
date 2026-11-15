// Copyright (c) 2026 Neomantra Corp.

package nt3dcam

import (
	"math"
	"time"

	nt3dease "nimbleterminal.dev/nt3d/cam/ease"
)

// DefaultFovY is the default vertical perspective field of view in radians (45 degrees).
const DefaultFovY = math.Pi / 4

// DefaultSensorHeightMM is the vertical gate height of a standard 36x24 mm full-frame sensor.
const DefaultSensorHeightMM = 24.0

// CameraState is the pure float64 result of evaluating or blending a camera at
// a single instant.
//
// Pos is in world/frame meters. Rot is a unit XYZW camera-to-world quaternion
// where local +X is right, +Y is up, and -Z is forward. FovY is vertical field
// of view in radians. FocusDist (meters) and Aperture (f-number, e.g. 2.8; 0
// means pinhole/disabled) describe depth of field. Near and Far are static clip
// distances in meters (0 defers to host automatic clipping).
type CameraState struct {
	Pos       Vec3d
	Rot       Quatd
	FovY      float64
	FocusDist float64
	Aperture  float64
	Near      float64
	Far       float64
}

// DefaultCameraState returns a sanitized default CameraState at the origin
// with identity rotation and 45-degree vertical field of view.
func DefaultCameraState() CameraState {
	return CameraState{
		Rot:  IdentityQuat(),
		FovY: DefaultFovY,
	}
}

// Forward returns the world-space unit viewing direction (-Z local) of s.
func (s CameraState) Forward() Vec3d {
	_, _, f := s.Rot.Basis()
	return f
}

// Up returns the world-space unit up vector (+Y local) of s.
func (s CameraState) Up() Vec3d {
	_, u, _ := s.Rot.Basis()
	return u
}

// Right returns the world-space unit right vector (+X local) of s.
func (s CameraState) Right() Vec3d {
	r, _, _ := s.Rot.Basis()
	return r
}

// TargetInfo describes a resolved external reference ("node:...", "panel:...", "path:...").
// Normal, Width, and Height are optional and populated when resolving spatial UI panels.
type TargetInfo struct {
	Pos    Vec3d
	Normal Vec3d
	Width  float64
	Height float64
}

// Resolver resolves external target references ("node:...", "panel:...", "path:...")
// at evaluation time t without coupling nt3dcam to a specific scene host.
// Implementations must be pure and safe for concurrent calls when Path.EvalWith
// is invoked concurrently.
type Resolver interface {
	ResolveTarget(ref string, t time.Duration) (TargetInfo, bool)
}

// ResolverFunc adapts a function to the Resolver interface.
type ResolverFunc func(ref string, t time.Duration) (TargetInfo, bool)

// ResolveTarget calls f(ref, t).
func (f ResolverFunc) ResolveTarget(ref string, t time.Duration) (TargetInfo, bool) {
	if f == nil {
		return TargetInfo{}, false
	}
	return f(ref, t)
}

// Blend interpolates linearly in position and lens scalars and via shortest-arc
// slerp in orientation between a (alpha <= 0) and b (alpha >= 1).
func Blend(a, b CameraState, alpha float64) CameraState {
	if !(alpha > 0) {
		a.Rot = a.Rot.Normalize()
		return a
	}
	if alpha >= 1 {
		b.Rot = b.Rot.Normalize()
		return b
	}
	fovA := a.FovY
	if fovA <= 0 {
		fovA = DefaultFovY
	}
	fovB := b.FovY
	if fovB <= 0 {
		fovB = DefaultFovY
	}
	near := blendClip(a.Near, b.Near, alpha)
	far := blendClip(a.Far, b.Far, alpha)
	if far > 0 && near >= far {
		near = far * 1e-4
	}
	return CameraState{
		Pos:       a.Pos.Lerp(b.Pos, alpha),
		Rot:       a.Rot.Slerp(b.Rot, alpha),
		FovY:      fovA + alpha*(fovB-fovA),
		FocusDist: a.FocusDist + alpha*(b.FocusDist-a.FocusDist),
		Aperture:  a.Aperture + alpha*(b.Aperture-a.Aperture),
		Near:      near,
		Far:       far,
	}
}

func blendClip(a, b, alpha float64) float64 {
	if a > 0 && b > 0 {
		return a + alpha*(b-a)
	}
	if alpha < 0.5 {
		return a
	}
	return b
}

// Switch selects or crossfades across an ordered slice of camera states using a
// continuous selector sel in [0, len(states)-1] (e.g. a Nebula switcher node).
// Integer sel values perform exact cuts; fractional values blend adjacent cameras
// shaped by ease.
func Switch(states []CameraState, sel float64, ease EaseSpec) CameraState {
	n := len(states)
	if n == 0 {
		return DefaultCameraState()
	}
	if n == 1 || !(sel > 0) {
		s := states[0]
		s.Rot = s.Rot.Normalize()
		return s
	}
	if sel >= float64(n-1) {
		s := states[n-1]
		s.Rot = s.Rot.Normalize()
		return s
	}
	i := int(math.Floor(sel))
	frac := ease.toEase(nt3dease.Linear).Eval(sel - float64(i))
	return Blend(states[i], states[i+1], frac)
}

// FocalLengthToFovY converts focal length (mm) and vertical sensor height (mm)
// into vertical field of view in radians: 2 * atan(sensorHeight / (2 * focalLength)).
func FocalLengthToFovY(focalLengthMM, sensorHeightMM float64) float64 {
	if sensorHeightMM <= 0 {
		sensorHeightMM = DefaultSensorHeightMM
	}
	if !(focalLengthMM > 0) {
		return DefaultFovY
	}
	return 2 * math.Atan(sensorHeightMM/(2*focalLengthMM))
}

// FovYToFocalLength converts vertical field of view in radians and sensor height
// (mm) into focal length in millimeters.
func FovYToFocalLength(fovYRad, sensorHeightMM float64) float64 {
	if sensorHeightMM <= 0 {
		sensorHeightMM = DefaultSensorHeightMM
	}
	if !(fovYRad > 0) || fovYRad >= math.Pi {
		fovYRad = DefaultFovY
	}
	return sensorHeightMM / (2 * math.Tan(0.5*fovYRad))
}

// FitRect returns the camera position and orientation along normal that frames
// a rectangular surface of width x height meters at vertical field of view
// fovYRad and viewport aspect (width/height), padded by margin (1.0 = exact fit).
func FitRect(center, normal, worldUp Vec3d, width, height, fovYRad, aspect, margin float64) (pos Vec3d, rot Quatd) {
	n := normal.Normalize()
	if n.LenSq() == 0 {
		n = Vec3d{Y: -1}
	}
	if !(fovYRad > 0) || fovYRad >= math.Pi {
		fovYRad = DefaultFovY
	}
	if !(aspect > 0) || math.IsInf(aspect, 0) {
		aspect = 1
	}
	if !(margin > 0) || math.IsInf(margin, 0) {
		margin = 1
	}
	tanHalfY := math.Tan(0.5 * fovYRad)
	tanHalfX := tanHalfY * aspect
	distY := 0.0
	if height > 0 {
		distY = (0.5 * height) / tanHalfY
	}
	distX := 0.0
	if width > 0 {
		distX = (0.5 * width) / tanHalfX
	}
	dist := math.Max(distX, distY) * margin
	if dist < 1e-4 {
		dist = 1
	}
	pos = center.Add(n.Scale(dist))
	rot = LookRotationQuat(n.Scale(-1), worldUp)
	return pos, rot
}
