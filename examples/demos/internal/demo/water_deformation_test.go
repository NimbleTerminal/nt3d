package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestWaterWakeDeformsMeshAndExpires(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	d.animationTime = 3
	if err := d.stepWater(); err != nil {
		t.Fatal(err)
	}
	if d.water.effects.deformation.count == 0 {
		t.Fatal("no displacement pulses")
	}
	maximum := 0.0
	for _, tile := range d.water.tiles {
		for _, v := range tile.wave.Geometry.Vertices {
			q, err := d.water.baseSurface.AtWorld(float64(v.Position.X), float64(v.Position.Y), 3)
			if err != nil {
				t.Fatal(err)
			}
			delta := math.Abs(float64(v.Position.Z - q.Position.Z))
			maximum = math.Max(maximum, delta)
			if delta > waterWakeHeight+1e-5 {
				t.Fatal("wake exceeded displacement limit")
			}
			query, err := d.water.surface.AtWorld(float64(v.Position.X), float64(v.Position.Y), 3)
			if err != nil || math.Abs(float64(query.Position.Z-v.Position.Z)) > 1e-5 {
				t.Fatal("mesh and deformed world query disagree", err)
			}
		}
	}
	if maximum < .01 {
		t.Fatal("wake never moved mesh visibly", maximum)
	}
	d.water.effects.deformation.prune(10)
	if d.water.effects.deformation.count != 0 {
		t.Fatal("wake did not expire")
	}
}
func TestWaterWakeEmissionAndToggle(t *testing.T) {
	var e waterEffects
	c := waterCraft{speed: 4, time: 1, airborne: true}
	e.ticks = 15
	e.deform(&c, false, 0, 0)
	if e.deformation.count != 0 {
		t.Fatal("airborne craft emitted wake")
	}
	e.deform(&c, false, 3, 0)
	if e.deformation.count != 4 {
		t.Fatal("landing missed displacement burst")
	}
	e.deformation.clear()
	c.airborne = false
	e.deform(&c, true, 0, 0)
	if e.deformation.count != 2 {
		t.Fatal("missing diverging wake crests")
	}
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	d.animationTime = 2
	if err := d.stepWater(); err != nil {
		t.Fatal(err)
	}
	before := d.water.craft
	for range 2 {
		if _, err := d.waterKey("z"); err != nil {
			t.Fatal(err)
		}
	}
	if d.water.effects.deformation.disabled || d.water.craft != before {
		t.Fatal("toggle changed body state")
	}
	// Switching the force field must not inject a discrete velocity change.
	d.animationTime += 1.0 / 120
	if err := d.stepWater(); err != nil {
		t.Fatal(err)
	}
	if math.Abs(d.water.craft.vz) > 10 {
		t.Fatal("toggle injected vertical impulse")
	}
}
