// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestForestShadow(t *testing.T) {
	for _, copies := range []bool{false, true} {
		d := &demo{scene: nt3d.NewScene()}
		if err := d.setupForest(); err != nil {
			t.Fatal(err)
		}
		d.fog = false
		if copies {
			if err := d.toggleInstancing(); err != nil {
				t.Fatal(err)
			}
		}
		lighting := nt3d.DefaultLighting()
		for step, strength := range []float32{.4, 0, 0} {
			if err := d.cycleShadows(); err != nil {
				t.Fatal(err)
			}
			f, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48)
			if err != nil {
				t.Fatal(err)
			}
			f.Lighting = lighting
			d.decorate(&f)
			if f.Lighting.Lights[0].Shadows != (step == 1) {
				t.Fatalf("step %d: sun", step)
			}
			if lighting.Lights[0].Shadows {
				t.Fatal("decorator mutated shared lighting")
			}
			trees := 0
			for i, inst := range f.Instances {
				want := strength
				if inst.Name == "ground" {
					want = 0
				} else {
					trees++
				}
				if f.Geometry[i].DropShadow.Strength != want {
					t.Fatalf("step %d: %s", step, inst.Name)
				}
			}
			if trees != len(d.forest.trees) {
				t.Fatalf("%d trees", trees)
			}
		}
	}
}
