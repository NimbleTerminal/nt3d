// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"slices"
)

// MaxWakeImpulses bounds the work performed by each surface query.
const MaxWakeImpulses = 128

// WakeImpulse is a compact, moving crest with a surrounding trough, centered
// at Origin + Velocity*(time-Start) in world XY. Origin.Z and Velocity.Z must
// be zero. Radius grows by Spread world units per second. Amplitude may be
// negative. The impulse eases in over min(.15, Duration/5) seconds and decays
// smoothly to zero at Duration. It is a visual wave packet, not a fluid solver.
type WakeImpulse struct {
	Origin, Velocity                           nt3dmath.Vec3
	Start, Duration, Amplitude, Radius, Spread float64
}

// WithWake returns an immutable surface snapshot with a copied wake history.
// It replaces any previous wake on s, preserving its Gerstner waves. Vertical
// wake displacement is evaluated at displaced world XY; horizontal motion is
// unchanged. MaxHeight must be positive and bounds the combined displacement
// smoothly, including when many impulses overlap. Sample and AtWorld include
// matching analytic tangents, normals, and material-point velocities.
func (s *WaveSurface) WithWake(impulses []WakeImpulse, maxHeight float64) (*WaveSurface, error) {
	if s == nil || len(impulses) > MaxWakeImpulses || !waveFinite(maxHeight) || maxHeight <= 0 || maxHeight > math.MaxFloat32 {
		return nil, fmt.Errorf("nt3d: invalid wake height limit or impulse count")
	}
	for i, p := range impulses {
		if !validFloat(p.Origin.X, p.Origin.Y, p.Origin.Z, p.Velocity.X, p.Velocity.Y, p.Velocity.Z) || p.Origin.Z != 0 || p.Velocity.Z != 0 || !waveFinite(p.Start, p.Duration, p.Amplitude, p.Radius, p.Spread, p.Start+p.Duration, p.Radius+p.Spread*p.Duration, 1/(p.Radius*p.Radius)) || p.Duration <= 0 || p.Radius <= 0 || p.Spread < 0 {
			return nil, fmt.Errorf("nt3d: invalid wake impulse %d", i)
		}
	}
	return &WaveSurface{waves: s.waves, wake: slices.Clone(impulses), wakeLimit: maxHeight}, nil
}

// wakeHeight returns H, dH/dx, dH/dy and dH/dt at a fixed world XY. The compact
// radial polynomial has zero area integral before the overlap limiter: its
// crest is balanced by its trough, with zero slope at the center and boundary.
func (s *WaveSurface) wakeHeight(x, y, time float64) (h, hx, hy, ht float64) {
	for _, p := range s.wake {
		age := time - p.Start
		if age <= 0 || age >= p.Duration || p.Amplitude == 0 {
			continue
		}
		radius := p.Radius + p.Spread*age
		dx, dy := x-float64(p.Origin.X)-float64(p.Velocity.X)*age, y-float64(p.Origin.Y)-float64(p.Velocity.Y)*age
		if math.Abs(dx) >= radius || math.Abs(dy) >= radius {
			continue
		}
		r2 := radius * radius
		q := (dx*dx + dy*dy) / r2
		if q >= 1 {
			continue
		}
		rise := math.Min(.15, p.Duration/5)
		u := math.Min(1, age/rise)
		up, dup := u*u*(3-2*u), 0.0
		if age < rise {
			dup = 6 * u * (1 - u) / rise
		}
		down := 1 - age/p.Duration
		envelope := up * down * down
		derivative := dup*down*down - 2*up*down/p.Duration
		one := 1 - q
		shape := one * one * one * (1 - 5*q)
		slope := one * one * (20*q - 8)
		amp := p.Amplitude * envelope
		h += amp * shape
		hx += amp * slope * 2 * dx / r2
		hy += amp * slope * 2 * dy / r2
		qt := -2*(dx*float64(p.Velocity.X)+dy*float64(p.Velocity.Y))/r2 - 2*q*p.Spread/radius
		ht += p.Amplitude * (derivative*shape + envelope*slope*qt)
	}
	if h != 0 || hx != 0 || hy != 0 || ht != 0 {
		limited := math.Tanh(h / s.wakeLimit)
		scale := 1 - limited*limited
		h = s.wakeLimit * limited
		hx *= scale
		hy *= scale
		ht *= scale
	}
	return
}
