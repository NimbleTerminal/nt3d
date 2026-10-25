// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestWaterCraftLaunchAndLand(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	initial := d.water.craft
	initial.autopilot = false
	initial.throttle = 1
	initial.shoreline = false
	initial.bound = 1000
	c := initial
	var airborne int
	for i := 1; i <= 120*20; i++ {
		if err := c.advance(d.water.surface, float64(i)/120); err != nil {
			t.Fatal(err)
		}
		if c.airborne {
			airborne++
		}
		if math.IsNaN(c.z) || math.Abs(c.z) > 5 {
			t.Fatal("unstable craft", c)
		}
	}
	if c.launches == 0 || c.landings == 0 || airborne == 0 {
		t.Fatalf("never launched and landed: %+v", c)
	}
	other := initial
	for i := 1; i <= 20*30; i++ {
		if err := other.advance(d.water.surface, float64(i)/30); err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(other.z-c.z) > 1e-9 || other.launches != c.launches {
		t.Fatal("frame-rate dependent physics", c, other)
	}
}

func TestWaterCraftFlatAndControls(t *testing.T) {
	c := waterCraft{}
	if err := c.advance(&nt3d.WaveSurface{}, 5); err != nil {
		t.Fatal(err)
	}
	if c.airborne || c.launches != 0 || math.Abs(c.z-waterRestHeight) > 1e-6 {
		t.Fatal("flat water launches craft", c)
	}
	d := &demo{water: &waterScene{craft: c}}
	for _, k := range []string{"w", "s", "j", "l", " "} {
		if handled, err := d.waterKey(k); !handled || err != nil {
			t.Fatal(k, handled, err)
		}
	}
}

func TestWaterCraftTerminalSpace(t *testing.T) {
	d := &demo{water: &waterScene{craft: waterCraft{autopilot: false, turn: 1}}}
	if handled, err := d.waterKey("space"); !handled || err != nil {
		t.Fatal(handled, err)
	}
	if !d.water.craft.autopilot || d.water.craft.turn != 0 {
		t.Fatal("terminal space did not restore autopilot")
	}
}

func TestWaterCraftForcesAndPlaning(t *testing.T) {
	flat := &nt3d.WaveSurface{}
	a := waterCraft{bound: 1000, throttle: .65}
	b := a
	b.noPlaning = true
	for _, c := range []*waterCraft{&a, &b} {
		if err := c.advance(flat, 10); err != nil {
			t.Fatal(err)
		}
	}
	if a.speed < 2 || a.z <= b.z || a.lift <= 0 {
		t.Fatal("planing did not raise a moving hull", a, b)
	}
	before := a.speed
	a.throttle = 0
	if err := a.advance(flat, 12); err != nil {
		t.Fatal(err)
	}
	if a.speed >= before {
		t.Fatal("unpowered hull did not coast down", a.speed, before)
	}
	// A dropped, unpowered hull must fall and settle through forces alone.
	c := waterCraft{ready: true, z: 1, orientation: [4]float64{1, 0, 0, 0}}
	if err := c.advance(flat, 5); err != nil {
		t.Fatal(err)
	}
	if c.landings == 0 || math.Abs(c.z-waterRestHeight) > .001 || math.Abs(c.vz) > .001 {
		t.Fatal("drop did not settle", c)
	}
}
