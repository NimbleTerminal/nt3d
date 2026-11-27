// Copyright (c) 2026 Neomantra Corp.

package nt3dgeo

import (
	"errors"

	"nimbleterminal.dev/nt3d/math"
)

// ENU is a local frame on the Earth: meters east (X), north (Y), and up (Z)
// of Origin, flat where the Earth curves away. It is the frame to draw a
// scene in, being Z-up like nt3d's world. The zero value sits where the
// equator meets the prime meridian. Origin must be valid.
//
// Two frames a few kilometers apart are turned slightly against each other,
// and a point 10 km from an origin lies about 8 m below its plane: go between
// frames through ECEF, with ToECEF and FromECEF, and nothing is ignored.
type ENU struct {
	Origin Geodetic
}

// ToECEF returns the transform from this frame's meters to Earth-centered
// meters. It is the matrix a 3D Tiles tile carries as its transform when its
// content is modeled east-north-up around Origin.
func (f ENU) ToECEF() nt3dmath.Mat4d {
	sinLat, cosLat := sincosDegrees(f.Origin.Lat)
	sinLon, cosLon := sincosDegrees(f.Origin.Lon)
	o := f.Origin.ECEF()
	return nt3dmath.Mat4d{
		-sinLon, cosLon, 0, 0,
		-sinLat * cosLon, -sinLat * sinLon, cosLat, 0,
		cosLat * cosLon, cosLat * sinLon, sinLat, 0,
		o.X, o.Y, o.Z, 1,
	}
}

// FromECEF returns the transform from Earth-centered meters to this frame's
// meters, the inverse of ToECEF.
func (f ENU) FromECEF() nt3dmath.Mat4d {
	m := f.ToECEF()
	// The frame's axes are unit and perpendicular, so the inverse of the
	// rotation is its transpose, exactly.
	inv := nt3dmath.Mat4d{
		m[0], m[4], m[8], 0,
		m[1], m[5], m[9], 0,
		m[2], m[6], m[10], 0,
		0, 0, 0, 1,
	}
	t := inv.Direction(nt3dmath.Vec3d{X: m[12], Y: m[13], Z: m[14]})
	inv[12], inv[13], inv[14] = -t.X, -t.Y, -t.Z
	return inv
}

// Point returns where g is in this frame. g must be valid.
func (f ENU) Point(g Geodetic) nt3dmath.Vec3d {
	// Subtract the origin before turning, so the millions of meters cancel
	// while the numbers still have the bits for it.
	m := f.ToECEF()
	d := g.ECEF().Sub(nt3dmath.Vec3d{X: m[12], Y: m[13], Z: m[14]})
	return nt3dmath.Vec3d{
		X: d.X*m[0] + d.Y*m[1] + d.Z*m[2],
		Y: d.X*m[4] + d.Y*m[5] + d.Z*m[6],
		Z: d.X*m[8] + d.Y*m[9] + d.Z*m[10],
	}
}

// Geodetic returns the position on the Earth of the point p of this frame.
func (f ENU) Geodetic(p nt3dmath.Vec3d) (Geodetic, error) {
	return FromECEF(f.ToECEF().Point(p))
}

// Place turns toECEF, a transform that carries a model into Earth-centered
// meters, into what places the model in a scene drawn in this frame: origin
// for Scene.SetNodeOrigin and local for the node's transform. The two are
// composed in float64 and split, so that the meters from this frame's origin
// stay in float64 and local, in float32, carries only how the model is
// turned and scaled. A camera in the same scene keeps its own position in
// Camera.Origin, and the scene subtracts the two before it rounds.
//
// toECEF is typically the product of a 3D Tiles tile's transform and its
// ancestors'. One that is not finite or not affine is an error.
func (f ENU) Place(toECEF nt3dmath.Mat4d) (origin [3]float64, local nt3dmath.Mat4, err error) {
	if !toECEF.IsFinite() || toECEF[3] != 0 || toECEF[7] != 0 || toECEF[11] != 0 || toECEF[15] != 1 {
		return origin, local, errors.New("nt3dgeo: transform must be finite and affine")
	}
	a := f.FromECEF().Mul(toECEF)
	if !a.IsFinite() {
		return origin, local, errors.New("nt3dgeo: transform is too large to place")
	}
	origin = [3]float64{a[12], a[13], a[14]}
	a[12], a[13], a[14] = 0, 0, 0
	return origin, a.Mat4(), nil
}
