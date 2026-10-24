// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func testWaveSurface(t *testing.T) *WaveSurface {
	t.Helper()
	s, err := NewWaveSurface([]GerstnerWave{
		{Direction: nt3dmath.Vec3{X: 3, Y: 1}, Wavelength: 4, Amplitude: .2},
		{Direction: nt3dmath.Vec3{X: -1, Y: 2}, Wavelength: 2, Amplitude: .07, Phase: .8},
	}, 9.81)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWaveDerivatives(t *testing.T) {
	s := testWaveSurface(t)
	const h = .001
	for _, time := range []float64{0, .7, 8, 10000} {
		x, y := -.82, 1.3
		q, err := s.Sample(x, y, time)
		if err != nil {
			t.Fatal(err)
		}
		xp, _ := s.Sample(x+h, y, time)
		xm, _ := s.Sample(x-h, y, time)
		yp, _ := s.Sample(x, y+h, time)
		ym, _ := s.Sample(x, y-h, time)
		tp, _ := s.Sample(x, y, time+h)
		tm, _ := s.Sample(x, y, time-h)
		for _, pair := range [][2]nt3dmath.Vec3{
			{q.TangentX, xp.Position.Sub(xm.Position).Scale(1 / (2 * h))},
			{q.TangentY, yp.Position.Sub(ym.Position).Scale(1 / (2 * h))},
			{q.Velocity, tp.Position.Sub(tm.Position).Scale(1 / (2 * h))},
		} {
			d := pair[0].Sub(pair[1])
			if d.Dot(d) > 1e-7 {
				t.Fatalf("derivative at %g: %v != %v", time, pair[0], pair[1])
			}
		}
		if math.Abs(float64(q.Normal.Dot(q.TangentX))) > 1e-6 || math.Abs(float64(q.Normal.Dot(q.TangentY))) > 1e-6 || math.Abs(float64(q.Normal.Dot(q.Normal))-1) > 1e-6 || q.Normal.Z <= 0 {
			t.Fatalf("invalid normal: %+v", q)
		}
	}
}

func TestSingleWaveCrestAndPeriod(t *testing.T) {
	// k=1, omega=2: at x=pi/2 and t=0 this is a crest. A full
	// period is pi seconds; the crest travels along +X at speed 2.
	s, err := NewWaveSurface([]GerstnerWave{{Direction: nt3dmath.Vec3{X: 7}, Wavelength: 2 * math.Pi, Amplitude: .25}}, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, time := range []float64{0, math.Pi, 2 * math.Pi} {
		q, err := s.Sample(math.Pi/2, 0, time)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(float64(q.Position.X)-math.Pi/2) > 1e-6 || math.Abs(float64(q.Position.Z)-.25) > 1e-6 || math.Abs(float64(q.Velocity.X)-.5) > 1e-6 || math.Abs(float64(q.Velocity.Z)) > 1e-6 || q.Normal.Z < .99999 {
			t.Fatal("incorrect crest", q)
		}
	}
	w := GerstnerWave{Direction: nt3dmath.Vec3{X: 1}, Wavelength: 2 * math.Pi, Amplitude: .6}
	if _, err := NewWaveSurface([]GerstnerWave{w, w}, 4); err == nil {
		t.Fatal("accepted combined folding waves")
	}
}

func TestWaveWorldInverse(t *testing.T) {
	for _, s := range []*WaveSurface{testWaveSurface(t), {waves: []surfaceWave{{x: 1, k: 1, amplitude: .99, omega: 2}}}} {
		for _, time := range []float64{0, .8, 6.4, 200} {
			for i := -20; i <= 20; i++ {
				x, y := float64(i)*.3, float64(i)*-.17
				p, _, _, _ := s.evaluate(x, y, time)
				q, err := s.AtWorld(p[0], p[1], time)
				if err != nil {
					t.Fatal(err)
				}
				if math.Abs(float64(q.Position.Z)-p[2]) > 2e-6 || math.Abs(float64(q.Position.X)-p[0]) > 2e-6 || math.Abs(float64(q.Position.Y)-p[1]) > 2e-6 {
					t.Fatalf("inverse: %v != %v", q.Position, p)
				}
			}
		}
	}
}

func TestWaveValidationAndFlat(t *testing.T) {
	var flat WaveSurface
	q, err := flat.AtWorld(-2, 3, 99)
	if err != nil || q.Position != (nt3dmath.Vec3{X: -2, Y: 3}) || q.Normal != (nt3dmath.Vec3{Z: 1}) || q.Velocity != (nt3dmath.Vec3{}) {
		t.Fatal(q, err)
	}
	valid := GerstnerWave{Direction: nt3dmath.Vec3{X: 1}, Wavelength: 4, Amplitude: .1}
	for _, mutate := range []func(*GerstnerWave){
		func(w *GerstnerWave) { w.Direction = nt3dmath.Vec3{} },
		func(w *GerstnerWave) { w.Direction.Z = 1 },
		func(w *GerstnerWave) { w.Wavelength = 0 },
		func(w *GerstnerWave) { w.Amplitude = -1 },
		func(w *GerstnerWave) { w.Amplitude = 1 },
		func(w *GerstnerWave) { w.Phase = math.NaN() },
	} {
		w := valid
		mutate(&w)
		if _, err := NewWaveSurface([]GerstnerWave{w}, 9.81); err == nil {
			t.Fatal("accepted", w)
		}
	}
	for _, g := range []float64{0, -1, math.Inf(1), math.NaN()} {
		if _, err := NewWaveSurface(nil, g); err == nil {
			t.Fatal("accepted gravity", g)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		if _, err := flat.Sample(0, 0, v); err == nil {
			t.Fatal("accepted invalid time")
		}
		if _, err := flat.AtWorld(v, 0, 0); err == nil {
			t.Fatal("accepted invalid coordinate")
		}
	}
	waves := []GerstnerWave{valid}
	s, err := NewWaveSurface(waves, 9.81)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Sample(1, 2, 3)
	waves[0].Amplitude = 9
	b, _ := s.Sample(1, 2, 3)
	if a != b {
		t.Fatal("surface retained caller's slice")
	}
}
