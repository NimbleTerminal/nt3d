// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestWaterRippleTilingAndAnimation(t *testing.T) {
	for _, time := range []float64{0, .7, 10} {
		x, y, z := rippleVector(.31, .72, time)
		a, b, c := rippleVector(1.31, -.28, time)
		if math.Abs(x-a)+math.Abs(y-b)+math.Abs(z-c) > 1e-12 {
			t.Fatal("ripple seam")
		}
		if math.Abs(x*x+y*y+z*z-1) > 1e-12 {
			t.Fatal("non-unit ripple normal")
		}
	}
	a, _ := waterRipples(0)
	b, _ := waterRipples(.7)
	if a.At(31, 47) == b.At(31, 47) {
		t.Fatal("ripples do not animate")
	}
}

func TestWaterMaterialTogglesPreservePose(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	d.animationTime = 1.2
	if err := d.stepWater(); err != nil {
		t.Fatal(err)
	}
	before, _ := d.scene.Node(d.water.float)
	for _, key := range []string{"n", "e", "n", "e", "t", "h", "t", "h"} {
		if handled, err := d.waterKey(key); !handled || err != nil {
			t.Fatal(key, handled, err)
		}
		after, _ := d.scene.Node(d.water.float)
		if before.Local != after.Local {
			t.Fatal("material toggle moved craft")
		}
	}
	if d.water.grid.Material.NormalStrength != 1 || d.water.grid.Material.Reflectivity != .02 {
		t.Fatal("toggles did not restore material")
	}
}
