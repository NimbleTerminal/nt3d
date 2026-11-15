// Copyright (c) 2026 Neomantra Corp.

// Package nt3dease provides pure float64 easing functions and scalar keyframe
// interpolation shared by timing, roll, and lens camera tracks.
package nt3dease

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Kind identifies an easing curve over normalized progress u in [0, 1].
type Kind uint8

// Supported easing curve kinds.
const (
	Linear       Kind = iota // f(u) = u.
	Ease                     // CSS ease: cubic-bezier(0.25, 0.1, 0.25, 1.0).
	EaseIn                   // CSS ease-in: cubic-bezier(0.42, 0.0, 1.0, 1.0).
	EaseOut                  // CSS ease-out: cubic-bezier(0.0, 0.0, 0.58, 1.0).
	EaseInOut                // CSS ease-in-out: cubic-bezier(0.42, 0.0, 0.58, 1.0).
	Smoothstep               // 3u^2 - 2u^3.
	Smootherstep             // 6u^5 - 15u^4 + 10u^3.
	Hold                     // Step: 0 for u < 1, 1 at u == 1.
	CubicBezier              // Custom CSS cubic-bezier(P[0], P[1], P[2], P[3]).
)

// Spec parameterizes a 1D easing curve.
type Spec struct {
	Kind Kind
	P    [4]float64 // [x1, y1, x2, y2] when Kind == CubicBezier.
}

// Validate checks that s is well-formed (for CubicBezier, finite control points
// with x1, x2 in [0, 1] so X(s) is monotonic on [0, 1]).
func (s Spec) Validate() error {
	switch s.Kind {
	case Linear, Ease, EaseIn, EaseOut, EaseInOut, Smoothstep, Smootherstep, Hold:
		return nil
	case CubicBezier:
		for _, v := range s.P {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("nt3dease: non-finite cubic-bezier control point")
			}
		}
		if s.P[0] < 0 || s.P[0] > 1 || s.P[2] < 0 || s.P[2] > 1 {
			return fmt.Errorf("nt3dease: cubic-bezier x control points [%g, %g] must be in [0, 1]", s.P[0], s.P[2])
		}
		return nil
	default:
		return fmt.Errorf("nt3dease: unknown easing kind %d", s.Kind)
	}
}

// Eval evaluates s at normalized progress u, clamping u<=0 to 0 and u>=1 to 1.
func (s Spec) Eval(u float64) float64 {
	if !(u > 0) {
		return 0
	}
	if u >= 1 {
		return 1
	}
	switch s.Kind {
	case Linear:
		return u
	case Ease:
		return evalCubicBezier(0.25, 0.1, 0.25, 1.0, u)
	case EaseIn:
		return evalCubicBezier(0.42, 0.0, 1.0, 1.0, u)
	case EaseOut:
		return evalCubicBezier(0.0, 0.0, 0.58, 1.0, u)
	case EaseInOut:
		return evalCubicBezier(0.42, 0.0, 0.58, 1.0, u)
	case Smoothstep:
		return u * u * (3 - 2*u)
	case Smootherstep:
		return u * u * u * (u*(u*6-15) + 10)
	case Hold:
		return 0
	case CubicBezier:
		return evalCubicBezier(s.P[0], s.P[1], s.P[2], s.P[3], u)
	default:
		return u
	}
}

// Derivative returns df/du at u in (0, 1), used to determine motion sign (ds/dt).
func (s Spec) Derivative(u float64) float64 {
	if math.IsNaN(u) || u <= 0 || u >= 1 {
		u = math.Max(1e-6, math.Min(1-1e-6, u))
	}
	switch s.Kind {
	case Linear:
		return 1
	case Ease:
		return derivCubicBezier(0.25, 0.1, 0.25, 1.0, u)
	case EaseIn:
		return derivCubicBezier(0.42, 0.0, 1.0, 1.0, u)
	case EaseOut:
		return derivCubicBezier(0.0, 0.0, 0.58, 1.0, u)
	case EaseInOut:
		return derivCubicBezier(0.42, 0.0, 0.58, 1.0, u)
	case Smoothstep:
		return 6 * u * (1 - u)
	case Smootherstep:
		return 30 * u * u * (u - 1) * (u - 1)
	case Hold:
		return 0
	case CubicBezier:
		return derivCubicBezier(s.P[0], s.P[1], s.P[2], s.P[3], u)
	default:
		return 1
	}
}

// evalCubicBezier evaluates CSS cubic-bezier(x1, y1, x2, y2) at x in (0, 1)
// by solving X(p) = x for p in [0, 1] using safeguarded Newton-Raphson with
// bisection fallback, then returning Y(p).
func evalCubicBezier(x1, y1, x2, y2, x float64) float64 {
	if x1 == y1 && x2 == y2 {
		return x
	}
	p := solveBezierX(x1, x2, x)
	cy := 3 * y1
	by := 3*(y2-y1) - cy
	ay := 1 - cy - by
	return ((ay*p+by)*p + cy) * p
}

func derivCubicBezier(x1, y1, x2, y2, x float64) float64 {
	if x1 == y1 && x2 == y2 {
		return 1
	}
	p := solveBezierX(x1, x2, x)
	cx := 3 * x1
	bx := 3*(x2-x1) - cx
	ax := 1 - cx - bx
	dx := (3*ax*p+2*bx)*p + cx

	cy := 3 * y1
	by := 3*(y2-y1) - cy
	ay := 1 - cy - by
	dy := (3*ay*p+2*by)*p + cy
	if math.Abs(dx) < 1e-12 {
		return 0
	}
	return dy / dx
}

func solveBezierX(x1, x2, x float64) float64 {
	cx := 3 * x1
	bx := 3*(x2-x1) - cx
	ax := 1 - cx - bx

	sampleX := func(p float64) float64 { return ((ax*p+bx)*p + cx) * p }
	derivX := func(p float64) float64 { return (3*ax*p+2*bx)*p + cx }

	lo, hi := 0.0, 1.0
	p := x
	for range 8 {
		err := sampleX(p) - x
		if math.Abs(err) < 1e-13 {
			return p
		}
		if err < 0 {
			lo = p
		} else {
			hi = p
		}
		d := derivX(p)
		if math.Abs(d) < 1e-9 {
			p = 0.5 * (lo + hi)
			continue
		}
		next := p - err/d
		if next <= lo || next >= hi {
			p = 0.5 * (lo + hi)
		} else {
			p = next
		}
	}
	for range 40 {
		mid := 0.5 * (lo + hi)
		err := sampleX(mid) - x
		if math.Abs(err) < 1e-13 || (hi-lo) < 1e-14 {
			return mid
		}
		if err < 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return 0.5 * (lo + hi)
}

// Key is a validated scalar keyframe at timestamp T.
type Key struct {
	T    time.Duration
	V    float64
	Ease Spec
}

// ValidateKeys checks that keys have non-negative strictly increasing timestamps,
// finite values, and valid easing specs. Empty slices are permitted.
func ValidateKeys(keys []Key) error {
	for i, k := range keys {
		if k.T < 0 {
			return fmt.Errorf("nt3dease: keyframe %d has negative time %v", i, k.T)
		}
		if i > 0 && k.T <= keys[i-1].T {
			return fmt.Errorf("nt3dease: keyframe %d time %v must exceed previous %v", i, k.T, keys[i-1].T)
		}
		if math.IsNaN(k.V) || math.IsInf(k.V, 0) {
			return fmt.Errorf("nt3dease: keyframe %d has non-finite value", i)
		}
		if err := k.Ease.Validate(); err != nil {
			return fmt.Errorf("nt3dease: keyframe %d: %w", i, err)
		}
	}
	return nil
}

// Sample evaluates a sorted slice of scalar keyframes at timestamp t in O(log n).
// Empty keys return def. Before keys[0].T it clamps to keys[0].V; at or after
// keys[n-1].T it clamps to keys[n-1].V. Over (keys[i].T, keys[i+1].T),
// keys[i+1].Ease governs the transition.
func Sample(keys []Key, t time.Duration, def float64) float64 {
	n := len(keys)
	if n == 0 {
		return def
	}
	if n == 1 || t <= keys[0].T {
		return keys[0].V
	}
	if t >= keys[n-1].T {
		return keys[n-1].V
	}
	hi := sort.Search(n, func(i int) bool { return keys[i].T > t })
	lo := hi - 1
	dt := float64(keys[hi].T - keys[lo].T)
	if dt <= 0 {
		return keys[hi].V
	}
	u := float64(t-keys[lo].T) / dt
	w := keys[hi].Ease.Eval(u)
	return keys[lo].V + w*(keys[hi].V-keys[lo].V)
}
