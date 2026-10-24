package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	"reflect"
	"testing"
)

func TestWaterEffectsFixedStepAndExpiry(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	a, b := d.water.craft, d.water.craft
	var ea, eb waterEffects
	for i := 1; i <= 1200; i++ {
		if err := a.advanceEffects(d.water.surface, float64(i)/120, &ea); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 300; i++ {
		if err := b.advanceEffects(d.water.surface, float64(i)/30, &eb); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(ea, eb) {
		t.Fatal("frame cadence changed effects")
	}
	if ea.count == 0 || ea.serial == 0 || a.landings == 0 {
		t.Fatal("missing wake, spray, or landings")
	}
	a.speed = 0
	for i := 1; i <= 600; i++ {
		a.time += 1.0 / 120
		if err := ea.step(d.water.surface, &a, 1.0/120, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	ea.pointSnapshot(a.time)
	if ea.count != 0 || ea.live != 0 {
		t.Fatal("effects did not expire", ea.count, ea.live)
	}
}

func TestWaterWakeSurfaceAndRunBreaks(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	var e waterEffects
	for i := 0; i < 4; i++ {
		e.addStation(wakeStation{x: float64(i) * .1, dx: 1, born: .1 * float64(i), speed: 4, run: uint64(i / 2)})
	}
	for _, side := range []float64{-1, 1} {
		g, err := e.ribbon(d.water.surface, 1, side)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Indices) != 24 {
			t.Fatal("ribbon bridged separate contact runs", len(g.Indices))
		}
		for _, v := range g.Vertices {
			q, err := d.water.surface.AtWorld(float64(v.Position.X), float64(v.Position.Y), 1)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(float64(v.Position.Z-q.Position.Z)-.025) > 1e-5 {
				t.Fatal("ribbon left surface")
			}
		}
	}
	for i := 0; i < 1000; i++ {
		e.addStation(wakeStation{})
		e.spawn(vec(0, 0, 1), vec(0, 0, 1), 0, 1, .1)
	}
	if e.count != waterWakeCapacity {
		t.Fatal("unbounded history")
	}
	e.pointSnapshot(2)
	for _, p := range e.points {
		if p.Color.A != 0 {
			t.Fatal("expired particle visible")
		}
	}
}

func TestWaterEffectsToggleDoesNotAdvance(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	d.animationTime = 2
	if err := d.stepWater(); err != nil {
		t.Fatal(err)
	}
	c, e := d.water.craft, d.water.effects
	for range 4 {
		if _, err := d.waterKey("f"); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(c, d.water.craft) || !reflect.DeepEqual(e, d.water.effects) {
		t.Fatal("visibility changed simulation")
	}
}
