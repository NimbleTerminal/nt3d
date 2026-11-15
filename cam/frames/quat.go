// Copyright (c) 2026 Neomantra Corp.

// Package nt3dframes implements float64 camera orientation primitives:
// unit XYZW quaternions, slerp and Shoemake squad keyframe tracks, look-at
// framing, and Hanson & Ma (1995) parallel-transport curve frames.
package nt3dframes

import (
	"fmt"
	"math"
	"sort"
	"time"

	nt3dcurve "nimbleterminal.dev/nt3d/cam/curve"
	nt3dease "nimbleterminal.dev/nt3d/cam/ease"
)

// Quat is a float64 quaternion in XYZW order.
type Quat struct {
	X, Y, Z, W float64
}

// Identity returns the identity unit quaternion (0, 0, 0, 1).
func Identity() Quat { return Quat{W: 1} }

// IsFinite reports whether all components of q are finite numbers.
func (q Quat) IsFinite() bool {
	return !math.IsNaN(q.X) && !math.IsInf(q.X, 0) &&
		!math.IsNaN(q.Y) && !math.IsInf(q.Y, 0) &&
		!math.IsNaN(q.Z) && !math.IsInf(q.Z, 0) &&
		!math.IsNaN(q.W) && !math.IsInf(q.W, 0)
}

// NormSq returns x^2 + y^2 + z^2 + w^2.
func (q Quat) NormSq() float64 {
	return q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W
}

// Norm returns sqrt(NormSq).
func (q Quat) Norm() float64 {
	return math.Sqrt(q.NormSq())
}

// Normalize returns q scaled to unit length, or Identity if q is zero.
func (q Quat) Normalize() Quat {
	n := q.Norm()
	if n == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return Identity()
	}
	inv := 1.0 / n
	return Quat{q.X * inv, q.Y * inv, q.Z * inv, q.W * inv}
}

// Neg returns -q (which represents the same 3D rotation on SO(3)).
func (q Quat) Neg() Quat { return Quat{-q.X, -q.Y, -q.Z, -q.W} }

// Conj returns the quaternion conjugate (-x, -y, -z, w). For unit quaternions, Conj == Inv.
func (q Quat) Conj() Quat { return Quat{-q.X, -q.Y, -q.Z, q.W} }

// Dot returns the 4D inner product a · b.
func (a Quat) Dot(b Quat) float64 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
}

// Mul returns the Hamilton product a ⊗ b. Rotating by a.Mul(b) applies b first, then a.
func (a Quat) Mul(b Quat) Quat {
	return Quat{
		X: a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		Y: a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		Z: a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
		W: a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
	}
}

// AxisAngle returns the unit quaternion rotating around axis by radians (right-handed).
func AxisAngle(axis nt3dcurve.Vec3, radians float64) Quat {
	u := axis.Normalize()
	if u.LenSq() == 0 {
		return Identity()
	}
	half := 0.5 * radians
	s, c := math.Sin(half), math.Cos(half)
	return Quat{X: u.X * s, Y: u.Y * s, Z: u.Z * s, W: c}.Normalize()
}

// Rotate rotates v by unit quaternion q.
func (q Quat) Rotate(v nt3dcurve.Vec3) nt3dcurve.Vec3 {
	qv := nt3dcurve.Vec3{X: q.X, Y: q.Y, Z: q.Z}
	t := qv.Cross(v).Scale(2)
	return v.Add(t.Scale(q.W)).Add(qv.Cross(t))
}

// Basis returns the world-space right (+X), up (+Y), and forward (-Z) unit axes of q.
func (q Quat) Basis() (right, up, forward nt3dcurve.Vec3) {
	u := q.Normalize()
	right = u.Rotate(nt3dcurve.Vec3{X: 1})
	up = u.Rotate(nt3dcurve.Vec3{Y: 1})
	forward = u.Rotate(nt3dcurve.Vec3{Z: -1})
	return
}

// FromBasis constructs a unit camera-to-world quaternion from orthonormal world
// axes where camera-local +X = right, +Y = up, and -Z = forward.
func FromBasis(right, up, forward nt3dcurve.Vec3) Quat {
	back := forward.Scale(-1)
	m00, m10, m20 := right.X, right.Y, right.Z
	m01, m11, m21 := up.X, up.Y, up.Z
	m02, m12, m22 := back.X, back.Y, back.Z

	tr := m00 + m11 + m22
	var q Quat
	if tr > 0 {
		s := math.Sqrt(tr+1.0) * 2
		q = Quat{
			X: (m21 - m12) / s,
			Y: (m02 - m20) / s,
			Z: (m10 - m01) / s,
			W: 0.25 * s,
		}
	} else if m00 > m11 && m00 > m22 {
		s := math.Sqrt(1.0+m00-m11-m22) * 2
		q = Quat{
			X: 0.25 * s,
			Y: (m01 + m10) / s,
			Z: (m02 + m20) / s,
			W: (m21 - m12) / s,
		}
	} else if m11 > m22 {
		s := math.Sqrt(1.0+m11-m00-m22) * 2
		q = Quat{
			X: (m01 + m10) / s,
			Y: 0.25 * s,
			Z: (m12 + m21) / s,
			W: (m02 - m20) / s,
		}
	} else {
		s := math.Sqrt(1.0+m22-m00-m11) * 2
		q = Quat{
			X: (m02 + m20) / s,
			Y: (m12 + m21) / s,
			Z: 0.25 * s,
			W: (m10 - m01) / s,
		}
	}
	return q.Normalize()
}

// LookRotation constructs a unit camera-to-world quaternion aiming local -Z along
// forward with camera +Y aligned toward worldUp. Collinear forward and worldUp
// select a deterministic orthogonal up axis without NaN.
func LookRotation(forward, worldUp nt3dcurve.Vec3) Quat {
	f := forward.Normalize()
	if f.LenSq() == 0 {
		f = nt3dcurve.Vec3{Y: 1}
	}
	uRef := worldUp.Normalize()
	if uRef.LenSq() == 0 {
		uRef = nt3dcurve.Vec3{Z: 1}
	}
	right := f.Cross(uRef)
	if right.LenSq() < 1e-16 {
		alt := nt3dcurve.Vec3{Y: 1}
		if math.Abs(f.Y) > 0.9 {
			alt = nt3dcurve.Vec3{X: 1}
		}
		right = f.Cross(alt)
	}
	right = right.Normalize()
	up := right.Cross(f).Normalize()
	return FromBasis(right, up, f)
}

// Slerp performs shortest-arc spherical linear interpolation between unit
// quaternions a (u=0) and b (u=1).
func Slerp(a, b Quat, u float64) Quat {
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
		return Quat{
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
	return Quat{
		X: wa*a.X + wb*b.X,
		Y: wa*a.Y + wb*b.Y,
		Z: wa*a.Z + wb*b.Z,
		W: wa*a.W + wb*b.W,
	}.Normalize()
}

// quatLog returns the pure-imaginary logarithm of a unit quaternion q (with q.W >= 0).
func quatLog(q Quat) nt3dcurve.Vec3 {
	q = q.Normalize()
	if q.W < 0 {
		q = q.Neg()
	}
	v := nt3dcurve.Vec3{X: q.X, Y: q.Y, Z: q.Z}
	sinA := v.Len()
	if sinA < 1e-12 {
		return v
	}
	angle := math.Atan2(sinA, q.W)
	return v.Scale(angle / sinA)
}

// quatExp returns exp((v.X, v.Y, v.Z, 0)) as a unit quaternion.
func quatExp(v nt3dcurve.Vec3) Quat {
	angle := v.Len()
	if angle < 1e-12 {
		return Quat{X: v.X, Y: v.Y, Z: v.Z, W: 1}.Normalize()
	}
	s := math.Sin(angle) / angle
	return Quat{X: v.X * s, Y: v.Y * s, Z: v.Z * s, W: math.Cos(angle)}.Normalize()
}

// Squad evaluates Shoemake's spherical cubic quadrangle interpolation over u in [0, 1].
func Squad(q0, q1, a0, a1 Quat, u float64) Quat {
	if u <= 0 {
		return q0.Normalize()
	}
	if u >= 1 {
		return q1.Normalize()
	}
	p := Slerp(q0, q1, u)
	q := Slerp(a0, a1, u)
	return Slerp(p, q, 2*u*(1-u))
}

// Key is a unit-quaternion keyframe at timestamp T.
type Key struct {
	T    time.Duration
	Q    Quat
	Ease nt3dease.Spec
}

// Track evaluates precompiled quaternion keyframes via Slerp or C1 Squad in O(log n).
type Track struct {
	keys     []Key
	tangents []Quat // Shoemake inner quadrangle control points a_i when squad == true.
	squad    bool
}

// NewTrack validates keys, canonicalizes hemisphere signs for shortest-arc motion,
// and precomputes Squad inner quadrangle tangents when useSquad is true.
func NewTrack(keys []Key, useSquad bool) (*Track, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("nt3dframes: quaternion track requires at least 1 keyframe")
	}
	cleaned := make([]Key, len(keys))
	for i, k := range keys {
		if k.T < 0 {
			return nil, fmt.Errorf("nt3dframes: keyframe %d has negative time %v", i, k.T)
		}
		if i > 0 && k.T <= keys[i-1].T {
			return nil, fmt.Errorf("nt3dframes: keyframe %d time %v must exceed previous %v", i, k.T, keys[i-1].T)
		}
		if !k.Q.IsFinite() || k.Q.NormSq() < 1e-24 {
			return nil, fmt.Errorf("nt3dframes: keyframe %d has zero or non-finite quaternion", i)
		}
		if err := k.Ease.Validate(); err != nil {
			return nil, fmt.Errorf("nt3dframes: keyframe %d: %w", i, err)
		}
		q := k.Q.Normalize()
		if i > 0 && cleaned[i-1].Q.Dot(q) < 0 {
			q = q.Neg()
		}
		cleaned[i] = Key{T: k.T, Q: q, Ease: k.Ease}
	}

	doSquad := useSquad && len(cleaned) >= 3
	var tangents []Quat
	if doSquad {
		n := len(cleaned)
		tangents = make([]Quat, n)
		for i := range n {
			qi := cleaned[i].Q
			qPrev := cleaned[max(0, i-1)].Q
			qNext := cleaned[min(n-1, i+1)].Q
			invQi := qi.Conj()
			lPrev := quatLog(invQi.Mul(qPrev))
			lNext := quatLog(invQi.Mul(qNext))
			tangents[i] = qi.Mul(quatExp(lPrev.Add(lNext).Scale(-0.25))).Normalize()
			if tangents[i].Dot(qi) < 0 {
				tangents[i] = tangents[i].Neg()
			}
		}
	}
	return &Track{keys: cleaned, tangents: tangents, squad: doSquad}, nil
}

// Eval evaluates the quaternion track at timestamp t in O(log n).
func (tr *Track) Eval(t time.Duration) Quat {
	n := len(tr.keys)
	if n == 0 {
		return Identity()
	}
	if n == 1 || t <= tr.keys[0].T {
		return tr.keys[0].Q
	}
	if t >= tr.keys[n-1].T {
		return tr.keys[n-1].Q
	}
	hi := sort.Search(n, func(i int) bool { return tr.keys[i].T > t })
	lo := hi - 1
	dt := float64(tr.keys[hi].T - tr.keys[lo].T)
	if dt <= 0 {
		return tr.keys[hi].Q
	}
	u := float64(t-tr.keys[lo].T) / dt
	w := tr.keys[hi].Ease.Eval(u)
	if tr.keys[hi].Ease.Kind == nt3dease.Hold {
		return tr.keys[lo].Q
	}
	if tr.squad {
		return Squad(tr.keys[lo].Q, tr.keys[hi].Q, tr.tangents[lo], tr.tangents[hi], w)
	}
	return Slerp(tr.keys[lo].Q, tr.keys[hi].Q, w)
}
