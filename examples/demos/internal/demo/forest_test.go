// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
	picture "nimbleterminal.dev/ntpicture"
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

func TestForestLOD(t *testing.T) {
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

		frameAt := func(dist float64, cull bool) (drawn, batches, tris int) {
			cam := nt3d.DefaultCamera()
			cam.Alpha, cam.Distance, cam.Projection = 24, dist, nt3d.Perspective
			f, err := d.scene.Frame(cam, 80, 48)
			if err != nil {
				t.Fatal(err)
			}
			f.Cull = cull
			d.decorate(&f)
			return d.forest.drawn, d.forest.batches, d.forest.triangles
		}

		// With frustum culling disabled, isolate pure SetLOD tier switching across zoom distances.
		visNear, batchesNear, trisNear := frameAt(3, false)
		visDefault, batchesDefault, trisDefault := frameAt(9, false)
		visFar, batchesFar, trisFar := frameAt(30, false)

		// Coarsest tier uses MaxDistance 0, so trees never pop/clip off the island when zooming out.
		if visNear != len(d.forest.trees) || visDefault != len(d.forest.trees) || visFar != len(d.forest.trees) {
			t.Fatalf("copies=%v: near/default/far visible=%d/%d/%d, want all %d", copies, visNear, visDefault, visFar, len(d.forest.trees))
		}
		if !(trisNear > trisDefault && trisDefault > trisFar && trisFar > 0) {
			t.Fatalf("copies=%v: expected near (%d) > default (%d) > far (%d) > 0", copies, trisNear, trisDefault, trisFar)
		}
		if copies {
			if batchesDefault != visDefault || batchesNear != visNear || batchesFar != visFar {
				t.Fatalf("copies=true: expected 1 batch per tree, got %d/%d/%d", batchesNear, batchesDefault, batchesFar)
			}
		} else if batchesDefault > len(d.forest.kinds)*3 || batchesFar != len(d.forest.kinds) {
			t.Fatalf("copies=false: expected <= %d default batches and %d far batches, got %d/%d",
				len(d.forest.kinds)*3, len(d.forest.kinds), batchesDefault, batchesFar)
		}

		// With frustum culling enabled up close, off-screen trees are culled too.
		drawnZoomed, _, _ := frameAt(3, true)
		if drawnZoomed >= visNear {
			t.Fatalf("copies=%v: expected frustum culling at dist 3 (%d < %d)", copies, drawnZoomed, visNear)
		}

		// Cycle LOD -> LOD tint -> LOD off.
		if err := d.cycleForestLOD(); err != nil {
			t.Fatal(err)
		}
		if d.forest.lod != forestLODTint {
			t.Fatalf("expected forestLODTint, got %v", d.forest.lod)
		}
		if _, _, trisTint := frameAt(9, false); trisTint != trisDefault {
			t.Fatalf("copies=%v: LOD tint triangles=%d, want %d", copies, trisTint, trisDefault)
		}
		if err := d.cycleForestLOD(); err != nil {
			t.Fatal(err)
		}
		if d.forest.lod != forestLODOff {
			t.Fatalf("expected forestLODOff, got %v", d.forest.lod)
		}
		visFull, batchesFull, trisFull := frameAt(30, false)
		if visFull != len(d.forest.trees) || trisFull <= trisNear {
			t.Fatalf("copies=%v: LOD off at dist 30 got drawn=%d tris=%d (near LOD=%d)", copies, visFull, trisFull, trisNear)
		}
		if !copies && batchesFull != len(d.forest.kinds) {
			t.Fatalf("copies=false LOD off: batches=%d, want %d", batchesFull, len(d.forest.kinds))
		}
	}
}

func TestForestLODKeyAndStatus(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupForest(); err != nil {
		t.Fatal(err)
	}
	d.fog = false
	cam := nt3d.DefaultCamera()
	cam.Alpha, cam.Distance, cam.Projection = 24, 9, nt3d.Perspective
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilityUnsupported})
	defer session.Close()
	d.view = nt3dwidget.New(d.scene, 20, 8, nt3dwidget.WithCamera(cam), nt3dwidget.WithRenderMode(nt3d.Software), nt3dwidget.WithTerminalSession(session), nt3dwidget.WithFrameDecorator(d.decorate))
	defer d.view.Close()

	settle(t, d.view, d.view.Init())
	lodOnStatus := d.forestView("")
	if !strings.Contains(lodOnStatus, "LOD on") {
		t.Fatalf("expected LOD on in status: %s", lodOnStatus)
	}
	lodTris := d.forest.triangles

	_, cmd := d.Update(tea.KeyPressMsg{Code: 'l'})
	settle(t, d.view, cmd)
	lodTintStatus := d.forestView("")
	if !strings.Contains(lodTintStatus, "LOD tint") {
		t.Fatalf("expected LOD tint in status: %s", lodTintStatus)
	}

	_, cmd = d.Update(tea.KeyPressMsg{Code: 'l'})
	settle(t, d.view, cmd)
	lodOffStatus := d.forestView("")
	if !strings.Contains(lodOffStatus, "LOD off") {
		t.Fatalf("expected LOD off in status: %s", lodOffStatus)
	}
	if d.forest.triangles <= lodTris {
		t.Fatalf("expected LOD off triangles (%d) > LOD on (%d)", d.forest.triangles, lodTris)
	}
}
