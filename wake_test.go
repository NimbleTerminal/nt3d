// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func testWake(t *testing.T) (*WaveSurface, *WaveSurface) {
	t.Helper()
	base, err := NewWaveSurface([]GerstnerWave{{Direction: nt3dmath.Vec3{X: 1, Y: .3}, Wavelength: 3.4, Amplitude: .14}}, 9.81)
	check(t, err)
	wake, err := base.WithWake([]WakeImpulse{{Origin: nt3dmath.Vec3{X: -.2, Y: .1}, Velocity: nt3dmath.Vec3{X: .2, Y: .35}, Start: 0, Duration: 3, Amplitude: .2, Radius: 1.2, Spread: .15}}, .18)
	check(t, err)
	return base, wake
}
func TestWakeDerivativesAndWorldQueries(t *testing.T) {
	base, wake := testWake(t)
	const delta = 1e-4
	for _, time := range []float64{.05, .3, 1, 2.9} {
		for _, xy := range [][2]float64{{0, 0}, {.4, .5}, {-.6, .7}} {
			x, y := xy[0], xy[1]
			s, err := wake.Sample(x, y, time)
			check(t, err)
			a, _ := wake.Sample(x+delta, y, time)
			b, _ := wake.Sample(x-delta, y, time)
			c, _ := wake.Sample(x, y+delta, time)
			d, _ := wake.Sample(x, y-delta, time)
			e, _ := wake.Sample(x, y, time+delta)
			f, _ := wake.Sample(x, y, time-delta)
			for _, pair := range [][2]nt3dmath.Vec3{{s.TangentX, a.Position.Sub(b.Position).Scale(1 / (2 * delta))}, {s.TangentY, c.Position.Sub(d.Position).Scale(1 / (2 * delta))}, {s.Velocity, e.Position.Sub(f.Position).Scale(1 / (2 * delta))}} {
				diff := pair[0].Sub(pair[1])
				if diff.Dot(diff) > 2e-6 {
					t.Fatal("wake derivative", time, xy, pair)
				}
			}
			q, err := wake.AtWorld(float64(s.Position.X), float64(s.Position.Y), time)
			check(t, err)
			if math.Abs(float64(s.Position.Z-q.Position.Z)) > 1e-6 || s.Normal.Sub(q.Normal).Dot(s.Normal.Sub(q.Normal)) > 1e-10 {
				t.Fatal("wake query differs from mesh", s, q)
			}
			original, _ := base.Sample(x, y, time)
			if s.Position.X != original.Position.X || s.Position.Y != original.Position.Y {
				t.Fatal("wake changed horizontal inversion")
			}
		}
	}
	a, _ := wake.Sample(0, 0, .3)
	b, _ := base.Sample(0, 0, .3)
	if a.Position.Z == b.Position.Z {
		t.Fatal("wake never displaced surface")
	}
}
func TestWakeSnapshotBoundsAndExpiry(t *testing.T) {
	base := &WaveSurface{}
	pulses := make([]WakeImpulse, MaxWakeImpulses)
	for i := range pulses {
		pulses[i] = WakeImpulse{Duration: 2, Amplitude: 10, Radius: 1}
	}
	wake, err := base.WithWake(pulses, .18)
	check(t, err)
	pulses[0].Amplitude = 0
	s, err := wake.Sample(0, 0, .4)
	check(t, err)
	if s.Position.Z <= .17 || s.Position.Z > .180001 {
		t.Fatal("overlap escaped height bound", s.Position)
	}
	for _, time := range []float64{-1, 0, 2, 3} {
		s, err := wake.Sample(0, 0, time)
		check(t, err)
		if s.Position.Z != 0 || s.Normal != (nt3dmath.Vec3{Z: 1}) || s.Velocity != (nt3dmath.Vec3{}) {
			t.Fatal("wake outside lifetime", time, s)
		}
	}
	outside, err := wake.Sample(2, 0, .4)
	check(t, err)
	if outside.Position.Z != 0 {
		t.Fatal("wake outside radius")
	}
	// Replacing history must not add old impulses to the new snapshot.
	cleared, err := wake.WithWake(nil, .18)
	check(t, err)
	s, err = cleared.Sample(0, 0, .4)
	check(t, err)
	if s.Position.Z != 0 {
		t.Fatal("history accumulated")
	}
	if wake.wake[0].Amplitude != 10 {
		t.Fatal("wake history aliases caller")
	}
}
func TestWakeValidation(t *testing.T) {
	base := &WaveSurface{}
	for _, p := range []WakeImpulse{{Radius: 1}, {Duration: 1, Radius: -1}, {Duration: 1, Radius: 1, Origin: nt3dmath.Vec3{Z: 1}}, {Duration: 1, Radius: 1, Spread: -1}, {Duration: 1, Radius: 1, Amplitude: math.NaN()}} {
		if _, err := base.WithWake([]WakeImpulse{p}, .2); err == nil {
			t.Fatal("accepted invalid wake", p)
		}
	}
	if _, err := base.WithWake(make([]WakeImpulse, MaxWakeImpulses+1), .2); err == nil {
		t.Fatal("unbounded history")
	}
	if _, err := base.WithWake(nil, 0); err == nil {
		t.Fatal("invalid height bound")
	}
}
