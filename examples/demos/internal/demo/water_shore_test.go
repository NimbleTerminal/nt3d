package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestWaterShoreModesAndSize(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	if len(d.water.tiles) != 4 {
		t.Fatal("default tiles", len(d.water.tiles))
	}
	d.animationTime = 2
	if err := d.stepWater(); err != nil {
		t.Fatal(err)
	}
	craft := d.water.craft
	foam := 0
	for _, tile := range d.water.tiles {
		g := shoreFoam(tile.wave.Geometry, 2)
		foam += len(g.Indices)
		if err := g.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if foam == 0 {
		t.Fatal("no shoreline foam")
	}
	for range 4 {
		if _, err := d.waterKey("v"); err != nil {
			t.Fatal(err)
		}
	}
	if d.water.craft != craft || d.water.shoreMode != 0 {
		t.Fatal("comparison changed simulation")
	}
	f, err := d.scene.Frame(nt3d.DefaultCamera(), 80, 60)
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.Geometry)
	for range 2 {
		if _, err := d.waterKey("m"); err != nil {
			t.Fatal(err)
		}
		if len(d.water.tiles) != 16 || d.water.extent != 15 || d.water.effects.limit() != 14.65 {
			t.Fatal("large size not propagated")
		}
		if _, err := d.waterKey("m"); err != nil {
			t.Fatal(err)
		}
		f, err = d.scene.Frame(nt3d.DefaultCamera(), 80, 60)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Geometry) != before || len(d.water.tiles) != 4 {
			t.Fatal("size toggle leaked nodes", len(f.Geometry), before)
		}
	}
}
func TestWaterBankKeepsHullWet(t *testing.T) {
	for _, y := range []float64{-4, 0, 4} {
		c := waterCraft{x: -5, y: y, heading: math.Pi, vx: -3}
		waterBank(&c)
		if c.x < waterCoast(c.y)+.949 || c.vx <= 0 || c.heading != math.Pi {
			t.Fatal("bank did not redirect craft", c)
		}
		if waterBedHeight(c.x, c.y) >= -.4 {
			t.Fatal("craft remained on dry land")
		}
	}
}

func TestWaterShrinkReturnsCraftInside(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.waterKey("m"); err != nil {
		t.Fatal(err)
	}
	d.water.craft.x = 10
	d.water.craft.airborne = true
	d.water.craft.vz = 4
	if _, err := d.waterKey("m"); err != nil {
		t.Fatal(err)
	}
	if d.water.craft.x > d.water.craft.bound || d.water.craft.airborne || d.water.craft.vz != 0 || d.water.effects.count != 0 {
		t.Fatal("resize left craft/effects outside the patch")
	}
}
