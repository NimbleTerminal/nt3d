// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// Quatd is a rotation as a unit quaternion (X, Y, Z, W) in float64. As a
// camera orientation it takes camera space, +X right, +Y up, -Z forward, to
// world space.
type Quatd struct {
	X, Y, Z, W float64
}

// QuatdIdentity returns the identity unit quaternion (0, 0, 0, 1).
func QuatdIdentity() Quatd { return Quatd{W: 1} }

// IsFinite reports whether all components of q are finite numbers.
func (q Quatd) IsFinite() bool {
	return !math.IsNaN(q.X) && !math.IsInf(q.X, 0) &&
		!math.IsNaN(q.Y) && !math.IsInf(q.Y, 0) &&
		!math.IsNaN(q.Z) && !math.IsInf(q.Z, 0) &&
		!math.IsNaN(q.W) && !math.IsInf(q.W, 0)
}

// NormSq returns x^2 + y^2 + z^2 + w^2.
func (q Quatd) NormSq() float64 {
	return q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W
}

// Norm returns sqrt(NormSq).
func (q Quatd) Norm() float64 {
	return math.Sqrt(q.NormSq())
}

// Normalize returns q scaled to unit length, or Identity if q is zero.
func (q Quatd) Normalize() Quatd {
	n := q.Norm()
	if n == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return QuatdIdentity()
	}
	inv := 1.0 / n
	return Quatd{q.X * inv, q.Y * inv, q.Z * inv, q.W * inv}
}

// Neg returns -q (which represents the same 3D rotation on SO(3)).
func (q Quatd) Neg() Quatd { return Quatd{-q.X, -q.Y, -q.Z, -q.W} }

// Conj returns the quaternion conjugate (-x, -y, -z, w). For unit quaternions, Conj == Inv.
func (q Quatd) Conj() Quatd { return Quatd{-q.X, -q.Y, -q.Z, q.W} }

// Dot returns the 4D inner product a · b.
func (a Quatd) Dot(b Quatd) float64 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
}

// Mul returns the Hamilton product a ⊗ b. Rotating by a.Mul(b) applies b first, then a.
func (a Quatd) Mul(b Quatd) Quatd {
	return Quatd{
		X: a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		Y: a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		Z: a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
		W: a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
	}
}

// QuatdAxisAngle returns the unit quaternion rotating around axis by radians (right-handed).
func QuatdAxisAngle(axis Vec3d, radians float64) Quatd {
	u := axis.Normalize()
	if u.LenSq() == 0 {
		return QuatdIdentity()
	}
	half := 0.5 * radians
	s, c := math.Sin(half), math.Cos(half)
	return Quatd{X: u.X * s, Y: u.Y * s, Z: u.Z * s, W: c}.Normalize()
}

// Rotate rotates v by unit quaternion q.
func (q Quatd) Rotate(v Vec3d) Vec3d {
	qv := Vec3d{X: q.X, Y: q.Y, Z: q.Z}
	t := qv.Cross(v).Scale(2)
	return v.Add(t.Scale(q.W)).Add(qv.Cross(t))
}

// Basis returns the world-space right (+X), up (+Y), and forward (-Z) unit axes of q.
func (q Quatd) Basis() (right, up, forward Vec3d) {
	u := q.Normalize()
	right = u.Rotate(Vec3d{X: 1})
	up = u.Rotate(Vec3d{Y: 1})
	forward = u.Rotate(Vec3d{Z: -1})
	return
}

// QuatdFromBasis constructs a unit camera-to-world quaternion from orthonormal world
// axes where camera-local +X = right, +Y = up, and -Z = forward.
func QuatdFromBasis(right, up, forward Vec3d) Quatd {
	back := forward.Scale(-1)
	m00, m10, m20 := right.X, right.Y, right.Z
	m01, m11, m21 := up.X, up.Y, up.Z
	m02, m12, m22 := back.X, back.Y, back.Z

	tr := m00 + m11 + m22
	var q Quatd
	if tr > 0 {
		s := math.Sqrt(tr+1.0) * 2
		q = Quatd{
			X: (m21 - m12) / s,
			Y: (m02 - m20) / s,
			Z: (m10 - m01) / s,
			W: 0.25 * s,
		}
	} else if m00 > m11 && m00 > m22 {
		s := math.Sqrt(1.0+m00-m11-m22) * 2
		q = Quatd{
			X: 0.25 * s,
			Y: (m01 + m10) / s,
			Z: (m02 + m20) / s,
			W: (m21 - m12) / s,
		}
	} else if m11 > m22 {
		s := math.Sqrt(1.0+m11-m00-m22) * 2
		q = Quatd{
			X: (m01 + m10) / s,
			Y: 0.25 * s,
			Z: (m12 + m21) / s,
			W: (m02 - m20) / s,
		}
	} else {
		s := math.Sqrt(1.0+m22-m00-m11) * 2
		q = Quatd{
			X: (m02 + m20) / s,
			Y: (m12 + m21) / s,
			Z: 0.25 * s,
			W: (m10 - m01) / s,
		}
	}
	return q.Normalize()
}

// QuatdLookRotation constructs a unit camera-to-world quaternion aiming local -Z along
// forward with camera +Y aligned toward worldUp. Collinear forward and worldUp
// select a deterministic orthogonal up axis without NaN.
func QuatdLookRotation(forward, worldUp Vec3d) Quatd {
	f := forward.Normalize()
	if f.LenSq() == 0 {
		f = Vec3d{Y: 1}
	}
	uRef := worldUp.Normalize()
	if uRef.LenSq() == 0 {
		uRef = Vec3d{Z: 1}
	}
	right := f.Cross(uRef)
	if right.LenSq() < 1e-16 {
		alt := Vec3d{Y: 1}
		if math.Abs(f.Y) > 0.9 {
			alt = Vec3d{X: 1}
		}
		right = f.Cross(alt)
	}
	right = right.Normalize()
	up := right.Cross(f).Normalize()
	return QuatdFromBasis(right, up, f)
}

// Slerp performs shortest-arc spherical linear interpolation between unit
// quaternions a (u=0) and b (u=1).
func (a Quatd) Slerp(b Quatd, u float64) Quatd {
	a = a.Normalize()
	b = b.Normalize()
	d := a.Dot(b)
	if d < 0 {
		b = b.Neg()
		d = -d
	}
	if u <= 0 {
		return a
	}
	if u >= 1 {
		return b
	}
	if d > 1-1e-9 {
		return Quatd{
			X: a.X + u*(b.X-a.X),
			Y: a.Y + u*(b.Y-a.Y),
			Z: a.Z + u*(b.Z-a.Z),
			W: a.W + u*(b.W-a.W),
		}.Normalize()
	}
	theta := math.Acos(math.Min(1, d))
	sinTheta := math.Sin(theta)
	wa := math.Sin((1-u)*theta) / sinTheta
	wb := math.Sin(u*theta) / sinTheta
	return Quatd{
		X: wa*a.X + wb*b.X,
		Y: wa*a.Y + wb*b.Y,
		Z: wa*a.Z + wb*b.Z,
		W: wa*a.W + wb*b.W,
	}.Normalize()
}
