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
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Quat is a float64 quaternion in XYZW order: nt3dmath.Quatd, under the name
// this package has always used. Its methods are documented there.
type Quat = nt3dmath.Quatd

// Identity returns the identity unit quaternion (0, 0, 0, 1).
func Identity() Quat { return nt3dmath.QuatdIdentity() }

// AxisAngle returns the unit quaternion rotating around axis by radians (right-handed).
func AxisAngle(axis nt3dcurve.Vec3, radians float64) Quat {
	return nt3dmath.QuatdAxisAngle(axis, radians)
}

// FromBasis constructs a unit camera-to-world quaternion from orthonormal world
// axes where camera-local +X = right, +Y = up, and -Z = forward.
func FromBasis(right, up, forward nt3dcurve.Vec3) Quat {
	return nt3dmath.QuatdFromBasis(right, up, forward)
}

// LookRotation constructs a unit camera-to-world quaternion aiming local -Z along
// forward with camera +Y aligned toward worldUp. Collinear forward and worldUp
// select a deterministic orthogonal up axis without NaN.
func LookRotation(forward, worldUp nt3dcurve.Vec3) Quat {
	return nt3dmath.QuatdLookRotation(forward, worldUp)
}

// Slerp performs shortest-arc spherical linear interpolation between unit
// quaternions a (u=0) and b (u=1).
func Slerp(a, b Quat, u float64) Quat { return a.Slerp(b, u) }

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
