// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// Quat is a rotation as a unit quaternion (X, Y, Z, W) in float32, the order
// glTF and animation tracks store. Quatd is its float64 counterpart.
type Quat struct {
	X, Y, Z, W float32
}

// QuatIdentity is no rotation: (0, 0, 0, 1).
func QuatIdentity() Quat { return Quat{W: 1} }

// D returns q in float64.
func (q Quat) D() Quatd { return Quatd{float64(q.X), float64(q.Y), float64(q.Z), float64(q.W)} }

// Quat returns q rounded to float32.
func (q Quatd) Quat() Quat { return Quat{float32(q.X), float32(q.Y), float32(q.Z), float32(q.W)} }

// Dot returns the 4D inner product a · b.
func (a Quat) Dot(b Quat) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }

// Normalize returns q scaled to unit length, or the identity if q is zero.
func (q Quat) Normalize() Quat {
	l := float32(math.Sqrt(float64(q.Dot(q))))
	if l == 0 {
		return QuatIdentity()
	}
	return Quat{q.X / l, q.Y / l, q.Z / l, q.W / l}
}

// Slerp interpolates along the shorter arc from a (t=0) to b (t=1), and
// returns a unit quaternion. Rotations nearly alike are mixed linearly, which
// is the same to within rounding and avoids dividing by a vanishing sine.
func (a Quat) Slerp(b Quat, t float32) Quat {
	a, b = a.Normalize(), b.Normalize()
	d := a.Dot(b)
	if d < 0 {
		b = Quat{-b.X, -b.Y, -b.Z, -b.W}
		d = -d
	}
	x, y := 1-t, t
	if d < .9995 {
		theta := math.Acos(float64(d))
		den := math.Sin(theta)
		x = float32(math.Sin(float64(1-t)*theta) / den)
		y = float32(math.Sin(float64(t)*theta) / den)
	}
	return Quat{x*a.X + y*b.X, x*a.Y + y*b.Y, x*a.Z + y*b.Z, x*a.W + y*b.W}.Normalize()
}

// TRS composes a transform from a translation, a rotation, and a scale,
// applied to a point in the order scale, rotation, translation. The rotation
// is normalized first.
func TRS(t Vec3, q Quat, s Vec3) Mat4 {
	q = q.Normalize()
	x, y, z, w := q.X, q.Y, q.Z, q.W
	return Mat4{(1 - 2*(y*y+z*z)) * s.X, 2 * (x*y + z*w) * s.X, 2 * (x*z - y*w) * s.X, 0, 2 * (x*y - z*w) * s.Y, (1 - 2*(x*x+z*z)) * s.Y, 2 * (y*z + x*w) * s.Y, 0, 2 * (x*z + y*w) * s.Z, 2 * (y*z - x*w) * s.Z, (1 - 2*(x*x+y*y)) * s.Z, 0, t.X, t.Y, t.Z, 1}
}

// Rotation returns the rotation of m's linear part, whatever positive scale
// its axes carry.
func (m Mat4) Rotation() Quat {
	for k := range 3 {
		l := float32(math.Sqrt(float64(m[k*4]*m[k*4] + m[k*4+1]*m[k*4+1] + m[k*4+2]*m[k*4+2])))
		for j := range 3 {
			m[k*4+j] /= l
		}
	}
	var q [4]float32
	tr := m[0] + m[5] + m[10]
	if tr > 0 {
		s := float32(math.Sqrt(float64(tr+1))) * 2
		q = [4]float32{(m[6] - m[9]) / s, (m[8] - m[2]) / s, (m[1] - m[4]) / s, s / 4}
	} else {
		i := 0
		if m[5] > m[0] {
			i = 1
		}
		if m[10] > m[i*5] {
			i = 2
		}
		j, k := (i+1)%3, (i+2)%3
		s := float32(math.Sqrt(float64(1+m[i*5]-m[j*5]-m[k*5]))) * 2
		q[i] = s / 4
		q[j] = (m[j*4+i] + m[i*4+j]) / s
		q[k] = (m[k*4+i] + m[i*4+k]) / s
		q[3] = (m[j*4+k] - m[k*4+j]) / s
	}
	return Quat{q[0], q[1], q[2], q[3]}.Normalize()
}
