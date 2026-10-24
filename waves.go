// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// GerstnerWave is a deep-water wave traveling along Direction in the XY plane.
// Direction is normalized at construction; Z must be zero. Wavelength and
// Amplitude use world units, Phase uses radians. Amplitude must be nonnegative.
type GerstnerWave struct {
	Direction                    nt3dmath.Vec3
	Wavelength, Amplitude, Phase float64
}

// WaveSurface is an immutable sum of Gerstner waves with mean height Z=0.
// The zero value is a flat surface. It is safe to sample concurrently.
type WaveSurface struct{ waves []surfaceWave }

type surfaceWave struct{ x, y, k, amplitude, omega, phase float64 }

// WaveSample describes a displaced material point. Velocity is its time
// derivative, not a current or the derivative of height at a fixed world XY.
// TangentX/Y are derivatives with respect to the undisplaced XY coordinates.
type WaveSample struct {
	Position, Normal, Velocity nt3dmath.Vec3
	TangentX, TangentY         nt3dmath.Vec3
}

// NewWaveSurface copies waves and computes deep-water angular speeds sqrt(g*k).
// Gravity must be positive (9.81 for meters and seconds). The sum of steepnesses
// 2*pi*Amplitude/Wavelength must be below 1, preventing folding and giving each
// world XY a unique surface point. Empty waves produce a flat surface.
func NewWaveSurface(waves []GerstnerWave, gravity float64) (*WaveSurface, error) {
	if !waveFinite(gravity) || gravity <= 0 {
		return nil, fmt.Errorf("nt3d: wave gravity must be finite and positive")
	}
	s := &WaveSurface{waves: make([]surfaceWave, len(waves))}
	steepness := 0.0
	for i, w := range waves {
		x, y := float64(w.Direction.X), float64(w.Direction.Y)
		length := math.Hypot(x, y)
		if !waveFinite(length, w.Wavelength, w.Amplitude, w.Phase) || length == 0 || w.Direction.Z != 0 || w.Wavelength <= 0 || w.Amplitude < 0 {
			return nil, fmt.Errorf("nt3d: invalid wave %d", i)
		}
		k := 2 * math.Pi / w.Wavelength
		omega := math.Sqrt(gravity * k)
		steepness += k * w.Amplitude
		if !waveFinite(k, omega, steepness) || steepness >= 1 {
			return nil, fmt.Errorf("nt3d: total wave steepness must be below 1")
		}
		s.waves[i] = surfaceWave{x / length, y / length, k, w.Amplitude, omega, w.Phase}
	}
	return s, nil
}

// Sample evaluates undisplaced XY coordinates at time in seconds. Use this to
// move mesh vertices. Inputs and the resulting float32 geometry must be finite.
func (s *WaveSurface) Sample(x, y, time float64) (WaveSample, error) {
	if !waveFinite(x, y, time) {
		return WaveSample{}, fmt.Errorf("nt3d: non-finite wave query")
	}
	p, dx, dy, v := s.evaluate(x, y, time)
	return makeWaveSample(p, dx, dy, v)
}

// AtWorld finds the surface above world XY, inverting horizontal displacement.
// Use it for floating objects; Sample(worldX, worldY, time) would query a
// different location. A damped Newton solve stops at 1e-7 world units or returns
// an error after 64 iterations. Extreme scales/near-folding waves may fail.
func (s *WaveSurface) AtWorld(x, y, time float64) (WaveSample, error) {
	if !waveFinite(x, y, time) {
		return WaveSample{}, fmt.Errorf("nt3d: non-finite wave query")
	}
	u, v := x, y
	for range 64 {
		p, dx, dy, velocity := s.evaluate(u, v, time)
		rx, ry := p[0]-x, p[1]-y
		err := math.Hypot(rx, ry)
		if err <= 1e-7 {
			return makeWaveSample(p, dx, dy, velocity)
		}
		det := dx[0]*dy[1] - dy[0]*dx[1]
		sx, sy := (dy[1]*rx-dy[0]*ry)/det, (dx[0]*ry-dx[1]*rx)/det
		accepted := false
		for step := 1.0; step >= 1.0/4096; step *= .5 {
			nu, nv := u-step*sx, v-step*sy
			np, _, _, _ := s.evaluate(nu, nv, time)
			if math.Hypot(np[0]-x, np[1]-y) < err {
				u, v, accepted = nu, nv, true
				break
			}
		}
		if !accepted {
			break
		}
	}
	return WaveSample{}, fmt.Errorf("nt3d: world wave query did not converge")
}

func (s *WaveSurface) evaluate(x, y, time float64) (p, dx, dy, v [3]float64) {
	p, dx, dy = [3]float64{x, y, 0}, [3]float64{1, 0, 0}, [3]float64{0, 1, 0}
	for _, w := range s.waves {
		sin, cos := math.Sincos(w.k*(w.x*x+w.y*y) - w.omega*time + w.phase)
		a, ak := w.amplitude, w.amplitude*w.k
		p[0] += a * w.x * cos
		p[1] += a * w.y * cos
		p[2] += a * sin
		dx[0] -= ak * w.x * w.x * sin
		dx[1] -= ak * w.x * w.y * sin
		dx[2] += ak * w.x * cos
		dy[0] -= ak * w.x * w.y * sin
		dy[1] -= ak * w.y * w.y * sin
		dy[2] += ak * w.y * cos
		v[0] += a * w.omega * w.x * sin
		v[1] += a * w.omega * w.y * sin
		v[2] -= a * w.omega * cos
	}
	return
}

func makeWaveSample(p, dx, dy, v [3]float64) (WaveSample, error) {
	vec := func(a [3]float64) nt3dmath.Vec3 {
		return nt3dmath.Vec3{X: float32(a[0]), Y: float32(a[1]), Z: float32(a[2])}
	}
	for _, a := range [][3]float64{p, dx, dy, v} {
		for _, f := range a {
			if !waveFinite(f) || math.Abs(f) > math.MaxFloat32 {
				return WaveSample{}, fmt.Errorf("nt3d: wave sample exceeds finite geometry range")
			}
		}
	}
	// Normalize in float64 so steep but valid surfaces retain a useful normal.
	n := [3]float64{dx[1]*dy[2] - dx[2]*dy[1], dx[2]*dy[0] - dx[0]*dy[2], dx[0]*dy[1] - dx[1]*dy[0]}
	l := math.Hypot(math.Hypot(n[0], n[1]), n[2])
	if !waveFinite(l) || l == 0 {
		return WaveSample{}, fmt.Errorf("nt3d: degenerate wave normal")
	}
	for i := range n {
		n[i] /= l
	}
	return WaveSample{Position: vec(p), Normal: vec(n), Velocity: vec(v), TangentX: vec(dx), TangentY: vec(dy)}, nil
}

func waveFinite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}
