// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	picture "nimbleterminal.dev/ntpicture"

	"nimbleterminal.dev/nt3d"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

// Every preset, at any moment and with either falloff, lights the scene
// with valid lighting, and shows a lamp for each point light.
func TestLightPresets(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupLights(); err != nil {
		t.Fatal(err)
	}
	for i, p := range lightPresets {
		d.lights.preset = i
		for _, ranged := range []bool{true, false} {
			d.lights.ranged = ranged
			for _, at := range []float64{0, 1.7, 40} {
				d.lights.t = at
				l, err := d.applyLights()
				if err != nil {
					t.Fatalf("%s at %.1f: %v", p.name, at, err)
				}
				if len(l.Lights) > nt3d.MaxLights {
					t.Fatalf("%s: %d lights", p.name, len(l.Lights))
				}
				f, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48)
				if err != nil {
					t.Fatal(err)
				}
				if p.environment != nil && f.Environment == nil {
					t.Fatalf("%s lost its environment", p.name)
				}
				if p.environment == nil && f.Environment != nil {
					t.Fatalf("%s retained previous environment", p.name)
				}
				f.Lighting = l
				if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
					t.Fatalf("%s at %.1f, ranged %v: %v", p.name, at, ranged, err)
				}
				shown := 0
				for _, lamp := range d.lights.lamps {
					if n, _ := d.scene.Node(lamp); n.Visible {
						shown++
					}
				}
				if shown != len(p.lamps) {
					t.Fatalf("%s: %d lamps shown for %d point lights", p.name, shown, len(p.lamps))
				}
			}
		}
	}
}

// Changing the lights leaves the view free to draw them: the view renders
// whatever the lights are now, by the frame decorator, so the demo never
// holds a render the view has scheduled and is waiting on.
func TestLightsKeepRendering(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupLights(); err != nil {
		t.Fatal(err)
	}
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilityUnsupported})
	defer session.Close()
	d.view = nt3dwidget.New(d.scene, 20, 8, nt3dwidget.WithRenderMode(nt3dwidget.Software), nt3dwidget.WithTerminalSession(session), nt3dwidget.WithFrameDecorator(d.decorate))
	defer d.view.Close()
	settle(t, d.view, d.view.Init())
	for range lightPresets {
		if err := d.cycleLightPreset(); err != nil {
			t.Fatal(err)
		}
		if err := d.stepLights(); err != nil {
			t.Fatal(err)
		}
		if d.view.Busy() {
			t.Fatalf("%s: the view is busy with a render no one will run", lightPresets[d.lights.preset].name)
		}
		var f nt3d.Frame
		d.decorate(&f)
		want := lightPresets[d.lights.preset].lights(d.lights.t, d.lights.ranged)
		if len(f.Lighting.Lights) != len(want.Lights) || f.Lighting.Ambient != want.Ambient {
			t.Fatalf("%s: the frame's lighting %+v, want %+v", lightPresets[d.lights.preset].name, f.Lighting, want)
		}
	}
}

// settle carries out a view's command as Bubble Tea would, handing the view
// the messages it leads to, until it settles.
func settle(t *testing.T, v *nt3dwidget.Model, cmd tea.Cmd) {
	t.Helper()
	count := 0
	var step func(tea.Cmd)
	step = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		if count++; count > 200 {
			t.Fatal("the view's commands did not settle")
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				step(c)
			}
			return
		}
		if msg == nil {
			return
		}
		if _, ok := msg.(tea.RawMsg); ok {
			return
		}
		_, next := v.Update(msg)
		step(next)
	}
	step(cmd)
}

// The cycle routes drop shadows to geometry and mapped shadows to the sun.
func TestShadowCycle(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupLights(); err != nil {
		t.Fatal(err)
	}
	// Select a direct-light preset explicitly: environment-only presets have no sun.
	for i, p := range lightPresets {
		if p.sun != nil {
			d.lights.preset = i
			break
		}
	}
	for step, strength := range []float32{.5, 0, 0} {
		if err := d.cycleShadows(); err != nil {
			t.Fatal(err)
		}
		f, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48)
		if err != nil {
			t.Fatal(err)
		}
		d.decorateLights(&f)
		if f.Lighting.Lights[0].Shadows != (step == 1) {
			t.Fatalf("step %d: sun", step)
		}
		found := false
		for i, inst := range f.Instances {
			if inst.Name == "cube" {
				found = true
				if f.Geometry[i].DropShadow.Strength != strength {
					t.Fatalf("step %d: cube", step)
				}
			}
		}
		if !found {
			t.Fatal("no cube")
		}
	}
}

// Exercise the actual key routing, refreshed widget and displayed status.
func TestShadowKeysRender(t *testing.T) {
	for _, forest := range []bool{false, true} {
		d := &demo{scene: nt3d.NewScene()}
		var err error
		if forest {
			err = d.setupForest()
		} else {
			err = d.setupLights()
		}
		if err != nil {
			t.Fatal(err)
		}
		session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilityUnsupported})
		d.fog = false
		d.view = nt3dwidget.New(d.scene, 20, 8, nt3dwidget.WithRenderMode(nt3d.Software), nt3dwidget.WithTerminalSession(session), nt3dwidget.WithFrameDecorator(d.decorate))
		settle(t, d.view, d.view.Init())
		wantModes := []shadowMode{shadowNone, shadowDrop, shadowMap, shadowNone}
		for step, key := range []rune{'2', 's', 's', 's'} {
			_, cmd := d.Update(tea.KeyPressMsg{Code: key})
			settle(t, d.view, cmd)
			mode := shadowNone
			if forest {
				mode = d.forest.shadows
			} else {
				mode = d.lights.shadows
			}
			want := wantModes[step]
			if mode != want {
				t.Fatalf("forest %v, step %d, key %c: mode %v, want %v", forest, step, key, mode, want)
			}
			var status string
			if forest {
				status = d.forestView("")
			} else {
				status = d.lightsView("")
			}
			if !strings.Contains(status, want.String()) {
				t.Fatalf("status: %s", status)
			}
			if d.view.Err() != nil {
				t.Fatal(d.view.Err())
			}
		}
		d.view.Close()
		session.Close()
	}
}

func TestNormalMapToggle(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupLights(); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false, true} {
		if d.lights.normal != enabled {
			if err := d.toggleNormalMap(); err != nil {
				t.Fatal(err)
			}
		}
		f, err := d.scene.Frame(nt3d.DefaultCamera(), 80, 60)
		if err != nil {
			t.Fatal(err)
		}
		for i, inst := range f.Instances {
			if inst.Name == "sphere" {
				m := f.Geometry[i].Material
				if (m != nil && m.NormalMap != nil) != enabled {
					t.Fatal("toggle did not reach sphere")
				}
			}
		}
		d.decorateLights(&f)
		if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
			t.Fatal(err)
		}
	}
}
