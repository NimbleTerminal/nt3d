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
	c := d.water.craft
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
	other := d.water.craft
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
	c := waterCraft{speed: 4, autopilot: true}
	if err := c.advance(&nt3d.WaveSurface{}, 5); err != nil {
		t.Fatal(err)
	}
	if c.airborne || c.launches != 0 || math.Abs(c.z-.065) > 1e-9 {
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
