// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
)

const waterWakeHeight = .18

type waterDeformation struct {
	pulses                   [nt3d.MaxWakeImpulses]nt3d.WakeImpulse
	first, count             int
	revision, cachedRevision uint64
	base, cached             *nt3d.WaveSurface
	disabled                 bool
}

func (w *waterDeformation) add(p nt3d.WakeImpulse) {
	if w.count == len(w.pulses) {
		w.first = (w.first + 1) % len(w.pulses)
		w.count--
	}
	w.pulses[(w.first+w.count)%len(w.pulses)] = p
	w.count++
	w.revision++
}
func (w *waterDeformation) clear() { w.first, w.count = 0, 0; w.revision++; w.cached = nil }
func (w *waterDeformation) prune(time float64) {
	// Durations differ for contact and landing packets. Keep birth order while
	// removing every expired pulse, so capacity never depends on render cadence.
	n := 0
	for i := 0; i < w.count; i++ {
		p := w.pulses[(w.first+i)%len(w.pulses)]
		if time-p.Start < p.Duration {
			w.pulses[(w.first+n)%len(w.pulses)] = p
			n++
		}
	}
	if n != w.count {
		w.count = n
		w.revision++
	}
}
func (w *waterDeformation) surface(base *nt3d.WaveSurface) (*nt3d.WaveSurface, error) {
	if w.disabled || w.count == 0 {
		return base, nil
	}
	if w.cached != nil && w.base == base && w.cachedRevision == w.revision {
		return w.cached, nil
	}
	var pulses [nt3d.MaxWakeImpulses]nt3d.WakeImpulse
	for i := 0; i < w.count; i++ {
		pulses[i] = w.pulses[(w.first+i)%len(w.pulses)]
	}
	surface, err := base.WithWake(pulses[:w.count], waterWakeHeight)
	if err != nil {
		return nil, err
	}
	w.base, w.cached, w.cachedRevision = base, surface, w.revision
	return surface, nil
}
func (e *waterEffects) deform(c *waterCraft, touching bool, impact, turn float64) {
	w := &e.deformation
	w.prune(c.time)
	dx, dy := math.Cos(c.heading), math.Sin(c.heading)
	if touching && (!e.contact || e.ticks%15 == 0) {
		for _, side := range []float64{-1, 1} {
			amplitude := math.Min(.11, c.speed*.018)
			if side*turn < 0 {
				amplitude *= 1 + math.Min(.3, math.Abs(turn)*.15)
			}
			spread := .23 + .025*c.speed
			w.add(nt3d.WakeImpulse{Origin: vec(c.x-.68*dx-side*.28*dy, c.y-.68*dy+side*.28*dx, 0), Velocity: vec(-side*dy*spread, side*dx*spread, 0), Start: c.time, Duration: waterWakeLife, Amplitude: amplitude, Radius: .8, Spread: .12})
		}
	}
	if impact > .8 {
		for i := range 4 {
			a := float64(i) * math.Pi / 2
			dx, dy := math.Cos(a), math.Sin(a)
			w.add(nt3d.WakeImpulse{Origin: vec(c.x+.15*dx, c.y+.15*dy, 0), Velocity: vec(.5*dx, .5*dy, 0), Start: c.time, Duration: 2.3, Amplitude: .012 * math.Min(6, impact), Radius: .7, Spread: .25})
		}
	}
}
